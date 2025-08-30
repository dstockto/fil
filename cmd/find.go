/*
Copyright © 2025 David Stockton <dave@davidstockton.com>
*/
package cmd

import (
	"fmt"
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

	// Allow additional filters later, for now, just default to 1.75mm filament
	filter := onlyStandardFilament

	for _, a := range args {
		spools, err := apiClient.FindSpoolsByName(a, filter)
		if err != nil {
			return fmt.Errorf("error finding spools: %v", err)
		}
		fmt.Printf("Found %d spools matching '%s':\n", len(spools), a)
		for _, s := range spools {
			fmt.Printf(" - %s\n", getSpoolFormattedForFind(s))
		}
		fmt.Println()
	}

	return nil
}

func getSpoolFormattedForFind(s models.FindSpool) string {
	//  - AMS B - #127 PolyTerra™ Cotton White (Matte PLA #E6DDDB) - 91.5g remaining, last used 2 days ago
	format := "%s - #%d %s (%s #%s) - %.1fg remaining, last used %s"
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
	return fmt.Sprintf(format, s.Location, s.Id, s.Filament.Name, s.Filament.Material, s.Filament.ColorHex, s.RemainingWeight, lastUsedDuration)
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
