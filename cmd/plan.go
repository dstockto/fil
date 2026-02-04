package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/fatih/color"
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

var planDeleteCmd = &cobra.Command{
	Use:     "delete",
	Aliases: []string{"del"},
	Short:   "Delete an active plan",
	RunE: func(cmd *cobra.Command, args []string) error {
		var path string
		var displayName string
		if len(args) > 0 {
			path = args[0]
			displayName = FormatPlanPath(path)
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
				displayName = plans[0].DisplayName
			} else {
				var items []string
				for _, p := range plans {
					items = append(items, p.DisplayName)
				}
				prompt := promptui.Select{
					Label:             "Select plan to delete",
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
				displayName = plans[selectedIdx].DisplayName
			}
		}

		confirmPrompt := promptui.Prompt{
			Label:     fmt.Sprintf("Are you sure you want to delete plan %s", displayName),
			IsConfirm: true,
			Stdout:    NoBellStdout,
		}

		_, err := confirmPrompt.Run()
		if err != nil {
			if errors.Is(err, promptui.ErrAbort) {
				fmt.Println("Deletion aborted.")
				return nil
			}
			return err
		}

		err = os.Remove(path)
		if err != nil {
			return fmt.Errorf("failed to delete plan: %w", err)
		}

		fmt.Printf("Plan %s deleted successfully.\n", displayName)
		return nil
	},
}

var planListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all discovered plans and their status",
	RunE: func(cmd *cobra.Command, args []string) error {
		paused, _ := cmd.Flags().GetBool("paused")
		all, _ := cmd.Flags().GetBool("all")

		plans, err := discoverPlansWithFilter(all, paused)
		if err != nil {
			return err
		}

		if len(plans) == 0 {
			fmt.Println("No plans found.")
			return nil
		}

		for _, p := range plans {
			fmt.Printf("Plan: %s\n", p.DisplayName)
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

var planEditCmd = &cobra.Command{
	Use:     "edit",
	Aliases: []string{"ed", "e"},
	Short:   "Edit an active plan file",
	RunE: func(cmd *cobra.Command, args []string) error {
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
					items = append(items, p.DisplayName)
				}
				prompt := promptui.Select{
					Label:             "Select plan file to edit",
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

		editor := os.Getenv("VISUAL")
		if editor == "" {
			editor = os.Getenv("EDITOR")
		}
		if editor == "" {
			// Fallback to common editors
			for _, e := range []string{"vim", "vi", "nano"} {
				if _, err := os.Stat("/usr/bin/" + e); err == nil {
					editor = e
					break
				}
				if _, err := os.Stat("/usr/local/bin/" + e); err == nil {
					editor = e
					break
				}
			}
		}
		if editor == "" {
			return fmt.Errorf("no editor found. Please set $VISUAL or $EDITOR environment variable")
		}

		// Handle editor with arguments (e.g. "code --wait")
		parts := strings.Fields(editor)
		editorCmd := parts[0]
		editorArgs := append(parts[1:], path)

		c := exec.Command(editorCmd, editorArgs...)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr

		return c.Run()
	},
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

var planResolveCmd = &cobra.Command{
	Use:     "resolve [file]",
	Aliases: []string{"r", "link"},
	Short:   "Interactively link filament names to IDs in a plan file",
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
					items = append(items, p.DisplayName)
				}
				prompt := promptui.Select{
					Label:             "Select plan file to resolve",
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
							fmt.Printf("  No matches found for '%s' '%s'. Choosing from full list...\n", need.Name, need.Material)
							spools, err = apiClient.FindSpoolsByName("*", nil, query)
							if err != nil {
								fmt.Printf("  Error fetching all filaments: %v\n", err)
								continue
							}
							if len(spools) == 0 {
								fmt.Printf("  Still no matches found in full list with type filtering.\n")
								continue
							}
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
						if len(matchIds) == 1 && need.Name != "" {
							// If we found exactly one match by name, use it.
							// But if we are in the "full list" fallback, we should probably still ask if need.Name was empty.
							// Actually, if it was found by FindSpoolsByName(need.Name), and it's unique, it's safe.
							selectedId = matchIds[0]
						} else {
							fmt.Printf("Resolving filament for: %s %s (%s)\n", need.Name, need.Material, path)
							var items []string
							for _, id := range matchIds {
								m := matches[id]
								items = append(items, fmt.Sprintf("%s - %s (%s) [#%d]", m.vendor, m.name, m.mat, id))
							}
							prompt := promptui.Select{
								Label:             "Select matching filament",
								Items:             items,
								Stdout:            NoBellStdout,
								Size:              10,
								StartInSearchMode: true,
								Searcher: func(input string, index int) bool {
									m := matches[matchIds[index]]
									needle := strings.ToLower(strings.TrimSpace(input))
									if needle == "" {
										return true
									}
									fields := []string{
										fmt.Sprintf("%d", m.id),
										m.name,
										m.mat,
										m.vendor,
									}
									joined := strings.ToLower(strings.Join(fields, " "))
									return strings.Contains(joined, needle)
								},
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
						// Spoolman API has /api/v1/filament/{id}
						filament, err := apiClient.GetFilamentById(need.FilamentID)
						if err == nil && filament != nil {
							need.Name = filament.Filament.Name
							need.Material = filament.Filament.Material
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
	planCmd.AddCommand(planListCmd)
	planCmd.AddCommand(planResolveCmd)
	planCmd.AddCommand(planCheckCmd)
	planCmd.AddCommand(planNextCmd)
	planCmd.AddCommand(planEditCmd)
	planCmd.AddCommand(planCompleteCmd)
	planCmd.AddCommand(planArchiveCmd)
	planCmd.AddCommand(planPauseCmd)
	planCmd.AddCommand(planResumeCmd)
	planCmd.AddCommand(planNewCmd)
	planCmd.AddCommand(planMoveCmd)
	planCmd.AddCommand(planMoveBackCmd)
	planCmd.AddCommand(planReprintCmd)
	planCmd.AddCommand(planDeleteCmd)

	planListCmd.Flags().BoolP("paused", "p", false, "Show only paused plans")
	planListCmd.Flags().BoolP("all", "a", false, "Show all plans, including paused ones")
	planReprintCmd.Flags().IntP("number", "n", 1, "Number of reprints")
	planNewCmd.Flags().BoolP("move", "m", false, "Move the created plan to the central plans directory")
	planCheckCmd.Flags().BoolP("verbose", "v", false, "Show which projects use each filament")
}

var planReprintCmd = &cobra.Command{
	Use:     "reprint",
	Aliases: []string{"rp"},
	Short:   "Reprint an archived project",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.ArchiveDir == "" || Cfg.PlansDir == "" {
			return fmt.Errorf("archive_dir and plans_dir must be configured in config.json")
		}

		// Ensure archive dir exists
		if _, err := os.Stat(Cfg.ArchiveDir); os.IsNotExist(err) {
			return fmt.Errorf("archive directory %s does not exist", FormatPlanPath(Cfg.ArchiveDir))
		}

		// Find yaml files in archive directory
		files, _ := filepath.Glob(filepath.Join(Cfg.ArchiveDir, "*.yaml"))
		files2, _ := filepath.Glob(filepath.Join(Cfg.ArchiveDir, "*.yml"))
		files = append(files, files2...)

		if len(files) == 0 {
			return fmt.Errorf("no archived plans found in %s", FormatPlanPath(Cfg.ArchiveDir))
		}

		var selectedPath string
		if len(files) == 1 {
			selectedPath = files[0]
		} else {
			var displayNames []string
			for _, f := range files {
				displayNames = append(displayNames, FormatPlanPath(f))
			}
			prompt := promptui.Select{
				Label:             "Select archived plan to reprint",
				Items:             displayNames,
				Stdout:            NoBellStdout,
				StartInSearchMode: true,
				Searcher: func(input string, index int) bool {
					name := strings.ToLower(displayNames[index])
					input = strings.ToLower(input)

					return strings.Contains(name, input)
				},
			}
			idx, _, err := prompt.Run()
			if err != nil {
				return err
			}
			selectedPath = files[idx]
		}

		// Read the plan
		data, err := os.ReadFile(selectedPath)
		if err != nil {
			return fmt.Errorf("failed to read archived plan: %w", err)
		}

		var plan models.PlanFile
		if err := yaml.Unmarshal(data, &plan); err != nil {
			return fmt.Errorf("failed to unmarshal plan: %w", err)
		}

		num, _ := cmd.Flags().GetInt("number")
		if num < 1 {
			num = 1
		}

		// Reset all plates and projects to todo
		for i := range plan.Projects {
			plan.Projects[i].Status = "todo"
			for j := range plan.Projects[i].Plates {
				plan.Projects[i].Plates[j].Status = "todo"
			}

			if num > 1 {
				originalPlates := plan.Projects[i].Plates
				for n := 1; n < num; n++ {
					plan.Projects[i].Plates = append(plan.Projects[i].Plates, originalPlates...)
				}
			}
		}

		// Determine new filename
		ext := filepath.Ext(selectedPath)
		base := strings.TrimSuffix(filepath.Base(selectedPath), ext)

		// Remove timestamp suffix if present (Format: 20060102150405, length 14)
		// Usually appended as -YYYYMMDDHHMMSS
		if len(base) >= 15 && base[len(base)-15] == '-' {
			timestampPart := base[len(base)-14:]
			// Check if it's all digits
			isDigits := true
			for _, r := range timestampPart {
				if r < '0' || r > '9' {
					isDigits = false
					break
				}
			}
			if isDigits {
				base = base[:len(base)-15]
			}
		}

		newFilename := base + ext
		destPath := filepath.Join(Cfg.PlansDir, newFilename)

		// Check if destination already exists and find a unique name
		counter := 1
		for {
			if _, err := os.Stat(destPath); os.IsNotExist(err) {
				break
			}
			destPath = filepath.Join(Cfg.PlansDir, fmt.Sprintf("%s-%d%s", base, counter, ext))
			counter++
		}

		// Save the reset plan to the new location
		updatedData, err := yaml.Marshal(plan)
		if err != nil {
			return fmt.Errorf("failed to marshal plan: %w", err)
		}

		if err := os.WriteFile(destPath, updatedData, 0644); err != nil {
			return fmt.Errorf("failed to write plan file: %w", err)
		}

		fmt.Printf("Successfully reprinted plan to %s\n", FormatPlanPath(destPath))
		return nil
	},
}

var planMoveBackCmd = &cobra.Command{
	Use:     "move-back",
	Aliases: []string{"mb"},
	Short:   "Move a plan file back to its original location",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.PlansDir == "" {
			return fmt.Errorf("plans_dir not configured in config.json")
		}

		// Find yaml files in plans directory
		files, _ := filepath.Glob(filepath.Join(Cfg.PlansDir, "*.yaml"))
		files2, _ := filepath.Glob(filepath.Join(Cfg.PlansDir, "*.yml"))
		files = append(files, files2...)

		if len(files) == 0 {
			return fmt.Errorf("no yaml files found in central plans directory")
		}

		var path string
		if len(files) == 1 {
			path = files[0]
		} else {
			prompt := promptui.Select{
				Label:             "Select plan file to move back",
				Items:             files,
				Stdout:            NoBellStdout,
				StartInSearchMode: true,
				Searcher: func(input string, index int) bool {
					return strings.Contains(strings.ToLower(files[index]), strings.ToLower(input))
				},
			}
			_, result, err := prompt.Run()
			if err != nil {
				return err
			}
			path = result
		}

		// Read the plan to find the original location
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read plan file: %w", err)
		}

		var plan models.PlanFile
		if err := yaml.Unmarshal(data, &plan); err != nil {
			return fmt.Errorf("failed to unmarshal plan: %w", err)
		}

		if plan.OriginalLocation == "" {
			return fmt.Errorf("plan file does not have an original location recorded")
		}

		// Ensure the directory for the original location exists
		destDir := filepath.Dir(plan.OriginalLocation)
		if _, err := os.Stat(destDir); os.IsNotExist(err) {
			if err := os.MkdirAll(destDir, 0755); err != nil {
				return fmt.Errorf("failed to create destination directory: %w", err)
			}
		}

		if _, err := os.Stat(plan.OriginalLocation); err == nil {
			return fmt.Errorf("file %s already exists at original location", plan.OriginalLocation)
		}

		// Clear OriginalLocation before moving back
		originalDest := plan.OriginalLocation
		plan.OriginalLocation = ""
		updatedData, err := yaml.Marshal(plan)
		if err != nil {
			return fmt.Errorf("failed to marshal plan: %w", err)
		}
		if err := os.WriteFile(path, updatedData, 0644); err != nil {
			return fmt.Errorf("failed to update plan file: %w", err)
		}

		err = os.Rename(path, originalDest)
		if err != nil {
			return fmt.Errorf("failed to move file back: %w", err)
		}
		fmt.Printf("Moved %s back to %s\n", path, originalDest)
		return nil
	},
}

var planMoveCmd = &cobra.Command{
	Use:     "move [file]",
	Aliases: []string{"mv", "m"},
	Short:   "Move a plan file to the central plans directory",
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
					Label:             "Select plan file to move",
					Items:             files,
					Stdout:            NoBellStdout,
					StartInSearchMode: true,
					Searcher: func(input string, index int) bool {
						return strings.Contains(strings.ToLower(files[index]), strings.ToLower(input))
					},
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
			_ = os.MkdirAll(Cfg.PlansDir, 0755)
		}

		// Load the plan to update OriginalLocation
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read plan file: %w", err)
		}

		var plan models.PlanFile
		if err := yaml.Unmarshal(data, &plan); err != nil {
			return fmt.Errorf("failed to unmarshal plan: %w", err)
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("failed to get absolute path: %w", err)
		}
		plan.OriginalLocation = absPath

		updatedData, err := yaml.Marshal(plan)
		if err != nil {
			return fmt.Errorf("failed to marshal plan: %w", err)
		}
		if err := os.WriteFile(path, updatedData, 0644); err != nil {
			return fmt.Errorf("failed to update plan file with original location: %w", err)
		}

		dest := filepath.Join(Cfg.PlansDir, filepath.Base(path))
		if _, err := os.Stat(dest); err == nil {
			return fmt.Errorf("file %s already exists in central Location", dest)
		}

		err = os.Rename(path, dest)
		if err != nil {
			return fmt.Errorf("failed to move file: %w", err)
		}
		fmt.Printf("Moved %s to %s\n", path, dest)
		return nil
	},
}

