package cmd

import (
	"reflect"
	"testing"

	"github.com/dstockto/fil/models"
)

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

func TestAllocateSharesProportional(t *testing.T) {
	plates := []failPlateRef{
		{
			project: "A", plate: "1",
			needs: []models.PlateRequirement{
				{Name: "PLA white", Amount: 50},
				{Name: "PETG black", Amount: 20},
			},
		},
		{
			project: "B", plate: "2",
			needs: []models.PlateRequirement{
				{Name: "PLA white", Amount: 30},
			},
		},
	}

	allocs, perPlate := allocateShares(plates, 30)

	// total planned = 100g, used = 30g → 30%
	// plate A: 50*0.3 = 15g PLA, 20*0.3 = 6g PETG = 21g total
	// plate B: 30*0.3 = 9g PLA
	wantPerPlate := []float64{21, 9}
	if !floatsClose(perPlate, wantPerPlate, 0.0001) {
		t.Errorf("perPlate = %v, want %v", perPlate, wantPerPlate)
	}

	if len(allocs) != 3 {
		t.Fatalf("got %d allocs, want 3 (one per non-zero need)", len(allocs))
	}
	// Sum of all share grams should equal totalUsedGrams
	sum := 0.0
	for _, a := range allocs {
		sum += a.shareGrams
	}
	if !floatClose(sum, 30, 0.0001) {
		t.Errorf("sum of share grams = %.4f, want 30", sum)
	}
}

func TestAllocateSharesZeroUsed(t *testing.T) {
	plates := []failPlateRef{
		{needs: []models.PlateRequirement{{Amount: 50}}},
	}
	allocs, perPlate := allocateShares(plates, 0)
	if len(allocs) != 0 {
		t.Errorf("expected no allocations when used=0, got %d", len(allocs))
	}
	if perPlate[0] != 0 {
		t.Errorf("expected 0g per plate, got %v", perPlate)
	}
}

func TestAllocateSharesNoPlannedAmount(t *testing.T) {
	// Plates with no Need.Amount can't be split; allocateShares should bail
	// without producing NaN/Inf shares.
	plates := []failPlateRef{
		{needs: []models.PlateRequirement{{Amount: 0}}},
	}
	allocs, perPlate := allocateShares(plates, 30)
	if len(allocs) != 0 {
		t.Errorf("expected no allocations when totalPlanned=0, got %d", len(allocs))
	}
	if perPlate[0] != 0 {
		t.Errorf("expected 0g per plate, got %v", perPlate)
	}
}

// makeFailSpool builds a FindSpool with just the fields findPrinterSpool inspects,
// avoiding the verbose anonymous-struct literal for the embedded Filament.
func makeFailSpool(id int, loc string, weight float64, filamentID int, name string) models.FindSpool {
	s := models.FindSpool{Id: id, Location: loc, RemainingWeight: weight}
	s.Filament.Id = filamentID
	s.Filament.Name = name
	return s
}

func TestFindPrinterSpoolByFilamentID(t *testing.T) {
	spools := []models.FindSpool{
		makeFailSpool(1, "AMS A1", 500, 100, "PLA white"),
		makeFailSpool(2, "Shelf 6B", 500, 100, "PLA white"),
		makeFailSpool(3, "AMS A2", 500, 200, "PETG black"),
	}
	got := findPrinterSpool(spools, []string{"AMS A1", "AMS A2"}, models.PlateRequirement{FilamentID: 100})
	if got == nil {
		t.Fatal("expected match")
	}
	if got.Id != 1 {
		t.Errorf("got spool %d, want 1 (only filament 100 in AMS slots)", got.Id)
	}
}

func TestFindPrinterSpoolPrefersWeightWhenAmbiguous(t *testing.T) {
	spools := []models.FindSpool{
		makeFailSpool(1, "AMS A1", 0, 100, ""),
		makeFailSpool(2, "AMS A2", 500, 100, ""),
	}
	got := findPrinterSpool(spools, []string{"AMS A1", "AMS A2"}, models.PlateRequirement{FilamentID: 100})
	if got == nil {
		t.Fatal("expected match")
	}
	if got.Id != 2 {
		t.Errorf("got spool %d, want 2 (the one with remaining weight)", got.Id)
	}
}

func TestFindPrinterSpoolReturnsNilWhenAmbiguous(t *testing.T) {
	spools := []models.FindSpool{
		makeFailSpool(1, "AMS A1", 500, 100, ""),
		makeFailSpool(2, "AMS A2", 500, 100, ""),
	}
	got := findPrinterSpool(spools, []string{"AMS A1", "AMS A2"}, models.PlateRequirement{FilamentID: 100})
	if got != nil {
		t.Errorf("expected nil for ambiguous match, got spool %d", got.Id)
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

func floatsClose(a, b []float64, eps float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !floatClose(a[i], b[i], eps) {
			return false
		}
	}
	return true
}

func floatClose(a, b, eps float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= eps
}
