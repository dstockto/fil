package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var planCmd = &cobra.Command{
	Use:     "plan",
	Aliases: []string{"p"},
	Short:   "Manage printing projects and plans",
	Long:    `Manage 3D printing projects and plans involving multiple plates and filaments.`,
}

type DiscoveredPlan struct {
	Path        string
	DisplayName string
	Plan        models.PlanFile
}

func FormatPlanPath(path string) string {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	// Check if it's in the pause directory
	if Cfg != nil && Cfg.PauseDir != "" {
		absPauseDir, err := filepath.Abs(Cfg.PauseDir)
		if err == nil {
			if strings.HasPrefix(absPath, absPauseDir) {
				rel, err := filepath.Rel(absPauseDir, absPath)
				if err == nil && !strings.HasPrefix(rel, "..") {
					return "<paused>/" + rel
				}
				return "<paused>"
			}
		}
	}

	// Check if it's in the current directory
	cwd, err := os.Getwd()
	if err == nil {
		absCwd, err := filepath.Abs(cwd)
		if err == nil {
			if strings.HasPrefix(absPath, absCwd) {
				rel, err := filepath.Rel(absCwd, absPath)
				if err == nil && !strings.HasPrefix(rel, "..") {
					return "./" + rel
				}
			}
		}
	}

	// Check if it's in the global plans directory
	if Cfg != nil && Cfg.PlansDir != "" {
		absPlansDir, err := filepath.Abs(Cfg.PlansDir)
		if err == nil {
			if strings.HasPrefix(absPath, absPlansDir) {
				rel, err := filepath.Rel(absPlansDir, absPath)
				if err == nil && !strings.HasPrefix(rel, "..") {
					return "<plans>/" + rel
				}
			}
		}
	}

	// Check if it's in the archive directory
	if Cfg != nil && Cfg.ArchiveDir != "" {
		absArchiveDir, err := filepath.Abs(Cfg.ArchiveDir)
		if err == nil {
			if strings.HasPrefix(absPath, absArchiveDir) {
				rel, err := filepath.Rel(absArchiveDir, absPath)
				if err == nil && !strings.HasPrefix(rel, "..") {
					return "<archive>/" + rel
				}
			}
		}
	}

	return absPath
}

func discoverPlans() ([]DiscoveredPlan, error) {
	return discoverPlansWithFilter(false, false)
}

func discoverPlansWithFilter(includePaused, pausedOnly bool) ([]DiscoveredPlan, error) {
	var plans []DiscoveredPlan
	fileMap := make(map[string]bool)

	// Directories to search
	var dirs []string

	// Always search CWD if not looking for only paused plans
	if !pausedOnly {
		if cwd, err := os.Getwd(); err == nil {
			dirs = append(dirs, cwd)
		} else {
			// Log warning but continue if CWD is inaccessible
			_, _ = fmt.Fprintf(os.Stderr, "Warning: failed to get current working directory: %v\n", err)
		}

		// Add global plans dir if configured
		if Cfg != nil && Cfg.PlansDir != "" {
			absPlansDir, err := filepath.Abs(Cfg.PlansDir)
			if err == nil {
				dirs = append(dirs, absPlansDir)
			} else {
				dirs = append(dirs, Cfg.PlansDir)
			}
		}
	}

	// Add pause dir if requested
	if (includePaused || pausedOnly) && Cfg != nil && Cfg.PauseDir != "" {
		absPauseDir, err := filepath.Abs(Cfg.PauseDir)
		if err == nil {
			dirs = append(dirs, absPauseDir)
		} else {
			dirs = append(dirs, Cfg.PauseDir)
		}
	}

	for _, dir := range dirs {
		// Evaluate symlinks for the root directory
		evalDir, err := filepath.EvalSymlinks(dir)
		if err == nil {
			dir = evalDir
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // skip errors for a single directory
		}

		for _, d := range entries {
			if d.IsDir() {
				continue
			}

			path := filepath.Join(dir, d.Name())
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".yaml" && ext != ".yml" {
				continue
			}

			absPath, err := filepath.Abs(path)
			if err != nil {
				absPath = path
			}
			if fileMap[absPath] {
				continue
			}
			fileMap[absPath] = true

			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var plan models.PlanFile
			if err := yaml.Unmarshal(data, &plan); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Warning: failed to parse %s: %v\n", path, err)
				continue
			}
			plan.DefaultStatus()
			if len(plan.Projects) > 0 {
				plans = append(plans, DiscoveredPlan{
					Path:        absPath,
					DisplayName: FormatPlanPath(absPath),
					Plan:        plan,
				})
			}
		}
	}
	return plans, nil
}

