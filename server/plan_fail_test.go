package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func postPlanFail(t *testing.T, s *PlanServer, req PlanFailRequest) *httptest.ResponseRecorder {
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

func TestPlanFailWritesPerPlateEntries(t *testing.T) {
	dir := t.TempDir()
	s := &PlanServer{PlansDir: dir}

	failedAt := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)
	req := PlanFailRequest{
		Printer:  "Bambu X1C",
		Cause:    "bed_adhesion",
		Reason:   "PETG residue suspected",
		FailedAt: failedAt,
		Plates: []PlanFailPlate{
			{Plan: "test.yaml", Project: "ProjA", Plate: "Plate 1", UsedGrams: 12.5, Filament: []HistoryFilament{{Name: "Polymaker PLA", Material: "PLA", Amount: 50}}},
			{Plan: "test.yaml", Project: "ProjA", Plate: "Plate 2", UsedGrams: 7.5, Filament: []HistoryFilament{{Name: "Polymaker PLA", Material: "PLA", Amount: 30}}},
		},
	}

	w := postPlanFail(t, s, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %q", w.Code, w.Body.String())
	}

	entries := readEntries(t, filepath.Join(dir, "print-history.jsonl"))
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	for i, e := range entries {
		if !e.Failed {
			t.Errorf("entry %d: Failed = false, want true", i)
		}
		if e.Cause != "bed_adhesion" {
			t.Errorf("entry %d: Cause = %q, want bed_adhesion", i, e.Cause)
		}
		if e.Reason != "PETG residue suspected" {
			t.Errorf("entry %d: Reason = %q", i, e.Reason)
		}
		if e.Printer != "Bambu X1C" {
			t.Errorf("entry %d: Printer = %q", i, e.Printer)
		}
		if e.Timestamp == "" {
			t.Errorf("entry %d: Timestamp empty", i)
		}
	}
	if entries[0].UsedGrams != 12.5 || entries[1].UsedGrams != 7.5 {
		t.Errorf("UsedGrams not preserved per plate: %+v", entries)
	}
}

func TestPlanFailRejectsInvalidCause(t *testing.T) {
	dir := t.TempDir()
	s := &PlanServer{PlansDir: dir}

	cases := []struct {
		name string
		req  PlanFailRequest
	}{
		{"empty cause", PlanFailRequest{Plates: []PlanFailPlate{{Plan: "x", Project: "p", Plate: "1"}}}},
		{"unknown cause", PlanFailRequest{Cause: "operator_error", Plates: []PlanFailPlate{{Plan: "x", Project: "p", Plate: "1"}}}},
		{"no plates", PlanFailRequest{Cause: "bed_adhesion"}},
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

func TestPlanFailDerivesPrevPrint(t *testing.T) {
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "print-history.jsonl")

	// Seed history with two prior completions on the same printer.
	prior := []HistoryEntry{
		{
			Timestamp:  "2026-04-30T10:00:00Z",
			FinishedAt: "2026-04-30T10:00:00Z",
			Plan:       "old.yaml", Project: "Old", Plate: "P1",
			Printer:  "Bambu X1C",
			Filament: []HistoryFilament{{Name: "Bambu PETG Basic", Material: "PETG"}},
		},
		{
			Timestamp:  "2026-04-30T13:00:00Z",
			FinishedAt: "2026-04-30T13:00:00Z",
			Plan:       "old.yaml", Project: "Old", Plate: "P2",
			Printer:  "Bambu X1C",
			Filament: []HistoryFilament{{Name: "Polymaker PolyTerra Matte PLA", Material: "PLA"}},
		},
		// Decoy: different printer, more recent.
		{
			Timestamp:  "2026-04-30T13:30:00Z",
			FinishedAt: "2026-04-30T13:30:00Z",
			Plan:       "old.yaml", Project: "Old", Plate: "P3",
			Printer:  "Prusa XL",
			Filament: []HistoryFilament{{Name: "Prusa PLA", Material: "PLA"}},
		},
	}
	f, err := os.Create(historyPath)
	if err != nil {
		t.Fatalf("create history: %v", err)
	}
	enc := json.NewEncoder(f)
	for _, e := range prior {
		_ = enc.Encode(e)
	}
	_ = f.Close()

	s := &PlanServer{PlansDir: dir}
	failedAt := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)
	req := PlanFailRequest{
		Printer:  "Bambu X1C",
		Cause:    "bed_adhesion",
		FailedAt: failedAt,
		Plates: []PlanFailPlate{
			{Plan: "new.yaml", Project: "New", Plate: "P1"},
		},
	}
	w := postPlanFail(t, s, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %q", w.Code, w.Body.String())
	}

	entries := readEntries(t, historyPath)
	failure := entries[len(entries)-1]
	if failure.PrevPrint == nil {
		t.Fatalf("PrevPrint = nil, want most-recent prior on Bambu X1C")
	}
	if failure.PrevPrint.Material != "PLA" || failure.PrevPrint.Name != "Polymaker PolyTerra Matte PLA" {
		t.Errorf("PrevPrint = %+v, want PLA / Polymaker PolyTerra Matte PLA (most recent on same printer)", failure.PrevPrint)
	}
	if failure.PrinterIdleMinutesBefore == nil {
		t.Fatal("PrinterIdleMinutesBefore = nil")
	}
	if got := *failure.PrinterIdleMinutesBefore; got != 60 {
		t.Errorf("PrinterIdleMinutesBefore = %d, want 60", got)
	}
}

