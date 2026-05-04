package plan

import (
	"context"
	"time"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
)

// Spoolman is the slice of the Spoolman API that LocalPlanOps actually uses.
// Kept narrow on purpose: tests fake three methods, not the full ~15-method
// SpoolmanAPI. *api.Client satisfies this interface incidentally.
type Spoolman interface {
	FindSpoolsByName(ctx context.Context, name string, filter api.SpoolFilter, query map[string]string) ([]models.FindSpool, error)
	UseFilament(ctx context.Context, spoolID int, amount float64) error
	PatchSpool(ctx context.Context, spoolID int, updates map[string]any) error
}

// PrinterLocations maps a printer name to the Spoolman locations that printer
// pulls from. Used during Fail to find which spool corresponds to a plate's
// filament need on a given printer.
type PrinterLocations interface {
	Locations(printer string) []string
}

// PlanStore loads, saves, and moves Plan YAML files by basename. Verbs that
// mutate plan state (Complete, Next, Stop) use Load+Save; workflow verbs
// (Pause, Resume) use Move. LocalPlanOps doesn't need to know whether plans
// live on the local filesystem or somewhere else.
type PlanStore interface {
	Load(ctx context.Context, name string) (models.PlanFile, error)
	Save(ctx context.Context, name string, plan models.PlanFile) error
	// Pause moves the named plan from active storage to paused storage.
	// Resume is the inverse.
	Pause(ctx context.Context, name string) error
	Resume(ctx context.Context, name string) error
}

// HistoryWriter persists one history record per Plate-level event. The
// default file-backed implementation appends to print-history.jsonl alongside
// the plans dir; tests pass an in-memory recorder.
type HistoryWriter interface {
	AppendFail(ctx context.Context, entries []FailHistoryEntry) error
	AppendComplete(ctx context.Context, entries []CompleteHistoryEntry) error
}

// FailHistoryEntry is the shape persisted to history. Mirrors the existing
// server-side HistoryEntry's fail-relevant fields. Prev-print enrichment
// (which earlier entry on the same printer this failure followed) is the
// HistoryWriter's concern, not LocalPlanOps' — keeps allocation logic clean.
type FailHistoryEntry struct {
	Timestamp         time.Time
	Plan              string
	Project           string
	Plate             string
	Printer           string
	StartedAt         string
	EstimatedDuration string
	Filament          []HistoryFilament
	Cause             string
	Reason            string
	UsedGrams         float64
}

// HistoryFilament is one filament line on a history entry.
type HistoryFilament struct {
	Name       string
	FilamentID int
	Material   string
	Amount     float64
}

// CompleteHistoryEntry is the shape persisted to history when a Plate
// completes successfully. No Cause/Reason/UsedGrams/PrevPrint — those are
// fail-only fields.
type CompleteHistoryEntry struct {
	Timestamp         time.Time
	FinishedAt        time.Time
	Plan              string
	Project           string
	Plate             string
	Printer           string
	StartedAt         string
	EstimatedDuration string
	Filament          []HistoryFilament
}

// Notifier delivers a best-effort notification. Errors are swallowed inside
// the implementation — a notification failure must never fail the fail.
type Notifier interface {
	Notify(ctx context.Context, title, body string)
}

// NoopNotifier is the zero-value notifier used when the user hasn't configured
// any notification channel. Local Mode wires this in by default.
type NoopNotifier struct{}

// Notify is a no-op.
func (NoopNotifier) Notify(context.Context, string, string) {}

// StaticPrinterLocations is a PrinterLocations backed by a plain map. The CLI
// builds one from cfg.Printers; the server builds one from the same shared
// config it loads at startup.
type StaticPrinterLocations map[string][]string

// Locations returns the configured locations for printer, or nil.
func (m StaticPrinterLocations) Locations(printer string) []string {
	return m[printer]
}
