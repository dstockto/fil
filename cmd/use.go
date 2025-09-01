/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/dstockto/fil/api"
	"github.com/spf13/cobra"
)

// useCmd represents the use command
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
		return fmt.Errorf("apiClient endpoint not configured")
	}

	apiClient := api.NewClient(Cfg.ApiBase)

	// arguments should be a spool ID followed by a filament amount. It should check that the spool exists and that the amount is valid.
	// then it should call the API to mark the spool so some of it is used (if there's enough filament). If there is not enough,
	// it should print an error.
	if len(args)%2 != 0 || len(args) < 2 {
		fmt.Println("Arguments must be a spool ID followed by a filament amount.")
		return fmt.Errorf("arguments should be a spool ID followed by a filament amount")
	}
	var usages []SpoolUsage
	for i := 0; i < len(args); i += 2 {
		spoolId, err := strconv.Atoi(args[i])
		if err != nil {
			fmt.Printf("Invalid spool ID (must be an integer): %s.\n")
			return fmt.Errorf("invalid spool ID")
		}
		amount, err := strconv.ParseFloat(args[i+1], 64)
		if err != nil {
			fmt.Printf("Invalid filament amount (must be a float): %s.\n", args[i+1])
			return fmt.Errorf("invalid filament amount")
		}
		// limit amount to 1 decimal places
		amount = float64(int(amount*10)) / 10
		// add to the list of usages
		usages = append(usages, SpoolUsage{
			SpoolId: spoolId,
			Amount:  amount,
		})
	}

	var errs error

	for _, u := range usages {
		// check that the spool exists
		spool, err := apiClient.FindSpoolsById(u.SpoolId)
		if errors.Is(err, api.ErrSpoolNotFound) {
			fmt.Printf("\u001B[38;2;200;0;0mSpool %d not found.\n\x1b[0m", u.SpoolId)
			continue
		}

		// check that the amount is available on the spool
		if spool.RemainingWeight < u.Amount {
			fmt.Printf("\u001B[38;2;200;200;0mNot enough filament on spool #%d [%s - %s] (only %.1fg available).\n\x1b[0m", u.SpoolId, spool.Filament.Name, spool.Filament.Vendor.Name, spool.RemainingWeight)
			errs = errors.Join(errs, fmt.Errorf("not enough filament on spool #%d [%s - %s] (only %.1fg available)", u.SpoolId, spool.Filament.Name, spool.Filament.Vendor.Name, spool.RemainingWeight))
			continue
		}

		// call the API to mark the spool as used
		err = apiClient.UseFilament(u.SpoolId, u.Amount)
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("failed to mark spool %d as used: %w", u.SpoolId, err))
			continue
		}

		remaining := spool.RemainingWeight - u.Amount
		if u.Amount < 0 {
			fmt.Printf("\u001B[38;2;255;0;255m - Unusing spool #%d [%s - %s] (%.1fg of filament) - %.1fg remaining.\x1b[0m\n", u.SpoolId, spool.Filament.Name, spool.Filament.Vendor.Name, u.Amount, remaining)
		} else {
			fmt.Printf("\u001B[38;2;0;255;0m - Marking spool #%d [%s - %s] as used (%.1fg of filament) - %.1fg remaining.\x1b[0m\n", u.SpoolId, spool.Filament.Name, spool.Filament.Vendor.Name, u.Amount, remaining)
		}
	}

	cmd.SilenceUsage = true
	return errs
}

func init() {
	rootCmd.AddCommand(useCmd)
}
