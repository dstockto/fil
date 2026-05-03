package plan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dstockto/fil/models"
	"gopkg.in/yaml.v3"
)

// FilePlanStore reads and writes Plan YAML files in a single directory. Used
// by both the CLI in Local Mode and by the plan-server's LocalPlanOps. The
// CLI's CWD-arg path (`fil plan complete some.yaml`) is not supported through
// this store — verbs that mutate the plan operate strictly on plans within
// the configured PlansDir.
type FilePlanStore struct {
	PlansDir string
}

// NewFilePlanStore returns a store rooted at plansDir.
func NewFilePlanStore(plansDir string) *FilePlanStore {
	return &FilePlanStore{PlansDir: plansDir}
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