func TestPlanFailNoPrevPrintWhenNoneOnPrinter(t *testing.T) {
	dir := t.TempDir()
	s := &PlanServer{PlansDir: dir}

	req := PlanFailRequest{
		Printer:  "Bambu X1C",
		Cause:    "bed_adhesion",
		FailedAt: time.Now().UTC(),
		Plates: []PlanFailPlate{
			{Plan: "new.yaml", Project: "New", Plate: "P1"},
		},
	}
	w := postPlanFail(t, s, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d", w.Code)
	}
	entries := readEntries(t, filepath.Join(dir, "print-history.jsonl"))
	if entries[0].PrevPrint != nil {
		t.Errorf("PrevPrint = %+v, want nil for empty history", entries[0].PrevPrint)
	}
	if entries[0].PrinterIdleMinutesBefore != nil {
		t.Errorf("PrinterIdleMinutesBefore = %d, want nil", *entries[0].PrinterIdleMinutesBefore)
	}
}

func TestPlanFailIgnoresPriorFailuresWhenDerivingPrev(t *testing.T) {
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "print-history.jsonl")

	prior := []HistoryEntry{
		{
			Timestamp:  "2026-04-30T10:00:00Z",
			FinishedAt: "2026-04-30T10:00:00Z",
			Plan:       "old.yaml", Project: "Old", Plate: "P1",
			Printer:  "Bambu X1C",
			Filament: []HistoryFilament{{Name: "Bambu PLA Basic", Material: "PLA"}},
		},
		// Earlier failure should NOT be picked as prev_print.
		{
			Timestamp: "2026-04-30T13:00:00Z",
			Plan:      "old.yaml", Project: "Old", Plate: "P2",
			Printer: "Bambu X1C",
			Failed:  true, Cause: "spaghetti",
		},
	}
	f, err := os.Create(historyPath)
	if err != nil {
		t.Fatalf("create history: %v", err)
	}
	enc := json.NewEncoder(f)
	for _, e := range prior {
		_ = enc.Encode(e)
	}
	_ = f.Close()

	s := &PlanServer{PlansDir: dir}
	req := PlanFailRequest{
		Printer:  "Bambu X1C",
		Cause:    "warping",
		FailedAt: time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC),
		Plates:   []PlanFailPlate{{Plan: "n.yaml", Project: "N", Plate: "1"}},
	}
	w := postPlanFail(t, s, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d", w.Code)
	}
	entries := readEntries(t, historyPath)
	failure := entries[len(entries)-1]
	if failure.PrevPrint == nil {
		t.Fatal("PrevPrint = nil; expected the 10:00 PLA completion (failures must be skipped)")
	}
	if failure.PrevPrint.Name != "Bambu PLA Basic" {
		t.Errorf("PrevPrint.Name = %q, want Bambu PLA Basic", failure.PrevPrint.Name)
	}
}
