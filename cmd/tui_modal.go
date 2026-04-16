package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/dstockto/fil/api"
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
	modalComplete
)

// tuiModalStage is the sub-state within a modal (e.g. picker vs. confirm).
type tuiModalStage int

const (
	stagePicker tuiModalStage = iota
	stageConfirm
	stageLoading
	stagePreview
	stageNeedsManual
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

// ─────────────────────────────────────────────────────────────────────────────
// Complete modal
// ─────────────────────────────────────────────────────────────────────────────

// completePlateRef identifies a plate candidate for completion.
type completePlateRef struct {
	discoveredIdx int
	projectIdx    int
	plateIdx      int
	projectName   string
	plateName     string
	plateStatus   string // "in-progress" or "todo"
	printer       string
	planName      string
}

// completeDeduction describes a single clean deduction ready to execute.
type completeDeduction struct {
	needName     string
	spoolID      int
	spoolDisplay string
	location     string
	amount       float64
	remaining    float64 // current remaining before deduction
}

// completeIssue describes a need that could not be cleanly matched.
type completeIssue struct {
	needName string
	reason   string
}

// tuiCompleteModal manages the complete-plate flow.
type tuiCompleteModal struct {
	plates      []completePlateRef
	cursor      int
	stage       tuiModalStage
	selected    *completePlateRef
	deductions  []completeDeduction
	issues      []completeIssue
	loadErrText string
}

func (r completePlateRef) displayLine() string {
	line := fmt.Sprintf("%s / %s", models.Sanitize(r.projectName), models.Sanitize(r.plateName))
	suffix := ""
	if r.plateStatus == "in-progress" && r.printer != "" {
		suffix = fmt.Sprintf(" (printing on %s)", models.Sanitize(r.printer))
	} else if r.plateStatus == "todo" {
		suffix = " (todo)"
	}
	return line + suffix
}

// collectCompletablePlates walks plans and returns non-completed plates,
// with in-progress plates sorted first.
func collectCompletablePlates() ([]completePlateRef, []DiscoveredPlan, error) {
	plans, err := discoverPlans()
	if err != nil {
		return nil, nil, err
	}
	var inProg, other []completePlateRef
	for di, dp := range plans {
		for pi, proj := range dp.Plan.Projects {
			if proj.Status == "completed" {
				continue
			}
			for pli, plate := range proj.Plates {
				if plate.Status == "completed" {
					continue
				}
				ref := completePlateRef{
					discoveredIdx: di,
					projectIdx:    pi,
					plateIdx:      pli,
					projectName:   proj.Name,
					plateName:     plate.Name,
					plateStatus:   plate.Status,
					printer:       plate.Printer,
					planName:      dp.DisplayName,
				}
				if plate.Status == "in-progress" {
					inProg = append(inProg, ref)
				} else {
					other = append(other, ref)
				}
			}
		}
	}
	return append(inProg, other...), plans, nil
}

// buildCompletePreview resolves the needs for the selected plate against the
// current Spoolman inventory. It returns either a list of clean deductions
// (safe to execute unattended) or a list of issues that require manual input.
func buildCompletePreview(ctx context.Context, apiClient *api.Client, ref completePlateRef) ([]completeDeduction, []completeIssue, error) {
	plans, err := discoverPlans()
	if err != nil {
		return nil, nil, err
	}
	if ref.discoveredIdx < 0 || ref.discoveredIdx >= len(plans) {
		return nil, nil, fmt.Errorf("invalid plan reference")
	}
	dp := plans[ref.discoveredIdx]
	if ref.projectIdx < 0 || ref.projectIdx >= len(dp.Plan.Projects) {
		return nil, nil, fmt.Errorf("invalid project reference")
	}
	proj := dp.Plan.Projects[ref.projectIdx]
	if ref.plateIdx < 0 || ref.plateIdx >= len(proj.Plates) {
		return nil, nil, fmt.Errorf("invalid plate reference")
	}
	plate := proj.Plates[ref.plateIdx]

	if ref.printer == "" {
		return nil, []completeIssue{{needName: "—", reason: "No printer assigned to this plate"}}, nil
	}
	printerCfg, ok := Cfg.Printers[ref.printer]
	if !ok {
		return nil, []completeIssue{{needName: "—", reason: fmt.Sprintf("Printer %q not in config", ref.printer)}}, nil
	}

	allSpools, err := apiClient.FindSpoolsByName(ctx, "*", onlyStandardFilament, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch spools: %w", err)
	}

	inPrinter := func(loc string) bool {
		for _, l := range printerCfg.Locations {
			if l == loc {
				return true
			}
		}
		return false
	}

	var deductions []completeDeduction
	var issues []completeIssue

	for _, req := range plate.Needs {
		var candidates []models.FindSpool
		for _, s := range allSpools {
			if !inPrinter(s.Location) {
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

		switch {
		case len(candidates) == 0:
			issues = append(issues, completeIssue{needName: req.Name, reason: "no matching spool loaded in printer"})
		case len(candidates) > 1:
			issues = append(issues, completeIssue{needName: req.Name, reason: fmt.Sprintf("%d spools match — needs manual pick", len(candidates))})
		default:
			c := candidates[0]
			if c.RemainingWeight < req.Amount {
				issues = append(issues, completeIssue{
					needName: req.Name,
					reason:   fmt.Sprintf("spool #%d has %.1fg, plate needs %.1fg (split across spools)", c.Id, c.RemainingWeight, req.Amount),
				})
				continue
			}
			deductions = append(deductions, completeDeduction{
				needName:     req.Name,
				spoolID:      c.Id,
				spoolDisplay: fmt.Sprintf("#%d %s", c.Id, models.Sanitize(c.Filament.Name)),
				location:     c.Location,
				amount:       req.Amount,
				remaining:    c.RemainingWeight,
			})
		}
	}

	return deductions, issues, nil
}

// executeComplete applies the planned deductions and marks the plate completed.
func executeComplete(ctx context.Context, apiClient *api.Client, ref completePlateRef, deductions []completeDeduction) tuiCompleteDoneMsg {
	for _, d := range deductions {
		spool, err := apiClient.FindSpoolsById(ctx, d.spoolID)
		if err != nil {
			return tuiCompleteDoneMsg{err: fmt.Errorf("failed to load spool #%d: %w", d.spoolID, err)}
		}
		if err := UseFilamentSafely(ctx, apiClient, spool, d.amount); err != nil {
			return tuiCompleteDoneMsg{err: fmt.Errorf("failed to deduct from spool #%d: %w", d.spoolID, err)}
		}
	}

	plans, err := discoverPlans()
	if err != nil {
		return tuiCompleteDoneMsg{err: fmt.Errorf("failed to reload plans: %w", err)}
	}
	if ref.discoveredIdx < 0 || ref.discoveredIdx >= len(plans) {
		return tuiCompleteDoneMsg{err: fmt.Errorf("invalid plan reference after reload")}
	}
	dp := &plans[ref.discoveredIdx]
	if ref.projectIdx < 0 || ref.projectIdx >= len(dp.Plan.Projects) {
		return tuiCompleteDoneMsg{err: fmt.Errorf("invalid project reference after reload")}
	}
	proj := &dp.Plan.Projects[ref.projectIdx]
	if ref.plateIdx < 0 || ref.plateIdx >= len(proj.Plates) {
		return tuiCompleteDoneMsg{err: fmt.Errorf("invalid plate reference after reload")}
	}

	proj.Plates[ref.plateIdx].Status = "completed"
	proj.Plates[ref.plateIdx].Printer = ""

	allDone := true
	for _, p := range proj.Plates {
		if p.Status != "completed" {
			allDone = false
			break
		}
	}
	if allDone {
		proj.Status = "completed"
	}

	if err := savePlan(*dp, dp.Plan); err != nil {
		return tuiCompleteDoneMsg{err: fmt.Errorf("failed to save plan: %w", err)}
	}

	return tuiCompleteDoneMsg{
		projectName: ref.projectName,
		plateName:   ref.plateName,
	}
}

func (m tuiModel) renderCompleteModal() string {
	cm := m.completeModal
	if cm == nil {
		return ""
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("34"))

	var b strings.Builder

	switch cm.stage {
	case stagePicker:
		b.WriteString(titleStyle.Render(fmt.Sprintf("Complete which plate? (%d available)", len(cm.plates))))
		b.WriteString("\n\n")
		for i, p := range cm.plates {
			line := p.displayLine()
			if i == cm.cursor {
				b.WriteString(selStyle.Render("› " + line))
			} else {
				b.WriteString("  " + line)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(hintStyle.Render("[↑/↓/j/k] navigate  [enter] select  [esc] cancel"))

	case stageLoading:
		b.WriteString(titleStyle.Render("Building preview..."))
		b.WriteString("\n")

	case stagePreview:
		if cm.selected == nil {
			return ""
		}
		b.WriteString(titleStyle.Render("Complete — preview"))
		b.WriteString("\n\n")
		b.WriteString("  Plate: " + cm.selected.displayLine() + "\n")
		b.WriteString("  Plan:  " + models.Sanitize(cm.selected.planName) + "\n\n")
		b.WriteString(okStyle.Render("Filament deductions:"))
		b.WriteString("\n")
		for _, d := range cm.deductions {
			b.WriteString(fmt.Sprintf("  %s → %s (%s)  %.1fg (%.1f → %.1f)\n",
				models.Sanitize(d.needName),
				d.spoolDisplay,
				models.Sanitize(d.location),
				d.amount,
				d.remaining,
				d.remaining-d.amount,
			))
		}
		b.WriteString("\n")
		b.WriteString(hintStyle.Render("[y/enter] confirm  [n/esc] cancel"))

	case stageNeedsManual:
		if cm.selected == nil {
			return ""
		}
		b.WriteString(titleStyle.Render("Complete — manual input required"))
		b.WriteString("\n\n")
		b.WriteString("  Plate: " + cm.selected.displayLine() + "\n\n")
		if len(cm.deductions) > 0 {
			b.WriteString(okStyle.Render("Would auto-deduct:"))
			b.WriteString("\n")
			for _, d := range cm.deductions {
				b.WriteString(fmt.Sprintf("  %s → %s  %.1fg\n",
					models.Sanitize(d.needName), d.spoolDisplay, d.amount))
			}
			b.WriteString("\n")
		}
		b.WriteString(warnStyle.Render("Needs manual input:"))
		b.WriteString("\n")
		for _, issue := range cm.issues {
			b.WriteString(fmt.Sprintf("  %s — %s\n", models.Sanitize(issue.needName), issue.reason))
		}
		b.WriteString("\n")
		if cm.loadErrText != "" {
			b.WriteString(warnStyle.Render("  " + cm.loadErrText))
			b.WriteString("\n\n")
		}
		b.WriteString(hintStyle.Render("Use `fil plan complete` from the shell to handle this.  [esc] close"))
	}

	return b.String()
}

// tuiCompletePreviewReadyMsg is emitted when the preview build finishes.
type tuiCompletePreviewReadyMsg struct {
	deductions []completeDeduction
	issues     []completeIssue
	err        error
}

// tuiCompleteDoneMsg is emitted when the complete action finishes.
type tuiCompleteDoneMsg struct {
	projectName string
	plateName   string
	err         error
}
