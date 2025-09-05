/*
Copyright © 2025 David Stockton <dave@davidstockton.com>
*/
package cmd

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/spf13/cobra"
)

// findCmd represents the find command
var findCmd = &cobra.Command{
	Use:   "find <name or id...>",
	Short: "find a spool based on name or id",
	Long: `Find a spool based on name or id. You can provide multiple names or ids. For multi-word names, enclose in quotes.
	To show all spools, use the wildcard character '*'.`,
	RunE:    runFind,
	Aliases: []string{"f"},
	Args:    cobra.MinimumNArgs(1),
}

func runFind(cmd *cobra.Command, args []string) error {
	if Cfg == nil || Cfg.ApiBase == "" {
		return fmt.Errorf("apiClient endpoint not configured")
	}

	apiClient := api.NewClient(Cfg.ApiBase)
	var spools []models.FindSpool
	var filters []api.SpoolFilter

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
		query["allow_archived"] = "true"        // allow archived is needed to get archived spools from the API
		filters = append(filters, archivedOnly) // the API doesn't support only returning archived spools, so we have to filter manually
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
	if location, err := cmd.Flags().GetString("location"); err == nil && location != "" {
		location = mapToAlias(location)
		query["location"] = location
		fmt.Printf("Filtering by location: %s\n", location)
	}

	// Allow additional filters later, for now, just default to 1.75mm filament
	aggFilter := aggregateFilter(filters...)

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
				return fmt.Errorf("error finding spools: %v", err)
			} else {
				spools = []models.FindSpool{*spool}
			}
		} else {
			spools, err = apiClient.FindSpoolsByName(a, aggFilter, query)
			if err != nil {
				return fmt.Errorf("error finding spools: %v", err)
			}
		}

		foundMsg := fmt.Sprintf(foundFmt, len(spools), name)
		if len(spools) == 0 {
			// print in red
			fmt.Printf("\033[31m%s\033[0m\n", foundMsg)
		} else {
			// print in green
			fmt.Printf("\033[32m%s\033[0m\n", foundMsg)
		}
		for _, s := range spools {
			fmt.Printf(" - %s\n", s)
		}
		fmt.Println()
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
	findCmd.Flags().StringP("location", "l", "", "filter by location, default is all")
}

// onlyStandardFilament returns true if the spool is 1.75 mm filament
func onlyStandardFilament(spool models.FindSpool) bool {
	return spool.Filament.Diameter == 1.75
}

// noFilter returns true for all spools
func noFilter(_ models.FindSpool) bool {
	return true
}

// ultimakerFilament returns true if the spool is Ultimaker filament (2.85mm)
func ultimakerFilament(spool models.FindSpool) bool {
	return spool.Filament.Diameter == 2.85
}

// onlyArchived returns true if the spool is archived
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

// aggregateFilter returns a function that returns true if all given filters return true
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
