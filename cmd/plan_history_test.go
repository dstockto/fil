package cmd

import (
	"reflect"
	"testing"
	"time"

	"github.com/dstockto/fil/api"
)

func TestMergeIntervals(t *testing.T) {
	tz := time.FixedZone("test", 0)
	at := func(layout string) time.Time {
		ts, err := time.ParseInLocation("2006-01-02 15:04", layout, tz)
		if err != nil {
			t.Fatalf("parse %q: %v", layout, err)
		}
		return ts
	}
	iv := func(s, e string) interval { return interval{start: at(s), end: at(e)} }

	tests := []struct {
		name string
		in   []interval
		want []interval
	}{
		{
			name: "empty",
			in:   nil,
			want: nil,
		},
		{
			name: "disjoint",
			in: []interval{
				iv("2026-04-14 09:00", "2026-04-14 10:00"),
				iv("2026-04-14 12:00", "2026-04-14 13:00"),
			},
			want: []interval{
				iv("2026-04-14 09:00", "2026-04-14 10:00"),
				iv("2026-04-14 12:00", "2026-04-14 13:00"),
			},
		},
		{
			name: "overlapping merges into union",
			in: []interval{
				iv("2026-04-14 09:00", "2026-04-14 11:00"),
				iv("2026-04-14 10:00", "2026-04-14 12:00"),
			},
			want: []interval{iv("2026-04-14 09:00", "2026-04-14 12:00")},
		},
		{
			name: "three near-identical windows collapse to one",
			in: []interval{
				iv("2026-04-13 23:34", "2026-04-14 07:25"),
				iv("2026-04-13 23:34", "2026-04-14 07:25"),
				iv("2026-04-13 23:34", "2026-04-14 07:25"),
			},
			want: []interval{iv("2026-04-13 23:34", "2026-04-14 07:25")},
		},
		{
			name: "touching intervals merge",
			in: []interval{
				iv("2026-04-14 09:00", "2026-04-14 10:00"),
				iv("2026-04-14 10:00", "2026-04-14 11:00"),
			},
			want: []interval{iv("2026-04-14 09:00", "2026-04-14 11:00")},
		},
		{
			name: "unsorted input is sorted",
			in: []interval{
				iv("2026-04-14 12:00", "2026-04-14 13:00"),
				iv("2026-04-14 09:00", "2026-04-14 10:00"),
			},
			want: []interval{
				iv("2026-04-14 09:00", "2026-04-14 10:00"),
				iv("2026-04-14 12:00", "2026-04-14 13:00"),
			},
		},
		{
			name: "contained interval does not extend outer",
			in: []interval{
				iv("2026-04-14 09:00", "2026-04-14 15:00"),
				iv("2026-04-14 10:00", "2026-04-14 11:00"),
			},
			want: []interval{iv("2026-04-14 09:00", "2026-04-14 15:00")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeIntervals(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompletionTimePrefersFinishedAt(t *testing.T) {
	finished := "2026-04-18T14:30:00Z"
	saved := "2026-04-18T18:45:00Z"

	tests := []struct {
		name string
		e    api.HistoryEntry
		want string
	}{
		{
			name: "uses FinishedAt when present",
			e:    api.HistoryEntry{Timestamp: saved, FinishedAt: finished},
			want: finished,
		},
		{
			name: "falls back to Timestamp when FinishedAt empty",
			e:    api.HistoryEntry{Timestamp: saved},
			want: saved,
		},
		{
			name: "falls back to Timestamp when FinishedAt is whitespace-equivalent empty",
			e:    api.HistoryEntry{Timestamp: saved, FinishedAt: ""},
			want: saved,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := completionTime(tt.e)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want, _ := time.Parse(time.RFC3339, tt.want)
			if !got.Equal(want) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

func TestBuildDailySummaryNoZeroPrintRows(t *testing.T) {
	tz := time.FixedZone("test", 0)
	rfc := func(s string) string {
		ts, err := time.ParseInLocation("2006-01-02 15:04", s, tz)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return ts.Format(time.RFC3339)
	}

	t.Run("single entry spanning multiple days produces one row on completion day", func(t *testing.T) {
		entries := []api.HistoryEntry{{
			Printer:    "X1C",
			StartedAt:  rfc("2026-05-09 14:00"),
			FinishedAt: rfc("2026-05-13 14:00"),
			Filament:   []api.HistoryFilament{{Amount: 100}},
		}}
		got := buildDailySummary(entries)
		if len(got) != 1 {
			t.Fatalf("got %d rows, want 1: %+v", len(got), got)
		}
		if got[0].date != "2026-05-13" {
			t.Errorf("date: got %s, want 2026-05-13", got[0].date)
		}
		if got[0].prints != 1 {
			t.Errorf("prints: got %d, want 1", got[0].prints)
		}
		wantDur := 4 * 24 * time.Hour
		if got[0].duration != wantDur {
			t.Errorf("duration: got %v, want %v", got[0].duration, wantDur)
		}
		if got[0].filament != 100 {
			t.Errorf("filament: got %v, want 100", got[0].filament)
		}
	})

	t.Run("two entries on different days produce two rows", func(t *testing.T) {
		entries := []api.HistoryEntry{
			{
				Printer:    "X1C",
				StartedAt:  rfc("2026-05-08 09:00"),
				FinishedAt: rfc("2026-05-08 11:00"),
				Filament:   []api.HistoryFilament{{Amount: 25}},
			},
			{
				Printer:    "X1C",
				StartedAt:  rfc("2026-05-10 14:00"),
				FinishedAt: rfc("2026-05-10 18:00"),
				Filament:   []api.HistoryFilament{{Amount: 60}},
			},
		}
		got := buildDailySummary(entries)
		if len(got) != 2 {
			t.Fatalf("got %d rows, want 2: %+v", len(got), got)
		}
		// Sorted ascending by date.
		if got[0].date != "2026-05-08" {
			t.Errorf("rows[0].date: got %s, want 2026-05-08", got[0].date)
		}
		if got[0].duration != 2*time.Hour {
			t.Errorf("rows[0].duration: got %v, want 2h", got[0].duration)
		}
		if got[0].filament != 25 {
			t.Errorf("rows[0].filament: got %v, want 25", got[0].filament)
		}
		if got[1].date != "2026-05-10" {
			t.Errorf("rows[1].date: got %s, want 2026-05-10", got[1].date)
		}
		if got[1].duration != 4*time.Hour {
			t.Errorf("rows[1].duration: got %v, want 4h", got[1].duration)
		}
		if got[1].filament != 60 {
			t.Errorf("rows[1].filament: got %v, want 60", got[1].filament)
		}
		// Critically: no row for 2026-05-09 (the gap day with no completions).
	})

	t.Run("two overlapping entries same printer same day merged once", func(t *testing.T) {
		// A multi-plate batch print: both plates share the same wall-clock
		// window on the same printer. mergeIntervals collapses them so the
		// shared duration isn't counted twice.
		entries := []api.HistoryEntry{
			{
				Printer:    "X1C",
				StartedAt:  rfc("2026-05-09 09:00"),
				FinishedAt: rfc("2026-05-09 12:00"),
				Filament:   []api.HistoryFilament{{Amount: 30}},
			},
			{
				Printer:    "X1C",
				StartedAt:  rfc("2026-05-09 10:00"),
				FinishedAt: rfc("2026-05-09 11:00"),
				Filament:   []api.HistoryFilament{{Amount: 20}},
			},
		}
		got := buildDailySummary(entries)
		if len(got) != 1 {
			t.Fatalf("got %d rows, want 1: %+v", len(got), got)
		}
		if got[0].date != "2026-05-09" {
			t.Errorf("date: got %s, want 2026-05-09", got[0].date)
		}
		if got[0].prints != 2 {
			t.Errorf("prints: got %d, want 2", got[0].prints)
		}
		// Merged interval is 09:00–12:00 = 3h, not 3h+1h = 4h.
		wantDur := 3 * time.Hour
		if got[0].duration != wantDur {
			t.Errorf("duration: got %v, want %v (should not double-count overlap)", got[0].duration, wantDur)
		}
		if got[0].filament != 50 {
			t.Errorf("filament: got %v, want 50", got[0].filament)
		}
	})
}

func TestCalcDurationUsesFinishedAt(t *testing.T) {
	// Print actually finished at 12:00 (printer-reported); user ran `fil p c`
	// 4 hours later at 16:00 (save-time). Duration should reflect 12:00, not 16:00.
	e := api.HistoryEntry{
		StartedAt:  "2026-04-18T08:00:00Z",
		FinishedAt: "2026-04-18T12:00:00Z",
		Timestamp:  "2026-04-18T16:00:00Z",
	}
	got := calcDuration(e)
	want := 4 * time.Hour
	if got != want {
		t.Errorf("with FinishedAt: got %v, want %v", got, want)
	}

	// Without FinishedAt, falls back to Timestamp = 16:00, so duration is 8h.
	e.FinishedAt = ""
	got = calcDuration(e)
	want = 8 * time.Hour
	if got != want {
		t.Errorf("fallback to Timestamp: got %v, want %v", got, want)
	}
}
