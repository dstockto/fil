package plan

import (
	"context"
	"errors"
	"testing"

	"github.com/dstockto/fil/models"
	"gopkg.in/yaml.v3"
)

// memPlanStore is an in-memory PlanStore for tests. Tracks the last saved
// plan so tests can assert on the post-mutation YAML state without hitting
// disk. paused/archived mirror the active map for the workflow verbs.
type memPlanStore struct {
	plans        map[string]models.PlanFile
	paused       map[string]models.PlanFile
	archived     map[string]models.PlanFile
	saveErr      error
	loadErr      error
	pauseErr     error
	resumeErr    error
	archiveErr   error
	unarchiveErr error
	deleteErr    error
	saveCalls    []models.PlanFile
}

func newMemPlanStore() *memPlanStore {
	return &memPlanStore{
		plans:    map[string]models.PlanFile{},
		paused:   map[string]models.PlanFile{},
		archived: map[string]models.PlanFile{},
	}
}

func (m *memPlanStore) Load(_ context.Context, name string) (models.PlanFile, error) {
	if m.loadErr != nil {
		return models.PlanFile{}, m.loadErr
	}
	p, ok := m.plans[name]
	if !ok {
		return models.PlanFile{}, errors.New("not found")
	}
	return p, nil
}

func (m *memPlanStore) Save(_ context.Context, name string, plan models.PlanFile) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.plans[name] = plan
	m.saveCalls = append(m.saveCalls, plan)
	return nil
}

// SaveBytes is unmarshal-then-store so tests can also exercise SaveBytes
// paths via the in-memory fake. Real callers care about byte preservation;
// the fake just round-trips through PlanFile.
func (m *memPlanStore) SaveBytes(ctx context.Context, name string, data []byte) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	var p models.PlanFile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return err
	}
	return m.Save(ctx, name, p)
}

func (m *memPlanStore) Pause(_ context.Context, name string) error {
	if m.pauseErr != nil {
		return m.pauseErr
	}
	p, ok := m.plans[name]
	if !ok {
		return errors.New("not found")
	}
	delete(m.plans, name)
	m.paused[name] = p
	return nil
}

func (m *memPlanStore) Resume(_ context.Context, name string) error {
	if m.resumeErr != nil {
		return m.resumeErr
	}
	p, ok := m.paused[name]
	if !ok {
		return errors.New("not found in paused")
	}
	delete(m.paused, name)
	m.plans[name] = p
	return nil
}

func (m *memPlanStore) Archive(_ context.Context, name string) error {
	if m.archiveErr != nil {
		return m.archiveErr
	}
	p, ok := m.plans[name]
	if !ok {
		return errors.New("not found")
	}
	delete(m.plans, name)
	m.archived[name] = p
	return nil
}

func (m *memPlanStore) Unarchive(_ context.Context, name string) error {
	if m.unarchiveErr != nil {
		return m.unarchiveErr
	}
	p, ok := m.archived[name]
	if !ok {
		return errors.New("not found in archived")
	}
	delete(m.archived, name)
	m.plans[name] = p
	return nil
}

func (m *memPlanStore) Delete(_ context.Context, name string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if _, ok := m.plans[name]; !ok {
		return errors.New("not found")
	}
	delete(m.plans, name)
	return nil
}

func samplePlan() models.PlanFile {
	return models.PlanFile{
		Projects: []models.Project{{
			Name: "Proj", Status: "in-progress",
			Plates: []models.Plate{
				{Name: "P1", Status: "in-progress", Printer: "Bambu X1C",
					Needs: []models.PlateRequirement{{FilamentID: 100, Name: "PLA white", Amount: 50}},
				},
				{Name: "P2", Status: "todo",
					Needs: []models.PlateRequirement{{FilamentID: 100, Name: "PLA white", Amount: 30}},
				},
			},
		}},
	}
}

func TestLocalCompleteHappyPath(t *testing.T) {
	sm := newFakeSpoolman(makeFailSpool(101, "AMS A1", 800, 100, "PLA white"))
	store := newMemPlanStore()
	store.plans["test.yaml"] = samplePlan()
	hist := &recordingHistory{}
	notif := &recordingNotifier{}
	ops := newLocalWithStore(t, sm, store, hist, notif)

	req := CompleteRequest{
		Plan:    "test.yaml",
		Project: "Proj",
		Plate:   "P1",
		Printer: "Bambu X1C",
		Deductions: []SpoolDeduction{
			{SpoolID: 101, Amount: 48.5},
		},
		Filament: []models.PlateRequirement{{FilamentID: 100, Name: "PLA white", Amount: 50}},
	}
	result, err := ops.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	saved := store.plans["test.yaml"]
	plate := saved.Projects[0].Plates[0]
	if plate.Status != "completed" {
		t.Errorf("plate status = %q, want completed", plate.Status)
	}
	if plate.Printer != "" {
		t.Errorf("plate printer = %q, want cleared", plate.Printer)
	}
	if got := sm.useCalls[101]; !floatClose(got, 48.5, 0.001) {
		t.Errorf("spool 101 deducted %.3fg, want 48.5", got)
	}
	if len(hist.completeEntries) != 1 {
		t.Errorf("history entries = %d, want 1", len(hist.completeEntries))
	}
	if len(notif.calls) != 1 {
		t.Errorf("notify calls = %d, want 1", len(notif.calls))
	}
	if result.ProjectCascaded {
		t.Errorf("ProjectCascaded = true, want false (P2 still todo)")
	}
}

