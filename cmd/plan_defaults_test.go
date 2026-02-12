package cmd

import (
	"testing"

	"github.com/dstockto/fil/models"
	"gopkg.in/yaml.v3"
)

func TestPlanDefaultStatus(t *testing.T) {
	yamlData := `
projects:
  - name: Project 1
    plates:
      - name: Plate 1
        needs:
          - filament_id: 1
            amount: 10
`
	var plan models.PlanFile
	err := yaml.Unmarshal([]byte(yamlData), &plan)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify initial state (blank)
	if plan.Projects[0].Status != "" {
		t.Errorf("Expected initial project status to be blank, got %s", plan.Projects[0].Status)
	}
	if plan.Projects[0].Plates[0].Status != "" {
		t.Errorf("Expected initial plate status to be blank, got %s", plan.Projects[0].Plates[0].Status)
	}

	// Apply defaults
	plan.DefaultStatus()

	// Verify defaulted state
	if plan.Projects[0].Status != "todo" {
		t.Errorf("Expected project status to be todo, got %s", plan.Projects[0].Status)
	}
	if plan.Projects[0].Plates[0].Status != "todo" {
		t.Errorf("Expected plate status to be todo, got %s", plan.Projects[0].Plates[0].Status)
	}
}

func TestPlanDefaultStatusPreservesExisting(t *testing.T) {
	yamlData := `
projects:
  - name: Project 1
    status: in-progress
    plates:
      - name: Plate 1
        status: completed
        needs:
          - filament_id: 1
            amount: 10
`
	var plan models.PlanFile
	err := yaml.Unmarshal([]byte(yamlData), &plan)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Apply defaults
	plan.DefaultStatus()

	// Verify state preserved
	if plan.Projects[0].Status != "in-progress" {
		t.Errorf("Expected project status to be in-progress, got %s", plan.Projects[0].Status)
	}
	if plan.Projects[0].Plates[0].Status != "completed" {
		t.Errorf("Expected plate status to be completed, got %s", plan.Projects[0].Plates[0].Status)
	}
}
