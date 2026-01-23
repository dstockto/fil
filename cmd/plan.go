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
	Use:   "plan",
	Short: "Manage printing projects and plans",
	Long:  `Manage 3D printing projects and plans involving multiple plates and filaments.`,
}

var planListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all discovered plans and their status",
	RunE: func(cmd *cobra.Command, args []string) error {
		plans, err := discoverPlans()
		if err != nil {
			return err
		}

		if len(plans) == 0 {
			fmt.Println("No plans found.")
			return nil
		}

		for _, p := range plans {
			fmt.Printf("Plan: %s\n", p.Path)
			for _, proj := range p.Plan.Projects {
				todo := 0
				total := len(proj.Plates)
				for _, plate := range proj.Plates {
					if plate.Status != "completed" {
						todo++
					}
				}
				fmt.Printf("  Project: %s [%s] (%d/%d plates remaining)\n", proj.Name, proj.Status, todo, total)
			}
			fmt.Println()
		}
		return nil
	},
}

type DiscoveredPlan struct {
	Path string
	Plan models.PlanFile
}

func discoverPlans() ([]DiscoveredPlan, error) {
	var plans []DiscoveredPlan
	files, _ := filepath.Glob("*.yaml")
	files2, _ := filepath.Glob("*.yml")
	files = append(files, files2...)

	if Cfg != nil && Cfg.PlansDir != "" {
		globalFiles, _ := filepath.Glob(filepath.Join(Cfg.PlansDir, "*.yaml"))
		globalFiles2, _ := filepath.Glob(filepath.Join(Cfg.PlansDir, "*.yml"))
		files = append(files, globalFiles...)
		files = append(files, globalFiles2...)
	}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var plan models.PlanFile
		if err := yaml.Unmarshal(data, &plan); err != nil {
			// Skip files that aren't plan files
			continue
		}
		if len(plan.Projects) > 0 {
			plans = append(plans, DiscoveredPlan{Path: f, Plan: plan})
		}
	}
	return plans, nil
}