func UseFilamentSafely(apiClient *api.Client, spool *models.FindSpool, amount float64) error {
	if amount > spool.RemainingWeight {
		overage := amount - spool.RemainingWeight
		fmt.Printf("Warning: Spool #%d (%s) only has %.1fg remaining, but usage is %.1fg.\n", spool.Id, spool.Filament.Name, spool.RemainingWeight, amount)
		fmt.Printf("Adjusting Spool #%d initial weight by adding %.1fg to prevent negative remaining weight.\n", spool.Id, overage)

		updates := map[string]any{
			"initial_weight": spool.InitialWeight + overage,
		}
		err := apiClient.PatchSpool(spool.Id, updates)
		if err != nil {
			return fmt.Errorf("failed to adjust initial weight for spool #%d: %w", spool.Id, err)
		}
	}

	return apiClient.UseFilament(spool.Id, amount)
}

// GetNeededFilamentIDs returns a set of Filament IDs that are needed by current plans
// but are not currently loaded on a printer.
func GetNeededFilamentIDs(apiClient *api.Client) (map[int]bool, error) {
	plans, err := discoverPlans()
	if err != nil {
		return nil, err
	}

	var paths []string
	for _, p := range plans {
		paths = append(paths, p.Path)
	}

	if len(paths) == 0 {
		return make(map[int]bool), nil
	}

	neededIDs := make(map[int]bool)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var plan models.PlanFile
		if err := yaml.Unmarshal(data, &plan); err != nil {
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
					if req.FilamentID != 0 {
						neededIDs[req.FilamentID] = true
					}
				}
			}
		}
	}

	if len(neededIDs) == 0 {
		return make(map[int]bool), nil
	}

	// Get all spools from Spoolman to check what is loaded
	allSpools, err := apiClient.FindSpoolsByName("*", nil, nil)
	if err != nil {
		return nil, err
	}

	printerLocs := make(map[string]bool)
	for _, locs := range Cfg.Printers {
		for _, loc := range locs {
			printerLocs[loc] = true
		}
	}

	loadedIDs := make(map[int]bool)
	for _, s := range allSpools {
		if !s.Archived && printerLocs[s.Location] {
			loadedIDs[s.Filament.Id] = true
		}
	}

	// Result is neededIDs minus loadedIDs
	result := make(map[int]bool)
	for id := range neededIDs {
		if !loadedIDs[id] {
			result[id] = true
		}
	}

	return result, nil
}

func init() {
	rootCmd.AddCommand(planCmd)
	planCmd.AddCommand(planNextCmd)
	planCmd.AddCommand(planCompleteCmd)
}

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

