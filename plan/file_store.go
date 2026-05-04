package plan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dstockto/fil/models"
	"gopkg.in/yaml.v3"
)

// FilePlanStore reads and writes Plan YAML files in the configured plans dir,
// and moves them between plans dir, pause dir, and archive dir for the
// workflow verbs. Used by both the CLI in Local Mode and by the plan-server's
// LocalPlanOps. The CLI's CWD-arg path is not supported through this store —
// verbs that mutate or move plans operate strictly on plans within the
// configured directories.
type FilePlanStore struct {
	PlansDir   string
	PauseDir   string
	ArchiveDir string
}

// NewFilePlanStore returns a store rooted at plansDir. pauseDir/archiveDir
// may be empty in setups that don't use those workflows; calling the
// corresponding methods on such a store returns an error.
func NewFilePlanStore(plansDir, pauseDir, archiveDir string) *FilePlanStore {
	return &FilePlanStore{PlansDir: plansDir, PauseDir: pauseDir, ArchiveDir: archiveDir}
}

// archiveTimestampRe matches the -YYYYMMDDHHMMSS suffix added by Archive.
var archiveTimestampRe = regexp.MustCompile(`-\d{14}$`)

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
	return s.SaveBytes(nil, name, out) //nolint:staticcheck // delegate; ctx not used
}

// SaveBytes writes raw bytes to <plansDir>/<name>. Bypasses unmarshal/marshal
// so $EDITOR-style flows preserve user formatting.
func (s *FilePlanStore) SaveBytes(_ context.Context, name string, data []byte) error {
	if err := validateName(name); err != nil {
		return err
	}
	path := filepath.Join(s.PlansDir, name)
	if err := os.WriteFile(path, data, 0644); err != nil {
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

// Archive moves <plansDir>/<name> to <archiveDir>/<name-without-ext>-YYYYMMDDHHMMSS<ext>,
// so re-archiving the same name doesn't collide. Creates archiveDir if missing.
func (s *FilePlanStore) Archive(_ context.Context, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if s.ArchiveDir == "" {
		return fmt.Errorf("archive_dir not configured")
	}
	if err := os.MkdirAll(s.ArchiveDir, 0755); err != nil {
		return fmt.Errorf("create archive dir: %w", err)
	}
	src := filepath.Join(s.PlansDir, name)
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	dst := filepath.Join(s.ArchiveDir, fmt.Sprintf("%s-%s%s", base, time.Now().Format("20060102150405"), ext))
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("archive %s: %w", name, err)
	}
	return nil
}

// Unarchive moves <archiveDir>/<name> to <plansDir>/<stripped-name>, where
// the timestamp suffix added by Archive is stripped. The caller passes the
// archived basename (the timestamped one), since that's what the archive
// listing surfaces.
func (s *FilePlanStore) Unarchive(_ context.Context, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if s.ArchiveDir == "" {
		return fmt.Errorf("archive_dir not configured")
	}
	src := filepath.Join(s.ArchiveDir, name)
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	restored := archiveTimestampRe.ReplaceAllString(base, "") + ext
	dst := filepath.Join(s.PlansDir, restored)
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("unarchive %s: %w", name, err)
	}
	return nil
}

// Delete removes <plansDir>/<name>. Errors if the file doesn't exist —
// callers should have just discovered it.
func (s *FilePlanStore) Delete(_ context.Context, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	path := filepath.Join(s.PlansDir, name)
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete %s: %w", name, err)
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
