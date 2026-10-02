package cmd

import (
	"reflect"
	"testing"

	"github.com/dstockto/fil/models"
)

func TestCompletablePlatesFromSortsInProgressFirstAcrossPlans(t *testing.T) {
	plans := []DiscoveredPlan{
		{
			DisplayName: "a.yaml",
			Plan: models.PlanFile{
				Projects: []models.Project{
					{
						Name: "Alpha",
						Plates: []models.Plate{
							{Name: "A1", Status: "todo"},
							{Name: "A2", Status: "completed"}, // skipped: done
							{Name: "A3", Status: "todo"},
						},
					},
					{
						Name:   "Done",
						Status: "completed", // skipped: whole project done
						Plates: []models.Plate{{Name: "D1", Status: "todo"}},
					},
				},
			},
		},
		{
			DisplayName: "b.yaml",
			Plan: models.PlanFile{
				Projects: []models.Project{{
					Name: "Bravo",
					Plates: []models.Plate{
						{Name: "B1", Status: "todo"},
						{Name: "B2", Status: "in-progress", Printer: "Prusa XL"},
					},
				}},
			},
		},
		{
			DisplayName: "c.yaml",
			Plan: models.PlanFile{
				Projects: []models.Project{{
					Name: "Charlie",
					Plates: []models.Plate{
						{Name: "C1", Status: "in-progress", Printer: "Bambu X1C"},
					},
				}},
			},
		},
	}

	got := completablePlatesFrom(plans)

	var gotPlates []string
	for _, r := range got {
		gotPlates = append(gotPlates, r.plateName)
	}
	want := []string{"B2", "C1", "A1", "A3", "B1"}
	if !reflect.DeepEqual(gotPlates, want) {
		t.Fatalf("got plates %v, want %v", gotPlates, want)
	}

	// Indices must point back at the right plate so the caller can resolve it.
	for _, r := range got {
		plate := plans[r.discoveredIdx].Plan.Projects[r.projectIdx].Plates[r.plateIdx]
		if plate.Name != r.plateName {
			t.Errorf("ref %s resolves to plate %s", r.plateName, plate.Name)
		}
	}
}

func TestCompletablePlatesFromEmpty(t *testing.T) {
	if got := completablePlatesFrom(nil); len(got) != 0 {
		t.Errorf("got %d refs, want 0", len(got))
	}
}
