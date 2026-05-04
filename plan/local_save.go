package plan

import (
	"context"
	"errors"

	"github.com/dstockto/fil/models"
)

// SaveAll runs the SaveAll-time color backfill (best-effort) and then writes
// the plan via PlanStore. The backfill is silently skipped when no Spoolman
// is configured.
func (l *LocalPlanOps) SaveAll(ctx context.Context, name string, plan models.PlanFile) error {
	if l.plans == nil {
		return errors.New("PlanStore not configured")
	}
	if name == "" {
		return errors.New("plan name is required")
	}
	applyColorBackfill(ctx, l.spoolman, &plan)
	return l.plans.Save(ctx, name, plan)
}