var planNextCmd = &cobra.Command{
	Use:     "next [file]",
	Aliases: []string{"n"},
	Short:   "Suggest the next plate to print and manage swaps",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.ApiBase == "" {
			return fmt.Errorf("api endpoint not configured")
		}
		apiClient := api.NewClient(Cfg.ApiBase)

		// 1. Select Printer
		if len(Cfg.Printers) == 0 {
			return fmt.Errorf("no printers configured in config.json")
		}
		var printerNames []string
		for name := range Cfg.Printers {
			printerNames = append(printerNames, name)
		}
		promptPrinter := promptui.Select{
			Label:             "Which printer are you using?",
			Items:             printerNames,
			Stdout:            NoBellStdout,
			StartInSearchMode: true,
			Searcher: func(input string, index int) bool {
				return strings.Contains(strings.ToLower(printerNames[index]), strings.ToLower(input))
			},
		}
		_, printerName, err := promptPrinter.Run()
		if err != nil {
			return err
		}
		printerLocations := Cfg.Printers[printerName]

		// 2. Discover and Load Plans
		var discovered []DiscoveredPlan
		if len(args) > 0 {
			data, err := os.ReadFile(args[0])
			if err == nil {
				var p models.PlanFile
				yaml.Unmarshal(data, &p)
				discovered = append(discovered, DiscoveredPlan{Path: args[0], Plan: p})
			}
		} else {
			discovered, _ = discoverPlans()
		}

		// 3. Collect all TODO plates
		type plateOption struct {
			planPath    string
			projectIdx  int
			plateIdx    int
			plate       models.Plate
			projectName string
			swapCost    int
			isReady     bool
		}
		var options []plateOption

		// Get current inventory & loaded spools
		allSpools, _ := apiClient.FindSpoolsByName("*", nil, nil)
		loadedSpools := make(map[string]models.FindSpool)
		for _, s := range allSpools {
			if s.Location != "" {
				// Use unique key since multiple spools can be in same Location
				key := s.Location + "_" + fmt.Sprint(s.Id)
				loadedSpools[key] = s
			}
		}

		for _, dp := range discovered {
			for i, proj := range dp.Plan.Projects {
				if proj.Status == "completed" {
					continue
				}
				for j, plate := range proj.Plates {
					if plate.Status == "completed" {
						continue
					}

					// Calculate swap cost and readiness
					cost := 0
					ready := true
					for _, req := range plate.Needs {
						// Check if already loaded in any of this printer's locations
						foundInPrinter := false
						for _, loc := range printerLocations {
							// Check if the filament is loaded in this Location
							for _, s := range loadedSpools {
								if s.Location == loc && s.Filament.Id == req.FilamentID {
									foundInPrinter = true
									break
								}
							}
							if foundInPrinter {
								break
							}
						}
						if !foundInPrinter {
							cost++
						}

						// Check total inventory readiness
						totalAvailable := 0.0
						for _, s := range allSpools {
							if !s.Archived && s.Filament.Id == req.FilamentID {
								totalAvailable += s.RemainingWeight
							}
						}
						if totalAvailable < req.Amount {
							ready = false
						}
					}

					options = append(options, plateOption{
						planPath:    dp.Path,
						projectIdx:  i,
						plateIdx:    j,
						plate:       plate,
						projectName: proj.Name,
						swapCost:    cost,
						isReady:     ready,
					})
				}
			}
		}

		if len(options) == 0 {
			fmt.Println("No pending plates found.")
			return nil
		}

		// Sort: Ready first, then lower swap cost
		// (Simple recommendation logic: just highlight the best one)
		bestIdx := -1
		minCost := 999
		for i, o := range options {
			if o.isReady && o.swapCost < minCost {
				minCost = o.swapCost
				bestIdx = i
			}
		}

		var items []string
		for i, o := range options {
			prefix := "  "
			if i == bestIdx {
				prefix = "* [REC] "
			}
			readyStr := ""
			if !o.isReady {
				readyStr = " (INSUFFICIENT FILAMENT)"
			}
			items = append(items, fmt.Sprintf("%s%s - %s [Swaps: %d]%s", prefix, o.projectName, o.plate.Name, o.swapCost, readyStr))
		}

		promptPlate := promptui.Select{
			Label:             "Select plate to print",
			Items:             items,
			Size:              10,
			Stdout:            NoBellStdout,
			StartInSearchMode: true,
			Searcher: func(input string, index int) bool {
				return strings.Contains(strings.ToLower(items[index]), strings.ToLower(input))
			},
		}
		selectedIdx, _, err := promptPlate.Run()
		if err != nil {
			return err
		}
		choice := options[selectedIdx]

		// 4. Swap Instructions
		fmt.Printf("\nPreparing to print: %s - %s\n", choice.projectName, choice.plate.Name)

		// Pre-calculate what filaments are needed for the current and future plates in the project
		neededFilamentIDs := make(map[int]bool)
		for i := choice.projectIdx; i < len(discovered[0].Plan.Projects); i++ {
			proj := discovered[0].Plan.Projects[i]
			if proj.Status == "completed" {
				continue
			}
			startPlate := 0
			if i == choice.projectIdx {
				startPlate = choice.plateIdx
			}
			for j := startPlate; j < len(proj.Plates); j++ {
				plate := proj.Plates[j]
				if plate.Status == "completed" {
					continue
				}
				for _, req := range plate.Needs {
					neededFilamentIDs[req.FilamentID] = true
				}
			}
		}

		// Pre-collect all locations that are assigned to ANY printer
		allPrinterLocations := make(map[string]string) // Location -> printer name
		for pName, locs := range Cfg.Printers {
			for _, l := range locs {
				allPrinterLocations[l] = pName
			}
		}

		swapsPerformed := false
		for _, req := range choice.plate.Needs {
			// Is it already loaded?
			loadedLoc := ""
			for _, loc := range printerLocations {
				// We need to check all spools in this Location
				for _, s := range loadedSpools {
					if s.Location == loc && s.Filament.Id == req.FilamentID {
						loadedLoc = loc
						break
					}
				}
				if loadedLoc != "" {
					break
				}
			}

			var bestSpool *models.FindSpool
			if loadedLoc != "" {
				// Check if enough is remaining
				var loadedSpool models.FindSpool
				for _, s := range loadedSpools {
					if s.Location == loadedLoc && s.Filament.Id == req.FilamentID {
						loadedSpool = s
						break
					}
				}

				if loadedSpool.RemainingWeight < req.Amount {
					fmt.Printf("! WARNING: Loaded spool #%d (%s) only has %.1fg remaining, but this plate requires %.1fg\n", loadedSpool.Id, req.Name, loadedSpool.RemainingWeight, req.Amount)

					// Suggest next spool to load
					var nextBest *models.FindSpool
					for _, s := range allSpools {
						if !s.Archived && s.Filament.Id == req.FilamentID && s.Id != loadedSpool.Id {
							if nextBest == nil || s.RemainingWeight > nextBest.RemainingWeight {
								nextBest = &s
							}
						}
					}
					if nextBest != nil {
						fmt.Printf("  Suggestion: Load spool #%d (%.1fg remaining) into another slot for automatic swap.\n", nextBest.Id, nextBest.RemainingWeight)
						prompt := promptui.Prompt{
							Label:     "Do you want to load this spool now?",
							IsConfirm: true,
							Stdout:    NoBellStdout,
						}
						_, err := prompt.Run()
						if err == nil {
							// Proceed to find a slot and load it
							bestSpool = nextBest
						}
					}
				} else {
					fmt.Printf("✓ %s is already loaded in %s (%.1fg remaining)\n", req.Name, loadedLoc, loadedSpool.RemainingWeight)
				}

				if bestSpool == nil {
					continue
				}
			}

			if bestSpool == nil {
				// Find best spool to load
				var candidates []models.FindSpool
				for _, s := range allSpools {
					if !s.Archived && s.Filament.Id == req.FilamentID {
						candidates = append(candidates, s)
					}
				}

				// Priority:
				// 1. Not in any printer Location
				// 2. Partially used
				// 3. Oldest (lowest ID)
				for i := range candidates {
					s := &candidates[i]
					if bestSpool == nil {
						bestSpool = s
						continue
					}

					_, curInPrinter := allPrinterLocations[bestSpool.Location]
					_, newInPrinter := allPrinterLocations[s.Location]

					// If current best is in a printer but this one isn't, this one is better
					if curInPrinter && !newInPrinter {
						bestSpool = s
						continue
					}
					// If this one is in a printer but current best isn't, current best is still better
					if !curInPrinter && newInPrinter {
						continue
					}

					// If both same in terms of "in printer", use existing priority
					// If current best is unused but this one is used
					if bestSpool.UsedWeight == 0 && s.UsedWeight > 0 {
						bestSpool = s
						continue
					}
					// If both same state, pick lowest ID
					if (bestSpool.UsedWeight > 0) == (s.UsedWeight > 0) && s.Id < bestSpool.Id {
						bestSpool = s
					}
				}

				if bestSpool == nil {
					fmt.Printf("! Error: Could not find any spool for %s\n", req.Name)
					continue
				}

				// If the best (or only) spool is in another printer, warn the user
				if otherPName, inOtherPrinter := allPrinterLocations[bestSpool.Location]; inOtherPrinter {
					fmt.Printf("! WARNING: Spool #%d (%s) is already in %s (Printer: %s)\n", bestSpool.Id, bestSpool.Filament.Name, bestSpool.Location, otherPName)
					prompt := promptui.Prompt{
						Label:     "Do you want to move it to this printer anyway?",
						IsConfirm: true,
						Stdout:    NoBellStdout,
					}
					_, err := prompt.Run()
					if err != nil {
						fmt.Println("Skipping this swap.")
						continue
					}
				}
			}

			swapsPerformed = true
			// Find an empty slot or one to swap out
			targetLoc := ""
			minLoad := 999
			for _, loc := range printerLocations {
				loadedInLoc := 0
				for _, s := range loadedSpools {
					if s.Location == loc {
						loadedInLoc++
					}
				}
				capacity := 1
				if capInfo, ok := Cfg.LocationCapacity[loc]; ok {
					capacity = capInfo.Capacity
				}
				if loadedInLoc < capacity {
					if loadedInLoc < minLoad {
						minLoad = loadedInLoc
						targetLoc = loc
					}
				}
			}

			if targetLoc == "" {
				// All locations are full. We need to find the best Location to unload from.
				// We want a Location that has a spool NOT needed for the current project.
				// And among those, the LRU spool.
				var bestUnloadLoc string
				var bestUnloadSpool models.FindSpool
				foundNonNeeded := false

				for _, loc := range printerLocations {
					var spoolsInLoc []models.FindSpool
					for _, s := range loadedSpools {
						if s.Location == loc {
							spoolsInLoc = append(spoolsInLoc, s)
						}
					}

					for _, s := range spoolsInLoc {
						isNeeded := neededFilamentIDs[s.Filament.Id]
						// Check if it's needed for other requirements of the CURRENT plate as well
						for _, otherReq := range choice.plate.Needs {
							if s.Filament.Id == otherReq.FilamentID {
								isNeeded = true
								break
							}
						}

						if !isNeeded {
							if !foundNonNeeded {
								bestUnloadLoc = loc
								bestUnloadSpool = s
								foundNonNeeded = true
							} else {
								// LRU Logic: Older LastUsed comes first. Never-used comes last.
								li, lj := bestUnloadSpool.LastUsed, s.LastUsed
								zi, zj := li.IsZero(), lj.IsZero()

								better := false
								if zi && !zj {
									better = true // s is used, bestUnloadSpool never used; s is better candidate to unload?
									// Wait, if it's never used, maybe we should keep it?
									// find.go says: "never-used appear last" for --lru.
									// "li.Before(lj) // older last-used first"
									// "zi && !zj { return false // i has never been used; place after j }"
									// So LRU order is: [Oldest Used] ... [Newest Used] [Never Used]
									// If we want to unload the LEAST recently used, we want the one at the start of that list.
								} else if !zi && !zj {
									if lj.Before(li) {
										better = true
									}
								}

								if better {
									bestUnloadLoc = loc
									bestUnloadSpool = s
								}
							}
						} else if !foundNonNeeded {
							// If we haven't found any non-needed spool yet, keep track of the LRU needed one as fallback
							if bestUnloadLoc == "" {
								bestUnloadLoc = loc
								bestUnloadSpool = s
							} else {
								li, lj := bestUnloadSpool.LastUsed, s.LastUsed
								zi, zj := li.IsZero(), lj.IsZero()
								if (!zi && !zj && lj.Before(li)) || (zi && !zj) {
									bestUnloadLoc = loc
									bestUnloadSpool = s
								}
							}
						}
					}
				}
				targetLoc = bestUnloadLoc
			}

			// If target Location is full, we need to unload something
			var spoolToUnload *models.FindSpool
			var unloadIdx = -1
			loadedInTarget := []models.FindSpool{}
			for _, s := range loadedSpools {
				if s.Location == targetLoc {
					loadedInTarget = append(loadedInTarget, s)
				}
			}

			capacity := 1
			if capInfo, ok := Cfg.LocationCapacity[targetLoc]; ok {
				capacity = capInfo.Capacity
			}

			if len(loadedInTarget) >= capacity {
				// Choose which one in this Location to unload.
				// Same logic: prioritize non-needed, then LRU.
				var candidate models.FindSpool
				foundNonNeeded := false
				for _, s := range loadedInTarget {
					isNeeded := neededFilamentIDs[s.Filament.Id]
					for _, otherReq := range choice.plate.Needs {
						if s.Filament.Id == otherReq.FilamentID {
							isNeeded = true
							break
						}
					}

					if !isNeeded {
						if !foundNonNeeded {
							candidate = s
							foundNonNeeded = true
						} else {
							li, lj := candidate.LastUsed, s.LastUsed
							zi, zj := li.IsZero(), lj.IsZero()
							if (!zi && !zj && lj.Before(li)) || (zi && !zj) {
								candidate = s
							}
						}
					} else if !foundNonNeeded {
						if candidate.Id == 0 {
							candidate = s
						} else {
							li, lj := candidate.LastUsed, s.LastUsed
							zi, zj := li.IsZero(), lj.IsZero()
							if (!zi && !zj && lj.Before(li)) || (zi && !zj) {
								candidate = s
							}
						}
					}
				}
				spoolToUnload = &candidate

				// Find the index of the spool being unloaded in its current location
				orders, _ := LoadLocationOrders(apiClient)
				if list, ok := orders[targetLoc]; ok {
					unloadIdx = indexOf(list, spoolToUnload.Id)
				}

				fmt.Printf("→ UNLOAD #%d (%s) from %s\n", spoolToUnload.Id, spoolToUnload.Filament.Name, targetLoc)
				fmt.Printf("  Where are you putting it? (Leave blank to keep in Spoolman as-is): ")
				var input string
				fmt.Scanln(&input)
				if input != "" {
					dspec, err := ParseDestSpec(input)
					if err != nil {
						fmt.Printf("  Error parsing location: %v. Moving to %s instead.\n", err, input)
						apiClient.MoveSpool(spoolToUnload.Id, input)
					} else {
						newLoc := dspec.Location
						apiClient.MoveSpool(spoolToUnload.Id, newLoc)

						// Also update locations_spoolorders if possible
						orders, err := LoadLocationOrders(apiClient)
						if err == nil {
							orders = RemoveFromAllOrders(orders, spoolToUnload.Id)
							list := orders[newLoc]
							if dspec.hasPos {
								p := dspec.pos
								if p < 1 {
									p = 1
								}
								if p > len(list)+1 {
									p = len(list) + 1
								}
								idx := p - 1
								list = InsertAt(list, idx, spoolToUnload.Id)
							} else {
								list = append(list, spoolToUnload.Id)
							}
							orders[newLoc] = list
							apiClient.PostSettingObject("locations_spoolorders", orders)
						}
					}
				} else {
					// Even if not moving to a new shelf, it's no longer in the printer
					// We should probably explicitly clear the Location if it's being unloaded from a printer
					// but the user might just want to move it.
				}
				// Remove from our local tracking of what's loaded
				for loc, s := range loadedSpools {
					if s.Id == spoolToUnload.Id {
						delete(loadedSpools, loc)
					}
				}
			}

			fmt.Printf("→ LOAD #%d (%s) into %s (currently at %s)\n", bestSpool.Id, bestSpool.Filament.Name, targetLoc, bestSpool.Location)
			fmt.Printf("Press Enter once the swap is complete...")
			var confirm string
			fmt.Scanln(&confirm)

			apiClient.MoveSpool(bestSpool.Id, targetLoc)

			// Update locations_spoolorders for LOAD
			orders, err := LoadLocationOrders(apiClient)
			if err == nil {
				orders = RemoveFromAllOrders(orders, bestSpool.Id)
				list := orders[targetLoc]
				if unloadIdx != -1 {
					list = InsertAt(list, unloadIdx, bestSpool.Id)
				} else {
					list = append(list, bestSpool.Id)
				}
				orders[targetLoc] = list
				apiClient.PostSettingObject("locations_spoolorders", orders)
			}

			// Update our local tracking
			bestSpool.Location = targetLoc
			loadedSpools[targetLoc+"_"+fmt.Sprint(bestSpool.Id)] = *bestSpool
		}

		if swapsPerformed {
			fmt.Println("\nSwaps complete. Happy printing!")
		} else {
			fmt.Println("\nEverything ready. Happy printing!")
		}
		return nil
	},
}
