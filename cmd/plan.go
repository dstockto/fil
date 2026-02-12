package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
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
}
