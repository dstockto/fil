package cmd

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// lowCmd lists spools that are running low so you know what to reorder.
var lowCmd = &cobra.Command{
	Use:     "low [name|#id]",
	Short:   "Show spools running low so you know what to reorder",
	Long:    "List filaments that are running low based on remaining grams.",
	Aliases: []string{"reorder"},
	RunE:    runLow,
}

func runLow(cmd *cobra.Command, args []string) error {
	makeAmazonSearch := func(vendor, name string) string {
		q := url.QueryEscape(strings.TrimSpace(vendor + " " + name))

		return "https://www.amazon.com/s?k=" + q
	}
	// Build an iTerm2-compatible OSC 8 hyperlink: label "text" pointing to "link".
	// Example format: \x1b]8;;http://example.com\x1b\\This is a link\x1b]8;;\x1b\\
	termLink := func(text, link string) string {
		return "\x1b]8;;" + link + "\x1b\\" + text + "\x1b]8;;\x1b\\"
	}

	if Cfg == nil || Cfg.ApiBase == "" {
		return errors.New("apiClient endpoint not configured")
	}

	// Default to wildcard if no name provided
	if len(args) == 0 {
		args = append(args, "*")
	}

	apiClient := api.NewClient(Cfg.ApiBase)

	// threshold (grams only)
	maxRemaining, err := cmd.Flags().GetFloat64("max-remaining")
	if err != nil {
		return fmt.Errorf("failed to get max-remaining: %w", err)
	}

	// helper to resolve custom threshold overrides from config.
	// Supports two key forms in LowThresholds (case-insensitive substring matching):
	//  1) "NamePart" → match by filament name only
	//  2) "VendorPart::NamePart" → match when both vendor and name contain the given parts
	resolveThreshold := func(vendor string, filamentName string) float64 {
		thr := maxRemaining

		if Cfg != nil && Cfg.LowThresholds != nil {
			lvendor := strings.ToLower(strings.TrimSpace(vendor))
			lname := strings.ToLower(strings.TrimSpace(filamentName))

			for k, v := range Cfg.LowThresholds {
				if k == "" {
					continue
				}

				if v <= 0 {
					continue
				}

				lk := strings.ToLower(strings.TrimSpace(k))
				if strings.Contains(lk, "::") {
					parts := strings.SplitN(lk, "::", 2)
					vendPart := strings.TrimSpace(parts[0])

					namePart := strings.TrimSpace(parts[1])
					if vendPart == "" || namePart == "" {
						continue
					}

					if strings.Contains(lvendor, vendPart) && strings.Contains(lname, namePart) {
						thr = v

						break
					}

					continue
				}
				// name-only fallback
				if strings.Contains(lname, lk) {
					thr = v

					break
				}
			}
		}

		return thr
	}

	// helper to determine if a filament should be ignored by low command
	// Supports two pattern forms in config LowIgnore:
	//  1) "NamePart" -> matches by filament name substring (case-insensitive)
	//  2) "VendorPart::NamePart" -> matches when both vendor and filament name contain the given substrings
	// (case-insensitive)
	isIgnored := func(vendor string, filamentName string) bool {
		if Cfg == nil || Cfg.LowIgnore == nil {
			return false
		}

		lvendor := strings.ToLower(vendor)
		lname := strings.ToLower(filamentName)

		for _, pat := range Cfg.LowIgnore {
			p := strings.TrimSpace(pat)
			if p == "" {
				continue
			}

			lp := strings.ToLower(p)
			if strings.Contains(lp, "::") {
				parts := strings.SplitN(lp, "::", 2)
				vendPart := strings.TrimSpace(parts[0])

				namePart := strings.TrimSpace(parts[1])
				if vendPart == "" || namePart == "" {
					continue
				}

				if strings.Contains(lvendor, vendPart) && strings.Contains(lname, namePart) {
					return true
				}

				continue
			}
			// name-only fallback
			if strings.Contains(lname, lp) {
				return true
			}
		}

		return false
	}

	// Build filters similar to find
	var filters []api.SpoolFilter

	// Diameter
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

	// Archived handling: always exclude archived spools for reorder evaluation
	filters = append(filters, func(s models.FindSpool) bool { return !s.Archived })

	// Note: We determine "low" status at the filament-group level (vendor+name+diameter),
	// not per-spool. So we do NOT include a low predicate here.

	aggFilter := aggregateFilter(filters...)

	// Build API query map
	query := make(map[string]string)
	if manufacturer, err := cmd.Flags().GetString("manufacturer"); err == nil && manufacturer != "" {
		query["manufacturer"] = manufacturer
	}

	// Execute for each arg (name or #id)
	for _, a := range args {
		name := a

		// ID lookups: keep legacy behavior (evaluate that single spool)
		if id, err := strconv.Atoi(a); err == nil {
			name = "#" + name

			var spoolsToShow []models.FindSpool

			if spool, err := apiClient.FindSpoolsById(id); err == nil && spool != nil && aggFilter(*spool) {
				// Skip ignored filaments
				if !isIgnored(spool.Filament.Vendor.Name, spool.Filament.Name) {
					// For a single spool, evaluate grams threshold with possible override
					grpRemaining := spool.RemainingWeight
					thr := resolveThreshold(spool.Filament.Vendor.Name, spool.Filament.Name)

					lowByGrams := thr > 0 && grpRemaining <= thr+1e-9
					if thr > 0 && lowByGrams {
						spoolsToShow = append(spoolsToShow, *spool)
					}
				}
			}

			header := fmt.Sprintf("Filaments running low matching '%s': %d\n", name, len(spoolsToShow))
			if len(spoolsToShow) == 0 {
				color.HiRed(header)

				continue
			}

			color.Green(header)

			for _, s := range spoolsToShow {
				fmt.Printf(" - %s\n%s\n", s, termLink("Amazon Order", makeAmazonSearch(s.Filament.Vendor.Name, s.Filament.Name)))
			}

			fmt.Println()

			continue
		}

		// Name/path lookups: fetch, then group by vendor+name+diameter, evaluate totals
		found, err := apiClient.FindSpoolsByName(a, aggFilter, query)
		if err != nil {
			return fmt.Errorf("error finding spools: %w", err)
		}

		// Build groups
		type group struct {
			Vendor    string
			Name      string
			Diameter  float64
			Spools    []models.FindSpool
			RemainSum float64
			InitSum   float64
		}

		groups := map[string]*group{}

		for _, s := range found {
			key := fmt.Sprintf("%s|%s|%.2f", s.Filament.Vendor.Name, s.Filament.Name, s.Filament.Diameter)

			g, ok := groups[key]
			if !ok {
				g = &group{Vendor: s.Filament.Vendor.Name, Name: s.Filament.Name, Diameter: s.Filament.Diameter}
				groups[key] = g
			}

			g.Spools = append(g.Spools, s)
			g.RemainSum += s.RemainingWeight
			g.InitSum += s.InitialWeight
		}

		// Decide which groups are low using per-filament threshold overrides when present
		var lowGroups []*group

		for _, g := range groups {
			// Skip ignored filaments
			if isIgnored(g.Vendor, g.Name) {
				continue
			}

			thr := resolveThreshold(g.Vendor, g.Name)

			lowByGrams := thr > 0 && g.RemainSum <= thr+1e-9
			if thr > 0 && lowByGrams {
				lowGroups = append(lowGroups, g)
			}
		}

		// Flatten spools from low groups for output
		var spools []models.FindSpool
		for _, g := range lowGroups {
			spools = append(spools, g.Spools...)
		}

		header := fmt.Sprintf("Filaments running low matching '%s': %d\n", name, len(lowGroups))
		if len(lowGroups) == 0 {
			color.HiRed(header)

			continue
		}

		color.Green(header)

		for _, s := range spools {
			fmt.Printf(
				" - %s\n%s\n",
				s,
				termLink("Amazon Order "+s.Filament.Name, makeAmazonSearch(s.Filament.Vendor.Name, s.Filament.Name)),
			)
		}

		fmt.Println()
	}

	return nil
}

func init() {
	rootCmd.AddCommand(lowCmd)

	lowCmd.Flags().Float64(
		"max-remaining",
		200,
		"threshold in grams; spools with remaining <= this are shown (0 to disable)",
	)
	lowCmd.Flags().StringP("manufacturer", "m", "", "filter by manufacturer, default is all")
	lowCmd.Flags().StringP(
		"diameter",
		"d",
		"1.75",
		"filter by diameter, default is 1.75mm, '*' for all",
	)
}