func TestLocalCompleteCascadesProjectStatus(t *testing.T) {
	sm := newFakeSpoolman(makeFailSpool(101, "AMS A1", 800, 100, "PLA white"))
	store := newMemPlanStore()
	plan := samplePlan()
	plan.Projects[0].Plates[1].Status = "completed" // P2 already done
	store.plans["test.yaml"] = plan
	ops := newLocalWithStore(t, sm, store, &recordingHistory{}, NoopNotifier{})

	result, err := ops.Complete(context.Background(), CompleteRequest{
		Plan: "test.yaml", Project: "Proj", Plate: "P1", Printer: "Bambu X1C",
		Deductions: []SpoolDeduction{{SpoolID: 101, Amount: 50}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !result.ProjectCascaded {
		t.Error("ProjectCascaded = false; expected cascade once all plates completed")
	}
	if got := store.plans["test.yaml"].Projects[0].Status; got != "completed" {
		t.Errorf("project status = %q, want completed", got)
	}
}

func TestLocalCompleteErrorsOnUnknownPlate(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = samplePlan()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	_, err := ops.Complete(context.Background(), CompleteRequest{
		Plan: "test.yaml", Project: "Proj", Plate: "Pnope",
	})
	if err == nil {
		t.Fatal("expected error for unknown plate")
	}
	// YAML must not have been touched.
	if len(store.saveCalls) != 0 {
		t.Errorf("Save was called %d times; expected 0 on lookup failure", len(store.saveCalls))
	}
}

func TestLocalCompleteSpoolmanFailureLeavesYAMLCompleted(t *testing.T) {
	// Spoolman write fails — but YAML save happened first, so the plate is
	// recorded as completed and the user can fix the deduction with `fil
	// use` without risking a double-deduction on retry.
	sm := newFakeSpoolman(makeFailSpool(101, "AMS A1", 800, 100, "PLA white"))
	sm.failOn[101] = errors.New("spoolman down")
	store := newMemPlanStore()
	store.plans["test.yaml"] = samplePlan()
	ops := newLocalWithStore(t, sm, store, &recordingHistory{}, NoopNotifier{})

	_, err := ops.Complete(context.Background(), CompleteRequest{
		Plan: "test.yaml", Project: "Proj", Plate: "P1", Printer: "Bambu X1C",
		Deductions: []SpoolDeduction{{SpoolID: 101, Amount: 50}},
	})
	if err == nil {
		t.Fatal("expected error from Spoolman failure")
	}
	saved := store.plans["test.yaml"]
	if got := saved.Projects[0].Plates[0].Status; got != "completed" {
		t.Errorf("plate status = %q after Spoolman failure; YAML must still record completion", got)
	}
}

func TestLocalCompleteSaveFailureSkipsSpoolman(t *testing.T) {
	// If the YAML save fails, no Spoolman writes should happen — that's the
	// whole reason we save first (avoid double-deduction on retry).
	sm := newFakeSpoolman(makeFailSpool(101, "AMS A1", 800, 100, "PLA white"))
	store := newMemPlanStore()
	store.plans["test.yaml"] = samplePlan()
	store.saveErr = errors.New("disk full")
	ops := newLocalWithStore(t, sm, store, &recordingHistory{}, NoopNotifier{})

	_, err := ops.Complete(context.Background(), CompleteRequest{
		Plan: "test.yaml", Project: "Proj", Plate: "P1", Printer: "Bambu X1C",
		Deductions: []SpoolDeduction{{SpoolID: 101, Amount: 50}},
	})
	if err == nil {
		t.Fatal("expected error from Save failure")
	}
	if len(sm.useCalls) != 0 {
		t.Errorf("UseFilament called despite save failure: %v", sm.useCalls)
	}
}

func TestLocalCompleteRequiresPlanStore(t *testing.T) {
	ops := NewLocal(newFakeSpoolman(), StaticPrinterLocations{}, nil, &recordingHistory{}, NoopNotifier{})
	_, err := ops.Complete(context.Background(), CompleteRequest{
		Plan: "test.yaml", Project: "Proj", Plate: "P1",
	})
	if err == nil {
		t.Fatal("expected error when PlanStore not configured")
	}
}