var planResolveCmd = &cobra.Command{
	Use:   "resolve [file]",
	Short: "Interactively link filament names to IDs in a plan file",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.ApiBase == "" {
			return fmt.Errorf("api endpoint not configured")
		}
		apiClient := api.NewClient(Cfg.ApiBase)

		var path string
		if len(args) > 0 {
			path = args[0]
		} else {
			plans, err := discoverPlans()
			if err != nil {
				return err
			}
			if len(plans) == 0 {
				return fmt.Errorf("no plans found")
			}
			if len(plans) == 1 {
				path = plans[0].Path
			} else {
				var items []string
				for _, p := range plans {
					items = append(items, p.Path)
				}
				prompt := promptui.Select{
					Label:  "Select plan file to resolve",
					Items:  items,
					Stdout: NoBellStdout,
				}
				_, result, err := prompt.Run()
				if err != nil {
					return err
				}
				path = result
			}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var plan models.PlanFile
		if err := yaml.Unmarshal(data, &plan); err != nil {
			return err
		}

		modified := false
		for i := range plan.Projects {
			for j := range plan.Projects[i].Plates {
				for k := range plan.Projects[i].Plates[j].Needs {
					need := &plan.Projects[i].Plates[j].Needs[k]
					if need.FilamentID == 0 && (need.Name != "" || need.Material != "") {
						// Search Spoolman
						query := make(map[string]string)
						if need.Material != "" {
							query["material"] = need.Material
						}
						spools, err := apiClient.FindSpoolsByName(need.Name, nil, query)
						if err != nil {
							fmt.Printf("Resolving filament for: %s %s (%s)\n", need.Name, need.Material, path)
							fmt.Printf("  Error searching Spoolman: %v\n", err)
							continue
						}

						if len(spools) == 0 {
							fmt.Printf("Resolving filament for: %s %s (%s)\n", need.Name, need.Material, path)
							fmt.Printf("  No matches found for '%s' '%s'\n", need.Name, need.Material)
							continue
						}

						// Group by filament ID to avoid picking individual spools
						type filMatch struct {
							id     int
							name   string
							mat    string
							vendor string
						}
						matches := make(map[int]filMatch)
						var matchIds []int
						for _, s := range spools {
							if _, ok := matches[s.Filament.Id]; !ok {
								matches[s.Filament.Id] = filMatch{
									id:     s.Filament.Id,
									name:   s.Filament.Name,
									mat:    s.Filament.Material,
									vendor: s.Filament.Vendor.Name,
								}
								matchIds = append(matchIds, s.Filament.Id)
							}
						}

						var selectedId int
						if len(matchIds) == 1 {
							selectedId = matchIds[0]
						} else {
							fmt.Printf("Resolving filament for: %s %s (%s)\n", need.Name, need.Material, path)
							var items []string
							for _, id := range matchIds {
								m := matches[id]
								items = append(items, fmt.Sprintf("%s - %s (%s) [#%d]", m.vendor, m.name, m.mat, id))
							}
							prompt := promptui.Select{
								Label:  "Select matching filament",
								Items:  items,
								Stdout: NoBellStdout,
							}
							idx, _, err := prompt.Run()
							if err != nil {
								return err
							}
							selectedId = matchIds[idx]
						}

						need.FilamentID = selectedId
						need.Name = matches[selectedId].name
						need.Material = matches[selectedId].mat
						modified = true
					} else if need.FilamentID != 0 && (need.Name == "" || need.Material == "") {
						// Reverse sync
						// We need a way to get filament info by ID.
						// Looking at api/client.go, there's FindSpoolsById but that returns a spool.
						// We can use that and get filament from it.
						spool, err := apiClient.FindSpoolsById(need.FilamentID) // This is a bit of a hack as it returns any spool of that filament
						if err == nil && spool != nil {
							need.Name = spool.Filament.Name
							need.Material = spool.Filament.Material
							modified = true
						}
					}
				}
			}
		}

		if modified {
			out, err := yaml.Marshal(plan)
			if err != nil {
				return err
			}
			return os.WriteFile(path, out, 0644)
		}

		fmt.Println("No changes needed.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(planCmd)
	planCmd.AddCommand(planListCmd)
	planCmd.AddCommand(planResolveCmd)
	planCmd.AddCommand(planCheckCmd)
	planCmd.AddCommand(planNextCmd)
	planCmd.AddCommand(planCompleteCmd)
	planCmd.AddCommand(planArchiveCmd)
	planCmd.AddCommand(planNewCmd)
	planCmd.AddCommand(planMoveCmd)

	planNewCmd.Flags().BoolP("move", "m", false, "Move the created plan to the central plans directory")
}

var planMoveCmd = &cobra.Command{
	Use:   "move [file]",
	Short: "Move a plan file to the central plans directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.PlansDir == "" {
			return fmt.Errorf("plans_dir not configured in config.json")
		}

		var path string
		if len(args) > 0 {
			path = args[0]
		} else {
			// Find yaml files in current directory
			files, _ := filepath.Glob("*.yaml")
			files2, _ := filepath.Glob("*.yml")
			files = append(files, files2...)

			if len(files) == 0 {
				return fmt.Errorf("no yaml files found in current directory")
			}
			if len(files) == 1 {
				path = files[0]
			} else {
				prompt := promptui.Select{
					Label:  "Select plan file to move",
					Items:  files,
					Stdout: NoBellStdout,
				}
				_, result, err := prompt.Run()
				if err != nil {
					return err
				}
				path = result
			}
		}

		// Ensure plans dir exists
		if _, err := os.Stat(Cfg.PlansDir); os.IsNotExist(err) {
			os.MkdirAll(Cfg.PlansDir, 0755)
		}

		dest := filepath.Join(Cfg.PlansDir, filepath.Base(path))
		if _, err := os.Stat(dest); err == nil {
			return fmt.Errorf("file %s already exists in central location", dest)
		}

		err := os.Rename(path, dest)
		if err != nil {
			return fmt.Errorf("failed to move file: %w", err)
		}
		fmt.Printf("Moved %s to %s\n", path, dest)
		return nil
	},
}

