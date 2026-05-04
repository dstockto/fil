package plan

import (
	"context"
	"errors"
)

// Pause delegates straight to the PlanStore — pause/resume are pure storage
// moves. See FilePlanStore.Pause for the file-backed implementation.
func (l *LocalPlanOps) Pause(ctx context.Context, name string) error {
	if l.plans == nil {
		return errors.New("PlanStore not configured")
	}
	if name == "" {
		return errors.New("plan name is required")
	}
	return l.plans.Pause(ctx, name)
}

// Resume is the inverse of Pause. Same delegation pattern.
func (l *LocalPlanOps) Resume(ctx context.Context, name string) error {
	if l.plans == nil {
		return errors.New("PlanStore not configured")
	}
	if name == "" {
		return errors.New("plan name is required")
	}
	return l.plans.Resume(ctx, name)
}
