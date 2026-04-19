package server

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dstockto/fil/models"
)

// fakeAdapter is a minimal PrinterAdapter for tests — exposes a settable state
// and no-op connect/close. Lets us register a printer with a known
// LastFinishedAt without needing real hardware.
type fakeAdapter struct {
	state PrinterState
}

func (f *fakeAdapter) Connect() error                              { return nil }
func (f *fakeAdapter) Close() error                                { return nil }
func (f *fakeAdapter) Status() PrinterState                        { return f.state }
func (f *fakeAdapter) PushTray(TrayUpdate) error                   { return nil }
func (f *fakeAdapter) OnStateChange(func(StateChangeEvent))        {}

func readEntries(t *testing.T, path string) []HistoryEntry {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open history: %v", err)
	}
	defer func() { _ = f.Close() }()

	var entries []HistoryEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e HistoryEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("decode entry: %v", err)
		}
		entries = append(entries, e)
	}
	return entries
}

func makePlan(plate models.Plate) *models.PlanFile {
	return &models.PlanFile{
		Projects: []models.Project{{
			Name:   "Test Project",
			Status: "in-progress",
			Plates: []models.Plate{plate},
		}},
	}
}

func TestLogCompletionsPopulatesFinishedAt(t *testing.T) {
	dir := t.TempDir()
	pm := NewPrinterManager()
	finishTime := time.Date(2026, 4, 18, 14, 30, 0, 0, time.UTC)
	if err := pm.AddAdapter("Bambu X1C", &fakeAdapter{
		state: PrinterState{Name: "Bambu X1C", LastFinishedAt: finishTime},
	}); err != nil {
		t.Fatalf("add adapter: %v", err)
	}

	s := &PlanServer{PlansDir: dir, Printers: pm}

	old := makePlan(models.Plate{
		Name:      "Plate 1",
		Status:    "in-progress",
		Printer:   "Bambu X1C",
		StartedAt: "2026-04-18T08:00:00Z",
	})
	newer := makePlan(models.Plate{
		Name:   "Plate 1",
		Status: "completed",
	})

	s.logCompletions("test-plan", old, newer)

	entries := readEntries(t, filepath.Join(dir, "print-history.jsonl"))
	if len(entries) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(entries))
	}
	e := entries[0]
	if e.FinishedAt == "" {
		t.Fatalf("expected FinishedAt to be populated, got empty")
	}
	got, err := time.Parse(time.RFC3339, e.FinishedAt)
	if err != nil {
		t.Fatalf("FinishedAt %q is not RFC3339: %v", e.FinishedAt, err)
	}
	if !got.Equal(finishTime) {
		t.Errorf("FinishedAt = %v, want %v", got, finishTime)
	}
	if e.Timestamp == "" {
		t.Error("Timestamp should still be set (save-time)")
	}
}

func TestLogCompletionsOmitsFinishedAtWhenNoPrinterData(t *testing.T) {
	dir := t.TempDir()
	pm := NewPrinterManager() // empty — no adapters

	s := &PlanServer{PlansDir: dir, Printers: pm}

	old := makePlan(models.Plate{
		Name:      "Plate 1",
		Status:    "in-progress",
		Printer:   "Unknown Printer",
		StartedAt: "2026-04-18T08:00:00Z",
	})
	newer := makePlan(models.Plate{
		Name:   "Plate 1",
		Status: "completed",
	})

	s.logCompletions("test-plan", old, newer)

	entries := readEntries(t, filepath.Join(dir, "print-history.jsonl"))
	if len(entries) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(entries))
	}
	if entries[0].FinishedAt != "" {
		t.Errorf("expected empty FinishedAt for unknown printer, got %q", entries[0].FinishedAt)
	}
	if entries[0].Timestamp == "" {
		t.Error("Timestamp should still be set as save-time fallback")
	}
}

func TestLogCompletionsOmitsFinishedAtWhenPrinterHasNoFinishYet(t *testing.T) {
	dir := t.TempDir()
	pm := NewPrinterManager()
	if err := pm.AddAdapter("Bambu X1C", &fakeAdapter{
		state: PrinterState{Name: "Bambu X1C"}, // LastFinishedAt is zero
	}); err != nil {
		t.Fatalf("add adapter: %v", err)
	}

	s := &PlanServer{PlansDir: dir, Printers: pm}

	old := makePlan(models.Plate{
		Name:      "Plate 1",
		Status:    "in-progress",
		Printer:   "Bambu X1C",
		StartedAt: "2026-04-18T08:00:00Z",
	})
	newer := makePlan(models.Plate{
		Name:   "Plate 1",
		Status: "completed",
	})

	s.logCompletions("test-plan", old, newer)

	entries := readEntries(t, filepath.Join(dir, "print-history.jsonl"))
	if len(entries) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(entries))
	}
	if entries[0].FinishedAt != "" {
		t.Errorf("expected empty FinishedAt when printer has no recorded finish, got %q", entries[0].FinishedAt)
	}
}
