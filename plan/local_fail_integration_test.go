package plan

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
)

// fakeSpoolman is an in-memory Spoolman for LocalPlanOps tests. UseFilament
// records (spoolID → grams) so assertions can verify the deduction shape; an
// optional failOn map injects errors for specific spool IDs.
type fakeSpoolman struct {
	spools     []models.FindSpool
	useCalls   map[int]float64
	patchCalls map[int]map[string]any
	failOn     map[int]error
}

func newFakeSpoolman(spools ...models.FindSpool) *fakeSpoolman {
	return &fakeSpoolman{
		spools:     spools,
		useCalls:   map[int]float64{},
		patchCalls: map[int]map[string]any{},
		failOn:     map[int]error{},
	}
}

func (f *fakeSpoolman) FindSpoolsByName(_ context.Context, _ string, _ api.SpoolFilter, _ map[string]string) ([]models.FindSpool, error) {
	return f.spools, nil
}

func (f *fakeSpoolman) UseFilament(_ context.Context, spoolID int, amount float64) error {
	if err := f.failOn[spoolID]; err != nil {
		return err
	}
	f.useCalls[spoolID] += amount
	return nil
}

func (f *fakeSpoolman) PatchSpool(_ context.Context, spoolID int, updates map[string]any) error {
	f.patchCalls[spoolID] = updates
	return nil
}

// recordingHistory is an in-memory HistoryWriter that captures the entries it
// would have written, so tests can assert without touching the filesystem.
type recordingHistory struct {
	entries []FailHistoryEntry
	err     error
}

func (r *recordingHistory) AppendFail(_ context.Context, e []FailHistoryEntry) error {
	if r.err != nil {
		return r.err
	}
	r.entries = append(r.entries, e...)
	return nil
}

// recordingNotifier captures Notify calls so tests can verify both that
// notifications fire and that body text is sensible.
type recordingNotifier struct {
	calls []string
}

func (r *recordingNotifier) Notify(_ context.Context, title, body string) {
	r.calls = append(r.calls, title+": "+body)
}

func newLocalForTest(t *testing.T, sm *fakeSpoolman, h HistoryWriter, n Notifier) *LocalPlanOps {
	t.Helper()
	printers := StaticPrinterLocations{
		"Bambu X1C": {"AMS A1", "AMS A2", "AMS A3", "AMS A4"},
	}
	return NewLocal(sm, printers, h, n)
}

func TestLocalFailHappyPath(t *testing.T) {
	sm := newFakeSpoolman(
		makeFailSpool(101, "AMS A1", 800, 100, "PLA white"),
		makeFailSpool(102, "AMS A2", 800, 200, "PETG black"),
	)
	hist := &recordingHistory{}
	notif := &recordingNotifier{}
	ops := newLocalForTest(t, sm, hist, notif)

	failedAt := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	req := FailRequest{
		Printer:   "Bambu X1C",
		Cause:     "bed_adhesion",
		UsedGrams: 30,
		FailedAt:  failedAt,
		Plates: []FailPlate{
			{
				Plan: "p.yaml", Project: "Proj", Plate: "1",
				Needs: []models.PlateRequirement{
					{FilamentID: 100, Name: "PLA white", Amount: 50},
					{FilamentID: 200, Name: "PETG black", Amount: 20},
				},
			},
			{
				Plan: "p.yaml", Project: "Proj", Plate: "2",
				Needs: []models.PlateRequirement{
					{FilamentID: 100, Name: "PLA white", Amount: 30},
				},
			},
		},
	}

	result, err := ops.Fail(context.Background(), req)
	if err != nil {
		t.Fatalf("Fail: %v", err)
	}

	// Total planned = 100g, used = 30g → 30%.
	// Spool 101 (PLA): plate1 50*0.3=15g + plate2 30*0.3=9g = 24g
	// Spool 102 (PETG): plate1 20*0.3=6g
	if got := sm.useCalls[101]; !floatClose(got, 24, 0.001) {
		t.Errorf("spool 101 deducted %.3fg, want 24", got)
	}
	if got := sm.useCalls[102]; !floatClose(got, 6, 0.001) {
		t.Errorf("spool 102 deducted %.3fg, want 6", got)
	}
	if len(result.Allocations) != 2 {
		t.Errorf("got %d allocations, want 2", len(result.Allocations))
	}
	if len(result.Unmatched) != 0 {
		t.Errorf("got %d unmatched, want 0: %+v", len(result.Unmatched), result.Unmatched)
	}
	if len(hist.entries) != 2 {
		t.Errorf("got %d history entries, want 2", len(hist.entries))
	}
	if len(notif.calls) != 1 {
		t.Errorf("got %d notify calls, want 1", len(notif.calls))
	}
}

func TestLocalFailUnmatchedFilament(t *testing.T) {
	// PLA spool is in the wrong location (Shelf instead of AMS), so the need
	// can't be auto-resolved.
	sm := newFakeSpoolman(
		makeFailSpool(101, "Shelf 6B", 800, 100, "PLA white"),
	)
	hist := &recordingHistory{}
	ops := newLocalForTest(t, sm, hist, NoopNotifier{})

	req := FailRequest{
		Printer:   "Bambu X1C",
		Cause:     "bed_adhesion",
		UsedGrams: 10,
		Plates: []FailPlate{{
			Plan: "p.yaml", Project: "Proj", Plate: "1",
			Needs: []models.PlateRequirement{{FilamentID: 100, Name: "PLA white", Amount: 50}},
		}},
	}

	result, err := ops.Fail(context.Background(), req)
	if err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if len(sm.useCalls) != 0 {
		t.Errorf("UseFilament should not have been called, got %v", sm.useCalls)
	}
	if len(result.Unmatched) != 1 {
		t.Fatalf("got %d unmatched, want 1", len(result.Unmatched))
	}
	if result.Unmatched[0].FilamentName != "PLA white" {
		t.Errorf("Unmatched[0].FilamentName = %q", result.Unmatched[0].FilamentName)
	}
	// History should still be written even when nothing was deducted —
	// the failure happened, the audit record exists.
	if len(hist.entries) != 1 {
		t.Errorf("got %d history entries, want 1", len(hist.entries))
	}
}

