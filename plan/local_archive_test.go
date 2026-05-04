package plan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalArchiveDelegatesToStore(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = samplePlan()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	if err := ops.Archive(context.Background(), "test.yaml"); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if _, stillActive := store.plans["test.yaml"]; stillActive {
		t.Error("plan still in active map after Archive")
	}
	if _, archived := store.archived["test.yaml"]; !archived {
		t.Error("plan not in archived map after Archive")
	}
}

func TestLocalUnarchiveDelegatesToStore(t *testing.T) {
	store := newMemPlanStore()
	store.archived["test.yaml"] = samplePlan()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	if err := ops.Unarchive(context.Background(), "test.yaml"); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	if _, stillArchived := store.archived["test.yaml"]; stillArchived {
		t.Error("plan still in archived map after Unarchive")
	}
	if _, active := store.plans["test.yaml"]; !active {
		t.Error("plan not in active map after Unarchive")
	}
}

func TestLocalDeleteDelegatesToStore(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = samplePlan()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	if err := ops.Delete(context.Background(), "test.yaml"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, stillActive := store.plans["test.yaml"]; stillActive {
		t.Error("plan still present after Delete")
	}
}

func TestLocalArchiveRejectsEmptyName(t *testing.T) {
	store := newMemPlanStore()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})
	if err := ops.Archive(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty name")
	}
	if err := ops.Unarchive(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty name")
	}
	if err := ops.Delete(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestLocalArchiveSurfacesStoreError(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = samplePlan()
	store.archiveErr = errors.New("disk full")
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	if err := ops.Archive(context.Background(), "test.yaml"); err == nil {
		t.Fatal("expected error from store")
	}
}

func TestLocalArchiveDeleteRequirePlanStore(t *testing.T) {
	ops := NewLocal(newFakeSpoolman(), StaticPrinterLocations{}, nil, &recordingHistory{}, NoopNotifier{})
	if err := ops.Archive(context.Background(), "x.yaml"); err == nil {
		t.Fatal("expected error when PlanStore not configured")
	}
	if err := ops.Unarchive(context.Background(), "x.yaml"); err == nil {
		t.Fatal("expected error when PlanStore not configured")
	}
	if err := ops.Delete(context.Background(), "x.yaml"); err == nil {
		t.Fatal("expected error when PlanStore not configured")
	}
}

// TestFilePlanStoreArchiveAddsTimestamp verifies the file-backed store
// preserves the historic naming behavior (foo.yaml → foo-YYYYMMDDHHMMSS.yaml)
// so existing archive listings stay backward-compatible.
func TestFilePlanStoreArchiveAddsTimestamp(t *testing.T) {
	dir := t.TempDir()
	plansDir := filepath.Join(dir, "plans")
	archiveDir := filepath.Join(dir, "archive")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	srcPath := filepath.Join(plansDir, "foo.yaml")
	if err := os.WriteFile(srcPath, []byte("projects: []\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFilePlanStore(plansDir, "", archiveDir)
	if err := store.Archive(context.Background(), "foo.yaml"); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d archived files, want 1", len(entries))
	}
	name := entries[0].Name()
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if !archiveTimestampRe.MatchString(stem) {
		t.Errorf("archived filename %q does not carry timestamp suffix", name)
	}
}

// TestFilePlanStoreUnarchiveStripsTimestamp verifies the file-backed store
// strips the suffix added by Archive when restoring.
func TestFilePlanStoreUnarchiveStripsTimestamp(t *testing.T) {
	dir := t.TempDir()
	plansDir := filepath.Join(dir, "plans")
	archiveDir := filepath.Join(dir, "archive")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		t.Fatal(err)
	}
	archivedName := "foo-20260403120000.yaml"
	if err := os.WriteFile(filepath.Join(archiveDir, archivedName), []byte("projects: []\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFilePlanStore(plansDir, "", archiveDir)
	if err := store.Unarchive(context.Background(), archivedName); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(plansDir, "foo.yaml")); err != nil {
		t.Errorf("expected restored foo.yaml in plans dir: %v", err)
	}
}
