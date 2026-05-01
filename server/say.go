package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dstockto/fil/models"
	"gopkg.in/yaml.v3"
)

// platePrinting is the per-printer plate info merged in from plan YAMLs.
type platePrinting struct {
	Project string
	Plate   string
}

// handleSay returns a TTS-friendly plain-text summary of what's printing.
// Designed for an iOS Shortcut piped to Speak Text — no JSON, no markup.
// On-network use only; the server has no auth.
func (s *PlanServer) handleSay(w http.ResponseWriter, r *http.Request) {
	plates := s.readInProgressPlates()

	var states []PrinterState
	if s.Printers != nil {
		states = s.Printers.AllStatus()
	}

	msg := formatSayResponse(plates, states, time.Now())
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(msg))
}

// readInProgressPlates scans PlansDir for in-progress plates, keyed by the
// printer field on the plate. When two plates target the same printer (a
// Bambu multi-plate batch), only the first encountered is reported — the
// "what's printing right now" question can't reasonably name two plates.
func (s *PlanServer) readInProgressPlates() map[string]platePrinting {
	out := map[string]platePrinting{}
	if s.PlansDir == "" {
		return out
	}
	entries, err := os.ReadDir(s.PlansDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.PlansDir, e.Name()))
		if err != nil {
			continue
		}
		var plan models.PlanFile
		if yaml.Unmarshal(data, &plan) != nil {
			continue
		}
		for _, proj := range plan.Projects {
			for _, plate := range proj.Plates {
				if plate.Status != "in-progress" || plate.Printer == "" {
					continue
				}
				if _, exists := out[plate.Printer]; exists {
					continue
				}
				out[plate.Printer] = platePrinting{Project: proj.Name, Plate: plate.Name}
			}
		}
	}
	return out
}

// formatSayResponse builds the spoken summary. now is injected so callers
// (and tests) can pin ETA/elapsed math.
func formatSayResponse(plates map[string]platePrinting, printers []PrinterState, now time.Time) string {
	// Pick out the printers worth mentioning — anything that isn't idle or
	// offline. Finished printers stay in the list until cleared, so they
	// get reported with "finished N minutes ago".
	var active []PrinterState
	for _, p := range printers {
		switch p.State {
		case "printing", "paused", "failed", "finished":
			active = append(active, p)
		}
	}
	if len(active) == 0 {
		return "Nothing is printing right now."
	}

	sort.Slice(active, func(i, j int) bool { return active[i].Name < active[j].Name })

	parts := make([]string, 0, len(active))
	for _, p := range active {
		parts = append(parts, sayPrinter(p, plates[p.Name], now))
	}
	return strings.Join(parts, " ")
}

// sayPrinter is one printer's sentence.
func sayPrinter(p PrinterState, plate platePrinting, now time.Time) string {
	plateRef := plateDescription(plate, p)

	switch p.State {
	case "printing":
		return printingSentence(p, plateRef, now)
	case "paused":
		if plateRef != "" {
			return fmt.Sprintf("%s is paused on %s.", p.Name, plateRef)
		}
		return fmt.Sprintf("%s is paused.", p.Name)
	case "failed":
		return fmt.Sprintf("%s has failed.", p.Name)
	case "finished":
		ago := finishedAgo(p.LastFinishedAt, now)
		if plateRef != "" && ago != "" {
			return fmt.Sprintf("%s finished %s %s.", p.Name, plateRef, ago)
		}
		if plateRef != "" {
			return fmt.Sprintf("%s finished %s.", p.Name, plateRef)
		}
		if ago != "" {
			return fmt.Sprintf("%s finished a print %s.", p.Name, ago)
		}
		return fmt.Sprintf("%s has finished.", p.Name)
	}
	return fmt.Sprintf("%s is %s.", p.Name, p.State)
}

// printingSentence formats the active-print case with progress + ETA when known.
func printingSentence(p PrinterState, plateRef string, now time.Time) string {
	var b strings.Builder
	b.WriteString(p.Name)
	b.WriteString(" is printing")
	if plateRef != "" {
		b.WriteString(" ")
		b.WriteString(plateRef)
	}

	// "45 percent" reads better than "45%" through TTS.
	if p.Progress > 0 {
		fmt.Fprintf(&b, ", %d percent", p.Progress)
	}

	if p.RemainingMins > 0 {
		eta := now.Add(time.Duration(p.RemainingMins) * time.Minute)
		fmt.Fprintf(&b, ", finishing around %s", formatClockTime(eta))
	}

	b.WriteString(".")
	return b.String()
}

// plateDescription returns the spoken phrase for the current plate/job, or
// an empty string when nothing meaningful is known.
func plateDescription(plate platePrinting, p PrinterState) string {
	if plate.Project != "" && plate.Plate != "" {
		return fmt.Sprintf("%s, plate %s", plate.Project, plate.Plate)
	}
	if plate.Project != "" {
		return plate.Project
	}
	// Fallback to whatever the printer's reporting as the file. Strip the
	// extension since gcode/3mf endings sound bad through TTS.
	if p.CurrentFile != "" {
		return trimExt(p.CurrentFile)
	}
	return ""
}

// formatClockTime renders a time.Time as a 12-hour clock string Siri can
// pronounce cleanly. Drops minutes when on the hour.
func formatClockTime(t time.Time) string {
	if t.Minute() == 0 {
		return t.Format("3 PM")
	}
	return t.Format("3:04 PM")
}

// finishedAgo describes how long ago a print finished. Returns an empty
// string if the timestamp is zero or implausibly far away.
func finishedAgo(finishedAt time.Time, now time.Time) string {
	if finishedAt.IsZero() {
		return ""
	}
	d := now.Sub(finishedAt)
	if d < 0 || d > 24*time.Hour {
		return ""
	}
	if d < time.Minute {
		return "just now"
	}
	mins := int(d.Minutes())
	if mins < 60 {
		return fmt.Sprintf("about %d minutes ago", mins)
	}
	hours := mins / 60
	leftover := mins % 60
	if leftover < 10 {
		return fmt.Sprintf("about %d hours ago", hours)
	}
	return fmt.Sprintf("about %d hours and %d minutes ago", hours, leftover)
}

func trimExt(name string) string {
	if i := strings.LastIndex(name, "."); i > 0 {
		return name[:i]
	}
	return name
}
