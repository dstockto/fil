/*
Copyright © 2025 David Stockton <dave@davidstockton.com>
*/
package cmd

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/spf13/cobra"
)

// findCmd represents the find command
var findCmd = &cobra.Command{
	Use:     "find",
	Short:   "find a spool based on name or id",
	Long:    `find a spool based on name or id`,
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
			fmt.Printf(" - %s\n", getSpoolFormattedForFind(s))
		}
		fmt.Println()
	}

	return nil
}

func getSpoolFormattedForFind(s models.FindSpool) string {
	//  - AMS B - #127 PolyTerra™ Cotton White (Matte PLA #E6DDDB) - 91.5g remaining, last used 2 days ago (archived)
	archived := ""
	if s.Archived {
		archived = " (archived)"
	}
	colorBlock := ""
	if s.Filament.ColorHex != "" {
		r, g, b := convertFromHex(s.Filament.ColorHex)
		blockChars := "████"
		if len(s.Filament.ColorHex) > 6 {
			blockChars = "▓▓▓▓"
		}
		colorBlock = fmt.Sprintf("\x1b[48;2;255;255;255m\x1b[38;2;%d;%d;%dm%s\x1b[0m ", r, g, b, blockChars)
	}
	// Default to not showing the diameter if it's 1.75
	diameter := ""
	if s.Filament.Diameter != 1.75 {
		diameter = fmt.Sprintf(" \x1b[38;2;200;128;0m(%.2fmm)\x1b[0m", s.Filament.Diameter)
	}

	format := "%s%s - #%d %s%s (%s%s) - %.1fg remaining, last used %s%s"
	var lastUsedDuration string
	if s.LastUsed.IsZero() {
		lastUsedDuration = "never"
	} else {
		duration := time.Since(s.LastUsed)
		if duration.Hours() > 24 {
			lastUsedDuration = fmt.Sprintf("%d days ago", int(duration.Truncate(24*time.Hour).Hours())/24)
		} else if duration.Hours() > 1 {
			lastUsedDuration = fmt.Sprintf("%d hours ago", int(duration.Truncate(time.Hour).Hours()))
		} else if duration.Minutes() > 1 {
			lastUsedDuration = fmt.Sprintf("%d minutes ago", int(duration.Truncate(time.Minute).Minutes()))
		} else if duration.Seconds() > 1 {
			lastUsedDuration = fmt.Sprintf("%d seconds ago", int(duration.Truncate(time.Second).Seconds()))
		} else {
			lastUsedDuration = time.Since(s.LastUsed).String() + " ago"
		}

	}
	colorHex := ""
	if s.Filament.ColorHex != "" {
		colorHex = " #" + s.Filament.ColorHex
	}
	return fmt.Sprintf(format, colorBlock, s.Location, s.Id, s.Filament.Name, diameter, s.Filament.Material, colorHex, s.RemainingWeight, lastUsedDuration, archived)
}

func convertFromHex(hex string) (int, int, int) {
	// convert the hex color like 45FFE0 to rgb integers
	r, _ := strconv.ParseInt(hex[0:2], 16, 8)
	g, _ := strconv.ParseInt(hex[2:4], 16, 8)
	b, _ := strconv.ParseInt(hex[4:6], 16, 8)
	return int(r), int(g), int(b)
}

func init() {
	rootCmd.AddCommand(findCmd)

	findCmd.Flags().StringP("diameter", "d", "1.75", "filter by diameter, default is 1.75mm, * for all")
	findCmd.Flags().StringP("manufacturer", "m", "", "filter by manufacturer, default is all")
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
