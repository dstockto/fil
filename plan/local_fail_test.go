package plan

import (
	"testing"

	"github.com/dstockto/fil/models"
)

func TestAllocateSharesProportional(t *testing.T) {
	plates := []FailPlate{
		{
			Project: "A", Plate: "1",
			Needs: []models.PlateRequirement{
				{Name: "PLA white", Amount: 50},
				{Name: "PETG black", Amount: 20},
			},
		},
		{
			Project: "B", Plate: "2",
			Needs: []models.PlateRequirement{
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
	sum := 0.0
	for _, a := range allocs {
		sum += a.shareGrams
	}
	if !floatClose(sum, 30, 0.0001) {
		t.Errorf("sum of share grams = %.4f, want 30", sum)
	}
}

func TestAllocateSharesZeroUsed(t *testing.T) {
	plates := []FailPlate{
		{Needs: []models.PlateRequirement{{Amount: 50}}},
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
	plates := []FailPlate{
		{Needs: []models.PlateRequirement{{Amount: 0}}},
	}
	allocs, perPlate := allocateShares(plates, 30)
	if len(allocs) != 0 {
		t.Errorf("expected no allocations when totalPlanned=0, got %d", len(allocs))
	}
	if perPlate[0] != 0 {
		t.Errorf("expected 0g per plate, got %v", perPlate)
	}
}

// makeFailSpool builds a FindSpool with just the fields findPrinterSpool
// inspects, avoiding the verbose anonymous-struct literal for the embedded
// Filament.
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
