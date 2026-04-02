package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dstockto/fil/models"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var planAmountsCmd = &cobra.Command{
	Use:     "amounts [file]",
	Aliases: []string{"amt"},
	Short:   "Interactively fill in filament amounts for plan plates",
	RunE: func(cmd *cobra.Command, args []string) error {
		var dp *DiscoveredPlan
		if len(args) > 0 {
			dp = &DiscoveredPlan{Path: args[0]}
			data, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			var plan models.PlanFile
			if err := yaml.Unmarshal(data, &plan); err != nil {
				return err
			}
			plan.DefaultStatus()
			dp.Plan = plan
			dp.DisplayName = FormatPlanPath(args[0])
		} else {
			plans, err := discoverPlans()
			if err != nil {
				return err
			}
			dp, err = selectPlan("Select plan to fill amounts", plans)
			if err != nil {
				return err
			}
		}

		showAll, _ := cmd.Flags().GetBool("all")
		plan := dp.Plan
		modified := false
		count := 0

		for i := range plan.Projects {
			proj := &plan.Projects[i]
			if proj.Status == "completed" {
				continue
			}
			for j := range proj.Plates {
				plate := &proj.Plates[j]
				if plate.Status == "completed" {
					continue
				}
				for k := range plate.Needs {
					need := &plate.Needs[k]
					if !showAll && need.Amount != 0 {
						continue
					}

					filamentDesc := need.Name
					if need.Material != "" {
						filamentDesc += " (" + need.Material + ")"
					}
					if need.Color != "" {
						filamentDesc += " [" + need.Color + "]"
					}
					label := fmt.Sprintf("%s > %s > %s (grams)", proj.Name, plate.Name, filamentDesc)

					defaultVal := "0"
					if need.Amount != 0 {
						defaultVal = strconv.FormatFloat(need.Amount, 'f', 1, 64)
					}

					prompt := promptui.Prompt{
						Label:   label,
						Default: defaultVal,
						Stdin:   os.Stdin,
						Stdout:  NoBellStdout,
						Validate: func(input string) error {
							input = strings.TrimSpace(input)
							if input == "" {
								return nil
							}
							val, err := strconv.ParseFloat(input, 64)
							if err != nil {
								return fmt.Errorf("enter a valid number")
							}
							if val < 0 {
								return fmt.Errorf("amount cannot be negative")
							}
							return nil
						},
					}

					result, err := prompt.Run()
					if err != nil {
						// Ctrl+C or interrupt
						if modified {
							fmt.Printf("\nInterrupted. Save %d change(s)? [Y/n] ", count)
							var confirm string
							_, _ = fmt.Scanln(&confirm)
							if confirm == "" || strings.EqualFold(confirm, "y") {
								if saveErr := savePlan(*dp, plan); saveErr != nil {
									return fmt.Errorf("failed to save plan: %w", saveErr)
								}
								fmt.Println("Plan saved.")
								return nil
							}
						}
						return err
					}

					result = strings.TrimSpace(result)
					val, _ := strconv.ParseFloat(result, 64)
					val = RoundAmount(val)
					if val != need.Amount {
						need.Amount = val
						modified = true
						count++
					}
				}
			}
		}

		if !modified {
			if showAll {
				fmt.Println("No amounts to edit (no incomplete plates with needs).")
			} else {
				fmt.Println("No zero-amount needs found. Use --all to edit all amounts.")
			}
			return nil
		}

		fmt.Printf("Updated %d amount(s). Saving plan...\n", count)
		if err := savePlan(*dp, plan); err != nil {
			return fmt.Errorf("failed to save plan: %w", err)
		}
		fmt.Println("Plan saved.")
		return nil
	},
}

func init() {
	planAmountsCmd.Flags().Bool("all", false, "Edit all amounts, not just zero-value ones")
	planCmd.AddCommand(planAmountsCmd)
}