var planNewCmd = &cobra.Command{
	Use:   "new",
	Short: "Create a new template plan file in the current directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		projectName := filepath.Base(cwd)

		var plates []models.Plate
		files, err := os.ReadDir(cwd)
		if err == nil {
			for _, f := range files {
				if f.IsDir() {
					continue
				}
				ext := strings.ToLower(filepath.Ext(f.Name()))
				if ext == ".stl" {
					name := strings.TrimSuffix(f.Name(), filepath.Ext(f.Name()))
					filamentName := strings.Map(func(r rune) rune {
						if r >= '0' && r <= '9' {
							return -1
						}
						return r
					}, name)
					filamentName = strings.TrimSpace(filamentName)
					if filamentName == "" {
						filamentName = "Replace Me"
					}

					plates = append(plates, models.Plate{
						Name:   name,
						Status: "todo",
						Needs: []models.PlateRequirement{
							{Name: filamentName, Material: "PLA", Amount: 0},
						},
					})
				}
			}
		}

		if len(plates) == 0 {
			plates = append(plates, models.Plate{
				Name:   "Sample Plate",
				Status: "todo",
				Needs: []models.PlateRequirement{
					{Name: "black", Material: "PLA", Amount: 100},
				},
			})
		}

		plan := models.PlanFile{
			Projects: []models.Project{
				{
					Name:   projectName,
					Status: "todo",
					Plates: plates,
				},
			},
		}

		filename := projectName + ".yaml"
		// If filename already exists, try to avoid overwriting by adding a suffix or just erroring
		if _, err := os.Stat(filename); err == nil {
			return fmt.Errorf("file %s already exists", filename)
		}

		out, err := yaml.Marshal(plan)
		if err != nil {
			return err
		}

		err = os.WriteFile(filename, out, 0644)
		if err != nil {
			return err
		}

		fmt.Printf("Created new plan: %s\n", filename)

		// Check if we should move it to central location
		moveToCentral, _ := cmd.Flags().GetBool("move")
		if moveToCentral {
			if Cfg == nil || Cfg.PlansDir == "" {
				fmt.Println("Warning: plans_dir not configured, cannot move to central location.")
				return nil
			}

			// Ensure plans dir exists
			if _, err := os.Stat(Cfg.PlansDir); os.IsNotExist(err) {
				os.MkdirAll(Cfg.PlansDir, 0755)
			}

			dest := filepath.Join(Cfg.PlansDir, filename)
			if _, err := os.Stat(dest); err == nil {
				return fmt.Errorf("file %s already exists in central location", dest)
			}

			err = os.Rename(filename, dest)
			if err != nil {
				return fmt.Errorf("failed to move file: %w", err)
			}
			fmt.Printf("Moved %s to %s\n", filename, dest)
		}

		return nil
	},
}

var planArchiveCmd = &cobra.Command{
	Use:   "archive [file]",
	Short: "Move completed plan files to the archive directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.ArchiveDir == "" {
			return fmt.Errorf("archive_dir not configured in config.json")
		}

		// Ensure archive dir exists
		if _, err := os.Stat(Cfg.ArchiveDir); os.IsNotExist(err) {
			os.MkdirAll(Cfg.ArchiveDir, 0755)
		}

		var paths []string
		if len(args) > 0 {
			paths = append(paths, args[0])
		} else {
			plans, _ := discoverPlans()
			for _, p := range plans {
				paths = append(paths, p.Path)
			}
		}

		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var plan models.PlanFile
			yaml.Unmarshal(data, &plan)

			allDone := true
			for _, proj := range plan.Projects {
				if proj.Status != "completed" {
					allDone = false
					break
				}
			}

			if allDone {
				dest := filepath.Join(Cfg.ArchiveDir, filepath.Base(path))
				fmt.Printf("Archiving %s to %s\n", path, dest)
				err := os.Rename(path, dest)
				if err != nil {
					fmt.Printf("  Error moving file: %v\n", err)
				}
			} else {
				fmt.Printf("Skipping %s (not all projects are completed)\n", path)
			}
		}

		return nil
	},
}

