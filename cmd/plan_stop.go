package cmd

import (
	"fmt"
	"strings"

	"github.com/dstockto/fil/models"
	"github.com/dstockto/fil/plan"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var planStopCmd = &cobra.Command{
	Use:     "stop",
	Aliases: []string{"cancel"},
	Short:   "Cancel an in-progress plate (set back to todo)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil {
			return fmt.Errorf("config not loaded")
		}
		if PlanOps == nil {
			return fmt.Errorf("plan operations not configured (need either plans_server or api_base+plans_dir)")
		}
		printerFilter, _ := cmd.Flags().GetString("printer")

		discovered, err := discoverPlans()
		if err != nil {
			return err
		}

		type inProgressPlate struct {
			discoveredIdx int
			projectName   string
			plateName     string
			printer       string
			displayName   string
		}
		var inProgress []inProgressPlate

		for di, dp := range discovered {
			for _, proj := range dp.Plan.Projects {
				if proj.Status == "completed" {
					continue
				}
				for _, plate := range proj.Plates {
					if plate.Status != "in-progress" {
						continue
					}
					if printerFilter != "" && plate.Printer != printerFilter {
						continue
					}
					display := fmt.Sprintf("%s - %s", models.Sanitize(proj.Name), models.Sanitize(plate.Name))
					if plate.Printer != "" {
						display += fmt.Sprintf(" (on %s)", models.Sanitize(plate.Printer))
					}
					inProgress = append(inProgress, inProgressPlate{
						discoveredIdx: di,
						projectName:   proj.Name,
						plateName:     plate.Name,
						printer:       plate.Printer,
						displayName:   display,
					})
				}
			}
		}

		if len(inProgress) == 0 {
			if printerFilter != "" {
				fmt.Printf("No in-progress plates found on %s.\n", printerFilter)
			} else {
				fmt.Println("No in-progress plates found.")
			}
			return nil
		}

		var selected inProgressPlate
		if len(inProgress) == 1 {
			selected = inProgress[0]
		} else {
			items := make([]string, 0, len(inProgress))
			for _, ip := range inProgress {
				items = append(items, ip.displayName)
			}
			prompt := promptui.Select{
				Label:             "Select plate to cancel",
				Items:             items,
				Stdout:            NoBellStdout,
				StartInSearchMode: true,
				Searcher: func(input string, index int) bool {
					return strings.Contains(strings.ToLower(items[index]), strings.ToLower(input))
				},
			}
			idx, _, err := prompt.Run()
			if err != nil {
				return err
			}
			selected = inProgress[idx]
		}

		err = PlanOps.Stop(cmd.Context(), plan.StopRequest{
			Plan:    planFileName(discovered[selected.discoveredIdx]),
			Project: selected.projectName,
			Plate:   selected.plateName,
		})
		if err != nil {
			return fmt.Errorf("stop: %w", err)
		}

		fmt.Printf("Cancelled %s - %s — set back to todo\n", models.Sanitize(selected.projectName), models.Sanitize(selected.plateName))
		return nil
	},
}

func init() {
	planCmd.AddCommand(planStopCmd)
	planStopCmd.Flags().StringP("printer", "p", "", "Filter to plates on a specific printer")
}
