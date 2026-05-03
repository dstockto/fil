// Package plan owns the verbs that mutate Plan state: failing plates,
// completing them, advancing to the next, etc. The CLI command files and the
// plan-server's HTTP handlers are both thin callers of this package — see
// CONTEXT.md at the repo root for domain language.
//
// PlanOperations has two adapters: LocalPlanOps (mutates YAML + Spoolman
// directly) and RemotePlanOps (HTTP calls to a plan-server). The fil
// installation runs in either Local Mode or Remote Mode based on whether
// PlansServer is configured; adapter selection happens once at startup.
package plan

import (
	"context"
	"time"

	"github.com/dstockto/fil/models"
)

// PlanOperations is the verb surface for Plan-state changes. The pilot
// implementation only includes Fail; subsequent verbs (Complete, Next, Resolve,
// Pause, Resume, Stop, Archive, Unarchive, Delete, Reprint, plus per-plan data
// edits) get added one PR at a time.
type PlanOperations interface {
	// Fail logs a print failure: deducts wasted filament from the matching
	// Spool(s), records one history entry per plate, and fires a notification.
	// Plate lifecycle status is not changed — callers who also want plates
	// returned to "todo" run plan stop separately. See CONTEXT.md.
	Fail(ctx context.Context, req FailRequest) (FailResult, error)
}

// FailRequest is the input to PlanOperations.Fail.
//
// Callers populate Plates from their already-loaded view of the world: the
// CLI from discoverPlans(), the plan-server from its on-disk YAML. Each
// FailPlate carries the Needs that drive share allocation, so LocalPlanOps
// never has to load a plan file itself.
type FailRequest struct {
	Plates    []FailPlate `json:"plates"`
	Printer   string      `json:"printer,omitempty"`
	Cause     string      `json:"cause"`
	Reason    string      `json:"reason,omitempty"`
	UsedGrams float64     `json:"used_grams,omitempty"`
	FailedAt  time.Time   `json:"failed_at,omitempty"`
}

// FailPlate identifies one plate inside a batch failure. Needs must match the
// plate's filament requirements at the time of the fail (callers should have
// just loaded the plan), since they're used to allocate UsedGrams across spools.
type FailPlate struct {
	Plan              string                    `json:"plan"`
	Project           string                    `json:"project"`
	Plate             string                    `json:"plate"`
	StartedAt         string                    `json:"started_at,omitempty"`
	EstimatedDuration string                    `json:"estimated_duration,omitempty"`
	Needs             []models.PlateRequirement `json:"needs"`
}

// FailResult reports what actually happened: which spools were deducted from
// (so the CLI can echo "Deducted Xg from #N") and which needs couldn't be
// auto-resolved (so the CLI can tell the user to run `fil use` manually).
type FailResult struct {
	Allocations []FailAllocation
	Unmatched   []FailUnmatched
}

// FailAllocation is one Spoolman deduction that the operation performed.
type FailAllocation struct {
	SpoolID int
	Label   string
	Grams   float64
}

// FailUnmatched is one filament need that could not be resolved to a Spool in
// the printer's locations. Caller surfaces these for manual deduction.
type FailUnmatched struct {
	Project      string
	Plate        string
	FilamentName string
	Grams        float64
}
