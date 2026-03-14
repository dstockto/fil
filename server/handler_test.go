package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestServer(t *testing.T) (*PlanServer, string) {
	t.Helper()
	base := t.TempDir()
	plansDir := filepath.Join(base, "plans")
	pauseDir := filepath.Join(base, "paused")
	archiveDir := filepath.Join(base, "archive")

	for _, d := range []string{plansDir, pauseDir, archiveDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	s := &PlanServer{
		PlansDir:   plansDir,
		PauseDir:   pauseDir,
		ArchiveDir: archiveDir,
	}
	return s, base
}

const testPlanYAML = `projects:
- name: Test Project
  status: todo
  plates:
  - name: Plate 1
    status: todo
    needs:
    - name: black
      material: PLA
      amount: 50
  - name: Plate 2
    status: completed
    needs:
    - name: white
      material: PLA
      amount: 30
`

func TestListPlansEmpty(t *testing.T) {
	s, _ := setupTestServer(t)
	mux := s.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plans", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var summaries []PlanSummary
	_ = json.NewDecoder(w.Body).Decode(&summaries)
	if len(summaries) != 0 {
		t.Fatalf("expected empty list, got %d", len(summaries))
	}
}

func TestPutAndGetPlan(t *testing.T) {
	s, _ := setupTestServer(t)
	mux := s.Routes()

	// PUT
	req := httptest.NewRequest(http.MethodPut, "/api/v1/plans/test.yaml", strings.NewReader(testPlanYAML))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("PUT expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// GET
	req = httptest.NewRequest(http.MethodGet, "/api/v1/plans/test.yaml", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "application/x-yaml" {
		t.Fatalf("expected application/x-yaml content type, got %s", w.Header().Get("Content-Type"))
	}
	body, _ := io.ReadAll(w.Body)
	if string(body) != testPlanYAML {
		t.Fatalf("body mismatch:\ngot: %s\nwant: %s", string(body), testPlanYAML)
	}
}

func TestListPlansWithData(t *testing.T) {
	s, _ := setupTestServer(t)
	mux := s.Routes()

	// Write a plan file
	_ = os.WriteFile(filepath.Join(s.PlansDir, "test.yaml"), []byte(testPlanYAML), 0644)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plans", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var summaries []PlanSummary
	_ = json.NewDecoder(w.Body).Decode(&summaries)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(summaries))
	}
	if summaries[0].Name != "test.yaml" {
		t.Fatalf("expected name test.yaml, got %s", summaries[0].Name)
	}
	if summaries[0].Projects != 1 {
		t.Fatalf("expected 1 project, got %d", summaries[0].Projects)
	}
	if summaries[0].PlatesTodo != 1 {
		t.Fatalf("expected 1 plate todo, got %d", summaries[0].PlatesTodo)
	}
}

func TestDeletePlan(t *testing.T) {
	s, _ := setupTestServer(t)
	mux := s.Routes()

	_ = os.WriteFile(filepath.Join(s.PlansDir, "test.yaml"), []byte(testPlanYAML), 0644)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/plans/test.yaml", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE expected 204, got %d", w.Code)
	}

	// Verify file is gone
	if _, err := os.Stat(filepath.Join(s.PlansDir, "test.yaml")); !os.IsNotExist(err) {
		t.Fatal("file should have been deleted")
	}
}

func TestDeletePlanNotFound(t *testing.T) {
	s, _ := setupTestServer(t)
	mux := s.Routes()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/plans/nonexistent.yaml", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestPausePlan(t *testing.T) {
	s, _ := setupTestServer(t)
	mux := s.Routes()

	_ = os.WriteFile(filepath.Join(s.PlansDir, "test.yaml"), []byte(testPlanYAML), 0644)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/plans/test.yaml/pause", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("PAUSE expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify moved
	if _, err := os.Stat(filepath.Join(s.PlansDir, "test.yaml")); !os.IsNotExist(err) {
		t.Fatal("file should have been moved from plans dir")
	}
	if _, err := os.Stat(filepath.Join(s.PauseDir, "test.yaml")); err != nil {
		t.Fatal("file should exist in pause dir")
	}
}

func TestResumePlan(t *testing.T) {
	s, _ := setupTestServer(t)
	mux := s.Routes()

	_ = os.WriteFile(filepath.Join(s.PauseDir, "test.yaml"), []byte(testPlanYAML), 0644)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/plans/test.yaml/resume", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("RESUME expected 204, got %d: %s", w.Code, w.Body.String())
	}

	if _, err := os.Stat(filepath.Join(s.PauseDir, "test.yaml")); !os.IsNotExist(err) {
		t.Fatal("file should have been moved from pause dir")
	}
	if _, err := os.Stat(filepath.Join(s.PlansDir, "test.yaml")); err != nil {
		t.Fatal("file should exist in plans dir")
	}
}

func TestArchivePlan(t *testing.T) {
	s, _ := setupTestServer(t)
	mux := s.Routes()

	_ = os.WriteFile(filepath.Join(s.PlansDir, "test.yaml"), []byte(testPlanYAML), 0644)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/plans/test.yaml/archive", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("ARCHIVE expected 204, got %d: %s", w.Code, w.Body.String())
	}

	if _, err := os.Stat(filepath.Join(s.PlansDir, "test.yaml")); !os.IsNotExist(err) {
		t.Fatal("file should have been moved from plans dir")
	}

	// Check archive dir has a timestamped file
	entries, _ := os.ReadDir(s.ArchiveDir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 archived file, got %d", len(entries))
	}
	if !strings.HasPrefix(entries[0].Name(), "test-") {
		t.Fatalf("expected archived file to start with test-, got %s", entries[0].Name())
	}
}

func TestGetPlanNotFound(t *testing.T) {
	s, _ := setupTestServer(t)
	mux := s.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plans/nonexistent.yaml", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestPutPlanInvalidYAML(t *testing.T) {
	s, _ := setupTestServer(t)
	mux := s.Routes()

	req := httptest.NewRequest(http.MethodPut, "/api/v1/plans/bad.yaml", strings.NewReader("{{{{not yaml"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListPausedPlans(t *testing.T) {
	s, _ := setupTestServer(t)
	mux := s.Routes()

	_ = os.WriteFile(filepath.Join(s.PauseDir, "paused.yaml"), []byte(testPlanYAML), 0644)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plans?status=paused", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var summaries []PlanSummary
	_ = json.NewDecoder(w.Body).Decode(&summaries)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 paused plan, got %d", len(summaries))
	}
}

func TestGetPausedPlan(t *testing.T) {
	s, _ := setupTestServer(t)
	mux := s.Routes()

	_ = os.WriteFile(filepath.Join(s.PauseDir, "paused.yaml"), []byte(testPlanYAML), 0644)

	// GET without status should 404 (not in plans dir)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/plans/paused.yaml", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("GET without status expected 404, got %d", w.Code)
	}

	// GET with status=paused should succeed
	req = httptest.NewRequest(http.MethodGet, "/api/v1/plans/paused.yaml?status=paused", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET with status=paused expected 200, got %d", w.Code)
	}
	body, _ := io.ReadAll(w.Body)
	if string(body) != testPlanYAML {
		t.Fatalf("body mismatch")
	}
}

func TestPausePlanNotFound(t *testing.T) {
	s, _ := setupTestServer(t)
	mux := s.Routes()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/plans/nonexistent.yaml/pause", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestResumePlanNotFound(t *testing.T) {
	s, _ := setupTestServer(t)
	mux := s.Routes()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/plans/nonexistent.yaml/resume", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
