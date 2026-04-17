package cmd

import (
	"reflect"
	"testing"
	"time"
)

func TestSplitDurationByDay(t *testing.T) {
	tz := time.FixedZone("test", -5*3600) // stable offset, no DST
	at := func(layout string) time.Time {
		ts, err := time.ParseInLocation("2006-01-02 15:04", layout, tz)
		if err != nil {
			t.Fatalf("parse %q: %v", layout, err)
		}
		return ts
	}

	tests := []struct {
		name  string
		start time.Time
		end   time.Time
		want  map[string]time.Duration
	}{
		{
			name:  "single day",
			start: at("2026-04-16 09:00"),
			end:   at("2026-04-16 14:30"),
			want:  map[string]time.Duration{"2026-04-16": 5*time.Hour + 30*time.Minute},
		},
		{
			name:  "crosses midnight once",
			start: at("2026-04-15 22:00"),
			end:   at("2026-04-16 03:00"),
			want: map[string]time.Duration{
				"2026-04-15": 2 * time.Hour,
				"2026-04-16": 3 * time.Hour,
			},
		},
		{
			name:  "spans three days",
			start: at("2026-04-14 23:00"),
			end:   at("2026-04-17 02:30"),
			want: map[string]time.Duration{
				"2026-04-14": 1 * time.Hour,
				"2026-04-15": 24 * time.Hour,
				"2026-04-16": 24 * time.Hour,
				"2026-04-17": 2*time.Hour + 30*time.Minute,
			},
		},
		{
			name:  "starts exactly at midnight",
			start: at("2026-04-16 00:00"),
			end:   at("2026-04-16 08:00"),
			want:  map[string]time.Duration{"2026-04-16": 8 * time.Hour},
		},
		{
			name:  "ends exactly at midnight",
			start: at("2026-04-15 20:00"),
			end:   at("2026-04-16 00:00"),
			want:  map[string]time.Duration{"2026-04-15": 4 * time.Hour},
		},
		{
			name:  "zero-length interval",
			start: at("2026-04-16 09:00"),
			end:   at("2026-04-16 09:00"),
			want:  map[string]time.Duration{},
		},
		{
			name:  "end before start",
			start: at("2026-04-16 12:00"),
			end:   at("2026-04-16 09:00"),
			want:  map[string]time.Duration{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitDurationByDay(tt.start, tt.end)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d buckets, want %d: got=%v want=%v", len(got), len(tt.want), got, tt.want)
			}
			for date, want := range tt.want {
				if got[date] != want {
					t.Errorf("bucket %s: got %v, want %v", date, got[date], want)
				}
			}
			// Sum must equal end.Sub(start) whenever end>start.
			if tt.end.After(tt.start) {
				var sum time.Duration
				for _, d := range got {
					sum += d
				}
				if sum != tt.end.Sub(tt.start) {
					t.Errorf("sum of buckets %v != total %v", sum, tt.end.Sub(tt.start))
				}
			}
		})
	}
}

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
