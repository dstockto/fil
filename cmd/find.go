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

func runFind(_ *cobra.Command, args []string) error {
	if Cfg == nil || Cfg.ApiBase == "" {
		return fmt.Errorf("apiClient endpoint not configured")
	}

	apiClient := api.NewClient(Cfg.ApiBase)
	var spools []models.FindSpool
	// Allow additional filters later, for now, just default to 1.75mm filament
	filter := onlyStandardFilament

	for _, a := range args {
		foundFmt := "Found %d spools matching '%s':\n"
		spools = nil
		name := a
		// figure out if argument is an id (int)
		id, err := strconv.Atoi(a)
		if err == nil {
			name = "#" + name
			foundFmt = "Found %d spool with ID %s:\n"
			spool, err := apiClient.FindSpoolsById(id)
			if errors.Is(err, api.NotFoundError) {
				spools = []models.FindSpool{}
			} else if err != nil {
				return fmt.Errorf("error finding spools: %v", err)
			} else {
				spools = []models.FindSpool{*spool}
			}
		} else {
			spools, err = apiClient.FindSpoolsByName(a, filter)
			if err != nil {
				return fmt.Errorf("error finding spools: %v", err)
			}
		}

		foundMsg := fmt.Sprintf(foundFmt, len(spools), name)
		if len(spools) == 0 {
			// print in red
			fmt.Printf("\033[31m%s\033[0m\n", foundMsg)
			return nil
		} else {
			// print in green
			fmt.Printf("\033[32m%s\033[0m\n", foundMsg)
			//fmt.Printf(foundFmt, len(spools), name)
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
		colorBlock = fmt.Sprintf(" \x1b[48;2;255;255;255m\x1b[38;2;%d;%d;%dm%s\x1b[0m ", r, g, b, blockChars)
	}

	format := "%s%s - #%d %s (%s%s) - %.1fg remaining, last used %s%s"
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
	return fmt.Sprintf(format, colorBlock, s.Location, s.Id, s.Filament.Name, s.Filament.Material, colorHex, s.RemainingWeight, lastUsedDuration, archived)
}

func convertFromHex(hex string) (int, int, int) {
	// convert the hex color like 45FFE0 to rgb integers
	r, _ := strconv.ParseInt(hex[0:2], 16, 32)
	g, _ := strconv.ParseInt(hex[2:4], 16, 32)
	b, _ := strconv.ParseInt(hex[4:6], 16, 32)
	return int(r), int(g), int(b)
}

func init() {
	rootCmd.AddCommand(findCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// findCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// findCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

func onlyStandardFilament(spool models.FindSpool) bool {
	return spool.Filament.Diameter == 1.75
}
