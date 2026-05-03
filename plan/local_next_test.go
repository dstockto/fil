package plan

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLocalNextHappyPath(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = samplePlan()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	startedAt := time.Date(2026, 5, 3, 9, 0, 0, 0, time.UTC)
	result, err := ops.Next(context.Background(), NextRequest{
		Plan: "test.yaml", Project: "Proj", Plate: "P2", Printer: "Bambu X1C",
		StartedAt: startedAt,
	})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	saved := store.plans["test.yaml"]
	plate := saved.Projects[0].Plates[1]
	if plate.Status != "in-progress" {
		t.Errorf("plate status = %q, want in-progress", plate.Status)
	}
	if plate.Printer != "Bambu X1C" {
		t.Errorf("plate printer = %q", plate.Printer)
	}
	if plate.StartedAt != startedAt.Format(time.RFC3339) {
		t.Errorf("plate started_at = %q", plate.StartedAt)
	}
	// samplePlan's project is already "in-progress" so no cascade.
	if result.ProjectStarted {
		t.Errorf("ProjectStarted = true, want false (project was already in-progress)")
	}
}

func TestLocalNextCascadesProjectFromTodo(t *testing.T) {
	store := newMemPlanStore()
	plan := samplePlan()
	plan.Projects[0].Status = "todo"
	plan.Projects[0].Plates[0].Status = "todo"
	plan.Projects[0].Plates[0].Printer = ""
	plan.Projects[0].Plates[0].StartedAt = ""
	store.plans["test.yaml"] = plan
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	result, err := ops.Next(context.Background(), NextRequest{
		Plan: "test.yaml", Project: "Proj", Plate: "P1", Printer: "Bambu X1C",
	})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if !result.ProjectStarted {
		t.Error("ProjectStarted = false; expected cascade from todo")
	}
	if got := store.plans["test.yaml"].Projects[0].Status; got != "in-progress" {
		t.Errorf("project status = %q, want in-progress", got)
	}
}

func TestLocalNextRejectsMissingFields(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = samplePlan()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	cases := []NextRequest{
		{Project: "Proj", Plate: "P1", Printer: "Bambu X1C"},                  // no Plan
		{Plan: "test.yaml", Plate: "P1", Printer: "Bambu X1C"},                 // no Project
		{Plan: "test.yaml", Project: "Proj", Printer: "Bambu X1C"},             // no Plate
		{Plan: "test.yaml", Project: "Proj", Plate: "P1"},                      // no Printer
	}
	for i, req := range cases {
		if _, err := ops.Next(context.Background(), req); err == nil {
			t.Errorf("case %d: expected error for incomplete request", i)
		}
	}
}

func TestLocalNextErrorsOnUnknownPlate(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = samplePlan()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	_, err := ops.Next(context.Background(), NextRequest{
		Plan: "test.yaml", Project: "Proj", Plate: "Pnope", Printer: "Bambu X1C",
	})
	if err == nil {
		t.Fatal("expected error for unknown plate")
	}
	if len(store.saveCalls) != 0 {
		t.Errorf("Save called despite lookup failure: %d times", len(store.saveCalls))
	}
}

func TestLocalNextSaveFailureSurfaces(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = samplePlan()
	store.saveErr = errors.New("disk full")
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	_, err := ops.Next(context.Background(), NextRequest{
		Plan: "test.yaml", Project: "Proj", Plate: "P2", Printer: "Bambu X1C",
	})
	if err == nil {
		t.Fatal("expected error from Save failure")
	}
}

func TestLocalNextRequiresPlanStore(t *testing.T) {
	ops := NewLocal(newFakeSpoolman(), StaticPrinterLocations{}, nil, &recordingHistory{}, NoopNotifier{})
	_, err := ops.Next(context.Background(), NextRequest{
		Plan: "test.yaml", Project: "Proj", Plate: "P1", Printer: "Bambu X1C",
	})
	if err == nil {
		t.Fatal("expected error when PlanStore not configured")
	}
}
