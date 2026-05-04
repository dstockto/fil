package plan

import (
	"context"
	"errors"
	"testing"

	"github.com/dstockto/fil/models"
)

func TestLocalSaveAllPersistsPlan(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = models.PlanFile{} // existing slot
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	mutated := samplePlan()
	if err := ops.SaveAll(context.Background(), "test.yaml", mutated); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}
	saved := store.plans["test.yaml"]
	if len(saved.Projects) != 1 || saved.Projects[0].Name != "Proj" {
		t.Errorf("plan not persisted as expected: %+v", saved)
	}
}

func TestLocalSaveAllBackfillsColors(t *testing.T) {
	// Plan has a need with FilamentID=100 and no Color. Spoolman has a spool
	// for that filament with ColorHex set. SaveAll should fill the color
	// before persisting.
	store := newMemPlanStore()
	store.plans["test.yaml"] = models.PlanFile{}

	sm := newFakeSpoolman(makeFailSpool(1, "AMS A1", 800, 100, "PLA white"))
	sm.spools[0].Filament.ColorHex = "#FFFFFF"

	ops := newLocalWithStore(t, sm, store, &recordingHistory{}, NoopNotifier{})

	plan := models.PlanFile{
		Projects: []models.Project{{
			Name: "Proj",
			Plates: []models.Plate{{
				Name:  "P1",
				Needs: []models.PlateRequirement{{FilamentID: 100, Name: "PLA white"}},
			}},
		}},
	}
	if err := ops.SaveAll(context.Background(), "test.yaml", plan); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}
	saved := store.plans["test.yaml"]
	if got := saved.Projects[0].Plates[0].Needs[0].Color; got != "#FFFFFF" {
		t.Errorf("Color = %q, want #FFFFFF (auto-backfilled from Spoolman)", got)
	}
}

func TestLocalSaveAllSkipsBackfillWhenSpoolmanNil(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = models.PlanFile{}
	// Pass nil spoolman; SaveAll must still succeed and just save without filling colors.
	ops := NewLocal(nil, StaticPrinterLocations{}, store, &recordingHistory{}, NoopNotifier{})

	plan := models.PlanFile{
		Projects: []models.Project{{
			Name: "Proj",
			Plates: []models.Plate{{
				Name:  "P1",
				Needs: []models.PlateRequirement{{FilamentID: 100, Name: "PLA white"}},
			}},
		}},
	}
	if err := ops.SaveAll(context.Background(), "test.yaml", plan); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}
	if got := store.plans["test.yaml"].Projects[0].Plates[0].Needs[0].Color; got != "" {
		t.Errorf("Color = %q, want empty (no spoolman to backfill from)", got)
	}
}

func TestLocalSaveAllRejectsEmptyName(t *testing.T) {
	store := newMemPlanStore()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})
	if err := ops.SaveAll(context.Background(), "", models.PlanFile{}); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestLocalSaveAllSurfacesSaveError(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = models.PlanFile{}
	store.saveErr = errors.New("disk full")
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	if err := ops.SaveAll(context.Background(), "test.yaml", samplePlan()); err == nil {
		t.Fatal("expected error from store")
	}
}

func TestLocalSaveAllRequiresPlanStore(t *testing.T) {
	ops := NewLocal(newFakeSpoolman(), StaticPrinterLocations{}, nil, &recordingHistory{}, NoopNotifier{})
	if err := ops.SaveAll(context.Background(), "test.yaml", models.PlanFile{}); err == nil {
		t.Fatal("expected error when PlanStore not configured")
	}
}

func TestLocalSaveBytesPersistsRawBytes(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = models.PlanFile{}
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	yamlBytes := []byte("projects:\n  - name: Proj\n    plates:\n      - name: P1\n")
	if err := ops.SaveBytes(context.Background(), "test.yaml", yamlBytes); err != nil {
		t.Fatalf("SaveBytes: %v", err)
	}
	saved := store.plans["test.yaml"]
	if len(saved.Projects) != 1 || saved.Projects[0].Name != "Proj" {
		t.Errorf("plan not persisted via SaveBytes round-trip: %+v", saved)
	}
}

func TestLocalSaveBytesRejectsEmptyName(t *testing.T) {
	store := newMemPlanStore()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})
	if err := ops.SaveBytes(context.Background(), "", []byte("projects: []\n")); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestLocalSaveBytesRequiresPlanStore(t *testing.T) {
	ops := NewLocal(newFakeSpoolman(), StaticPrinterLocations{}, nil, &recordingHistory{}, NoopNotifier{})
	if err := ops.SaveBytes(context.Background(), "test.yaml", []byte("projects: []\n")); err == nil {
		t.Fatal("expected error when PlanStore not configured")
	}
}
