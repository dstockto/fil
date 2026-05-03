package plan

// LocalPlanOps is the adapter used in Local Mode (no plan-server) and inside
// the plan-server's HTTP handlers when running in Remote Mode. It mutates
// Spoolman directly and writes to the local print-history log.
type LocalPlanOps struct {
	spoolman     Spoolman
	printers     PrinterLocations
	plans        PlanStore
	history      HistoryWriter
	notifier     Notifier
	spoolPattern string // pattern passed to FindSpoolsByName; default "*"
}

// NewLocal constructs a LocalPlanOps. plans may be nil for callers that only
// use Fail (Fail doesn't mutate plan files); verbs that need to load/save the
// plan (Complete, Next, ...) require a non-nil PlanStore.
func NewLocal(spoolman Spoolman, printers PrinterLocations, plans PlanStore, history HistoryWriter, notifier Notifier) *LocalPlanOps {
	if notifier == nil {
		notifier = NoopNotifier{}
	}
	return &LocalPlanOps{
		spoolman:     spoolman,
		printers:     printers,
		plans:        plans,
		history:      history,
		notifier:     notifier,
		spoolPattern: "*",
	}
}
