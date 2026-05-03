package plan

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dstockto/fil/models"
)

// findPlate locates a (project, plate) pair within a plan and returns the
// indices for in-place mutation. Returns an error if either is missing —
// stale references shouldn't silently no-op.
func findPlate(plan models.PlanFile, projectName, plateName string) (int, int, error) {
	for pi, proj := range plan.Projects {
		if proj.Name != projectName {
			continue
		}
		for pj, plate := range proj.Plates {
			if plate.Name == plateName {
				return pi, pj, nil
			}
		}
		return 0, 0, fmt.Errorf("plate %q not found in project %q", plateName, projectName)
	}
	return 0, 0, fmt.Errorf("project %q not found", projectName)
}

// fetchSpoolsByID gets the subset of Spools referenced by deductions, keyed
// by ID. Goes through FindSpoolsByName("*") because that's already in the
// narrow Spoolman interface — adding a per-ID lookup would expand the seam.
func (l *LocalPlanOps) fetchSpoolsByID(ctx context.Context, deductions []SpoolDeduction) (map[int]models.FindSpool, error) {
	if len(deductions) == 0 {
		return nil, nil
	}
	all, err := l.spoolman.FindSpoolsByName(ctx, l.spoolPattern, nil, nil)
	if err != nil {
		return nil, err
	}
	wanted := map[int]struct{}{}
	for _, d := range deductions {
		wanted[d.SpoolID] = struct{}{}
	}
	out := map[int]models.FindSpool{}
	for _, s := range all {
		if _, ok := wanted[s.Id]; ok {
			out[s.Id] = s
		}
	}
	return out, nil
}

// Complete mutates the Plan: sets the named Plate's Status to "completed",
// clears Plate.Printer, and cascades Project.Status to "completed" if every
// Plate in the project is now done. Save the YAML first, then deduct via
// Spoolman, then write history, then notify.
//
// Order rationale: if YAML save and Spoolman were swapped, a YAML-save
// failure after a successful deduction would leave the Plate looking
// in-progress — the user would re-run Complete and double-deduct.
// Saving first means a Spoolman-write failure leaves the YAML correct
// (Plate completed) but a Spool under-deducted; the user fixes that with
// `fil use`, no double-deduction risk.
func (l *LocalPlanOps) Complete(ctx context.Context, req CompleteRequest) (CompleteResult, error) {
	if l.plans == nil {
		return CompleteResult{}, errors.New("PlanStore not configured")
	}
	if req.Plan == "" || req.Project == "" || req.Plate == "" {
		return CompleteResult{}, errors.New("plan, project, and plate are required")
	}
	if req.FinishedAt.IsZero() {
		req.FinishedAt = time.Now().UTC()
	}

	plan, err := l.plans.Load(ctx, req.Plan)
	if err != nil {
		return CompleteResult{}, err
	}

	projIdx, plateIdx, err := findPlate(plan, req.Project, req.Plate)
	if err != nil {
		return CompleteResult{}, err
	}

	plate := &plan.Projects[projIdx].Plates[plateIdx]
	plate.Status = "completed"
	plate.Printer = ""

	cascaded := false
	allDone := true
	for _, p := range plan.Projects[projIdx].Plates {
		if p.Status != "completed" {
			allDone = false
			break
		}
	}
	if allDone {
		plan.Projects[projIdx].Status = "completed"
		cascaded = true
	}

	if err := l.plans.Save(ctx, req.Plan, plan); err != nil {
		return CompleteResult{ProjectCascaded: cascaded}, fmt.Errorf("save plan: %w", err)
	}

	// YAML is now the source of truth: the Plate is completed. Past this
	// point we keep going on partial Spoolman failures and surface a joined
	// error so the caller can warn but the audit trail still gets written.
	var deductErrs []error
	if len(req.Deductions) > 0 {
		spools, err := l.fetchSpoolsByID(ctx, req.Deductions)
		if err != nil {
			deductErrs = append(deductErrs, fmt.Errorf("list spools: %w", err))
		} else {
			for _, d := range req.Deductions {
				spool, ok := spools[d.SpoolID]
				if !ok {
					deductErrs = append(deductErrs, fmt.Errorf("spool #%d not found", d.SpoolID))
					continue
				}
				if err := l.useFilamentSafely(ctx, spool, d.Amount); err != nil {
					deductErrs = append(deductErrs, fmt.Errorf("deduct spool #%d: %w", d.SpoolID, err))
				}
			}
		}
	}

	if l.history != nil {
		entry := completeHistoryEntry(req)
		if err := l.history.AppendComplete(ctx, []CompleteHistoryEntry{entry}); err != nil {
			deductErrs = append(deductErrs, fmt.Errorf("write history: %w", err))
		}
	}

	if l.notifier != nil {
		l.notifier.Notify(ctx, "Print completed", fmt.Sprintf("%s / %s on %s", req.Project, req.Plate, req.Printer))
	}

	if len(deductErrs) > 0 {
		return CompleteResult{ProjectCascaded: cascaded}, errors.Join(deductErrs...)
	}
	return CompleteResult{ProjectCascaded: cascaded}, nil
}

func completeHistoryEntry(req CompleteRequest) CompleteHistoryEntry {
	var fil []HistoryFilament
	for _, n := range req.Filament {
		fil = append(fil, HistoryFilament{
			Name:       n.Name,
			FilamentID: n.FilamentID,
			Material:   n.Material,
			Amount:     n.Amount,
		})
	}
	return CompleteHistoryEntry{
		Timestamp:         req.FinishedAt,
		FinishedAt:        req.FinishedAt,
		Plan:              req.Plan,
		Project:           req.Project,
		Plate:             req.Plate,
		Printer:           req.Printer,
		StartedAt:         req.StartedAt,
		EstimatedDuration: req.EstimatedDuration,
		Filament:          fil,
	}
}
