package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/fatih/color"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var editCmd = &cobra.Command{
	Use:          "edit [spool-id]",
	Aliases:      []string{"e"},
	Short:        "Edit spool weight properties",
	Long:         `Edit remaining weight, initial weight, spool empty weight, or measured weight for a spool.`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE:         runEdit,
}

func runEdit(cmd *cobra.Command, args []string) error {
	if Cfg == nil || Cfg.ApiBase == "" {
		return errors.New("api endpoint not configured")
	}

	apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
	ctx := cmd.Context()

	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Find the spool
	var spoolID int
	if len(args) > 0 {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid spool ID: %s", args[0])
		}
		spoolID = id
	} else {
		// Interactive search
		spool, canceled, err := selectSpoolInteractively(ctx, apiClient, "", nil, nil, false)
		if err != nil {
			return err
		}
		if canceled {
			return nil
		}
		spoolID = spool.Id
	}

	spool, err := apiClient.FindSpoolsById(ctx, spoolID)
	if err != nil {
		return fmt.Errorf("spool #%d not found: %w", spoolID, err)
	}

	// Display current state
	spoolWeight := spool.SpoolWeight
	if spoolWeight == 0 {
		spoolWeight = spool.Filament.SpoolWeight
	}
	measuredWeight := spool.RemainingWeight + spoolWeight

	fmt.Printf("\nSpool #%d: %s %s (%s)\n", spool.Id, spool.Filament.Vendor.Name, spool.Filament.Name, spool.Filament.Material)
	fmt.Printf("  Remaining:     %.1fg\n", spool.RemainingWeight)
	fmt.Printf("  Initial:       %.1fg\n", spool.InitialWeight)
	fmt.Printf("  Spool weight:  %.1fg\n", spoolWeight)
	fmt.Printf("  Measured:      %.1fg (remaining + spool weight)\n", measuredWeight)
	if spool.Location != "" {
		fmt.Printf("  Location:      %s\n", spool.Location)
	}
	fmt.Println()

	// Choose what to update
	actionPrompt := promptui.Select{
		Label: "What would you like to update?",
		Items: []string{
			"Set measured weight (I weighed the spool)",
			"Set remaining weight (I'm estimating)",
			"Set spool empty weight",
			"Set initial weight",
			"Cancel",
		},
		Stdout: NoBellStdout,
	}
	actionIdx, _, err := actionPrompt.Run()
	if err != nil {
		return nil
	}

	reader := bufio.NewReader(os.Stdin)
	updates := map[string]any{}

	switch actionIdx {
	case 0: // Measured weight
		// Ask for spool empty weight with current as default
		newSpoolWeight, err := promptFloat(reader, "Spool empty weight", spoolWeight)
		if err != nil {
			return nil
		}

		newMeasured, err := promptFloatRequired(reader, "Measured weight on scale")
		if err != nil {
			return nil
		}

		remaining := newMeasured - newSpoolWeight
		if remaining < 0 {
			color.Red("Measured weight (%.1fg) is less than spool empty weight (%.1fg)", newMeasured, newSpoolWeight)
			return nil
		}

		fmt.Printf("  → Remaining: %.1fg (%.1f - %.1f)\n", remaining, newMeasured, newSpoolWeight)

		if newSpoolWeight != spoolWeight {
			updates["spool_weight"] = newSpoolWeight
		}
		updates["remaining_weight"] = remaining

		// Check if remaining exceeds initial weight
		if remaining > spool.InitialWeight {
			fmt.Printf("  → Initial weight is %.1fg but remaining is %.1fg\n", spool.InitialWeight, remaining)
			newInitial := math.Ceil(remaining)
			confirm := promptYesNo(reader, fmt.Sprintf("    Update initial weight to %.0fg?", newInitial))
			if confirm {
				updates["initial_weight"] = newInitial
			}
		}

	case 1: // Remaining weight
		newRemaining, err := promptFloatRequired(reader, "Remaining weight")
		if err != nil {
			return nil
		}
		updates["remaining_weight"] = newRemaining

		if newRemaining > spool.InitialWeight {
			fmt.Printf("  → Initial weight is %.1fg but remaining is %.1fg\n", spool.InitialWeight, newRemaining)
			newInitial := math.Ceil(newRemaining)
			confirm := promptYesNo(reader, fmt.Sprintf("    Update initial weight to %.0fg?", newInitial))
			if confirm {
				updates["initial_weight"] = newInitial
			}
		}

	case 2: // Spool empty weight
		newSpoolWeight, err := promptFloatRequired(reader, "Spool empty weight")
		if err != nil {
			return nil
		}
		updates["spool_weight"] = newSpoolWeight

	case 3: // Initial weight
		newInitial, err := promptFloatRequired(reader, "Initial weight")
		if err != nil {
			return nil
		}
		updates["initial_weight"] = newInitial

	case 4: // Cancel
		return nil
	}

	if len(updates) == 0 {
		fmt.Println("No changes to make.")
		return nil
	}

	// Show summary
	fmt.Println()
	for field, val := range updates {
		fmt.Printf("  %s → %.1f\n", field, val)
	}

	if dryRun {
		color.HiRed("Dry run — no changes made.")
		return nil
	}

	if err := apiClient.PatchSpool(ctx, spoolID, updates); err != nil {
		return fmt.Errorf("failed to update spool: %w", err)
	}

	color.Green("Spool #%d updated.", spoolID)
	return nil
}

func promptFloat(reader *bufio.Reader, label string, defaultVal float64) (float64, error) {
	fmt.Printf("%s [%.1f]: ", label, defaultVal)
	input, err := reader.ReadString('\n')
	if err != nil {
		return 0, err
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal, nil
	}
	val, err := strconv.ParseFloat(input, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number: %s", input)
	}
	return val, nil
}

func promptFloatRequired(reader *bufio.Reader, label string) (float64, error) {
	fmt.Printf("%s: ", label)
	input, err := reader.ReadString('\n')
	if err != nil {
		return 0, err
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return 0, fmt.Errorf("value required")
	}
	val, err := strconv.ParseFloat(input, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number: %s", input)
	}
	return val, nil
}

func promptYesNo(reader *bufio.Reader, label string) bool {
	fmt.Printf("%s [Y/n]: ", label)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	input = strings.TrimSpace(strings.ToLower(input))
	return input == "" || input == "y" || input == "yes"
}

func init() {
	rootCmd.AddCommand(editCmd)
	editCmd.Flags().BoolP("dry-run", "d", false, "show what would change without updating")
}
