package plan

import (
	"context"
	"errors"
)

// Archive delegates to PlanStore — archive is a pure storage move with a
// rename rule. See FilePlanStore.Archive for the file-backed implementation.
func (l *LocalPlanOps) Archive(ctx context.Context, name string) error {
	if l.plans == nil {
		return errors.New("PlanStore not configured")
	}
	if name == "" {
		return errors.New("plan name is required")
	}
	return l.plans.Archive(ctx, name)
}

// Unarchive is the inverse of Archive.
func (l *LocalPlanOps) Unarchive(ctx context.Context, name string) error {
	if l.plans == nil {
		return errors.New("PlanStore not configured")
	}
	if name == "" {
		return errors.New("plan name is required")
	}
	return l.plans.Unarchive(ctx, name)
}

// Delete removes the named plan from active storage.
func (l *LocalPlanOps) Delete(ctx context.Context, name string) error {
	if l.plans == nil {
		return errors.New("PlanStore not configured")
	}
	if name == "" {
		return errors.New("plan name is required")
	}
	return l.plans.Delete(ctx, name)
}
