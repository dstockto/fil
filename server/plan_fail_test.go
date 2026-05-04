package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dstockto/fil/models"
	"github.com/dstockto/fil/plan"
)

// fakePlanOps lets handler tests inspect the request the server forwards
// without needing a real LocalPlanOps + Spoolman + history writer chain.
type fakePlanOps struct {
	failCalled     bool
	failGot        plan.FailRequest
	failRet        plan.FailResult
	failErr        error
	completeCalled bool
	completeGot    plan.CompleteRequest
	completeRet    plan.CompleteResult
	completeErr    error
	nextCalled     bool
	nextGot        plan.NextRequest
	nextRet        plan.NextResult
	nextErr        error
	stopCalled     bool
	stopGot        plan.StopRequest
	stopErr        error
	pauseCalled    bool
	pauseGotName   string
	pauseErr       error
	resumeCalled    bool
	resumeGotName   string
	resumeErr       error
	archiveCalled   bool
	archiveGotName  string
	archiveErr      error
	unarchiveCalled bool
	unarchiveGotName string
	unarchiveErr    error
	deleteCalled    bool
	deleteGotName   string
	deleteErr       error
	resolveCalled   bool
	resolveGot      plan.ResolveRequest
	resolveErr      error
	saveAllCalled    bool
	saveAllGotName   string
	saveAllErr       error
	saveBytesCalled  bool
	saveBytesGotName string
	saveBytesErr     error
}

func (f *fakePlanOps) Fail(_ context.Context, req plan.FailRequest) (plan.FailResult, error) {
	f.failCalled = true
	f.failGot = req
	return f.failRet, f.failErr
}

func (f *fakePlanOps) Complete(_ context.Context, req plan.CompleteRequest) (plan.CompleteResult, error) {
	f.completeCalled = true
	f.completeGot = req
	return f.completeRet, f.completeErr
}

func (f *fakePlanOps) Next(_ context.Context, req plan.NextRequest) (plan.NextResult, error) {
	f.nextCalled = true
	f.nextGot = req
	return f.nextRet, f.nextErr
}

func (f *fakePlanOps) Stop(_ context.Context, req plan.StopRequest) error {
	f.stopCalled = true
	f.stopGot = req
	return f.stopErr
}

func (f *fakePlanOps) Pause(_ context.Context, name string) error {
	f.pauseCalled = true
	f.pauseGotName = name
	return f.pauseErr
}

func (f *fakePlanOps) Resume(_ context.Context, name string) error {
	f.resumeCalled = true
	f.resumeGotName = name
	return f.resumeErr
}

func (f *fakePlanOps) Archive(_ context.Context, name string) error {
	f.archiveCalled = true
	f.archiveGotName = name
	return f.archiveErr
}

func (f *fakePlanOps) Unarchive(_ context.Context, name string) error {
	f.unarchiveCalled = true
	f.unarchiveGotName = name
	return f.unarchiveErr
}

func (f *fakePlanOps) Delete(_ context.Context, name string) error {
	f.deleteCalled = true
	f.deleteGotName = name
	return f.deleteErr
}

func (f *fakePlanOps) Resolve(_ context.Context, req plan.ResolveRequest) error {
	f.resolveCalled = true
	f.resolveGot = req
	return f.resolveErr
}

func (f *fakePlanOps) SaveAll(_ context.Context, name string, _ models.PlanFile) error {
	f.saveAllCalled = true
	f.saveAllGotName = name
	return f.saveAllErr
}

func (f *fakePlanOps) SaveBytes(_ context.Context, name string, _ []byte) error {
	f.saveBytesCalled = true
	f.saveBytesGotName = name
	return f.saveBytesErr
}

func postPlanFail(t *testing.T, s *PlanServer, req plan.FailRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/plan-fail", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handlePlanFail(w, r)
	return w
}

func TestPlanFailDelegatesToPlanOps(t *testing.T) {
	fake := &fakePlanOps{}
	s := &PlanServer{PlansDir: t.TempDir(), PlanOps: fake}

	failedAt := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)
	req := plan.FailRequest{
		Printer:  "Bambu X1C",
		Cause:    "bed_adhesion",
		Reason:   "PETG residue suspected",
		FailedAt: failedAt,
		Plates: []plan.FailPlate{
			{Plan: "test.yaml", Project: "ProjA", Plate: "Plate 1"},
			{Plan: "test.yaml", Project: "ProjA", Plate: "Plate 2"},
		},
	}

	w := postPlanFail(t, s, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %q", w.Code, w.Body.String())
	}
	if !fake.failCalled {
		t.Fatal("PlanOps.Fail was not called")
	}
	if fake.failGot.Cause != "bed_adhesion" {
		t.Errorf("got cause %q", fake.failGot.Cause)
	}
	if len(fake.failGot.Plates) != 2 {
		t.Errorf("got %d plates, want 2", len(fake.failGot.Plates))
	}
}

func TestPlanFailRejectsInvalidCause(t *testing.T) {
	s := &PlanServer{PlansDir: t.TempDir(), PlanOps: &fakePlanOps{}}

	cases := []struct {
		name string
		req  plan.FailRequest
	}{
		{"empty cause", plan.FailRequest{Plates: []plan.FailPlate{{Plan: "x", Project: "p", Plate: "1"}}}},
		{"unknown cause", plan.FailRequest{Cause: "operator_error", Plates: []plan.FailPlate{{Plan: "x", Project: "p", Plate: "1"}}}},
		{"no plates", plan.FailRequest{Cause: "bed_adhesion"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := postPlanFail(t, s, tc.req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body = %q", w.Code, w.Body.String())
			}
		})
	}
}

func TestPlanFailReturns500WhenPlanOpsErrors(t *testing.T) {
	fake := &fakePlanOps{failErr: errors.New("boom")}
	s := &PlanServer{PlansDir: t.TempDir(), PlanOps: fake}
	req := plan.FailRequest{
		Cause:    "bed_adhesion",
		FailedAt: time.Now().UTC(),
		Plates:   []plan.FailPlate{{Plan: "x", Project: "p", Plate: "1"}},
	}
	w := postPlanFail(t, s, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestPlanFailReturns500WhenPlanOpsNotConfigured(t *testing.T) {
	s := &PlanServer{PlansDir: t.TempDir()}
	req := plan.FailRequest{
		Cause:    "bed_adhesion",
		FailedAt: time.Now().UTC(),
		Plates:   []plan.FailPlate{{Plan: "x", Project: "p", Plate: "1"}},
	}
	w := postPlanFail(t, s, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}
