package plan

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func readDiskEntries(t *testing.T, path string) []onDiskEntry {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()

	var entries []onDiskEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e onDiskEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		entries = append(entries, e)
	}
	return entries
}

func TestFileHistoryWritePerPlateFields(t *testing.T) {
	dir := t.TempDir()
	w := NewFileHistoryWriter(dir)

	failedAt := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)
	entries := []FailHistoryEntry{
		{
			Timestamp: failedAt, Plan: "test.yaml", Project: "ProjA", Plate: "Plate 1",
			Printer: "Bambu X1C", Cause: "bed_adhesion", Reason: "PETG residue suspected",
			UsedGrams: 12.5,
			Filament:  []HistoryFilament{{Name: "Polymaker PLA", Material: "PLA", Amount: 50}},
		},
		{
			Timestamp: failedAt, Plan: "test.yaml", Project: "ProjA", Plate: "Plate 2",
			Printer: "Bambu X1C", Cause: "bed_adhesion", Reason: "PETG residue suspected",
			UsedGrams: 7.5,
			Filament:  []HistoryFilament{{Name: "Polymaker PLA", Material: "PLA", Amount: 30}},
		},
	}
	if err := w.AppendFail(context.Background(), entries); err != nil {
		t.Fatalf("AppendFail: %v", err)
	}

	got := readDiskEntries(t, filepath.Join(dir, "print-history.jsonl"))
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	for i, e := range got {
		if !e.Failed {
			t.Errorf("entry %d: Failed = false", i)
		}
		if e.Cause != "bed_adhesion" {
			t.Errorf("entry %d: Cause = %q", i, e.Cause)
		}
		if e.Printer != "Bambu X1C" {
			t.Errorf("entry %d: Printer = %q", i, e.Printer)
		}
	}
	if got[0].UsedGrams != 12.5 || got[1].UsedGrams != 7.5 {
		t.Errorf("UsedGrams not preserved: %+v", got)
	}
}

func TestFileHistoryDerivesPrevPrint(t *testing.T) {
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "print-history.jsonl")

	prior := []onDiskEntry{
		{
			Timestamp: "2026-04-30T10:00:00Z", FinishedAt: "2026-04-30T10:00:00Z",
			Plan: "old.yaml", Project: "Old", Plate: "P1", Printer: "Bambu X1C",
			Filament: []onDiskFilament{{Name: "Bambu PETG Basic", Material: "PETG"}},
		},
		{
			Timestamp: "2026-04-30T13:00:00Z", FinishedAt: "2026-04-30T13:00:00Z",
			Plan: "old.yaml", Project: "Old", Plate: "P2", Printer: "Bambu X1C",
			Filament: []onDiskFilament{{Name: "Polymaker PolyTerra Matte PLA", Material: "PLA"}},
		},
		// Decoy on different printer, more recent.
		{
			Timestamp: "2026-04-30T13:30:00Z", FinishedAt: "2026-04-30T13:30:00Z",
			Plan: "old.yaml", Project: "Old", Plate: "P3", Printer: "Prusa XL",
			Filament: []onDiskFilament{{Name: "Prusa PLA", Material: "PLA"}},
		},
	}
	seedHistory(t, historyPath, prior)

	failedAt := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)
	w := NewFileHistoryWriter(dir)
	err := w.AppendFail(context.Background(), []FailHistoryEntry{
		{Timestamp: failedAt, Plan: "new.yaml", Project: "New", Plate: "P1", Printer: "Bambu X1C", Cause: "bed_adhesion"},
	})
	if err != nil {
		t.Fatalf("AppendFail: %v", err)
	}

	all := readDiskEntries(t, historyPath)
	failure := all[len(all)-1]
	if failure.PrevPrint == nil {
		t.Fatal("PrevPrint = nil, want most-recent prior on Bambu X1C")
	}
	if failure.PrevPrint.Material != "PLA" || failure.PrevPrint.Name != "Polymaker PolyTerra Matte PLA" {
		t.Errorf("PrevPrint = %+v, want PLA / Polymaker PolyTerra Matte PLA", failure.PrevPrint)
	}
	if failure.PrinterIdleMinutesBefore == nil {
		t.Fatal("PrinterIdleMinutesBefore = nil")
	}
	if got := *failure.PrinterIdleMinutesBefore; got != 60 {
		t.Errorf("PrinterIdleMinutesBefore = %d, want 60", got)
	}
}

func TestFileHistoryNoPrevPrintWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	w := NewFileHistoryWriter(dir)
	err := w.AppendFail(context.Background(), []FailHistoryEntry{
		{Timestamp: time.Now().UTC(), Plan: "new.yaml", Project: "New", Plate: "P1", Printer: "Bambu X1C", Cause: "bed_adhesion"},
	})
	if err != nil {
		t.Fatalf("AppendFail: %v", err)
	}
	got := readDiskEntries(t, filepath.Join(dir, "print-history.jsonl"))
	if got[0].PrevPrint != nil {
		t.Errorf("PrevPrint = %+v, want nil for empty history", got[0].PrevPrint)
	}
	if got[0].PrinterIdleMinutesBefore != nil {
		t.Errorf("PrinterIdleMinutesBefore = %d, want nil", *got[0].PrinterIdleMinutesBefore)
	}
}

func TestFileHistoryIgnoresPriorFailures(t *testing.T) {
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "print-history.jsonl")

	prior := []onDiskEntry{
		{
			Timestamp: "2026-04-30T10:00:00Z", FinishedAt: "2026-04-30T10:00:00Z",
			Plan: "old.yaml", Project: "Old", Plate: "P1", Printer: "Bambu X1C",
			Filament: []onDiskFilament{{Name: "Bambu PLA Basic", Material: "PLA"}},
		},
		// Earlier failure — must NOT be picked.
		{
			Timestamp: "2026-04-30T13:00:00Z",
			Plan: "old.yaml", Project: "Old", Plate: "P2", Printer: "Bambu X1C",
			Failed: true, Cause: "spaghetti",
		},
	}
	seedHistory(t, historyPath, prior)

	w := NewFileHistoryWriter(dir)
	err := w.AppendFail(context.Background(), []FailHistoryEntry{
		{
			Timestamp: time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC),
			Plan:      "n.yaml", Project: "N", Plate: "1",
			Printer: "Bambu X1C", Cause: "warping",
		},
	})
	if err != nil {
		t.Fatalf("AppendFail: %v", err)
	}

	all := readDiskEntries(t, historyPath)
	failure := all[len(all)-1]
	if failure.PrevPrint == nil {
		t.Fatal("PrevPrint = nil; expected the 10:00 PLA completion")
	}
	if failure.PrevPrint.Name != "Bambu PLA Basic" {
		t.Errorf("PrevPrint.Name = %q, want Bambu PLA Basic", failure.PrevPrint.Name)
	}
}

func seedHistory(t *testing.T, path string, entries []onDiskEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create history: %v", err)
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
}
