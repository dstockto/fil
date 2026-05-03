package plan

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileHistoryWriter appends fail history records to print-history.jsonl in
// the configured plans dir. Each call appends one JSON object per line per
// plate. Prev-print enrichment (which earlier completion this failure
// followed on the same printer) is computed by reading back the file before
// writing, mirroring the server's existing behaviour.
type FileHistoryWriter struct {
	HistoryPath string
}

// NewFileHistoryWriter writes to <plansDir>/print-history.jsonl.
func NewFileHistoryWriter(plansDir string) *FileHistoryWriter {
	return &FileHistoryWriter{HistoryPath: filepath.Join(plansDir, "print-history.jsonl")}
}

// AppendFail writes one JSON entry per plate to the history file. The on-disk
// shape matches server.HistoryEntry — both readers (the GET /history endpoint,
// downstream analysis) consume the same format.
func (w *FileHistoryWriter) AppendFail(_ context.Context, entries []FailHistoryEntry) error {
	if len(entries) == 0 {
		return nil
	}

	prev, idle := derivePrevPrint(w.HistoryPath, entries[0].Printer, entries[0].Timestamp)

	f, err := os.OpenFile(w.HistoryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open history: %w", err)
	}
	defer func() { _ = f.Close() }()

	enc := json.NewEncoder(f)
	for _, e := range entries {
		on := onDiskEntry{
			Timestamp:                e.Timestamp.UTC().Format(time.RFC3339),
			Plan:                     e.Plan,
			Project:                  e.Project,
			Plate:                    e.Plate,
			Printer:                  e.Printer,
			StartedAt:                e.StartedAt,
			EstimatedDuration:        e.EstimatedDuration,
			Failed:                   true,
			Cause:                    e.Cause,
			Reason:                   e.Reason,
			UsedGrams:                e.UsedGrams,
			PrevPrint:                prev,
			PrinterIdleMinutesBefore: idle,
		}
		for _, fil := range e.Filament {
			on.Filament = append(on.Filament, onDiskFilament(fil))
		}
		if err := enc.Encode(on); err != nil {
			return fmt.Errorf("encode entry: %w", err)
		}
	}
	return nil
}

// onDiskEntry mirrors server.HistoryEntry's JSON tags exactly so the existing
// readers (GET /history, doctor checks, analysis scripts) keep working
// unchanged. Treat field names as load-bearing.
type onDiskEntry struct {
	Timestamp                string           `json:"timestamp"`
	FinishedAt               string           `json:"finished_at,omitempty"`
	Plan                     string           `json:"plan"`
	Project                  string           `json:"project"`
	Plate                    string           `json:"plate"`
	Printer                  string           `json:"printer,omitempty"`
	StartedAt                string           `json:"started_at,omitempty"`
	EstimatedDuration        string           `json:"estimated_duration,omitempty"`
	Filament                 []onDiskFilament `json:"filament,omitempty"`
	Failed                   bool             `json:"failed,omitempty"`
	Cause                    string           `json:"cause,omitempty"`
	Reason                   string           `json:"reason,omitempty"`
	UsedGrams                float64          `json:"used_grams,omitempty"`
	PrevPrint                *onDiskPrev      `json:"prev_print,omitempty"`
	PrinterIdleMinutesBefore *int             `json:"printer_idle_minutes_before,omitempty"`
}

type onDiskFilament struct {
	Name       string  `json:"name,omitempty"`
	FilamentID int     `json:"filament_id,omitempty"`
	Material   string  `json:"material,omitempty"`
	Amount     float64 `json:"amount"`
}

type onDiskPrev struct {
	Timestamp string `json:"timestamp"`
	Material  string `json:"material,omitempty"`
	Name      string `json:"name,omitempty"`
}

// derivePrevPrint scans the existing print-history JSONL for the most recent
// non-failed entry on the same printer that completed before failedAt. Returns
// (nil, nil) when no prior completion exists or the file is missing.
//
// Ported from server/plan_fail.go so the FileHistoryWriter is the single owner
// of the on-disk format and its enrichment.
func derivePrevPrint(historyPath, printer string, failedAt time.Time) (*onDiskPrev, *int) {
	if printer == "" {
		return nil, nil
	}
	f, err := os.Open(historyPath)
	if err != nil {
		return nil, nil
	}
	defer func() { _ = f.Close() }()

	var bestEntry *onDiskEntry
	var bestTime time.Time

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var e onDiskEntry
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

	prev := &onDiskPrev{Timestamp: bestTime.Format(time.RFC3339)}
	if len(bestEntry.Filament) > 0 {
		prev.Material = bestEntry.Filament[0].Material
		prev.Name = bestEntry.Filament[0].Name
	}
	idle := int(failedAt.Sub(bestTime).Minutes())
	return prev, &idle
}

func completionTime(e onDiskEntry) time.Time {
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
