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
		byProject, _ := cmd.Flags().GetBool("by-project")

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

		// Pre-compute display info for each filament need
		type filamentDisplay struct {
			colorBlock    string
			displayStatus string
			onHand        float64
			loaded        string
			status        string
		}
		displayInfo := make(map[string]*filamentDisplay)

		allMet := true
		for key, n := range needs {
			d := &filamentDisplay{}
			if n.id != 0 {
				d.onHand = inventory[n.id]
				if c, ok := filamentColors[n.id]; ok {
					n.colorHex = c.colorHex
					n.multiColorHexes = c.multiColorHexes
				}
				if isLoaded[n.id] {
					d.loaded = "✅"
					if color.NoColor {
						d.loaded = "YES"
					}
				}
				d.status = "OK"
			} else {
				d.status = "UNRESOLVED"
			}

			if d.onHand < n.amount {
				if d.status == "OK" {
					d.status = "LOW"
				}
				allMet = false
			} else if n.id != 0 {
				// Check if projected amount is below threshold
				info := filamentInfo[n.id]
				threshold := ResolveLowThreshold(info.vendor, info.name)
				if d.onHand-n.amount < threshold {
					d.status = "WARN"
				}
			}

			switch d.status {
			case "OK":
				d.displayStatus = color.GreenString("OK")
			case "UNRESOLVED":
				d.displayStatus = color.YellowString("UNRESOLVED")
			case "LOW":
				d.displayStatus = color.RedString("LOW")
			case "WARN":
				d.displayStatus = color.YellowString("WARN")
			}

			// Manually pad displayStatus to maintain right alignment
			paddingLen := 10 - len(d.status)
			d.displayStatus = strings.Repeat(" ", paddingLen) + d.displayStatus

			d.colorBlock = models.GetColorBlock(n.colorHex, n.multiColorHexes)
			if d.colorBlock == "" {
				d.colorBlock = "    "
			}
			displayInfo[key] = d
		}

		fmt.Printf("%-5s %-30s %10s %10s %10s %6s\n", "", "Filament", "Needed", "On Hand", "Status", "Loaded")
		fmt.Println(strings.Repeat("-", 78))

		if !byProject {
			for key, n := range needs {
				d := displayInfo[key]
				fmt.Printf("%s %-30s %10.1fg %10.1fg %s %6s\n", d.colorBlock, TruncateFront(n.name, 30), n.amount, d.onHand, d.displayStatus, d.loaded)

				if verbose {
					for _, p := range n.projects {
						fmt.Printf("    - %s (%.1fg)\n", p.projectName, p.amount)
					}
				}
			}
		} else {
			// Build project -> filament needs index
			type projectFilamentEntry struct {
				key    string
				need   *totalNeed
				amount float64
			}
			projectNeeds := make(map[string][]projectFilamentEntry)
			var projectOrder []string

			for key, n := range needs {
				for _, p := range n.projects {
					if _, ok := projectNeeds[p.projectName]; !ok {
						projectOrder = append(projectOrder, p.projectName)
					}
					projectNeeds[p.projectName] = append(projectNeeds[p.projectName], projectFilamentEntry{
						key:    key,
						need:   n,
						amount: p.amount,
					})
				}
			}

			for _, projName := range projectOrder {
				fmt.Printf("\nProject: %s\n", projName)
				for _, entry := range projectNeeds[projName] {
					d := displayInfo[entry.key]
					fmt.Printf("%s %-30s %10.1fg %10.1fg %s %6s\n", d.colorBlock, TruncateFront(entry.need.name, 30), entry.amount, d.onHand, d.displayStatus, d.loaded)
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
	planCheckCmd.Flags().Bool("by-project", false, "Group output by project instead of by filament")
}
