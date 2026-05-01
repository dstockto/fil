package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// validCauses is the closed enum of failure causes accepted by /api/v1/plan-fail.
var validCauses = map[string]struct{}{
	"bed_adhesion":    {},
	"spaghetti":       {},
	"layer_shift":     {},
	"blob_of_death":   {},
	"bad_first_layer": {},
	"warping":         {},
	"other":           {},
}

// PlanFailRequest is the body of POST /api/v1/plan-fail.
// Each plate in Plates becomes its own HistoryEntry with Failed=true sharing
// Cause/Reason/FailedAt across the batch.
type PlanFailRequest struct {
	Printer  string          `json:"printer,omitempty"`
	Cause    string          `json:"cause"`
	Reason   string          `json:"reason,omitempty"`
	FailedAt time.Time       `json:"failed_at,omitempty"`
	Plates   []PlanFailPlate `json:"plates"`
}

// PlanFailPlate is one plate within a batch failure.
type PlanFailPlate struct {
	Plan              string            `json:"plan"`
	Project           string            `json:"project"`
	Plate             string            `json:"plate"`
	StartedAt         string            `json:"started_at,omitempty"`
	EstimatedDuration string            `json:"estimated_duration,omitempty"`
	UsedGrams         float64           `json:"used_grams,omitempty"`
	Filament          []HistoryFilament `json:"filament,omitempty"`
}

// handlePlanFail appends one HistoryEntry per plate to print-history.jsonl,
// enriching each with prev_print and printer_idle_minutes_before derived from
// the existing log.
func (s *PlanServer) handlePlanFail(w http.ResponseWriter, r *http.Request) {
	var req PlanFailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Cause == "" {
		http.Error(w, "cause is required", http.StatusBadRequest)
		return
	}
	if _, ok := validCauses[req.Cause]; !ok {
		http.Error(w, fmt.Sprintf("invalid cause %q", req.Cause), http.StatusBadRequest)
		return
	}
	if len(req.Plates) == 0 {
		http.Error(w, "plates is required", http.StatusBadRequest)
		return
	}
	if req.FailedAt.IsZero() {
		req.FailedAt = time.Now().UTC()
	}

	historyPath := filepath.Join(s.PlansDir, "print-history.jsonl")

	// Derive prev_print + idle once per request from the existing log.
	prev, idleRef := derivePrevPrint(historyPath, req.Printer, req.FailedAt)

	entries := make([]HistoryEntry, 0, len(req.Plates))
	for _, p := range req.Plates {
		entry := HistoryEntry{
			Timestamp:         req.FailedAt.Format(time.RFC3339),
			Plan:              p.Plan,
			Project:           p.Project,
			Plate:             p.Plate,
			Printer:           req.Printer,
			StartedAt:         p.StartedAt,
			EstimatedDuration: p.EstimatedDuration,
			Filament:          p.Filament,
			Failed:            true,
			Cause:             req.Cause,
			Reason:            req.Reason,
			UsedGrams:         p.UsedGrams,
		}
		if prev != nil {
			cp := *prev
			entry.PrevPrint = &cp
		}
		if idleRef != nil {
			cp := *idleRef
			entry.PrinterIdleMinutesBefore = &cp
		}
		entries = append(entries, entry)
	}

	f, err := os.OpenFile(historyPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		http.Error(w, "failed to open history file", http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()

	enc := json.NewEncoder(f)
	for _, entry := range entries {
		if err := enc.Encode(entry); err != nil {
			http.Error(w, "failed to write history entry", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// derivePrevPrint scans the print-history JSONL for the most recent
// non-failed entry on the same printer that completed before failedAt.
// Returns (nil, nil) when no prior completion exists or the file is missing.
func derivePrevPrint(historyPath, printer string, failedAt time.Time) (*PrevPrint, *int) {
	if printer == "" {
		return nil, nil
	}
	f, err := os.Open(historyPath)
	if err != nil {
		return nil, nil
	}
	defer func() { _ = f.Close() }()

	var bestEntry *HistoryEntry
	var bestTime time.Time

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var e HistoryEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		if e.Failed {
			continue
		}
		if e.Printer != printer {
			continue
		}
		ts := completionTime(e)
		if ts.IsZero() || !ts.Before(failedAt) {
			continue
		}
		if bestEntry == nil || ts.After(bestTime) {
			cp := e
			bestEntry = &cp
			bestTime = ts
		}
	}
	if bestEntry == nil {
		return nil, nil
	}

	prev := &PrevPrint{
		Timestamp: bestTime.Format(time.RFC3339),
	}
	if len(bestEntry.Filament) > 0 {
		prev.Material = bestEntry.Filament[0].Material
		prev.Name = bestEntry.Filament[0].Name
	}

	idle := int(failedAt.Sub(bestTime).Minutes())
	return prev, &idle
}

// completionTime mirrors the client-side helper: prefer FinishedAt, fall back
// to Timestamp. Returns the zero time when neither parses.
func completionTime(e HistoryEntry) time.Time {
	if e.FinishedAt != "" {
		if t, err := time.Parse(time.RFC3339, e.FinishedAt); err == nil {
			return t
		}
	}
	if e.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339, e.Timestamp); err == nil {
			return t
		}
	}
	return time.Time{}
}
