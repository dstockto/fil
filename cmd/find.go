/*
Copyright © 2025 David Stockton <dave@davidstockton.com>
*/
package cmd

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// findCmd represents the find command.
var findCmd = &cobra.Command{
	Use:   "find <name or id...>",
	Short: "find a spool based on name or id",
	Long: `Find a spool based on name or id. You can provide multiple names or ids. For multi-word names, enclose in 
	quotes. To show all spools, use the wildcard character '*'.`,
	RunE:    runFind,
	Aliases: []string{"f"},
}

func runFind(cmd *cobra.Command, args []string) error {
	if Cfg == nil || Cfg.ApiBase == "" {
		return errors.New("apiClient endpoint not configured")
	}

	if len(args) == 0 {
		args = append(args, "*")
	}

	apiClient := api.NewClient(Cfg.ApiBase)

	var (
		spools  []models.FindSpool
		filters []api.SpoolFilter
	)

	// Preload settings-based Location orders to sort results accordingly.
	// The settings key 'locations_spoolorders' stores, per Location, the ordered list of spool IDs.
	orders, err := LoadLocationOrders(apiClient)
	if err != nil {
		// Non-fatal: if settings cannot be loaded, continue without settings-based ordering.
		orders = map[string][]int{}
	}
	// Build quick lookup of ranks per Location for O(1) index lookups.
	ranks := make(map[string]map[int]int, len(orders))
	for loc, ids := range orders {
		m := make(map[int]int, len(ids))
		for i, id := range ids {
			m[id] = i
		}
		ranks[loc] = m
	}

	// API doesn't support diameter, so we have to filter manually
	diameter, err := cmd.Flags().GetString("diameter")
	if err != nil {
		return fmt.Errorf("failed to get diameter flag: %w", err)
	}

	switch diameter {
	case "*":
		filters = append(filters, noFilter)
	case "2.85":
		filters = append(filters, ultimakerFilament)
	default:
		filters = append(filters, onlyStandardFilament)
	}

	query := make(map[string]string)

	if manufacturer, err := cmd.Flags().GetString("manufacturer"); err == nil && manufacturer != "" {
		query["manufacturer"] = manufacturer
	}

	if allowedArchived, err := cmd.Flags().GetBool("allowed-archived"); err == nil && allowedArchived {
		query["allow_archived"] = "true"
	}

	if onlyArchived, err := cmd.Flags().GetBool("archived-only"); err == nil && onlyArchived {
		query["allow_archived"] = "true" // allow archived is needed to get archived spools from the API

		// the API doesn't support only returning archived spools, so we have to filter manually
		filters = append(filters, archivedOnly)
	}

	if hasComment, err := cmd.Flags().GetBool("has-comment"); err == nil && hasComment {
		filters = append(filters, getCommentFilter("*"))
	}

	if comment, err := cmd.Flags().GetString("comment"); err == nil && comment != "" {
		filters = append(filters, getCommentFilter(comment))
	}

	if used, err := cmd.Flags().GetBool("used"); err == nil && used {
		filters = append(filters, func(s models.FindSpool) bool {
			return s.UsedWeight != 0.0
		})
	}

	if pristine, err := cmd.Flags().GetBool("pristine"); err == nil && pristine {
		filters = append(filters, func(s models.FindSpool) bool {
			return s.UsedWeight == 0.0
		})
	}

	if location, err := cmd.Flags().GetString("Location"); err == nil && location != "" {
		location = MapToAlias(location)
		query["Location"] = location
		fmt.Printf("Filtering by Location: %s\n", location)
	}

	// Allow additional filters later, for now, just default to 1.75mm filament
	aggFilter := aggregateFilter(filters...)

	// determine if we should sort by least or most recently used
	lruSort, _ := cmd.Flags().GetBool("lru")
	mruSort, _ := cmd.Flags().GetBool("mru")
	if lruSort && mruSort {
		return fmt.Errorf("flags --lru and --mru are mutually exclusive; please specify only one")
	}

	for _, a := range args {
		foundFmt := "Found %d spools matching '%s':\n"
		name := a
		// figure out if the argument is an id (int)
		id, err := strconv.Atoi(a)
		if err == nil {
			name = "#" + name
			foundFmt = "Found %d spool with ID %s:\n"

			spool, err := apiClient.FindSpoolsById(id)
			if errors.Is(err, api.ErrSpoolNotFound) {
				spools = []models.FindSpool{}
			} else if err != nil {
				return fmt.Errorf("error finding spools: %w", err)
			} else {
				spools = []models.FindSpool{*spool}
			}
		} else {
			spools, err = apiClient.FindSpoolsByName(a, aggFilter, query)
			if err != nil {
				return fmt.Errorf("error finding spools: %w", err)
			}
		}

		// If requested, sort by least- or most-recently used; never-used at the end
		if (lruSort || mruSort) && len(spools) > 1 {
			sort.Slice(spools, func(i, j int) bool {
				li, lj := spools[i].LastUsed, spools[j].LastUsed
				zi, zj := li.IsZero(), lj.IsZero()
				if zi && !zj {
					return false // i has never been used; place after j
				}
				if !zi && zj {
					return true // i used, j never used; i comes first
				}
				if zi && zj {
					return false // keep relative order for never-used
				}
				if lruSort {
					return li.Before(lj) // older last-used first
				}
				return li.After(lj) // newer last-used first
			})
		} else if len(spools) > 1 && len(ranks) > 0 {
			// Default behavior: sort according to settings-defined order within each Location.
			// Items with a known rank (present in settings for their Location) come before unknowns.
			sort.SliceStable(spools, func(i, j int) bool {
				ai := spools[i]
				aj := spools[j]
				locI := MapToAlias(ai.Location)
				locJ := MapToAlias(aj.Location)
				rI, okI := ranks[locI][ai.Id]
				rJ, okJ := ranks[locJ][aj.Id]
				if okI && !okJ {
					return true
				}
				if !okI && okJ {
					return false
				}
				if okI && okJ {
					// When both have ranks (possibly in different locations), order by rank number.
					if rI != rJ {
						return rI < rJ
					}
					// As a tie-breaker across locations with same rank, keep stable order by returning false.
					return false
				}
				// Neither has a rank; keep current relative order (stable sort preserves input order).
				return false
			})
		}

		foundMsg := fmt.Sprintf(foundFmt, len(spools), name)
		if len(spools) == 0 {
			// print in red
			color.HiRed(foundMsg)
		} else {
			// print in green
			color.Green(foundMsg)
		}

		totalRemaining := 0.0
		totalUsed := 0.0

		showPurchase, _ := cmd.Flags().GetBool("purchase")

		for _, s := range spools {
			fmt.Printf(" - %s\n", s)
			if showPurchase {
				fmt.Printf(" - %s\n%s\n", s, amazonLink(s.Filament.Vendor.Name, s.Filament.Name))
				//fmt.Printf("%s\n", amazonLink(s.Filament.Name, s.Filament.Vendor.Name))
			}
			totalRemaining += s.RemainingWeight
			totalUsed += s.UsedWeight
		}

		if len(spools) > 0 {
			bold := color.New(color.Bold).SprintFunc()
			spoolPlural := "spools"

			if len(spools) == 1 {
				spoolPlural = "spool"
			}

			fmt.Printf(
				"%s: %d %s, %s: %.1fg, %s: %.1fg\n\n",
				bold("Summary"),
				len(spools),
				spoolPlural,
				bold("Remaining"),
				totalRemaining,
				bold("Used"),
				totalUsed,
			)
		}
	}

	return nil
}