var planNewCmd = &cobra.Command{
	Use:     "new [filename]",
	Aliases: []string{"n"},
	Short:   "Create a new template plan file in the current directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current working directory (it may have been deleted): %w", err)
		}
		projectName := filepath.Base(cwd)

		var filename string
		if len(args) > 0 {
			filename = args[0]
			if !strings.HasSuffix(filename, ".yaml") && !strings.HasSuffix(filename, ".yml") {
				filename += ".yaml"
			}
			projectName = ToProjectName(strings.TrimSuffix(strings.TrimSuffix(filename, ".yaml"), ".yml"))
		} else {
			filename = projectName + ".yaml"
			projectName = ToProjectName(projectName)
		}

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

		fmt.Printf("Created new plan: %s\n", FormatPlanPath(filename))

		// Check if we should move it to central Location
		moveToCentral, _ := cmd.Flags().GetBool("move")
		if moveToCentral {
			if Cfg == nil || Cfg.PlansDir == "" {
				fmt.Println("Warning: plans_dir not configured, cannot move to central Location.")
				return nil
			}

			// Ensure plans dir exists
			if _, err := os.Stat(Cfg.PlansDir); os.IsNotExist(err) {
				_ = os.MkdirAll(Cfg.PlansDir, 0755)
			}

			// Load the plan to update OriginalLocation
			absPath, err := filepath.Abs(filename)
			if err != nil {
				return fmt.Errorf("failed to get absolute path: %w", err)
			}
			plan.OriginalLocation = absPath

			updatedData, err := yaml.Marshal(plan)
			if err != nil {
				return fmt.Errorf("failed to marshal plan: %w", err)
			}
			if err := os.WriteFile(filename, updatedData, 0644); err != nil {
				return fmt.Errorf("failed to update plan file with original location: %w", err)
			}

			dest := filepath.Join(Cfg.PlansDir, filename)
			if _, err := os.Stat(dest); err == nil {
				return fmt.Errorf("file %s already exists in central Location", dest)
			}

			err = os.Rename(filename, dest)
			if err != nil {
				return fmt.Errorf("failed to move file: %w", err)
			}
			fmt.Printf("Moved %s to %s\n", FormatPlanPath(filename), FormatPlanPath(dest))
		}

		return nil
	},
}

