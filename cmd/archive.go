/*
Copyright © 2025 David Stockton <dstockton@i3logix.com>
*/
package cmd

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// archiveCmd represents the archive command.
var archiveCmd = &cobra.Command{
	Use:          "archive",
	Short:        "Archives a spool and moves it out of any locations",
	Long:         `Archives a spool and moves it out of any locations.`,
	RunE:         runArchive,
	Aliases:      []string{"a"},
	Args:         cobra.MinimumNArgs(1),
	SilenceUsage: true,
}

func runArchive(cmd *cobra.Command, args []string) error {
	if Cfg == nil || Cfg.ApiBase == "" {
		return errors.New("apiClient endpoint not configured")
	}

	apiClient := api.NewClient(Cfg.ApiBase)

	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return err
	}

	location, err := cmd.Flags().GetString("location")
	if err != nil {
		return err
	}

	location = mapToAlias(location)

	if dryRun {
		color.HiRed("Dry run mode enabled. Nothing will be changed.")
	}

	var errs error

	spools := []models.FindSpool{}

	for _, a := range args {
		selector := a

		if id, err := strconv.Atoi(selector); err == nil {
			spool, err := apiClient.FindSpoolsById(id)
			if err != nil {
				color.Red("Error finding spool %d: %v\n", id, err)
				errs = errors.Join(errs, fmt.Errorf("error finding spool %d: %w", id, err))

				continue
			} else {
				spools = []models.FindSpool{*spool}

				continue
			}
		}

		query := make(map[string]string)
		if location != "" {
			query["location"] = location
		}

		foundSpools, err := apiClient.FindSpoolsByName(a, nil, query)
		if err != nil {
			color.Red("Error finding spool '%s': %v\n", selector, err)
			errs = errors.Join(errs, err)

			continue
		}

		if len(foundSpools) == 0 {
			color.Red("No spools found for '%s'\n", selector)

			errs = errors.Join(errs, errors.New("no spools found for '%s'"))

			continue
		}

		if len(foundSpools) > 1 {
			color.Red("Multiple spools found for '%s'\n", selector)
			errs = errors.Join(errs, fmt.Errorf("multiple spools found for '%s'", selector))

			continue
		}

		spools = append(spools, foundSpools[0])
	}

	// Load current locations_spoolorders to compute removals for dry-run and updates
	orders, loadErr := loadLocationOrders(apiClient)
	if loadErr != nil {
		return loadErr
	}

	// Remove each selected spool ID from all location lists
	for _, s := range spools {
		orders = removeFromAllOrders(orders, s.Id)
	}

	if dryRun {
		for _, s := range spools {
			fmt.Printf("Would archive %s and remove it from locations_spoolorders.\n", s)
		}
		return errs
	}

	// Persist settings first so UI order reflects immediately
	if err := apiClient.PostSettingObject("locations_spoolorders", orders); err != nil {
		return fmt.Errorf("failed to update locations_spoolorders: %w", err)
	}

	// Then archive each spool (sets archived=true and clears location)
	for _, s := range spools {
		err := apiClient.ArchiveSpool(s.Id)
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("error archiving spool %d: %w", s.Id, err))
			continue
		}
		color.Green("Archived %s\n", s)
	}

	return errs
}

func init() {
	rootCmd.AddCommand(archiveCmd)

	archiveCmd.Flags().BoolP("dry-run", "d", false, "show what would be archived, but don't actually archive anything")
	archiveCmd.Flags().StringP("location", "l", "", "filter by location, default is all")
	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// archiveCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// archiveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
