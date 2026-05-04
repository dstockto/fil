package plan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dstockto/fil/models"
	"gopkg.in/yaml.v3"
)

// FilePlanStore reads and writes Plan YAML files in the configured plans dir,
// and moves them between plans dir and pause dir for Pause/Resume. Used by
// both the CLI in Local Mode and by the plan-server's LocalPlanOps. The CLI's
// CWD-arg path is not supported through this store — verbs that mutate or
// move plans operate strictly on plans within the configured directories.
type FilePlanStore struct {
	PlansDir string
	PauseDir string
}

// NewFilePlanStore returns a store rooted at plansDir. pauseDir may be empty
// in setups that don't use pause/resume; calling Pause/Resume on such a
// store returns an error.
func NewFilePlanStore(plansDir, pauseDir string) *FilePlanStore {
	return &FilePlanStore{PlansDir: plansDir, PauseDir: pauseDir}
}

// Load reads <plansDir>/<name> as YAML and applies status defaults. The name
// must be a basename (no path separators); callers pass the same value the
// plan-server would use.
func (s *FilePlanStore) Load(_ context.Context, name string) (models.PlanFile, error) {
	if err := validateName(name); err != nil {
		return models.PlanFile{}, err
	}
	path := filepath.Join(s.PlansDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return models.PlanFile{}, fmt.Errorf("read plan %s: %w", name, err)
	}
	var plan models.PlanFile
	if err := yaml.Unmarshal(data, &plan); err != nil {
		return models.PlanFile{}, fmt.Errorf("parse plan %s: %w", name, err)
	}
	plan.DefaultStatus()
	return plan, nil
}

// Save writes the marshaled plan to <plansDir>/<name>, replacing any existing
// file. Atomic-rename is intentionally not used — Spoolman writes happen
// after a successful save, and the plans dir is single-writer in practice.
func (s *FilePlanStore) Save(_ context.Context, name string, plan models.PlanFile) error {
	if err := validateName(name); err != nil {
		return err
	}
	out, err := yaml.Marshal(plan)
	if err != nil {
		return fmt.Errorf("marshal plan %s: %w", name, err)
	}
	path := filepath.Join(s.PlansDir, name)
	if err := os.WriteFile(path, out, 0644); err != nil {
		return fmt.Errorf("write plan %s: %w", name, err)
	}
	return nil
}

// Pause moves <plansDir>/<name> to <pauseDir>/<name>. Creates pauseDir if
// missing — the user may have configured the path without precreating it.
func (s *FilePlanStore) Pause(_ context.Context, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if s.PauseDir == "" {
		return fmt.Errorf("pause_dir not configured")
	}
	if err := os.MkdirAll(s.PauseDir, 0755); err != nil {
		return fmt.Errorf("create pause dir: %w", err)
	}
	src := filepath.Join(s.PlansDir, name)
	dst := filepath.Join(s.PauseDir, name)
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("pause %s: %w", name, err)
	}
	return nil
}

// Resume moves <pauseDir>/<name> back to <plansDir>/<name>.
func (s *FilePlanStore) Resume(_ context.Context, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if s.PauseDir == "" {
		return fmt.Errorf("pause_dir not configured")
	}
	src := filepath.Join(s.PauseDir, name)
	dst := filepath.Join(s.PlansDir, name)
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("resume %s: %w", name, err)
	}
	return nil
}

// validateName rejects path separators and parent-directory traversal to keep
// callers honest — Load/Save expect a basename, never a path.
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("plan name is empty")
	}
	if filepath.Base(name) != name {
		return fmt.Errorf("plan name %q must be a basename, not a path", name)
	}
	return nil
}