var planPauseCmd = &cobra.Command{
	Use:     "pause [file]",
	Aliases: []string{"p"},
	Short:   "Move a plan file to the pause directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.PauseDir == "" {
			return fmt.Errorf("pause_dir not configured in config.json")
		}

		// Ensure pause dir exists
		if _, err := os.Stat(Cfg.PauseDir); os.IsNotExist(err) {
			_ = os.MkdirAll(Cfg.PauseDir, 0755)
		}

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
					Label:             "Select plan file to pause",
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

		dest := filepath.Join(Cfg.PauseDir, filepath.Base(path))
		err := os.Rename(path, dest)
		if err != nil {
			return fmt.Errorf("failed to move file: %w", err)
		}

		fmt.Printf("Moved %s to %s\n", FormatPlanPath(path), FormatPlanPath(dest))
		return nil
	},
}

var planResumeCmd = &cobra.Command{
	Use:     "resume [file]",
	Aliases: []string{"res"},
	Short:   "Move a plan file from the pause directory back to the active plans directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.PauseDir == "" || Cfg.PlansDir == "" {
			return fmt.Errorf("pause_dir and plans_dir must be configured in config.json")
		}

		var path string
		if len(args) > 0 {
			path = args[0]
		} else {
			plans, err := discoverPlansWithFilter(false, true)
			if err != nil {
				return err
			}
			if len(plans) == 0 {
				return fmt.Errorf("no paused plans found")
			}
			if len(plans) == 1 {
				path = plans[0].Path
			} else {
				var items []string
				for _, p := range plans {
					items = append(items, p.DisplayName)
				}
				prompt := promptui.Select{
					Label:             "Select plan file to resume",
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

		dest := filepath.Join(Cfg.PlansDir, filepath.Base(path))
		err := os.Rename(path, dest)
		if err != nil {
			return fmt.Errorf("failed to move file: %w", err)
		}

		fmt.Printf("Moved %s to %s\n", FormatPlanPath(path), FormatPlanPath(dest))
		return nil
	},
}

var planArchiveCmd = &cobra.Command{
	Use:     "archive [file]",
	Aliases: []string{"a"},
	Short:   "Move completed plan files to the archive directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.ArchiveDir == "" {
			return fmt.Errorf("archive_dir not configured in config.json")
		}

		// Ensure archive dir exists
		if _, err := os.Stat(Cfg.ArchiveDir); os.IsNotExist(err) {
			_ = os.MkdirAll(Cfg.ArchiveDir, 0755)
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
			_ = yaml.Unmarshal(data, &plan)

			allDone := true
			for _, proj := range plan.Projects {
				if proj.Status != "completed" {
					allDone = false
					break
				}
			}

			if allDone {
				ext := filepath.Ext(path)
				base := strings.TrimSuffix(filepath.Base(path), ext)
				timestamp := time.Now().Format("20060102150405")
				newFilename := fmt.Sprintf("%s-%s%s", base, timestamp, ext)

				dest := filepath.Join(Cfg.ArchiveDir, newFilename)
				fmt.Printf("Archiving %s to %s\n", FormatPlanPath(path), FormatPlanPath(dest))
				err := os.Rename(path, dest)
				if err != nil {
					fmt.Printf("  Error moving file: %v\n", err)
				}
			} else {
				fmt.Printf("Skipping %s (not all projects are completed)\n", FormatPlanPath(path))
			}
		}

		return nil
	},
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
							fmt.Printf("Using spool #%d (%s) from %s (%.1fg remaining)\n", matchedSpool.Id, matchedSpool.Filament.Name, matchedSpool.Location, matchedSpool.RemainingWeight)
						} else if len(candidates) > 1 {
							var items []string
							for _, c := range candidates {
								items = append(items, fmt.Sprintf("#%d: %s (%s) - %.1fg remaining", c.Id, c.Filament.Name, c.Location, c.RemainingWeight))
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
				if color, ok := filamentColors[n.id]; ok {
					n.colorHex = color.colorHex
					n.multiColorHexes = color.multiColorHexes
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
