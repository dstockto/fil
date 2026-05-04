package plan

import (
	"context"
	"errors"
	"testing"

	"github.com/dstockto/fil/models"
)

func resolvableSamplePlan() models.PlanFile {
	// Build a plan whose plates have unresolved Needs (FilamentID=0).
	return models.PlanFile{
		Projects: []models.Project{{
			Name: "Proj", Status: "todo",
			Plates: []models.Plate{
				{Name: "P1", Status: "todo", Needs: []models.PlateRequirement{
					{Name: "PLA white", Material: "PLA"}, // index 0
					{Name: "PETG black", Material: "PETG"}, // index 1
				}},
			},
		}},
	}
}

func TestLocalResolveAppliesResolutions(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = resolvableSamplePlan()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	req := ResolveRequest{
		Plan: "test.yaml",
		Resolutions: []NeedResolution{
			{Project: "Proj", Plate: "P1", NeedIndex: 0, FilamentID: 100, Name: "Polymaker PLA white", Material: "PLA"},
			{Project: "Proj", Plate: "P1", NeedIndex: 1, FilamentID: 200, Name: "Bambu PETG black", Material: "PETG"},
		},
	}
	if err := ops.Resolve(context.Background(), req); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	saved := store.plans["test.yaml"]
	needs := saved.Projects[0].Plates[0].Needs
	if needs[0].FilamentID != 100 || needs[0].Name != "Polymaker PLA white" {
		t.Errorf("need[0] = %+v, want filament 100", needs[0])
	}
	if needs[1].FilamentID != 200 || needs[1].Name != "Bambu PETG black" {
		t.Errorf("need[1] = %+v, want filament 200", needs[1])
	}
}

func TestLocalResolveEmptyResolutionsIsNoOp(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = resolvableSamplePlan()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	if err := ops.Resolve(context.Background(), ResolveRequest{Plan: "test.yaml"}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(store.saveCalls) != 0 {
		t.Errorf("Save called %d times for empty resolutions; expected 0", len(store.saveCalls))
	}
}

func TestLocalResolveErrorsOnUnknownPlate(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = resolvableSamplePlan()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	req := ResolveRequest{
		Plan: "test.yaml",
		Resolutions: []NeedResolution{
			{Project: "Proj", Plate: "Pnope", NeedIndex: 0, FilamentID: 100},
		},
	}
	if err := ops.Resolve(context.Background(), req); err == nil {
		t.Fatal("expected error for unknown plate")
	}
	if len(store.saveCalls) != 0 {
		t.Errorf("Save called despite resolution failure: %d", len(store.saveCalls))
	}
}

func TestLocalResolveErrorsOnNeedIndexOutOfRange(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = resolvableSamplePlan()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	req := ResolveRequest{
		Plan: "test.yaml",
		Resolutions: []NeedResolution{
			{Project: "Proj", Plate: "P1", NeedIndex: 99, FilamentID: 100},
		},
	}
	if err := ops.Resolve(context.Background(), req); err == nil {
		t.Fatal("expected error for need_index out of range")
	}
}

func TestLocalResolveSaveFailureSurfaces(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = resolvableSamplePlan()
	store.saveErr = errors.New("disk full")
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	req := ResolveRequest{
		Plan: "test.yaml",
		Resolutions: []NeedResolution{
			{Project: "Proj", Plate: "P1", NeedIndex: 0, FilamentID: 100, Name: "x", Material: "PLA"},
		},
	}
	if err := ops.Resolve(context.Background(), req); err == nil {
		t.Fatal("expected error from Save failure")
	}
}

func TestLocalResolveRequiresPlanStore(t *testing.T) {
	ops := NewLocal(newFakeSpoolman(), StaticPrinterLocations{}, nil, &recordingHistory{}, NoopNotifier{})
	err := ops.Resolve(context.Background(), ResolveRequest{
		Plan: "test.yaml",
		Resolutions: []NeedResolution{{Project: "Proj", Plate: "P1", FilamentID: 100}},
	})
	if err == nil {
		t.Fatal("expected error when PlanStore not configured")
	}
}
