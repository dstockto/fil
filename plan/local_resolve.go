package plan

import (
	"context"
	"errors"
	"fmt"
)

// Resolve loads the plan, applies each NeedResolution in place, and saves.
// Empty Resolutions returns nil without touching the store — callers can
// invoke Resolve unconditionally after their interactive flow without
// re-checking whether the user actually changed anything.
func (l *LocalPlanOps) Resolve(ctx context.Context, req ResolveRequest) error {
	if l.plans == nil {
		return errors.New("PlanStore not configured")
	}
	if req.Plan == "" {
		return errors.New("plan name is required")
	}
	if len(req.Resolutions) == 0 {
		return nil
	}

	plan, err := l.plans.Load(ctx, req.Plan)
	if err != nil {
		return err
	}

	for _, res := range req.Resolutions {
		projIdx, plateIdx, err := findPlate(plan, res.Project, res.Plate)
		if err != nil {
			return err
		}
		needs := plan.Projects[projIdx].Plates[plateIdx].Needs
		if res.NeedIndex < 0 || res.NeedIndex >= len(needs) {
			return fmt.Errorf("resolution for %s/%s: need_index %d out of range (0..%d)",
				res.Project, res.Plate, res.NeedIndex, len(needs))
		}
		need := &plan.Projects[projIdx].Plates[plateIdx].Needs[res.NeedIndex]
		need.FilamentID = res.FilamentID
		need.Name = res.Name
		need.Material = res.Material
	}

	applyColorBackfill(ctx, l.spoolman, &plan)

	if err := l.plans.Save(ctx, req.Plan, plan); err != nil {
		return fmt.Errorf("save plan: %w", err)
	}
	return nil
}
