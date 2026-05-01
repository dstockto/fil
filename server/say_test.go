package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFormatSayResponseAllIdle(t *testing.T) {
	now := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)
	got := formatSayResponse(nil, []PrinterState{
		{Name: "Bambu X1C", State: "idle"},
		{Name: "Prusa XL", State: "offline"},
	}, now)
	want := "Nothing is printing right now."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatSayResponseNoPrinters(t *testing.T) {
	now := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)
	got := formatSayResponse(nil, nil, now)
	if got != "Nothing is printing right now." {
		t.Errorf("got %q", got)
	}
}

func TestFormatSayResponseSingleActiveSkipsIdle(t *testing.T) {
	now := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)
	plates := map[string]platePrinting{
		"Bambu X1C": {Project: "Viking Guard", Plate: "3"},
	}
	got := formatSayResponse(plates, []PrinterState{
		{Name: "Bambu X1C", State: "printing", Progress: 45, RemainingMins: 150},
		{Name: "Prusa XL", State: "idle"},
	}, now)

	// Idle Prusa must NOT appear; ETA = 14:00 + 150min = 16:30 UTC = 4:30 PM.
	want := "Bambu X1C is printing Viking Guard, plate 3, 45 percent, finishing around 4:30 PM."
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestFormatSayResponseMultipleActiveSortedByName(t *testing.T) {
	now := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)
	plates := map[string]platePrinting{
		"Bambu X1C": {Project: "Viking", Plate: "3"},
		"Prusa XL":  {Project: "Cup", Plate: "1"},
	}
	got := formatSayResponse(plates, []PrinterState{
		// Reversed input order — output must alphabetize.
		{Name: "Prusa XL", State: "printing", Progress: 80, RemainingMins: 75},
		{Name: "Bambu X1C", State: "printing", Progress: 45, RemainingMins: 150},
	}, now)

	want := "Bambu X1C is printing Viking, plate 3, 45 percent, finishing around 4:30 PM. " +
		"Prusa XL is printing Cup, plate 1, 80 percent, finishing around 3:15 PM."
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestFormatSayResponsePaused(t *testing.T) {
	now := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)
	plates := map[string]platePrinting{
		"Bambu X1C": {Project: "Viking", Plate: "3"},
	}
	got := formatSayResponse(plates, []PrinterState{
		{Name: "Bambu X1C", State: "paused"},
	}, now)

	want := "Bambu X1C is paused on Viking, plate 3."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatSayResponseFailed(t *testing.T) {
	now := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)
	got := formatSayResponse(nil, []PrinterState{
		{Name: "Bambu X1C", State: "failed"},
	}, now)
	if got != "Bambu X1C has failed." {
		t.Errorf("got %q", got)
	}
}

func TestFormatSayResponseFinishedWithAge(t *testing.T) {
	finished := time.Date(2026, 4, 30, 13, 48, 0, 0, time.UTC)
	now := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)
	plates := map[string]platePrinting{
		"Bambu X1C": {Project: "Viking", Plate: "3"},
	}
	got := formatSayResponse(plates, []PrinterState{
		{Name: "Bambu X1C", State: "finished", LastFinishedAt: finished},
	}, now)

	want := "Bambu X1C finished Viking, plate 3 about 12 minutes ago."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatSayResponsePrintingNoPlateUsesCurrentFile(t *testing.T) {
	now := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)
	got := formatSayResponse(nil, []PrinterState{
		{Name: "Bambu X1C", State: "printing", Progress: 30, RemainingMins: 60, CurrentFile: "test_print.gcode"},
	}, now)

	want := "Bambu X1C is printing test_print, 30 percent, finishing around 3 PM."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatSayResponsePrintingNoData(t *testing.T) {
	// Printing state but no plate match, no current file, no progress, no ETA —
	// shouldn't crash, should produce a sensible degenerate sentence.
	now := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)
	got := formatSayResponse(nil, []PrinterState{
		{Name: "Bambu X1C", State: "printing"},
	}, now)
	want := "Bambu X1C is printing."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFinishedAgoBoundaries(t *testing.T) {
	now := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		offset time.Duration
		want   string
	}{
		{"zero", 0, ""}, // handled by caller; cover that finishedAgo treats zero specially via offset==now path
		{"30 seconds", 30 * time.Second, "just now"},
		{"5 minutes", 5 * time.Minute, "about 5 minutes ago"},
		{"59 minutes", 59 * time.Minute, "about 59 minutes ago"},
		{"60 minutes", 60 * time.Minute, "about 1 hours ago"},
		{"2h05m", 2*time.Hour + 5*time.Minute, "about 2 hours ago"},
		{"2h30m", 2*time.Hour + 30*time.Minute, "about 2 hours and 30 minutes ago"},
		{"25 hours capped", 25 * time.Hour, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			finishedAt := now.Add(-tc.offset)
			if tc.name == "zero" {
				finishedAt = time.Time{}
			}
			got := finishedAgo(finishedAt, now)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSayHandlerReadsPlanYAML(t *testing.T) {
	dir := t.TempDir()
	yaml := []byte(`projects:
  - name: Viking Guard
    status: in-progress
    plates:
      - name: "3"
        status: in-progress
        printer: Bambu X1C
        needs: []
`)
	if err := os.WriteFile(filepath.Join(dir, "viking.yaml"), yaml, 0644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	s := &PlanServer{PlansDir: dir}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/say", nil)
	w := httptest.NewRecorder()
	s.handleSay(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if ct := w.Result().Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	body, _ := io.ReadAll(w.Result().Body)
	// No live printers configured → say should fall back to "Nothing is printing"
	// even though the plan has an in-progress plate. The endpoint is about
	// what the *printer* says, not what the plan says.
	if string(body) != "Nothing is printing right now." {
		t.Errorf("body = %q", body)
	}
}