var planCompleteCmd = &cobra.Command{
	Use:   "complete [file]",
	Short: "Mark a plate or project as completed",
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
					items = append(items, p.Path)
				}
				prompt := promptui.Select{
					Label:  "Select plan file",
					Items:  items,
					Stdout: NoBellStdout,
				}
				_, result, err := prompt.Run()
				if err != nil {
					return err
				}
				path = result
			}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var plan models.PlanFile
		yaml.Unmarshal(data, &plan)

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
			Label:  "What did you complete?",
			Items:  options,
			Size:   10,
			Stdout: NoBellStdout,
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

				// Find which spool to deduct from (must be loaded in a printer or we ask)
				// Simplified: ask for Spool ID
				fmt.Printf("Enter Spool ID to deduct from (or leave blank to skip): ")
				var spoolIdStr string
				fmt.Scanln(&spoolIdStr)
				if spoolIdStr != "" {
					var sid int
					fmt.Sscanf(spoolIdStr, "%d", &sid)
					apiClient.UseFilament(sid, used)
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
	Use:   "next [file]",
	Short: "Suggest the next plate to print and manage swaps",
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
			Label:  "Which printer are you using?",
			Items:  printerNames,
			Stdout: NoBellStdout,
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
				loadedSpools[s.Location] = s
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
							if s, ok := loadedSpools[loc]; ok && s.Filament.Id == req.FilamentID {
								foundInPrinter = true
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
			Label:  "Select plate to print",
			Items:  items,
			Size:   10,
			Stdout: NoBellStdout,
		}
		selectedIdx, _, err := promptPlate.Run()
		if err != nil {
			return err
		}
		choice := options[selectedIdx]

		// 4. Swap Instructions
		fmt.Printf("\nPreparing to print: %s - %s\n", choice.projectName, choice.plate.Name)

		for _, req := range choice.plate.Needs {
			// Is it already loaded?
			loadedLoc := ""
			for _, loc := range printerLocations {
				if s, ok := loadedSpools[loc]; ok && s.Filament.Id == req.FilamentID {
					loadedLoc = loc
					break
				}
			}

			if loadedLoc != "" {
				fmt.Printf("✓ %s is already loaded in %s\n", req.Name, loadedLoc)
				continue
			}

			// Find best spool to load
			var candidates []models.FindSpool
			for _, s := range allSpools {
				if !s.Archived && s.Filament.Id == req.FilamentID {
					candidates = append(candidates, s)
				}
			}

			// Priority: 1. Partially used, 2. Oldest (lowest ID)
			// (Simplified: just pick the best one automatically for now)
			var bestSpool *models.FindSpool
			for _, s := range candidates {
				if bestSpool == nil {
					bestSpool = &s
					continue
				}
				// If current best is unused but this one is used
				if bestSpool.UsedWeight == 0 && s.UsedWeight > 0 {
					bestSpool = &s
					continue
				}
				// If both same state, pick lowest ID
				if (bestSpool.UsedWeight > 0) == (s.UsedWeight > 0) && s.Id < bestSpool.Id {
					bestSpool = &s
				}
			}

			if bestSpool == nil {
				fmt.Printf("! Error: Could not find any spool for %s\n", req.Name)
				continue
			}

			// Find an empty slot or one to swap out
			targetLoc := ""
			for _, loc := range printerLocations {
				if _, ok := loadedSpools[loc]; !ok {
					targetLoc = loc
					break
				}
			}

			if targetLoc == "" {
				// Pick first slot of the printer for now.
				// In a real scenario, we might want to ask which slot to use.
				targetLoc = printerLocations[0]
			}

			if current, ok := loadedSpools[targetLoc]; ok {
				fmt.Printf("→ UNLOAD #%d (%s) from %s\n", current.Id, current.Filament.Name, targetLoc)
				fmt.Printf("  Where are you putting it? (Leave blank to keep in Spoolman as-is): ")
				var newLoc string
				fmt.Scanln(&newLoc)
				if newLoc != "" {
					apiClient.MoveSpool(current.Id, newLoc)
				}
			}

			fmt.Printf("→ LOAD #%d (%s) into %s (currently at %s)\n", bestSpool.Id, bestSpool.Filament.Name, targetLoc, bestSpool.Location)
			apiClient.MoveSpool(bestSpool.Id, targetLoc)
		}

		fmt.Println("\nSwaps complete. Happy printing!")
		return nil
	},
}

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
			paths = append(paths, args[0])
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

		// Aggregate needs by FilamentID
		type totalNeed struct {
			id       int
			name     string
			material string
			amount   float64
		}
		needs := make(map[int]*totalNeed)

		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var plan models.PlanFile
			yaml.Unmarshal(data, &plan)

			for _, proj := range plan.Projects {
				if proj.Status == "completed" {
					continue
				}
				for _, plate := range proj.Plates {
					if plate.Status == "completed" {
						continue
					}
					for _, req := range plate.Needs {
						if req.FilamentID == 0 {
							fmt.Printf("Warning: Plate '%s' in '%s' has unresolved filament '%s'\n", plate.Name, proj.Name, req.Name)
							continue
						}
						if _, ok := needs[req.FilamentID]; !ok {
							needs[req.FilamentID] = &totalNeed{
								id:       req.FilamentID,
								name:     req.Name,
								material: req.Material,
							}
						}
						needs[req.FilamentID].amount += req.Amount
					}
				}
			}
		}

		if len(needs) == 0 {
			fmt.Println("No pending needs found.")
			return nil
		}

		// Get all spools from Spoolman
		allSpools, err := apiClient.FindSpoolsByName("*", nil, nil)
		if err != nil {
			return err
		}

		// Inventory by Filament ID
		inventory := make(map[int]float64)
		for _, s := range allSpools {
			if !s.Archived {
				inventory[s.Filament.Id] += s.RemainingWeight
			}
		}

		fmt.Printf("%-30s %10s %10s %10s\n", "Filament", "Needed", "On Hand", "Status")
		fmt.Println(strings.Repeat("-", 65))

		allMet := true
		for _, n := range needs {
			onHand := inventory[n.id]
			status := "OK"
			if onHand < n.amount {
				status = "LOW"
				allMet = false
			}
			fmt.Printf("%-30s %10.1fg %10.1fg %10s\n", n.name, n.amount, onHand, status)
		}

		if allMet {
			fmt.Println("\nAll requirements met.")
		} else {
			fmt.Println("\nSome filaments are missing or low.")
		}

		return nil
	},
}
