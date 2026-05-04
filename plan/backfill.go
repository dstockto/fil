package plan

import (
	"context"

	"github.com/dstockto/fil/models"
)

// backfillPlanColors fills in missing Color fields on PlateRequirements by
// looking up each FilamentID in the provided spool list and copying the
// filament's color_hex onto the requirement. Returns true if any colors
// were filled. No-op for needs that already have a color or no FilamentID.
//
// Used internally by SaveAll on both adapters and by `fil plan backfill-colors`.
func backfillPlanColors(plan *models.PlanFile, spools []models.FindSpool) bool {
	colorByFilament := map[int]string{}
	for _, s := range spools {
		if s.Filament.Id != 0 && s.Filament.ColorHex != "" {
			colorByFilament[s.Filament.Id] = s.Filament.ColorHex
		}
	}

	changed := false
	for i := range plan.Projects {
		for j := range plan.Projects[i].Plates {
			for k := range plan.Projects[i].Plates[j].Needs {
				need := &plan.Projects[i].Plates[j].Needs[k]
				if need.Color == "" && need.FilamentID != 0 {
					if hex, ok := colorByFilament[need.FilamentID]; ok {
						need.Color = hex
						changed = true
					}
				}
			}
		}
	}
	return changed
}

// applyColorBackfill is the SaveAll-time hook: best-effort fetch of all
// spools followed by an in-place backfill on the plan. Errors from the
// Spoolman fetch are swallowed — backfill is convenience, not correctness.
func applyColorBackfill(ctx context.Context, sm Spoolman, plan *models.PlanFile) {
	if sm == nil {
		return
	}
	spools, err := sm.FindSpoolsByName(ctx, "*", nil, nil)
	if err != nil {
		return
	}
	backfillPlanColors(plan, spools)
}
