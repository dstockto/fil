package plan

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dstockto/fil/models"
)

// Fail runs the full fail flow: allocate, deduct from Spoolman, append to
// history, notify. Order is Spoolman → history → notify; if Spoolman partial
// failure occurs we still write history (the failure happened either way) and
// return a joined error so the caller can warn.
func (l *LocalPlanOps) Fail(ctx context.Context, req FailRequest) (FailResult, error) {
	if req.FailedAt.IsZero() {
		req.FailedAt = time.Now().UTC()
	}
	var result FailResult

	allocations, perPlate := allocateShares(req.Plates, req.UsedGrams)

	// Pull all spools once so per-need lookups don't N+1 the API.
	var allSpools []models.FindSpool
	if len(allocations) > 0 {
		s, err := l.spoolman.FindSpoolsByName(ctx, l.spoolPattern, nil, nil)
		if err != nil {
			return result, fmt.Errorf("list spools: %w", err)
		}
		allSpools = s
	}

	printerLocs := l.printers.Locations(req.Printer)

	// Group share-grams by chosen spool so a single spool powering two slots
	// only triggers one Spoolman update.
	type pending struct {
		spool models.FindSpool
		grams float64
		label string
	}
	bySpool := map[int]*pending{}
	var unmatched []FailUnmatched

	for _, a := range allocations {
		need := req.Plates[a.plateRef].Needs[a.needIdx]
		spool := findPrinterSpool(allSpools, printerLocs, need)
		if spool == nil {
			unmatched = append(unmatched, FailUnmatched{
				Project:      req.Plates[a.plateRef].Project,
				Plate:        req.Plates[a.plateRef].Plate,
				FilamentName: need.Name,
				Grams:        a.shareGrams,
			})
			continue
		}
		if cur, ok := bySpool[spool.Id]; ok {
			cur.grams += a.shareGrams
		} else {
			label := fmt.Sprintf("#%d %s @ %s", spool.Id, spool.Filament.Name, spool.Location)
			bySpool[spool.Id] = &pending{spool: *spool, grams: a.shareGrams, label: label}
		}
	}

	var deductErrs []error
	for _, p := range bySpool {
		if err := l.useFilamentSafely(ctx, p.spool, p.grams); err != nil {
			deductErrs = append(deductErrs, fmt.Errorf("deduct %s: %w", p.label, err))
			continue
		}
		result.Allocations = append(result.Allocations, FailAllocation{
			SpoolID: p.spool.Id,
			Label:   p.label,
			Grams:   p.grams,
		})
	}
	result.Unmatched = unmatched

	// Always write history, even on partial deduction failure — the print
	// failed, so the audit record needs to exist regardless.
	entries := make([]FailHistoryEntry, 0, len(req.Plates))
	for i, p := range req.Plates {
		var fil []HistoryFilament
		for _, n := range p.Needs {
			fil = append(fil, HistoryFilament{
				Name:       n.Name,
				FilamentID: n.FilamentID,
				Material:   n.Material,
				Amount:     n.Amount,
			})
		}
		entries = append(entries, FailHistoryEntry{
			Timestamp:         req.FailedAt,
			Plan:              p.Plan,
			Project:           p.Project,
			Plate:             p.Plate,
			Printer:           req.Printer,
			StartedAt:         p.StartedAt,
			EstimatedDuration: p.EstimatedDuration,
			Filament:          fil,
			Cause:             req.Cause,
			Reason:            req.Reason,
			UsedGrams:         perPlate[i],
		})
	}

	var historyErr error
	if l.history != nil && len(entries) > 0 {
		historyErr = l.history.AppendFail(ctx, entries)
	}

	if l.notifier != nil {
		l.notifier.Notify(ctx, "Print failed", failNotificationBody(req, result, perPlate))
	}

	var allErrs []error
	allErrs = append(allErrs, deductErrs...)
	if historyErr != nil {
		allErrs = append(allErrs, fmt.Errorf("write history: %w", historyErr))
	}
	if len(allErrs) > 0 {
		return result, errors.Join(allErrs...)
	}
	return result, nil
}

