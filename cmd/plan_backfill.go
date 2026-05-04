package cmd

import (
	"fmt"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/spf13/cobra"
)

var planBackfillColorsCmd = &cobra.Command{
	Use:   "backfill-colors",
	Short: "Populate missing filament colors on all plan needs from Spoolman",
	Long: `Looks up each need's FilamentID in Spoolman and writes the color_hex
into the plan YAML. SaveAll on data-edit verbs auto-fills colors for newly
resolved needs; this command sweeps existing plans for needs that pre-date
that auto-fill.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.ApiBase == "" {
			return fmt.Errorf("api_base must be configured")
		}
		if PlanOps == nil {
			return fmt.Errorf("plan operations not configured (need either plans_server or api_base+plans_dir)")
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

			// fillPlanColors mirrors plan.backfillPlanColors but lives here so
			// the cmd layer doesn't need to import internal plan helpers. The
			// canonical owner of the fill rule is plan/backfill.go.
			changed := fillPlanColors(&plan, spools)
			if !changed {
				continue
			}

			if dryRun {
				fmt.Printf("  %s: would fill %d color(s)\n", dp.DisplayName, filled)
			} else {
				if err := PlanOps.SaveAll(ctx, planFileName(dp), plan); err != nil {
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

// fillPlanColors fills missing Need.Color values from a spool list. Mirrors
// plan/backfill.go's backfillPlanColors so the dry-run path can detect
// whether anything would change before deciding to call SaveAll.
func fillPlanColors(plan *models.PlanFile, spools []models.FindSpool) bool {
	colorByFilament := map[int]string{}
	for _, s := range spools {
		if s.Filament.Id != 0 && s.Filament.ColorHex != "" {
			colorByFilament[s.Filament.Id] = s.Filament.ColorHex
		}
	}
	changed := false
	for i := range plan.Projects {
		for j := range plan.Projects[i].Plates {
			for k := range plan.Projects[i].Plates[j].Needs {
				need := &plan.Projects[i].Plates[j].Needs[k]
				if need.Color == "" && need.FilamentID != 0 {
					if hex, ok := colorByFilament[need.FilamentID]; ok {
						need.Color = hex
						changed = true
					}
				}
			}
		}
	}
	return changed
}

//nolint:gochecknoinits
func init() {
	planCmd.AddCommand(planBackfillColorsCmd)
	planBackfillColorsCmd.Flags().Bool("dry-run", false, "show what would be filled without saving")
}
