package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newScanServer(t *testing.T) (*PlanServer, string) {
	t.Helper()
	dir := t.TempDir()
	return &PlanServer{PlansDir: dir}, dir
}

func TestHandleScanHistoryPost_Appends(t *testing.T) {
	s, dir := newScanServer(t)

	ev := ScanEvent{
		ClientHost: "laptop",
		SpoolID:    127,
		FilamentID: 42,
		ScannedHex: "#ead9d4",
		ScannedTD:  2.47,
		HasTD:      true,
		Action:     "commit",
		RawCSV:     "scan1,X,PLA,Y,2.47,EAD9D4,Yes,u",
	}
	body, _ := json.Marshal(ev)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scan-history", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleScanHistoryPost(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	data, err := os.ReadFile(filepath.Join(dir, "scan-history.jsonl"))
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	var decoded ScanEvent
	if err := json.Unmarshal([]byte(lines[0]), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.SpoolID != 127 {
		t.Errorf("spool_id = %d, want 127", decoded.SpoolID)
	}
	if decoded.Timestamp.IsZero() {
		t.Error("timestamp should be set by server when missing")
	}
}

func TestHandleScanHistoryPost_RejectsMissingAction(t *testing.T) {
	s, _ := newScanServer(t)

	body, _ := json.Marshal(ScanEvent{SpoolID: 1})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scan-history", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleScanHistoryPost(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleScanHistoryPost_RejectsBadJSON(t *testing.T) {
	s, _ := newScanServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scan-history", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	s.handleScanHistoryPost(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleScanHistoryGet_EmptyFile(t *testing.T) {
	s, _ := newScanServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scan-history", nil)
	w := httptest.NewRecorder()
	s.handleScanHistoryGet(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := strings.TrimSpace(w.Body.String()); got != "[]" {
		t.Fatalf("expected [], got %q", got)
	}
}

func TestHandleScanHistoryGet_FiltersAndReturns(t *testing.T) {
	s, dir := newScanServer(t)

	now := time.Now().UTC()
	writeEvent := func(ts time.Time, id int) {
		ev := ScanEvent{Timestamp: ts, SpoolID: id, Action: "commit"}
		b, _ := json.Marshal(ev)
		f, _ := os.OpenFile(filepath.Join(dir, "scan-history.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		_, _ = f.Write(append(b, '\n'))
		_ = f.Close()
	}
	writeEvent(now.Add(-48*time.Hour), 1)
	writeEvent(now.Add(-24*time.Hour), 2)
	writeEvent(now, 3)

	// limit=2 → most recent two
	req := httptest.NewRequest(http.MethodGet, "/api/v1/scan-history?limit=2", nil)
	w := httptest.NewRecorder()
	s.handleScanHistoryGet(w, req)

	var got []ScanEvent
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	if got[0].SpoolID != 2 || got[1].SpoolID != 3 {
		t.Errorf("expected spool IDs 2,3 got %d,%d", got[0].SpoolID, got[1].SpoolID)
	}
}
