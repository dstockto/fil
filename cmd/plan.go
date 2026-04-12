package cmd

import (
	"context"
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
	Path        string // filesystem path (empty for remote)
	RemoteName  string // filename on server (empty for local)
	DisplayName string
	Plan        models.PlanFile
	Remote      bool
}

// FormatDiscoveredPlan returns a display name for a DiscoveredPlan,
// handling both local and remote plans.
func FormatDiscoveredPlan(dp DiscoveredPlan) string {
	if dp.Remote {
		return "<server>/" + dp.RemoteName
	}
	return FormatPlanPath(dp.Path)
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
	// Track filenames (without path) so remote dedup works against local plans
	localNames := make(map[string]bool)

	// Directories to search
	var dirs []string

	if !pausedOnly {
		// Search plans_dir if configured
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
			localNames[d.Name()] = true

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

	// Fetch remote plans from plan server if configured
	if Cfg != nil && Cfg.PlansServer != "" {
		client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
		ctx := context.Background()

		var statuses []string
		if !pausedOnly {
			statuses = append(statuses, "")
		}
		if includePaused || pausedOnly {
			statuses = append(statuses, "paused")
		}

		for _, status := range statuses {
			summaries, err := client.ListPlans(ctx, status)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Warning: could not reach plan server: %v\n", err)
				continue
			}

			for _, summary := range summaries {
				// Skip if we already have a local plan with the same filename
				if localNames[summary.Name] {
					continue
				}

				data, err := client.GetPlan(ctx, summary.Name, status)
				if err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "Warning: failed to fetch remote plan %s: %v\n", summary.Name, err)
					continue
				}

				var plan models.PlanFile
				if err := yaml.Unmarshal(data, &plan); err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "Warning: failed to parse remote plan %s: %v\n", summary.Name, err)
					continue
				}
				plan.DefaultStatus()

				if len(plan.Projects) > 0 {
					fileMap[summary.Name] = true
					plans = append(plans, DiscoveredPlan{
						RemoteName:  summary.Name,
						DisplayName: "<server>/" + summary.Name,
						Plan:        plan,
						Remote:      true,
					})
				}
			}
		}
	}

	return plans, nil
}

// loadPlanYAML unmarshals YAML data into a PlanFile and sets defaults.
func loadPlanYAML(data []byte, plan *models.PlanFile) error {
	if err := yaml.Unmarshal(data, plan); err != nil {
		return err
	}
	plan.DefaultStatus()
	return nil
}

// savePlan writes the plan back to its source — either local file or remote server.
func savePlan(dp DiscoveredPlan, plan models.PlanFile) error {
	// Best-effort backfill of missing colors before saving.
	if Cfg != nil && Cfg.ApiBase != "" {
		apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
		if spools, err := apiClient.FindSpoolsByName(context.Background(), "*", nil, nil); err == nil {
			backfillPlanColors(&plan, spools)
		}
	}

	out, err := yaml.Marshal(plan)
	if err != nil {
		return err
	}
	if dp.Remote {
		client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
		return client.PutPlan(context.Background(), dp.RemoteName, out)
	}
	return os.WriteFile(dp.Path, out, 0644)
}

// backfillPlanColors fills in missing Color fields on PlateRequirements
// by looking up the FilamentID in the provided spool list and extracting color_hex.
// Returns true if any colors were added.
func backfillPlanColors(plan *models.PlanFile, spools []models.FindSpool) bool {
	// Build filament ID → color hex lookup
	colorByFilament := make(map[int]string)
	for _, s := range spools {
		if s.Filament.Id != 0 && s.Filament.ColorHex != "" {
			colorByFilament[s.Filament.Id] = s.Filament.ColorHex
		}
	}

	changed := false
	for i := range plan.Projects {
		for j := range plan.Projects[i].Plates {
			for k := range plan.Projects[i].Plates[j].Needs {
				need := &plan.Projects[i].Plates[j].Needs[k]
				if need.Color == "" && need.FilamentID != 0 {
					if hex, ok := colorByFilament[need.FilamentID]; ok {
						need.Color = hex
						changed = true
					}
				}
			}
		}
	}
	return changed
}

// selectPlan prompts the user to select a plan from a list of discovered plans.
// Returns the selected DiscoveredPlan. If only one plan exists, it is returned directly.
func selectPlan(label string, plans []DiscoveredPlan) (*DiscoveredPlan, error) {
	if len(plans) == 0 {
		return nil, fmt.Errorf("no plans found")
	}
	if len(plans) == 1 {
		return &plans[0], nil
	}

	var items []string
	for _, p := range plans {
		items = append(items, p.DisplayName)
	}

	prompt := promptui.Select{
		Label:             label,
		Items:             items,
		Stdout:            NoBellStdout,
		StartInSearchMode: true,
		Searcher: func(input string, index int) bool {
			return strings.Contains(strings.ToLower(items[index]), strings.ToLower(input))
		},
	}
	selectedIdx, _, err := prompt.Run()
	if err != nil {
		return nil, err
	}
	return &plans[selectedIdx], nil
}

func UseFilamentSafely(ctx context.Context, apiClient *api.Client, spool *models.FindSpool, amount float64) error {
	if amount > spool.RemainingWeight {
		overage := amount - spool.RemainingWeight
		fmt.Printf("Warning: Spool #%d (%s) only has %.1fg remaining, but usage is %.1fg.\n", spool.Id, models.Sanitize(spool.Filament.Name), spool.RemainingWeight, amount)
		fmt.Printf("Adjusting Spool #%d initial weight by adding %.1fg to prevent negative remaining weight.\n", spool.Id, overage)

		updates := map[string]any{
			"initial_weight": spool.InitialWeight + overage,
		}
		err := apiClient.PatchSpool(ctx, spool.Id, updates)
		if err != nil {
			return fmt.Errorf("failed to adjust initial weight for spool #%d: %w", spool.Id, err)
		}
	}

	return apiClient.UseFilament(ctx, spool.Id, amount)
}

// GetNeededFilamentIDs returns a set of Filament IDs that are needed by current plans
// but are not currently loaded on a printer.
func GetNeededFilamentIDs(ctx context.Context, apiClient *api.Client) (map[int]bool, error) {
	plans, err := discoverPlans()
	if err != nil {
		return nil, err
	}

	if len(plans) == 0 {
		return make(map[int]bool), nil
	}

	neededIDs := make(map[int]bool)
	for _, dp := range plans {
		for _, proj := range dp.Plan.Projects {
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
	allSpools, err := apiClient.FindSpoolsByName(ctx, "*", onlyStandardFilament, nil)
	if err != nil {
		return nil, err
	}

	printerLocs := make(map[string]bool)
	for _, pCfg := range Cfg.Printers {
		for _, loc := range pCfg.Locations {
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
}
