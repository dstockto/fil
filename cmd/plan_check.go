package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var planCheckCmd = &cobra.Command{
	Use:   "check [file]",
	Short: "Check if enough filament is available for a plan",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.ApiBase == "" {
			return fmt.Errorf("api endpoint not configured")
		}
		apiClient := api.NewClient(Cfg.ApiBase)

		var paths []string
		if len(args) > 0 {
			paths = append(paths, args...)
		} else {
			plans, err := discoverPlans()
			if err != nil {
				return err
			}
			for _, p := range plans {
				paths = append(paths, p.Path)
			}
		}

		if len(paths) == 0 {
			fmt.Println("No plans found to check.")
			return nil
		}

		// Aggregate needs by FilamentID (if resolved) or Name+Material (if unresolved)
		type projectUsage struct {
			projectName string
			amount      float64
		}
		type totalNeed struct {
			id              int
			name            string
			material        string
			colorHex        string
			multiColorHexes string
			amount          float64
			projects        []projectUsage
		}
		needs := make(map[string]*totalNeed)

		type zeroAmountWarning struct {
			projectName string
			plateName   string
			filament    string
			planPath    string
		}
		var zeroWarnings []zeroAmountWarning

		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				fmt.Printf("Error: Failed to read plan file %s: %v\n", FormatPlanPath(path), err)
				continue
			}
			var plan models.PlanFile
			if err := yaml.Unmarshal(data, &plan); err != nil {
				fmt.Printf("Error: Failed to parse plan file %s: %v\n", FormatPlanPath(path), err)
				continue
			}
			plan.DefaultStatus()

			for _, proj := range plan.Projects {
				if proj.Status == "completed" {
					continue
				}
				for _, plate := range proj.Plates {
					if plate.Status == "completed" {
						continue
					}
					for _, req := range plate.Needs {
						if req.Amount == 0 {
							zeroWarnings = append(zeroWarnings, zeroAmountWarning{
								projectName: proj.Name,
								plateName:   plate.Name,
								filament:    req.Name,
								planPath:    FormatPlanPath(path),
							})
						}
						key := fmt.Sprintf("id:%d", req.FilamentID)
						if req.FilamentID == 0 {
							key = fmt.Sprintf("name:%s:%s", req.Name, req.Material)
							// fmt.Printf("Warning: Plate '%s' in '%s' (%s) has unresolved filament '%s'\n", plate.Name, proj.Name, FormatPlanPath(path), req.Name)
						}
						if _, ok := needs[key]; !ok {
							needs[key] = &totalNeed{
								id:       req.FilamentID,
								name:     req.Name,
								material: req.Material,
								colorHex: req.Color,
							}
						} else if req.FilamentID != 0 && needs[key].name != req.Name {
							// If the same ID is used with different names, we should probably let the user know
							// but we will continue to aggregate them as they are technically the same filament ID
							fmt.Printf("Note: Filament ID %d is used for both '%s' and '%s'. Aggregating needs.\n", req.FilamentID, needs[key].name, req.Name)
						}
						needs[key].amount += req.Amount

						// Track project usage
						found := false
						for i, p := range needs[key].projects {
							if p.projectName == proj.Name {
								needs[key].projects[i].amount += req.Amount
								found = true
								break
							}
						}
						if !found {
							needs[key].projects = append(needs[key].projects, projectUsage{
								projectName: proj.Name,
								amount:      req.Amount,
							})
						}
					}
				}
			}
		}

		if len(needs) == 0 {
			fmt.Println("No pending needs found.")
			return nil
		}

		verbose, _ := cmd.Flags().GetBool("verbose")

		// Get all spools from Spoolman
		allSpools, err := apiClient.FindSpoolsByName("*", nil, nil)
		if err != nil {
			return err
		}

		// Inventory by Filament ID
		inventory := make(map[int]float64)
		isLoaded := make(map[int]bool)
		filamentColors := make(map[int]struct {
			colorHex        string
			multiColorHexes string
		})
		filamentInfo := make(map[int]struct {
			name   string
			vendor string
		})

		printerLocs := make(map[string]bool)
		for _, locs := range Cfg.Printers {
			for _, loc := range locs {
				printerLocs[loc] = true
			}
		}

		for _, s := range allSpools {
			if !s.Archived {
				inventory[s.Filament.Id] += s.RemainingWeight
				if printerLocs[s.Location] {
					isLoaded[s.Filament.Id] = true
				}
				filamentColors[s.Filament.Id] = struct {
					colorHex        string
					multiColorHexes string
				}{
					colorHex:        s.Filament.ColorHex,
					multiColorHexes: s.Filament.MultiColorHexes,
				}
				filamentInfo[s.Filament.Id] = struct {
					name   string
					vendor string
				}{
					name:   s.Filament.Name,
					vendor: s.Filament.Vendor.Name,
				}
			}
		}

		fmt.Printf("%-5s %-30s %10s %10s %10s %6s\n", "", "Filament", "Needed", "On Hand", "Status", "Loaded")
		fmt.Println(strings.Repeat("-", 78))

		allMet := true
		for _, n := range needs {
			onHand := 0.0
			status := "OK"
			loaded := ""
			if n.id != 0 {
				onHand = inventory[n.id]
				if c, ok := filamentColors[n.id]; ok {
					n.colorHex = c.colorHex
					n.multiColorHexes = c.multiColorHexes
				}
				if isLoaded[n.id] {
					loaded = "✅"
					if color.NoColor {
						loaded = "YES"
					}
				}
			} else {
				status = "UNRESOLVED"
			}

			if onHand < n.amount {
				if status == "OK" {
					status = "LOW"
				}
				allMet = false
			} else if n.id != 0 {
				// Check if projected amount is below threshold
				info := filamentInfo[n.id]
				threshold := ResolveLowThreshold(info.vendor, info.name)
				if onHand-n.amount < threshold {
					status = "WARN"
				}
			}

			displayStatus := status
			switch status {
			case "OK":
				displayStatus = color.GreenString("OK")
			case "UNRESOLVED":
				displayStatus = color.YellowString("UNRESOLVED")
			case "LOW":
				displayStatus = color.RedString("LOW")
			case "WARN":
				displayStatus = color.YellowString("WARN")
			}

			// Manually pad displayStatus to maintain right alignment
			// The original width was 10, so we need to add 10 - len(status) spaces before the colorized string
			paddingLen := 10 - len(status)
			padding := strings.Repeat(" ", paddingLen)
			displayStatus = padding + displayStatus

			colorBlock := models.GetColorBlock(n.colorHex, n.multiColorHexes)
			if colorBlock == "" {
				colorBlock = "    "
			}
			fmt.Printf("%s %-30s %10.1fg %10.1fg %s %6s\n", colorBlock, TruncateFront(n.name, 30), n.amount, onHand, displayStatus, loaded)

			if verbose {
				for _, p := range n.projects {
					fmt.Printf("    - %s (%.1fg)\n", p.projectName, p.amount)
				}
			}
		}

		if allMet {
			fmt.Println("\nAll requirements met.")
		} else {
			fmt.Println("\nSome filaments are missing or low.")
		}

		if len(zeroWarnings) > 0 {
			fmt.Println()
			warningLabel := color.YellowString("Warning:")
			warningsByProject := make(map[string][]zeroAmountWarning)
			var projectNames []string
			for _, w := range zeroWarnings {
				if _, ok := warningsByProject[w.projectName]; !ok {
					projectNames = append(projectNames, w.projectName)
				}
				warningsByProject[w.projectName] = append(warningsByProject[w.projectName], w)
			}

			for _, projName := range projectNames {
				ws := warningsByProject[projName]
				fmt.Printf("%s Project '%s' has filaments with 0 amount that may not be set up:\n", warningLabel, projName)
				for _, w := range ws {
					fmt.Printf("  - %s (Plate: %s, Plan: %s)\n", w.filament, w.plateName, w.planPath)
				}
			}
		}

		return nil
	},
}

func init() {
	planCmd.AddCommand(planCheckCmd)
	planCheckCmd.Flags().BoolP("verbose", "v", false, "Show which projects use each filament")
}
