package plan

// LocalPlanOps is the adapter used in Local Mode (no plan-server) and inside
// the plan-server's HTTP handlers when running in Remote Mode. It mutates
// Spoolman directly and writes to the local print-history log.
type LocalPlanOps struct {
	spoolman  Spoolman
	printers  PrinterLocations
	history   HistoryWriter
	notifier  Notifier
	spoolPattern string // pattern passed to FindSpoolsByName; default "*"
}

// NewLocal constructs a LocalPlanOps. Pass a real *api.Client for spoolman
// and a fileHistoryWriter pointed at the plans dir. notifier may be a
// NoopNotifier when the user hasn't configured any channel.
func NewLocal(spoolman Spoolman, printers PrinterLocations, history HistoryWriter, notifier Notifier) *LocalPlanOps {
	if notifier == nil {
		notifier = NoopNotifier{}
	}
	return &LocalPlanOps{
		spoolman:     spoolman,
		printers:     printers,
		history:      history,
		notifier:     notifier,
		spoolPattern: "*",
	}
}
