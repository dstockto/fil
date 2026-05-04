package cmd

import (
	"fmt"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var planArchiveCmd = &cobra.Command{
	Use:     "archive",
	Aliases: []string{"a"},
	Short:   "Move completed plan files to the archive directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil {
			return fmt.Errorf("config not loaded")
		}
		if PlanOps == nil {
			return fmt.Errorf("plan operations not configured (need either plans_server or api_base+plans_dir)")
		}

		pick, _ := cmd.Flags().GetBool("pick")

		discovered, _ := discoverPlans()

		// Filter to plans where every project is completed.
		var completed []DiscoveredPlan
		for _, dp := range discovered {
			allDone := true
			for _, proj := range dp.Plan.Projects {
				if proj.Status != "completed" {
					allDone = false
					break
				}
			}
			if allDone {
				completed = append(completed, dp)
			} else {
				fmt.Printf("Skipping %s (not all projects are completed)\n", dp.DisplayName)
			}
		}

		if len(completed) == 0 {
			fmt.Println("No completed plans to archive.")
			return nil
		}

		if pick {
			selected, err := selectPlanToArchive(completed)
			if err != nil {
				return err
			}
			if selected == nil {
				fmt.Println("No plan selected.")
				return nil
			}
			completed = []DiscoveredPlan{*selected}
		}

		ctx := cmd.Context()
		for _, dp := range completed {
			if err := PlanOps.Archive(ctx, planFileName(dp)); err != nil {
				fmt.Printf("  Error archiving %s: %v\n", dp.DisplayName, err)
				continue
			}
			fmt.Printf("Archived %s\n", dp.DisplayName)
		}
		return nil
	},
}

func selectPlanToArchive(plans []DiscoveredPlan) (*DiscoveredPlan, error) {
	if len(plans) == 1 {
		return &plans[0], nil
	}

	items := make([]string, len(plans))
	for i, dp := range plans {
		items[i] = dp.DisplayName
	}

	prompt := promptui.Select{
		Label: "Select a plan to archive",
		Items: items,
		Size:  10,
	}

	idx, _, err := prompt.Run()
	if err != nil {
		if err == promptui.ErrInterrupt || err == promptui.ErrEOF {
			return nil, nil
		}
		return nil, err
	}

	return &plans[idx], nil
}

func init() {
	planArchiveCmd.Flags().BoolP("pick", "p", false, "interactively select which completed plan to archive")
	planCmd.AddCommand(planArchiveCmd)
}
