package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/dstockto/fil/models"
)

// ─────────────────────────────────────────────────────────────────────────────
// Modal infrastructure
//
// Modals are self-contained overlays that take control of key input and viewport
// content until dismissed. They render in the middle (scrollable) section; the
// header/footer continue to render normally but may show modal-specific hints.
// ─────────────────────────────────────────────────────────────────────────────

// tuiModalKind identifies which modal (if any) is currently active.
type tuiModalKind int

const (
	modalNone tuiModalKind = iota
	modalStop
)

// tuiModalStage is the sub-state within a modal (e.g. picker vs. confirm).
type tuiModalStage int

const (
	stagePicker tuiModalStage = iota
	stageConfirm
)

// stopPlateRef holds a pointer back into the plans data for the stop modal.
type stopPlateRef struct {
	discoveredIdx int
	projectIdx    int
	plateIdx      int
	projectName   string
	plateName     string
	printer       string
	planName      string
}

// tuiStopModal manages the stop-plate flow: optional picker, then confirmation.
type tuiStopModal struct {
	plates []stopPlateRef
	cursor int
	stage  tuiModalStage
}

// displayLine returns a human-readable line for a plate in the picker.
func (r stopPlateRef) displayLine() string {
	line := fmt.Sprintf("%s / %s", models.Sanitize(r.projectName), models.Sanitize(r.plateName))
	if r.printer != "" {
		line += fmt.Sprintf(" (on %s)", models.Sanitize(r.printer))
	}
	return line
}

// collectInProgressPlates walks all discovered plans and returns in-progress plates.
func collectInProgressPlates() ([]stopPlateRef, []DiscoveredPlan, error) {
	plans, err := discoverPlans()
	if err != nil {
		return nil, nil, err
	}
	var refs []stopPlateRef
	for di, dp := range plans {
		for pi, proj := range dp.Plan.Projects {
			if proj.Status == "completed" {
				continue
			}
			for pli, plate := range proj.Plates {
				if plate.Status != "in-progress" {
					continue
				}
				refs = append(refs, stopPlateRef{
					discoveredIdx: di,
					projectIdx:    pi,
					plateIdx:      pli,
					projectName:   proj.Name,
					plateName:     plate.Name,
					printer:       plate.Printer,
					planName:      dp.DisplayName,
				})
			}
		}
	}
	return refs, plans, nil
}

// renderStopModal renders the stop modal content for the viewport.
func (m tuiModel) renderStopModal() string {
	sm := m.stopModal
	if sm == nil {
		return ""
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51"))

	var b strings.Builder

	switch sm.stage {
	case stagePicker:
		b.WriteString(titleStyle.Render(fmt.Sprintf("Stop which plate? (%d in progress)", len(sm.plates))))
		b.WriteString("\n\n")
		for i, p := range sm.plates {
			line := p.displayLine()
			if i == sm.cursor {
				b.WriteString(selStyle.Render("› " + line))
			} else {
				b.WriteString("  " + line)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(hintStyle.Render("[↑/↓/j/k] navigate  [enter] select  [esc] cancel"))
	case stageConfirm:
		if sm.cursor < 0 || sm.cursor >= len(sm.plates) {
			return ""
		}
		p := sm.plates[sm.cursor]
		b.WriteString(titleStyle.Render("Confirm stop"))
		b.WriteString("\n\n")
		b.WriteString("  Plate: " + p.displayLine() + "\n")
		b.WriteString("  Plan:  " + models.Sanitize(p.planName) + "\n")
		b.WriteString("\n")
		b.WriteString("This will set the plate back to todo and clear its printer/start time.\n\n")
		b.WriteString(hintStyle.Render("[y] yes  [n/esc] cancel"))
	}

	return b.String()
}

// stopSelectedPlate saves the plan with the selected plate reverted to todo.
func stopSelectedPlate(ref stopPlateRef, plans []DiscoveredPlan) tuiStopDoneMsg {
	if ref.discoveredIdx < 0 || ref.discoveredIdx >= len(plans) {
		return tuiStopDoneMsg{err: fmt.Errorf("invalid plan reference")}
	}
	dp := &plans[ref.discoveredIdx]
	if ref.projectIdx < 0 || ref.projectIdx >= len(dp.Plan.Projects) {
		return tuiStopDoneMsg{err: fmt.Errorf("invalid project reference")}
	}
	proj := &dp.Plan.Projects[ref.projectIdx]
	if ref.plateIdx < 0 || ref.plateIdx >= len(proj.Plates) {
		return tuiStopDoneMsg{err: fmt.Errorf("invalid plate reference")}
	}
	proj.Plates[ref.plateIdx].Status = "todo"
	proj.Plates[ref.plateIdx].Printer = ""
	proj.Plates[ref.plateIdx].StartedAt = ""

	if err := savePlan(*dp, dp.Plan); err != nil {
		return tuiStopDoneMsg{err: fmt.Errorf("failed to save plan: %w", err)}
	}

	return tuiStopDoneMsg{
		projectName: ref.projectName,
		plateName:   ref.plateName,
	}
}

// tuiStopDoneMsg is emitted after a stop action completes (successfully or not).
type tuiStopDoneMsg struct {
	projectName string
	plateName   string
	err         error
}
