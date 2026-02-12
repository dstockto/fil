package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var planCompleteCmd = &cobra.Command{
	Use:     "complete [file]",
	Aliases: []string{"done", "c"},
	Short:   "Mark a plate or project as completed",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.ApiBase == "" {
			return fmt.Errorf("api endpoint not configured")
		}
		apiClient := api.NewClient(Cfg.ApiBase)

		var path string
		if len(args) > 0 {
			path = args[0]
		} else {
			plans, _ := discoverPlans()
			if len(plans) == 0 {
				return fmt.Errorf("no plans found")
			}
			if len(plans) == 1 {
				path = plans[0].Path
			} else {
				var items []string
				for _, p := range plans {
					items = append(items, p.DisplayName)
				}
				prompt := promptui.Select{
					Label:             "Select plan file",
					Items:             items,
					Stdout:            NoBellStdout,
					StartInSearchMode: true,
					Searcher: func(input string, index int) bool {
						return strings.Contains(strings.ToLower(items[index]), strings.ToLower(input))
					},
				}
				selectedIdx, _, err := prompt.Run()
				if err != nil {
					return err
				}
				path = plans[selectedIdx].Path
			}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var plan models.PlanFile
		yaml.Unmarshal(data, &plan)
		plan.DefaultStatus()

		// Select Project and Plate
		var options []string
		type opt struct {
			projIdx  int
			plateIdx int
			isProj   bool
		}
		var optMap []opt

		for i, proj := range plan.Projects {
			if proj.Status != "completed" {
				options = append(options, fmt.Sprintf("Project: %s", proj.Name))
				optMap = append(optMap, opt{projIdx: i, isProj: true})
				for j, plate := range proj.Plates {
					if plate.Status != "completed" {
						options = append(options, fmt.Sprintf("  Plate: %s", plate.Name))
						optMap = append(optMap, opt{projIdx: i, plateIdx: j, isProj: false})
					}
				}
			}
		}

		if len(options) == 0 {
			fmt.Println("Nothing to complete.")
			return nil
		}

		prompt := promptui.Select{
			Label:             "What did you complete?",
			Items:             options,
			Size:              10,
			Stdout:            NoBellStdout,
			StartInSearchMode: true,
			Searcher: func(input string, index int) bool {
				return strings.Contains(strings.ToLower(options[index]), strings.ToLower(input))
			},
		}
		idx, _, err := prompt.Run()
		if err != nil {
			return err
		}

		choice := optMap[idx]
		if choice.isProj {
			plan.Projects[choice.projIdx].Status = "completed"
			for j := range plan.Projects[choice.projIdx].Plates {
				plan.Projects[choice.projIdx].Plates[j].Status = "completed"
			}
		} else {
			plan.Projects[choice.projIdx].Plates[choice.plateIdx].Status = "completed"
			// Check if all plates in project are done
			allDone := true
			for _, p := range plan.Projects[choice.projIdx].Plates {
				if p.Status != "completed" {
					allDone = false
					break
				}
			}
			if allDone {
				plan.Projects[choice.projIdx].Status = "completed"
			}

			// Printer selection for filament usage tracking
			var printerName string
			if len(Cfg.Printers) > 0 {
				var printerNames []string
				for name := range Cfg.Printers {
					printerNames = append(printerNames, name)
				}
				if len(printerNames) == 1 {
					printerName = printerNames[0]
				} else {
					promptPrinter := promptui.Select{
						Label:             "Which printer was used?",
						Items:             append([]string{"None/Other"}, printerNames...),
						Stdout:            NoBellStdout,
						StartInSearchMode: true,
						Searcher: func(input string, index int) bool {
							items := append([]string{"None/Other"}, printerNames...)
							return strings.Contains(strings.ToLower(items[index]), strings.ToLower(input))
						},
					}
					_, result, err := promptPrinter.Run()
					if err == nil && result != "None/Other" {
						printerName = result
					}
				}
			}

			var printerLocations []string
			if printerName != "" {
				printerLocations = Cfg.Printers[printerName]
			}

			// Interactive usage recording
			fmt.Printf("Updating filament usage for %s...\n", plan.Projects[choice.projIdx].Plates[choice.plateIdx].Name)
			for _, req := range plan.Projects[choice.projIdx].Plates[choice.plateIdx].Needs {
				fmt.Printf("Filament: %s. Amount used (default %.1fg): ", req.Name, req.Amount)
				var input string
				fmt.Scanln(&input)
				used := req.Amount
				if input != "" {
					fmt.Sscanf(input, "%f", &used)
				}

				for used > 0 {
					// Find which spool to deduct from
					var matchedSpool *models.FindSpool

					// 1. Try to find matching spools in the printer
					if len(printerLocations) > 0 {
						allSpools, _ := apiClient.FindSpoolsByName("*", nil, nil)
						var candidates []models.FindSpool
						for _, s := range allSpools {
							inPrinter := false
							for _, loc := range printerLocations {
								if s.Location == loc {
									inPrinter = true
									break
								}
							}
							if !inPrinter {
								continue
							}

							// Check if it matches the requirement (either by ID or by name)
							if req.FilamentID != 0 && s.Filament.Id == req.FilamentID {
								candidates = append(candidates, s)
							} else if req.Name != "" && strings.Contains(strings.ToLower(s.Filament.Name), strings.ToLower(req.Name)) {
								candidates = append(candidates, s)
							}
						}

						// filter out candidates with no remaining weight if we have more than 1
						if len(candidates) > 1 {
							var withWeight []models.FindSpool
							for _, c := range candidates {
								if c.RemainingWeight > 0 {
									withWeight = append(withWeight, c)
								}
							}
							if len(withWeight) > 0 {
								candidates = withWeight
							}
						}

						if len(candidates) == 1 {
							matchedSpool = &candidates[0]
							fmt.Printf("Using spool #%d (%s) from %s (%.1fg -> %.1fg remaining)\n", matchedSpool.Id, matchedSpool.Filament.Name, matchedSpool.Location, matchedSpool.RemainingWeight, matchedSpool.RemainingWeight-used)
						} else if len(candidates) > 1 {
							var items []string
							for _, c := range candidates {
								items = append(items, fmt.Sprintf("#%d: %s (%s) - %.1fg -> %.1fg remaining", c.Id, c.Filament.Name, c.Location, c.RemainingWeight, c.RemainingWeight-used))
							}
							promptSpool := promptui.Select{
								Label:             fmt.Sprintf("Multiple matching spools found in %s. Select one:", printerName),
								Items:             append(items, "Other/Manual"),
								Stdout:            NoBellStdout,
								StartInSearchMode: true,
								Searcher: func(input string, index int) bool {
									all := append(items, "Other/Manual")
									return strings.Contains(strings.ToLower(all[index]), strings.ToLower(input))
								},
							}
							idx, _, err := promptSpool.Run()
							if err == nil && idx < len(candidates) {
								matchedSpool = &candidates[idx]
							}
						}
					}

					if matchedSpool != nil {
						amountToDeduct := used
						if used > matchedSpool.RemainingWeight && matchedSpool.RemainingWeight > 0 {
							fmt.Printf("Spool #%d only has %.1fg remaining. Deduct all of it and pick another spool for the rest? [Y/n] ", matchedSpool.Id, matchedSpool.RemainingWeight)
							var confirm string
							fmt.Scanln(&confirm)
							if confirm == "" || strings.ToLower(confirm) == "y" {
								amountToDeduct = matchedSpool.RemainingWeight
							}
						}

						err := UseFilamentSafely(apiClient, matchedSpool, amountToDeduct)
						if err == nil {
							used -= amountToDeduct
						} else {
							fmt.Printf("Error updating filament usage: %v\n", err)
							break
						}
					} else {
						// Fallback: ask for Spool ID
						fmt.Printf("Enter Spool ID to deduct from (%.1fg remaining to account for, or leave blank to skip): ", used)
						var spoolIdStr string
						fmt.Scanln(&spoolIdStr)
						if spoolIdStr != "" {
							var sid int
							fmt.Sscanf(spoolIdStr, "%d", &sid)
							spool, err := apiClient.FindSpoolsById(sid)
							if err == nil {
								amountToDeduct := used
								if used > spool.RemainingWeight && spool.RemainingWeight > 0 {
									fmt.Printf("Spool #%d only has %.1fg remaining. Deduct all of it and pick another spool for the rest? [Y/n] ", spool.Id, spool.RemainingWeight)
									var confirm string
									fmt.Scanln(&confirm)
									if confirm == "" || strings.ToLower(confirm) == "y" {
										amountToDeduct = spool.RemainingWeight
									}
								}
								err := UseFilamentSafely(apiClient, spool, amountToDeduct)
								if err == nil {
									used -= amountToDeduct
								} else {
									fmt.Printf("Error updating filament usage: %v\n", err)
									break
								}
							} else {
								fmt.Printf("Error finding spool #%d: %v. Using %.1fg anyway (may result in negative weight if not found in spoolman correctly)\n", sid, err, used)
								apiClient.UseFilament(sid, used)
								used = 0
							}
						} else {
							break
						}
					}
				}
			}
		}

		out, _ := yaml.Marshal(plan)
		os.WriteFile(path, out, 0644)
		fmt.Println("Plan updated.")
		return nil
	},
}

func init() {
	planCmd.AddCommand(planCompleteCmd)
}