func TestLocalFailContinuesHistoryAfterDeductError(t *testing.T) {
	sm := newFakeSpoolman(makeFailSpool(101, "AMS A1", 800, 100, "PLA white"))
	sm.failOn[101] = errors.New("spoolman down")
	hist := &recordingHistory{}
	ops := newLocalForTest(t, sm, hist, NoopNotifier{})

	req := FailRequest{
		Printer:   "Bambu X1C",
		Cause:     "bed_adhesion",
		UsedGrams: 10,
		Plates: []FailPlate{{
			Plan: "p.yaml", Project: "Proj", Plate: "1",
			Needs: []models.PlateRequirement{{FilamentID: 100, Name: "PLA white", Amount: 50}},
		}},
	}

	_, err := ops.Fail(context.Background(), req)
	if err == nil {
		t.Fatal("Fail should return error when Spoolman fails")
	}
	if len(hist.entries) != 1 {
		t.Errorf("history not written despite Spoolman failure: %d entries", len(hist.entries))
	}
}

func TestLocalFailZeroGramsSkipsSpoolman(t *testing.T) {
	sm := newFakeSpoolman(makeFailSpool(101, "AMS A1", 800, 100, "PLA white"))
	hist := &recordingHistory{}
	ops := newLocalForTest(t, sm, hist, NoopNotifier{})

	req := FailRequest{
		Printer:   "Bambu X1C",
		Cause:     "bed_adhesion",
		UsedGrams: 0,
		Plates: []FailPlate{{
			Plan: "p.yaml", Project: "Proj", Plate: "1",
			Needs: []models.PlateRequirement{{FilamentID: 100, Name: "PLA white", Amount: 50}},
		}},
	}

	_, err := ops.Fail(context.Background(), req)
	if err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if len(sm.useCalls) != 0 {
		t.Errorf("UseFilament should not have been called for 0g, got %v", sm.useCalls)
	}
	if len(hist.entries) != 1 {
		t.Errorf("history must still be written for zero-gram failure: %d entries", len(hist.entries))
	}
}

func TestLocalFailBumpsInitialWeightWhenOverdrawn(t *testing.T) {
	// Spool only has 5g remaining but allocation will deduct 30g — Spoolman
	// would reject the negative-remaining write, so LocalPlanOps must first
	// patch initial_weight up by the overage.
	sm := newFakeSpoolman(models.FindSpool{
		Id: 101, Location: "AMS A1", RemainingWeight: 5, InitialWeight: 1000,
	})
	sm.spools[0].Filament.Id = 100
	hist := &recordingHistory{}
	ops := newLocalForTest(t, sm, hist, NoopNotifier{})

	req := FailRequest{
		Printer:   "Bambu X1C",
		Cause:     "bed_adhesion",
		UsedGrams: 30,
		Plates: []FailPlate{{
			Plan: "p.yaml", Project: "Proj", Plate: "1",
			Needs: []models.PlateRequirement{{FilamentID: 100, Amount: 30}},
		}},
	}

	if _, err := ops.Fail(context.Background(), req); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if patched, ok := sm.patchCalls[101]; !ok {
		t.Fatal("expected initial_weight to be bumped, no PatchSpool call")
	} else {
		want := 1000.0 + (30 - 5) // overage = 25g
		if got := patched["initial_weight"]; got != want {
			t.Errorf("initial_weight bumped to %v, want %v", got, want)
		}
	}
}

// Wire FileHistoryWriter into a real round-trip to confirm the historyPath
// JSONL file ends up with the expected entry — guards against silent format
// drift.
func TestLocalFailWritesToFileHistory(t *testing.T) {
	dir := t.TempDir()
	sm := newFakeSpoolman(makeFailSpool(101, "AMS A1", 800, 100, "PLA white"))
	hist := NewFileHistoryWriter(dir)
	ops := newLocalForTest(t, sm, hist, NoopNotifier{})

	req := FailRequest{
		Printer:   "Bambu X1C",
		Cause:     "warping",
		Reason:    "first layer lifted",
		UsedGrams: 5,
		FailedAt:  time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		Plates: []FailPlate{{
			Plan: "p.yaml", Project: "Proj", Plate: "1",
			Needs: []models.PlateRequirement{{FilamentID: 100, Name: "PLA white", Amount: 50}},
		}},
	}
	if _, err := ops.Fail(context.Background(), req); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	got := readDiskEntries(t, filepath.Join(dir, "print-history.jsonl"))
	if len(got) != 1 {
		t.Fatalf("got %d entries on disk, want 1", len(got))
	}
	if !got[0].Failed || got[0].Cause != "warping" || got[0].Reason != "first layer lifted" {
		t.Errorf("on-disk entry malformed: %+v", got[0])
	}
}
