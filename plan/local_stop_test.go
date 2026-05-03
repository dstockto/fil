package plan

import (
	"context"
	"errors"
	"testing"
)

func TestLocalStopHappyPath(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = samplePlan() // P1 is in-progress on Bambu X1C
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	err := ops.Stop(context.Background(), StopRequest{
		Plan: "test.yaml", Project: "Proj", Plate: "P1",
	})
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	plate := store.plans["test.yaml"].Projects[0].Plates[0]
	if plate.Status != "todo" {
		t.Errorf("plate status = %q, want todo", plate.Status)
	}
	if plate.Printer != "" {
		t.Errorf("plate printer = %q, want cleared", plate.Printer)
	}
	if plate.StartedAt != "" {
		t.Errorf("plate started_at = %q, want cleared", plate.StartedAt)
	}
}

func TestLocalStopDoesNotRegressProjectStatus(t *testing.T) {
	// Project should *not* be moved back to todo even if all its plates
	// end up in todo — Project status is forward-only.
	store := newMemPlanStore()
	plan := samplePlan()
	plan.Projects[0].Status = "in-progress" // explicit
	store.plans["test.yaml"] = plan
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	if err := ops.Stop(context.Background(), StopRequest{
		Plan: "test.yaml", Project: "Proj", Plate: "P1",
	}); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := store.plans["test.yaml"].Projects[0].Status; got != "in-progress" {
		t.Errorf("project status = %q, want in-progress (forward-only)", got)
	}
}

func TestLocalStopRejectsMissingFields(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = samplePlan()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	cases := []StopRequest{
		{Project: "Proj", Plate: "P1"},
		{Plan: "test.yaml", Plate: "P1"},
		{Plan: "test.yaml", Project: "Proj"},
	}
	for i, req := range cases {
		if err := ops.Stop(context.Background(), req); err == nil {
			t.Errorf("case %d: expected error for incomplete request", i)
		}
	}
}

func TestLocalStopErrorsOnUnknownPlate(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = samplePlan()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	err := ops.Stop(context.Background(), StopRequest{
		Plan: "test.yaml", Project: "Proj", Plate: "Pnope",
	})
	if err == nil {
		t.Fatal("expected error for unknown plate")
	}
	if len(store.saveCalls) != 0 {
		t.Errorf("Save called despite lookup failure: %d times", len(store.saveCalls))
	}
}

func TestLocalStopSaveFailureSurfaces(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = samplePlan()
	store.saveErr = errors.New("disk full")
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	err := ops.Stop(context.Background(), StopRequest{
		Plan: "test.yaml", Project: "Proj", Plate: "P1",
	})
	if err == nil {
		t.Fatal("expected error from Save failure")
	}
}

func TestLocalStopRequiresPlanStore(t *testing.T) {
	ops := NewLocal(newFakeSpoolman(), StaticPrinterLocations{}, nil, &recordingHistory{}, NoopNotifier{})
	err := ops.Stop(context.Background(), StopRequest{
		Plan: "test.yaml", Project: "Proj", Plate: "P1",
	})
	if err == nil {
		t.Fatal("expected error when PlanStore not configured")
	}
}
