package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var planBackfillColorsCmd = &cobra.Command{
	Use:   "backfill-colors",
	Short: "Populate missing filament colors on all plan needs from Spoolman",
	Long: `Looks up each need's FilamentID in Spoolman and writes the color_hex
into the plan YAML. Run once to backfill existing plans; future saves
will populate colors automatically.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.ApiBase == "" {
			return fmt.Errorf("api_base must be configured")
		}

		ctx := cmd.Context()
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
		spools, err := apiClient.FindSpoolsByName(ctx, "*", nil, nil)
		if err != nil {
			return fmt.Errorf("failed to fetch spools: %w", err)
		}

		plans, err := discoverPlans()
		if err != nil {
			return fmt.Errorf("failed to discover plans: %w", err)
		}

		totalFilled := 0
		for _, dp := range plans {
			plan := dp.Plan
			filled := countMissingColors(&plan, spools)
			if filled == 0 {
				continue
			}

			changed := backfillPlanColors(&plan, spools)
			if !changed {
				continue
			}

			if dryRun {
				fmt.Printf("  %s: would fill %d color(s)\n", dp.DisplayName, filled)
			} else {
				if err := savePlanDirect(dp, plan); err != nil {
					fmt.Printf("  %s: error saving: %v\n", dp.DisplayName, err)
					continue
				}
				fmt.Printf("  %s: filled %d color(s)\n", dp.DisplayName, filled)
			}
			totalFilled += filled
		}

		if totalFilled == 0 {
			fmt.Println("All plans already have colors populated.")
		} else if dryRun {
			fmt.Printf("\n%d color(s) would be filled (dry run).\n", totalFilled)
		} else {
			fmt.Printf("\n%d color(s) filled.\n", totalFilled)
		}
		return nil
	},
}

// countMissingColors counts how many needs in a plan could be backfilled.
func countMissingColors(plan *models.PlanFile, spools []models.FindSpool) int {
	colorByFilament := make(map[int]string)
	for _, s := range spools {
		if s.Filament.Id != 0 && s.Filament.ColorHex != "" {
			colorByFilament[s.Filament.Id] = s.Filament.ColorHex
		}
	}

	count := 0
	for _, proj := range plan.Projects {
		for _, plate := range proj.Plates {
			for _, need := range plate.Needs {
				if need.Color == "" && need.FilamentID != 0 {
					if _, ok := colorByFilament[need.FilamentID]; ok {
						count++
					}
				}
			}
		}
	}
	return count
}

// savePlanDirect saves a plan without the backfill hook (to avoid double-backfill).
func savePlanDirect(dp DiscoveredPlan, plan models.PlanFile) error {
	out, err := yaml.Marshal(plan)
	if err != nil {
		return err
	}
	if dp.Remote {
		client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
		return client.PutPlan(context.Background(), dp.RemoteName, out)
	}
	return os.WriteFile(dp.Path, out, 0644)
}

//nolint:gochecknoinits
func init() {
	planCmd.AddCommand(planBackfillColorsCmd)
	planBackfillColorsCmd.Flags().Bool("dry-run", false, "show what would be filled without saving")
}
