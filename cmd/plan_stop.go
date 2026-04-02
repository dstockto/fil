package cmd

import (
	"fmt"
	"strings"

	"github.com/dstockto/fil/models"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var planStopCmd = &cobra.Command{
	Use:     "stop",
	Aliases: []string{"cancel"},
	Short:   "Cancel an in-progress plate (set back to todo)",
	RunE: func(cmd *cobra.Command, args []string) error {
		printerFilter, _ := cmd.Flags().GetString("printer")

		plans, err := discoverPlans()
		if err != nil {
			return err
		}

		type inProgressPlate struct {
			discoveredIdx int
			projectIdx    int
			plateIdx      int
			projectName   string
			plateName     string
			printer       string
			displayName   string
		}
		var inProgress []inProgressPlate

		for di, dp := range plans {
			for pi, proj := range dp.Plan.Projects {
				if proj.Status == "completed" {
					continue
				}
				for pli, plate := range proj.Plates {
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
						projectIdx:    pi,
						plateIdx:      pli,
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
			var items []string
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

		dp := &plans[selected.discoveredIdx]
		dp.Plan.Projects[selected.projectIdx].Plates[selected.plateIdx].Status = "todo"
		dp.Plan.Projects[selected.projectIdx].Plates[selected.plateIdx].Printer = ""
		dp.Plan.Projects[selected.projectIdx].Plates[selected.plateIdx].StartedAt = ""

		if err := savePlan(*dp, dp.Plan); err != nil {
			return fmt.Errorf("failed to save plan: %w", err)
		}

		fmt.Printf("Cancelled %s - %s — set back to todo\n", models.Sanitize(selected.projectName), models.Sanitize(selected.plateName))
		return nil
	},
}

func init() {
	planCmd.AddCommand(planStopCmd)
	planStopCmd.Flags().StringP("printer", "p", "", "Filter to plates on a specific printer")
}
