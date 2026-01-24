/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/dstockto/fil/api"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// useCmd represents the use command.
var useCmd = &cobra.Command{
	Use:     "use",
	Short:   "Use marks a spool so that some portion of it is used (or unused with a negative value)",
	Long:    `Use marks a spool so that some portion of it is used (or unused with a negative value)`,
	RunE:    runUse,
	Aliases: []string{"u"},
}

type SpoolUsage struct {
	SpoolId int
	Amount  float64
}

func runUse(cmd *cobra.Command, args []string) error {
	if Cfg == nil || Cfg.ApiBase == "" {
		return errors.New("apiClient endpoint not configured")
	}

	apiClient := api.NewClient(Cfg.ApiBase)

	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return err
	}

	nonInteractive, err := cmd.Flags().GetBool("non-interactive")
	if err != nil {
		return err
	}
	simpleSelect, _ := cmd.Flags().GetBool("simple-select")
	allowInteractive := isInteractiveAllowed(nonInteractive)

	if dryRun {
		_, err := color.New(color.FgHiYellow).Println("Dry run mode enabled. Nothing will be changed.")
		if err != nil {
			return err
		}
	}

	// arguments should be a spool ID followed by a filament amount. It should check that the spool exists and that
	// the amount is valid. then it should call the API to mark the spool so some of it is used (if there's enough
	// filament). If there is not enough, it should print an error.
	if len(args)%2 != 0 || len(args) < 2 {
		fmt.Println("Arguments must be a spool ID followed by a filament amount.")

		return errors.New("arguments should be a spool ID followed by a filament amount")
	}

	var (
		usages []SpoolUsage
		errs   error
	)

	location, locerr := cmd.Flags().GetString("Location")
	fmt.Printf("Filtering by Location: %s\n", location)
	for i := 0; i < len(args); i += 2 {
		spoolSelector := args[i]
		// Try for an ID first
		spoolId := -1
		if id, interr := strconv.Atoi(spoolSelector); interr == nil {
			spoolId = id
		}

		if spoolId == -1 {
			query := make(map[string]string)

			if locerr == nil && location != "" {
				location = MapToAlias(location)
				query["Location"] = location
			}

			spools, finderr := apiClient.FindSpoolsByName(args[i], nil, query)
			if finderr != nil {
				errs = errors.Join(errs, fmt.Errorf("error looking up spool '%s': %w", spoolSelector, finderr))

				continue
			}

			if len(spools) == 0 {
				errs = errors.Join(errs, fmt.Errorf("spool not found: %s", spoolSelector))

				continue
			}

			if len(spools) != 1 {
				if allowInteractive {
					chosen, canceled, selErr := selectSpoolInteractively(apiClient, spoolSelector, query, spools, simpleSelect)
					if selErr != nil {
						errs = errors.Join(errs, fmt.Errorf("selection error: %w", selErr))
						continue
					}
					if canceled {
						return errors.New("selection canceled; no usages executed")
					}
					spoolId = chosen.Id
				} else {
					errs = errors.Join(errs, fmt.Errorf("multiple spools found (%d): %s", len(spools), spoolSelector))
					fmt.Printf("Multiple spools found (%d): %s\n", len(spools), spoolSelector)
					for _, s := range spools {
						fmt.Printf(" - %s\n", s)
					}
					fmt.Println()
					continue
				}
			} else {
				spoolId = spools[0].Id
			}
		}

		amount, floatErr := strconv.ParseFloat(args[i+1], 64)
		if floatErr != nil {
			fmt.Printf("Invalid filament usage amount (must be a number): %s.\n", args[i+1])

			return errors.New("invalid filament amount")
		}

		// round to 1 decimal place
		amount = math.RoundToEven(amount*10) / 10

		// add to the list of usages
		usages = append(usages, SpoolUsage{
			SpoolId: spoolId,
			Amount:  amount,
		})
	}

	for _, u := range usages {
		// check that the spool exists
		spool, err := apiClient.FindSpoolsById(u.SpoolId)
		if errors.Is(err, api.ErrSpoolNotFound) {
			notFound := color.RGB(200, 0, 0).Sprintf("Spool %d not found.\n", u.SpoolId)
			fmt.Println(notFound)

			continue
		}

		// check that the amount is available on the spool
		if spool.RemainingWeight < (u.Amount - 0.1) {
			color.Yellow(
				"Not enough filament on spool #%d [%s - %s] (only %.1fg available).\n",
				u.SpoolId,
				spool.Filament.Name,
				spool.Filament.Vendor.Name,
				spool.RemainingWeight,
			)
			errs = errors.Join(
				errs,
				fmt.Errorf(
					"not enough filament on spool #%d [%s - %s] (only %.1fg available)",
					u.SpoolId,
					spool.Filament.Name,
					spool.Filament.Vendor.Name,
					spool.RemainingWeight,
				),
			)

			continue
		}

		if !dryRun {
			// call the API to mark the spool as used
			useErr := apiClient.UseFilament(u.SpoolId, u.Amount)
			if useErr != nil {
				errs = errors.Join(errs, fmt.Errorf("failed to mark spool %d as used: %w", u.SpoolId, useErr))

				continue
			}
		}

		remaining := spool.RemainingWeight - u.Amount
		if u.Amount < 0 {
			_, _ = color.RGB(
				255,
				0,
				255,
			).Printf(
				" - Unusing spool #%d [%s - %s] (%.1fg of filament) - %.1fg remaining.\n",
				u.SpoolId,
				spool.Filament.Name,
				spool.Filament.Vendor.Name,
				u.Amount,
				remaining,
			)
		} else {
			_, _ = color.RGB(
				0,
				255,
				0,
			).Printf(
				" - Marking spool #%d [%s - %s] as used (%.1fg of filament) - %.1fg remaining.\n",
				u.SpoolId,
				spool.Filament.Name,
				spool.Filament.Vendor.Name,
				u.Amount,
				remaining,
			)
		}
	}

	cmd.SilenceUsage = true

	return errs
}

func init() {
	rootCmd.AddCommand(useCmd)

	useCmd.Flags().BoolP("dry-run", "d", false, "show what would be used, but don't actually use anything")
	useCmd.Flags().StringP("Location", "l", "", "filter by Location, default is all")
	useCmd.Flags().BoolP("non-interactive", "n", false, "do not prompt; if multiple spools match, behave as current non-interactive error behavior")
	useCmd.Flags().Bool("simple-select", false, "use a basic numbered selector instead of interactive menu (fallback for limited terminals)")
}
