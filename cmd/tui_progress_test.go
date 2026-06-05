package cmd

import (
	"strings"
	"testing"

	"github.com/dstockto/fil/api"
)

// TestRenderActivePrinterZeroProgressShowsBar is a regression test for the case
// where a genuinely printing printer sits at 0% for the first several minutes
// (warmup + early layers). Previously renderActivePrinter gated the progress bar
// on Progress > 0, so nothing rendered until progress ticked to 1% — on a long
// Prusa print that meant ~5 minutes with no bar at all. The bar must render
// whenever the printer is in a printing/paused/failed state, regardless of %.
func TestRenderActivePrinterZeroProgressShowsBar(t *testing.T) {
	tests := []struct {
		name     string
		state    string
		progress int
		wantBar  bool
	}{
		{name: "printing at 0% renders bar", state: "printing", progress: 0, wantBar: true},
		{name: "printing at 1% renders bar", state: "printing", progress: 1, wantBar: true},
		{name: "paused at 0% renders bar", state: "paused", progress: 0, wantBar: true},
		{name: "failed at 0% renders bar", state: "failed", progress: 0, wantBar: true},
		{name: "idle renders no bar", state: "idle", progress: 0, wantBar: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const name = "Prusa XL"
			m := tuiModel{
				printerStatuses: map[string]api.PrinterStatus{
					name: {Name: name, Type: "prusa", State: tt.state, Progress: tt.progress},
				},
				printerMap: map[string][]tuiPrintingInfo{
					name: {{Project: "proj", Plate: "plate"}},
				},
			}

			var b strings.Builder
			m.renderActivePrinter(&b, name, 80)
			out := b.String()

			// The progress bar is the only place the filled/empty block runes appear.
			hasBar := strings.ContainsAny(out, "█░")
			if hasBar != tt.wantBar {
				t.Errorf("state=%q progress=%d: hasBar=%v, want %v\noutput:\n%s",
					tt.state, tt.progress, hasBar, tt.wantBar, out)
			}
		})
	}
}
