package plan

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Next marks a Plate as in-progress on the given Printer, stamps StartedAt,
// and cascades the parent Project from "todo" to "in-progress" if needed.
// No Spoolman calls; no history. Filament-swap orchestration is the caller's
// responsibility.
func (l *LocalPlanOps) Next(ctx context.Context, req NextRequest) (NextResult, error) {
	if l.plans == nil {
		return NextResult{}, errors.New("PlanStore not configured")
	}
	if req.Plan == "" || req.Project == "" || req.Plate == "" || req.Printer == "" {
		return NextResult{}, errors.New("plan, project, plate, and printer are required")
	}
	if req.StartedAt.IsZero() {
		req.StartedAt = time.Now().UTC()
	}

	plan, err := l.plans.Load(ctx, req.Plan)
	if err != nil {
		return NextResult{}, err
	}

	projIdx, plateIdx, err := findPlate(plan, req.Project, req.Plate)
	if err != nil {
		return NextResult{}, err
	}

	plate := &plan.Projects[projIdx].Plates[plateIdx]
	plate.Status = "in-progress"
	plate.Printer = req.Printer
	plate.StartedAt = req.StartedAt.Format(time.RFC3339)

	projectStarted := false
	if plan.Projects[projIdx].Status == "todo" {
		plan.Projects[projIdx].Status = "in-progress"
		projectStarted = true
	}

	if err := l.plans.Save(ctx, req.Plan, plan); err != nil {
		return NextResult{ProjectStarted: projectStarted}, fmt.Errorf("save plan: %w", err)
	}
	return NextResult{ProjectStarted: projectStarted}, nil
}