// useFilamentSafely mirrors cmd.UseFilamentSafely: when the requested amount
// would push remaining_weight negative, bump initial_weight by the overage
// first so Spoolman doesn't reject the write.
func (l *LocalPlanOps) useFilamentSafely(ctx context.Context, spool models.FindSpool, amount float64) error {
	if amount > spool.RemainingWeight {
		overage := amount - spool.RemainingWeight
		updates := map[string]any{
			"initial_weight": spool.InitialWeight + overage,
		}
		if err := l.spoolman.PatchSpool(ctx, spool.Id, updates); err != nil {
			return fmt.Errorf("adjust initial weight for spool #%d: %w", spool.Id, err)
		}
	}
	return l.spoolman.UseFilament(ctx, spool.Id, amount)
}

// allocateShares splits totalUsedGrams across each (plate, need) pair in
// proportion to its planned amount. Returns one allocation per Need plus the
// per-plate share total used in the JSONL log.
func allocateShares(plates []FailPlate, totalUsedGrams float64) ([]shareAllocation, []float64) {
	perPlate := make([]float64, len(plates))
	if len(plates) == 0 {
		return nil, perPlate
	}

	totalPlanned := 0.0
	for _, p := range plates {
		for _, n := range p.Needs {
			totalPlanned += n.Amount
		}
	}
	if totalPlanned <= 0 || totalUsedGrams <= 0 {
		return nil, perPlate
	}

	var allocations []shareAllocation
	ratio := totalUsedGrams / totalPlanned
	for i, p := range plates {
		for j, n := range p.Needs {
			share := n.Amount * ratio
			if share <= 0 {
				continue
			}
			allocations = append(allocations, shareAllocation{
				plateRef:     i,
				needIdx:      j,
				plannedGrams: n.Amount,
				shareGrams:   share,
			})
			perPlate[i] += share
		}
	}
	return allocations, perPlate
}

// shareAllocation is one filament-deduction the operation will perform. Same
// shape as the original cmd-side struct, kept unexported.
type shareAllocation struct {
	plateRef     int
	needIdx      int
	plannedGrams float64
	shareGrams   float64
}

// findPrinterSpool picks a single spool in the printer's locations matching
// the requirement. Returns nil when there are zero matches or multiple
// candidates without a clear winner.
func findPrinterSpool(spools []models.FindSpool, printerLocations []string, req models.PlateRequirement) *models.FindSpool {
	if len(printerLocations) == 0 {
		return nil
	}
	locSet := map[string]struct{}{}
	for _, l := range printerLocations {
		locSet[l] = struct{}{}
	}

	var candidates []models.FindSpool
	for _, s := range spools {
		if _, ok := locSet[s.Location]; !ok {
			continue
		}
		if req.FilamentID != 0 {
			if s.Filament.Id == req.FilamentID {
				candidates = append(candidates, s)
			}
		} else if req.Name != "" && strings.Contains(strings.ToLower(s.Filament.Name), strings.ToLower(req.Name)) {
			candidates = append(candidates, s)
		}
	}

	if len(candidates) > 1 {
		var withWeight []models.FindSpool
		for _, c := range candidates {
			if c.RemainingWeight > 0 {
				withWeight = append(withWeight, c)
			}
		}
		if len(withWeight) > 0 {
			candidates = withWeight
		}
	}
	if len(candidates) == 1 {
		c := candidates[0]
		return &c
	}
	return nil
}

// failNotificationBody composes a one-line summary used for push/voice
// notifications. Kept terse because Voice Monkey reads it aloud.
func failNotificationBody(req FailRequest, result FailResult, perPlate []float64) string {
	totalDeducted := 0.0
	for _, a := range result.Allocations {
		totalDeducted += a.Grams
	}
	plateNames := make([]string, 0, len(req.Plates))
	for _, p := range req.Plates {
		plateNames = append(plateNames, p.Project+"/"+p.Plate)
	}
	return fmt.Sprintf("%s on %s — cause=%s, used=%.1fg",
		strings.Join(plateNames, ", "), req.Printer, req.Cause, totalDeducted)
}
