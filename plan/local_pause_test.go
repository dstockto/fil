package plan

import (
	"context"
	"errors"
	"testing"
)

func TestLocalPauseDelegatesToStore(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = samplePlan()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	if err := ops.Pause(context.Background(), "test.yaml"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if _, stillActive := store.plans["test.yaml"]; stillActive {
		t.Error("plan still in active map after Pause")
	}
	if _, paused := store.paused["test.yaml"]; !paused {
		t.Error("plan not in paused map after Pause")
	}
}

func TestLocalResumeDelegatesToStore(t *testing.T) {
	store := newMemPlanStore()
	store.paused["test.yaml"] = samplePlan()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	if err := ops.Resume(context.Background(), "test.yaml"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if _, stillPaused := store.paused["test.yaml"]; stillPaused {
		t.Error("plan still in paused map after Resume")
	}
	if _, active := store.plans["test.yaml"]; !active {
		t.Error("plan not in active map after Resume")
	}
}

func TestLocalPauseRejectsEmptyName(t *testing.T) {
	store := newMemPlanStore()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})
	if err := ops.Pause(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestLocalResumeRejectsEmptyName(t *testing.T) {
	store := newMemPlanStore()
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})
	if err := ops.Resume(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestLocalPauseSurfacesStoreError(t *testing.T) {
	store := newMemPlanStore()
	store.plans["test.yaml"] = samplePlan()
	store.pauseErr = errors.New("disk full")
	ops := newLocalWithStore(t, newFakeSpoolman(), store, &recordingHistory{}, NoopNotifier{})

	if err := ops.Pause(context.Background(), "test.yaml"); err == nil {
		t.Fatal("expected error from store")
	}
}

func TestLocalPauseRequiresPlanStore(t *testing.T) {
	ops := NewLocal(newFakeSpoolman(), StaticPrinterLocations{}, nil, &recordingHistory{}, NoopNotifier{})
	if err := ops.Pause(context.Background(), "x.yaml"); err == nil {
		t.Fatal("expected error when PlanStore not configured")
	}
}

func TestLocalResumeRequiresPlanStore(t *testing.T) {
	ops := NewLocal(newFakeSpoolman(), StaticPrinterLocations{}, nil, &recordingHistory{}, NoopNotifier{})
	if err := ops.Resume(context.Background(), "x.yaml"); err == nil {
		t.Fatal("expected error when PlanStore not configured")
	}
}
