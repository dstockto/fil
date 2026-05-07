package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/dstockto/fil/models"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var planTimeCmd = &cobra.Command{
	Use:     "time [duration]",
	Aliases: []string{"t"},
	Short:   "Set estimated print time for an in-progress plate",
	Long:    "Set the estimated remaining print time for an in-progress plate. Duration is always interpreted as remaining from now (e.g. 6h25m, 2h, 45m).",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if PlanOps == nil {
			return fmt.Errorf("plan operations not configured (need either plans_server or api_base+plans_dir)")
		}
		plans, err := discoverPlans()
		if err != nil {
			return err
		}

		type inProgressPlate struct {
			discoveredIdx     int
			projectIdx        int
			plateIdx          int
			projectName       string
			plateName         string
			printer           string
			estimatedDuration string
			displayName       string
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
					display := fmt.Sprintf("%s - %s", models.Sanitize(proj.Name), models.Sanitize(plate.Name))
					if plate.Printer != "" {
						display += fmt.Sprintf(" (on %s)", models.Sanitize(plate.Printer))
					}
					inProgress = append(inProgress, inProgressPlate{
						discoveredIdx:     di,
						projectIdx:        pi,
						plateIdx:          pli,
						projectName:       proj.Name,
						plateName:         plate.Name,
						printer:           plate.Printer,
						estimatedDuration: plate.EstimatedDuration,
						displayName:       display,
					})
				}
			}
		}

		if len(inProgress) == 0 {
			fmt.Println("No in-progress plates found.")
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
				Label:             "Select plate to set time for",
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

		var durationStr string
		if len(args) == 1 {
			durationStr = args[0]
		} else {
			defaultVal := selected.estimatedDuration
			label := "Estimated time remaining"
			if defaultVal != "" {
				label = fmt.Sprintf("Estimated time remaining (current: %s)", defaultVal)
			}
			prompt := promptui.Prompt{
				Label:   label,
				Default: defaultVal,
				Stdout:  NoBellStdout,
			}
			result, err := prompt.Run()
			if err != nil {
				return err
			}
			durationStr = result
		}

		if durationStr == "" {
			fmt.Println("No duration provided.")
			return nil
		}

		// Validate the duration
		_, err = time.ParseDuration(durationStr)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w (examples: 6h25m, 2h, 45m)", durationStr, err)
		}

		dp := &plans[selected.discoveredIdx]
		dp.Plan.Projects[selected.projectIdx].Plates[selected.plateIdx].StartedAt = time.Now().UTC().Format(time.RFC3339)
		dp.Plan.Projects[selected.projectIdx].Plates[selected.plateIdx].EstimatedDuration = durationStr

		if err := PlanOps.SaveAll(cmd.Context(), planFileName(*dp), dp.Plan); err != nil {
			return fmt.Errorf("failed to save plan: %w", err)
		}

		dur, _ := time.ParseDuration(durationStr)
		eta := time.Now().Add(dur)
		fmt.Printf("Set %s - %s: %s remaining (done ~%s)\n",
			models.Sanitize(selected.projectName),
			models.Sanitize(selected.plateName),
			durationStr,
			eta.Format("3:04pm"),
		)

		return nil
	},
}

func init() {
	planCmd.AddCommand(planTimeCmd)
}
