package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// ScanEvent is one line in scan-history.jsonl — a single TD-1 scan attempt
// with its outcome (committed to Spoolman, skipped, rescanned, or errored).
type ScanEvent struct {
	Timestamp    time.Time `json:"timestamp"`
	ClientHost   string    `json:"client_host,omitempty"`
	DeviceUID    string    `json:"device_uid,omitempty"`  // from CSV uid field
	DeviceUUID   string    `json:"device_uuid,omitempty"` // from CSV Uuid field
	SpoolID      int       `json:"spool_id,omitempty"`
	FilamentID   int       `json:"filament_id,omitempty"`
	ScannedHex   string    `json:"scanned_hex,omitempty"`
	ScannedTD    float64   `json:"scanned_td_mm,omitempty"`
	HasTD        bool      `json:"has_td,omitempty"`
	PreviousHex  string    `json:"previous_hex,omitempty"`
	PreviousTD   *float64  `json:"previous_td_mm,omitempty"` // pointer so "unset" is distinguishable from 0.0
	ColorWritten bool      `json:"color_written,omitempty"`
	TDWritten    bool      `json:"td_written,omitempty"`
	Action       string    `json:"action"` // "commit" | "skip" | "rescan" | "error"
	Error        string    `json:"error,omitempty"`
	RawCSV       string    `json:"raw_csv,omitempty"`
}

// handleScanHistoryPost appends one ScanEvent to scan-history.jsonl.
func (s *PlanServer) handleScanHistoryPost(w http.ResponseWriter, r *http.Request) {
	var ev ScanEvent
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if ev.Action == "" {
		http.Error(w, "action is required", http.StatusBadRequest)
		return
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}

	path := filepath.Join(s.PlansDir, "scan-history.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		http.Error(w, "failed to open scan history file", http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()

	if err := json.NewEncoder(f).Encode(ev); err != nil {
		http.Error(w, "failed to write scan history entry", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleScanHistoryGet returns scan events filtered by since/until/limit.
// Follows the same query-param shape as handleHistory for consistency.
func (s *PlanServer) handleScanHistoryGet(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(s.PlansDir, "scan-history.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
			return
		}
		http.Error(w, "failed to read scan history", http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()

	since := r.URL.Query().Get("since")
	until := r.URL.Query().Get("until")
	limitStr := r.URL.Query().Get("limit")

	var sinceTime, untilTime time.Time
	if since != "" {
		sinceTime, _ = time.Parse("2006-01-02", since)
	}
	if until != "" {
		untilTime, _ = time.Parse("2006-01-02", until)
		untilTime = untilTime.Add(24*time.Hour - time.Second)
	}
	limit := 0
	if limitStr != "" {
		limit, _ = strconv.Atoi(limitStr)
	}

	var events []ScanEvent
	scanner := bufio.NewScanner(f)
	// scan-history lines may contain longer raw_csv than the default 64KB buffer;
	// bump the max to 1MB defensively.
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var ev ScanEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if !sinceTime.IsZero() && ev.Timestamp.Before(sinceTime) {
			continue
		}
		if !untilTime.IsZero() && ev.Timestamp.After(untilTime) {
			continue
		}
		events = append(events, ev)
	}

	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(events)
}
