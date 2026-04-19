package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dstockto/fil/models"
)

// HistoryFilament records filament usage for a completed plate.
type HistoryFilament struct {
	Name       string  `json:"name,omitempty"`
	FilamentID int     `json:"filament_id,omitempty"`
	Material   string  `json:"material,omitempty"`
	Amount     float64 `json:"amount"`
}

// HistoryEntry records a single plate completion event.
type HistoryEntry struct {
	Timestamp         string            `json:"timestamp"`              // when fil recorded the entry (save-time)
	FinishedAt        string            `json:"finished_at,omitempty"`  // when the printer reported FINISH; empty when no live printer data was available
	Plan              string            `json:"plan"`
	Project           string            `json:"project"`
	Plate             string            `json:"plate"`
	Printer           string            `json:"printer,omitempty"`
	StartedAt         string            `json:"started_at,omitempty"`
	EstimatedDuration string            `json:"estimated_duration,omitempty"`
	Filament          []HistoryFilament `json:"filament,omitempty"`
}

// logCompletions compares old and new plan states and appends history entries
// for any plates that transitioned to "completed".
func (s *PlanServer) logCompletions(planName string, oldPlan, newPlan *models.PlanFile) {
	if oldPlan == nil || newPlan == nil {
		return
	}

	// Build a set of old plate statuses keyed by project+plate name
	type plateID struct {
		project string
		plate   string
	}
	oldStatus := make(map[plateID]string)
	oldPlates := make(map[plateID]models.Plate)
	for _, proj := range oldPlan.Projects {
		for _, plate := range proj.Plates {
			key := plateID{project: proj.Name, plate: plate.Name}
			oldStatus[key] = plate.Status
			oldPlates[key] = plate
		}
	}

	var entries []HistoryEntry
	for _, proj := range newPlan.Projects {
		for _, plate := range proj.Plates {
			if plate.Status != "completed" {
				continue
			}
			key := plateID{project: proj.Name, plate: plate.Name}
			prev, exists := oldStatus[key]
			if !exists || prev == "completed" {
				continue // new plate or already completed
			}

			// Use the old plate's printer/time info since those get cleared on completion
			old := oldPlates[key]
			printer := plate.Printer
			if printer == "" {
				printer = old.Printer
			}
			startedAt := plate.StartedAt
			if startedAt == "" {
				startedAt = old.StartedAt
			}
			estimatedDuration := plate.EstimatedDuration
			if estimatedDuration == "" {
				estimatedDuration = old.EstimatedDuration
			}

			var filament []HistoryFilament
			for _, need := range plate.Needs {
				filament = append(filament, HistoryFilament{
					Name:       need.Name,
					FilamentID: need.FilamentID,
					Material:   need.Material,
					Amount:     need.Amount,
				})
			}

			finishedAt := ""
			if printer != "" && s.Printers != nil {
				if t, ok := s.Printers.LastFinishedAt(printer); ok {
					finishedAt = t.Format(time.RFC3339)
				}
			}

			entries = append(entries, HistoryEntry{
				Timestamp:         time.Now().Format(time.RFC3339),
				FinishedAt:        finishedAt,
				Plan:              planName,
				Project:           proj.Name,
				Plate:             plate.Name,
				Printer:           printer,
				StartedAt:         startedAt,
				EstimatedDuration: estimatedDuration,
				Filament:          filament,
			})
		}
	}

	if len(entries) == 0 {
		return
	}

	historyPath := filepath.Join(s.PlansDir, "print-history.jsonl")
	f, err := os.OpenFile(historyPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, entry := range entries {
		_ = enc.Encode(entry)
	}
}

// handleHistory serves the print history with optional filters.
func (s *PlanServer) handleHistory(w http.ResponseWriter, r *http.Request) {
	historyPath := filepath.Join(s.PlansDir, "print-history.jsonl")

	f, err := os.Open(historyPath)
	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
			return
		}
		http.Error(w, "failed to read history", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	// Parse query filters
	since := r.URL.Query().Get("since")
	until := r.URL.Query().Get("until")
	printer := r.URL.Query().Get("printer")
	limitStr := r.URL.Query().Get("limit")

	var sinceTime, untilTime time.Time
	if since != "" {
		sinceTime, _ = time.Parse("2006-01-02", since)
	}
	if until != "" {
		untilTime, _ = time.Parse("2006-01-02", until)
		// Include the entire "until" day
		untilTime = untilTime.Add(24*time.Hour - time.Second)
	}

	limit := 0
	if limitStr != "" {
		limit, _ = strconv.Atoi(limitStr)
	}

	var entries []HistoryEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry HistoryEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		// Apply filters
		if printer != "" && !strings.EqualFold(entry.Printer, printer) {
			continue
		}

		if !sinceTime.IsZero() || !untilTime.IsZero() {
			ts, err := time.Parse(time.RFC3339, entry.Timestamp)
			if err != nil {
				continue
			}
			if !sinceTime.IsZero() && ts.Before(sinceTime) {
				continue
			}
			if !untilTime.IsZero() && ts.After(untilTime) {
				continue
			}
		}

		entries = append(entries, entry)
	}

	// Apply limit (from the end — most recent)
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entries)
}