func init() {
	rootCmd.AddCommand(findCmd)

	findCmd.Flags().StringP("diameter", "d", "1.75", "filter by diameter, default is 1.75mm, '*' for all")
	findCmd.Flags().StringP("manufacturer", "m", "", "filter by manufacturer, default is all")
	findCmd.Flags().BoolP("allowed-archived", "a", false, "show archived spools, default is false")
	findCmd.Flags().Bool("archived-only", false, "show only archived spools, default is false")
	findCmd.Flags().Bool("has-comment", false, "show only spools with comments, default is false")
	findCmd.Flags().StringP("comment", "c", "", "find spools with a comment matching the provided value")
	findCmd.Flags().BoolP("used", "u", false, "show only spools that have been used")
	findCmd.Flags().BoolP("pristine", "p", false, "show only (pristine) spools that have not been used")
	findCmd.Flags().StringP("Location", "l", "", "filter by Location, default is all")
	findCmd.Flags().Bool("lru", false, "sort by least recently used first; never-used appear last")
	findCmd.Flags().Bool("mru", false, "sort by most recently used first; never-used appear last")
	findCmd.Flags().Bool("purchase", false, "show purchase link for each spool")
}

// onlyStandardFilament returns true if the spool is 1.75 mm filament.
func onlyStandardFilament(spool models.FindSpool) bool {
	return spool.Filament.Diameter == 1.75
}

// noFilter returns true for all spools.
func noFilter(_ models.FindSpool) bool {
	return true
}

// ultimakerFilament returns true if the spool is Ultimaker filament (2.85mm).
func ultimakerFilament(spool models.FindSpool) bool {
	return spool.Filament.Diameter == 2.85
}

// onlyArchived returns true if the spool is archived.
func archivedOnly(spool models.FindSpool) bool {
	return spool.Archived
}

func getCommentFilter(comment string) api.SpoolFilter {
	if comment == "*" {
		return func(spool models.FindSpool) bool {
			return spool.Comment != ""
		}
	}

	lowerComment := strings.ToLower(comment)

	return func(s models.FindSpool) bool {
		return strings.Contains(strings.ToLower(s.Comment), lowerComment)
	}
}

// aggregateFilter returns a function that returns true if all given filters return true.
func aggregateFilter(filters ...api.SpoolFilter) api.SpoolFilter {
	return func(s models.FindSpool) bool {
		for _, f := range filters {
			if !f(s) {
				return false
			}
		}

		return true
	}
}
