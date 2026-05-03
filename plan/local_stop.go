package plan

import (
	"context"
	"errors"
	"fmt"
)

// Stop sets the named Plate's Status back to "todo" and clears Printer and
// StartedAt. EstimatedDuration is preserved — the next Next call may want to
// reuse it. Project status is not regressed (forward-only convention).
func (l *LocalPlanOps) Stop(ctx context.Context, req StopRequest) error {
	if l.plans == nil {
		return errors.New("PlanStore not configured")
	}
	if req.Plan == "" || req.Project == "" || req.Plate == "" {
		return errors.New("plan, project, and plate are required")
	}

	plan, err := l.plans.Load(ctx, req.Plan)
	if err != nil {
		return err
	}
	projIdx, plateIdx, err := findPlate(plan, req.Project, req.Plate)
	if err != nil {
		return err
	}

	plate := &plan.Projects[projIdx].Plates[plateIdx]
	plate.Status = "todo"
	plate.Printer = ""
	plate.StartedAt = ""

	if err := l.plans.Save(ctx, req.Plan, plan); err != nil {
		return fmt.Errorf("save plan: %w", err)
	}
	return nil
}
