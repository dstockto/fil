package cmd

import (
	"reflect"
	"testing"

	"github.com/dstockto/fil/models"
)

// Tests for allocateShares and findPrinterSpool moved to plan/ package along
// with the functions themselves. This file keeps tests for cmd-level helpers
// (parsePlateSelection, collectFailableInProgress).

func TestParsePlateSelection(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		n       int
		want    []int
		wantErr bool
	}{
		{"single", "1", 3, []int{0}, false},
		{"comma", "1,3", 3, []int{0, 2}, false},
		{"range", "1-3", 3, []int{0, 1, 2}, false},
		{"all", "all", 3, []int{0, 1, 2}, false},
		{"all upper", "ALL", 3, []int{0, 1, 2}, false},
		{"mixed", "1,2-3", 4, []int{0, 1, 2}, false},
		{"dedup", "1,1,2", 3, []int{0, 1}, false},
		{"out of range", "5", 3, nil, true},
		{"zero", "0", 3, nil, true},
		{"empty", "", 3, nil, true},
		{"bad token", "abc", 3, nil, true},
		{"reversed range", "3-1", 3, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePlateSelection(tc.input, tc.n)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCollectFailableInProgressFiltersByStatusAndPrinter(t *testing.T) {
	plans := []DiscoveredPlan{
		{
			DisplayName: "p.yaml",
			Plan: models.PlanFile{
				Projects: []models.Project{{
					Name: "Proj",
					Plates: []models.Plate{
						{Name: "A", Status: "in-progress", Printer: "Bambu X1C"},
						{Name: "B", Status: "todo"},        // skipped: not in-progress
						{Name: "C", Status: "in-progress"}, // skipped: no printer
						{Name: "D", Status: "in-progress", Printer: "Prusa XL"},
					},
				}},
			},
		},
	}
	got := collectFailableInProgress(plans)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2 (A and D only)", len(got))
	}
	gotPlates := []string{got[0].plate, got[1].plate}
	if !reflect.DeepEqual(gotPlates, []string{"A", "D"}) {
		t.Errorf("got plates %v, want [A D]", gotPlates)
	}
}

