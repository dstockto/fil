package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

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
	modalNext
)

// tuiModalStage is the sub-state within a modal (e.g. picker vs. confirm).
type tuiModalStage int

const (
	stagePicker tuiModalStage = iota
	stageConfirm
	stageLoading
	stagePreview
	stageNeedsManual
	stagePrinterPicker
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

	if PlanOps == nil {
		return tuiStopDoneMsg{err: fmt.Errorf("plan operations not configured")}
	}
	if err := PlanOps.SaveAll(context.Background(), planFileName(*dp), dp.Plan); err != nil {
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

	if PlanOps == nil {
		return tuiCompleteDoneMsg{err: fmt.Errorf("plan operations not configured")}
	}
	if err := PlanOps.SaveAll(context.Background(), planFileName(*dp), dp.Plan); err != nil {
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

// ─────────────────────────────────────────────────────────────────────────────
// Next modal
// ─────────────────────────────────────────────────────────────────────────────

// nextPlateRef identifies a todo plate for the next modal with swap cost.
type nextPlateRef struct {
	discoveredIdx int
	projectIdx    int
	plateIdx      int
	projectName   string
	plateName     string
	planName      string
	needs         []models.PlateRequirement
	swapCost      int
	isReady       bool
}

// nextKeepOp represents a need that is already satisfied by loaded spools.
type nextKeepOp struct {
	needName     string
	spoolID      int
	spoolDisplay string
	location     string
	remaining    float64
}

// nextLoadOp represents a spool to be loaded into a printer slot.
type nextLoadOp struct {
	needName     string
	spoolID      int
	spoolDisplay string
	fromLocation string
	toLocation   string
	slotPos      int // 1-based slot position within the location
}

// nextIssue represents a need that can't be cleanly resolved in the happy path.
type nextIssue struct {
	needName string
	reason   string
}

// tuiNextModal manages the plan-next flow.
type tuiNextModal struct {
	stage           tuiModalStage
	loadingMsg      string
	printerNames    []string
	printerCursor   int
	selectedPrinter string
	plates          []nextPlateRef
	plateCursor     int
	selectedPlate   *nextPlateRef
	keepOps         []nextKeepOp
	loadOps         []nextLoadOp
	issues          []nextIssue
}

// nextDisplayPlate is used for rendering the plate picker line.
func (r nextPlateRef) displayLine() string {
	line := fmt.Sprintf("%s / %s", models.Sanitize(r.projectName), models.Sanitize(r.plateName))
	meta := fmt.Sprintf(" [swaps: %d]", r.swapCost)
	if !r.isReady {
		meta += " (insufficient filament)"
	}
	return line + meta
}

// nextPreparedData carries results from the async prepare step.
type nextPreparedData struct {
	plates []nextPlateRef
	err    error
}

// nextPreviewReady carries results from the async preview build.
type nextPreviewReady struct {
	keepOps []nextKeepOp
	loadOps []nextLoadOp
	issues  []nextIssue
	err     error
}

// tuiNextDoneMsg is emitted when the plan-next action finishes.
type tuiNextDoneMsg struct {
	projectName string
	plateName   string
	err         error
}

// preparePlates fetches spools and computes per-plate swap cost for the chosen printer.
func preparePlates(ctx context.Context, apiClient *api.Client, printerName string) nextPreparedData {
	plans, err := discoverPlans()
	if err != nil {
		return nextPreparedData{err: err}
	}
	printerCfg, ok := Cfg.Printers[printerName]
	if !ok {
		return nextPreparedData{err: fmt.Errorf("printer %q not in config", printerName)}
	}
	printerLocs := printerCfg.Locations

	allSpools, err := apiClient.FindSpoolsByName(ctx, "*", onlyStandardFilament, nil)
	if err != nil {
		return nextPreparedData{err: fmt.Errorf("failed to fetch spools: %w", err)}
	}

	// Build set of loaded spools
	loadedInPrinter := map[int]models.FindSpool{}
	for _, s := range allSpools {
		for _, loc := range printerLocs {
			if s.Location == loc {
				loadedInPrinter[s.Id] = s
			}
		}
	}

	var plates []nextPlateRef
	for di, dp := range plans {
		for pi, proj := range dp.Plan.Projects {
			if proj.Status == "completed" {
				continue
			}
			for pli, plate := range proj.Plates {
				if plate.Status != "todo" {
					continue
				}

				cost := 0
				ready := true
				for _, req := range plate.Needs {
					foundInPrinter := false
					for _, s := range loadedInPrinter {
						if s.Filament.Id == req.FilamentID {
							foundInPrinter = true
							break
						}
					}
					if !foundInPrinter {
						cost++
					}
					total := 0.0
					for _, s := range allSpools {
						if !s.Archived && s.Filament.Id == req.FilamentID {
							total += s.RemainingWeight
						}
					}
					if total < req.Amount {
						ready = false
					}
				}

				plates = append(plates, nextPlateRef{
					discoveredIdx: di,
					projectIdx:    pi,
					plateIdx:      pli,
					projectName:   proj.Name,
					plateName:     plate.Name,
					planName:      dp.DisplayName,
					needs:         plate.Needs,
					swapCost:      cost,
					isReady:       ready,
				})
			}
		}
	}

	// Sort: ready first, then lowest swap cost
	sort.SliceStable(plates, func(i, j int) bool {
		if plates[i].isReady != plates[j].isReady {
			return plates[i].isReady
		}
		return plates[i].swapCost < plates[j].swapCost
	})

	return nextPreparedData{plates: plates}
}

// buildNextPreview resolves each need to either a keep or a single load op,
// or records an issue that pushes the flow to the CLI.
func buildNextPreview(ctx context.Context, apiClient *api.Client, printerName string, plate nextPlateRef) nextPreviewReady {
	printerCfg, ok := Cfg.Printers[printerName]
	if !ok {
		return nextPreviewReady{err: fmt.Errorf("printer %q not in config", printerName)}
	}
	printerLocs := printerCfg.Locations

	allSpools, err := apiClient.FindSpoolsByName(ctx, "*", onlyStandardFilament, nil)
	if err != nil {
		return nextPreviewReady{err: fmt.Errorf("failed to fetch spools: %w", err)}
	}

	// Location -> spool IDs currently loaded (simulated, will update as we plan ops)
	loaded := map[string][]models.FindSpool{}
	for _, s := range allSpools {
		for _, loc := range printerLocs {
			if s.Location == loc {
				loaded[loc] = append(loaded[loc], s)
			}
		}
	}

	// Pre-collect all locations that are assigned to ANY printer (for "in other printer" detection)
	allPrinterLocations := make(map[string]string)
	for pName, pCfg := range Cfg.Printers {
		for _, l := range pCfg.Locations {
			allPrinterLocations[l] = pName
		}
	}

	locCapacity := func(loc string) int {
		if Cfg.LocationCapacity != nil {
			if lc, ok := Cfg.LocationCapacity[loc]; ok && lc.Capacity > 0 {
				return lc.Capacity
			}
		}
		return 1
	}

	findEmptyLocation := func() (string, bool) {
		best := ""
		bestLoad := 999
		for _, loc := range printerLocs {
			cap := locCapacity(loc)
			cur := len(loaded[loc])
			if cur < cap && cur < bestLoad {
				best = loc
				bestLoad = cur
			}
		}
		return best, best != ""
	}

	var keepOps []nextKeepOp
	var loadOps []nextLoadOp
	var issues []nextIssue

	for _, req := range plate.needs {
		// Collect all matching loaded spools (by filament_id)
		var loadedMatches []models.FindSpool
		totalLoaded := 0.0
		for _, loc := range printerLocs {
			for _, s := range loaded[loc] {
				if s.Filament.Id == req.FilamentID {
					loadedMatches = append(loadedMatches, s)
					totalLoaded += s.RemainingWeight
				}
			}
		}

		if len(loadedMatches) > 0 && totalLoaded >= req.Amount {
			// Already loaded with enough
			for _, s := range loadedMatches {
				keepOps = append(keepOps, nextKeepOp{
					needName:     req.Name,
					spoolID:      s.Id,
					spoolDisplay: fmt.Sprintf("#%d %s", s.Id, models.Sanitize(s.Filament.Name)),
					location:     s.Location,
					remaining:    s.RemainingWeight,
				})
			}
			continue
		}

		if len(loadedMatches) > 0 && totalLoaded < req.Amount {
			issues = append(issues, nextIssue{
				needName: req.Name,
				reason:   fmt.Sprintf("loaded has %.1fg, plate needs %.1fg — needs overflow spool decision", totalLoaded, req.Amount),
			})
			continue
		}

		// Not loaded — find best candidate by CLI priority:
		// 1. Not in any printer location
		// 2. Partially used (UsedWeight > 0)
		// 3. Oldest (lowest ID)
		var candidates []models.FindSpool
		for _, s := range allSpools {
			if !s.Archived && s.Filament.Id == req.FilamentID {
				candidates = append(candidates, s)
			}
		}
		if len(candidates) == 0 {
			issues = append(issues, nextIssue{
				needName: req.Name,
				reason:   "no spools with this filament_id available",
			})
			continue
		}

		var best *models.FindSpool
		for i := range candidates {
			s := &candidates[i]
			if best == nil {
				best = s
				continue
			}
			_, curInPrinter := allPrinterLocations[best.Location]
			_, newInPrinter := allPrinterLocations[s.Location]
			if curInPrinter && !newInPrinter {
				best = s
				continue
			}
			if !curInPrinter && newInPrinter {
				continue
			}
			if best.UsedWeight == 0 && s.UsedWeight > 0 {
				best = s
				continue
			}
			if (best.UsedWeight > 0) == (s.UsedWeight > 0) && s.Id < best.Id {
				best = s
			}
		}
		if best == nil {
			issues = append(issues, nextIssue{needName: req.Name, reason: "could not pick a best spool"})
			continue
		}

		if otherPrinter, inOther := allPrinterLocations[best.Location]; inOther && otherPrinter != printerName {
			issues = append(issues, nextIssue{
				needName: req.Name,
				reason:   fmt.Sprintf("best spool #%d is in %s (%s) — needs manual confirmation", best.Id, otherPrinter, best.Location),
			})
			continue
		}

		// Find an empty slot in this printer
		targetLoc, found := findEmptyLocation()
		if !found {
			issues = append(issues, nextIssue{
				needName: req.Name,
				reason:   "no empty slot available — would need to unload something",
			})
			continue
		}

		// Simulate the load so the next iteration sees the updated state
		loaded[targetLoc] = append(loaded[targetLoc], *best)

		loadOps = append(loadOps, nextLoadOp{
			needName:     req.Name,
			spoolID:      best.Id,
			spoolDisplay: fmt.Sprintf("#%d %s", best.Id, models.Sanitize(best.Filament.Name)),
			fromLocation: best.Location,
			toLocation:   targetLoc,
		})

		_ = ctx // keep linter happy
	}

	return nextPreviewReady{keepOps: keepOps, loadOps: loadOps, issues: issues}
}

// executeNext performs all the planned swaps and marks the plate in-progress.
func executeNext(ctx context.Context, apiClient *api.Client, printerName string, plate nextPlateRef, loadOps []nextLoadOp) tuiNextDoneMsg {
	// Load current orders once
	orders, err := LoadLocationOrders(ctx, apiClient)
	if err != nil {
		return tuiNextDoneMsg{err: fmt.Errorf("failed to load location orders: %w", err)}
	}

	// Apply each load op
	for i, op := range loadOps {
		// Move the spool in Spoolman
		if err := apiClient.MoveSpool(ctx, op.spoolID, op.toLocation); err != nil {
			return tuiNextDoneMsg{err: fmt.Errorf("failed to move spool #%d: %w", op.spoolID, err)}
		}

		// Update orders: remove from wherever and place in target
		orders = RemoveFromAllOrders(orders, op.spoolID)
		list := orders[op.toLocation]
		if IsPrinterLocation(op.toLocation) {
			emptyIdx := FirstEmptySlot(list)
			if emptyIdx >= 0 {
				list[emptyIdx] = op.spoolID
				loadOps[i].slotPos = emptyIdx + 1
			} else {
				list = append(list, op.spoolID)
				loadOps[i].slotPos = len(list)
			}
		} else {
			list = append(list, op.spoolID)
			loadOps[i].slotPos = len(list)
		}
		orders[op.toLocation] = list
	}

	if err := apiClient.PostSettingObject(ctx, "locations_spoolorders", orders); err != nil {
		return tuiNextDoneMsg{err: fmt.Errorf("failed to update locations_spoolorders: %w", err)}
	}

	// Push trays to printer for each load op (if applicable)
	if Cfg.PlansServer != "" {
		planClient := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
		for _, op := range loadOps {
			if !IsPrinterLocation(op.toLocation) {
				continue
			}
			mapping := MapLocationToTray(op.toLocation, op.slotPos)
			if mapping == nil || !mapping.SupportsTrayPush() {
				continue
			}
			spool, err := apiClient.FindSpoolsById(ctx, op.spoolID)
			if err != nil {
				continue // tray push is best-effort
			}
			colorHex := strings.TrimPrefix(spool.Filament.ColorHex, "#")
			if len(colorHex) == 6 {
				colorHex += "FF"
			}
			trayType := spool.Filament.Material
			infoIdx := ""
			if profile := LookupFilamentProfile(spool.Filament.Vendor.Name, spool.Filament.Name, spool.Filament.Material); profile != nil {
				trayType = profile.TrayType
				infoIdx = profile.InfoIdx
			}
			_ = planClient.PushTray(ctx, mapping.PrinterName, api.TrayPushRequest{
				AmsID:   mapping.AmsID,
				TrayID:  mapping.TrayID,
				Color:   strings.ToUpper(colorHex),
				Type:    trayType,
				TempMin: 190,
				TempMax: 240,
				InfoIdx: infoIdx,
			})
		}
	}

	// Mark plate in-progress
	plans, err := discoverPlans()
	if err != nil {
		return tuiNextDoneMsg{err: fmt.Errorf("failed to reload plans: %w", err)}
	}
	if plate.discoveredIdx < 0 || plate.discoveredIdx >= len(plans) {
		return tuiNextDoneMsg{err: fmt.Errorf("invalid plan reference after reload")}
	}
	dp := &plans[plate.discoveredIdx]
	if plate.projectIdx < 0 || plate.projectIdx >= len(dp.Plan.Projects) {
		return tuiNextDoneMsg{err: fmt.Errorf("invalid project reference after reload")}
	}
	proj := &dp.Plan.Projects[plate.projectIdx]
	if plate.plateIdx < 0 || plate.plateIdx >= len(proj.Plates) {
		return tuiNextDoneMsg{err: fmt.Errorf("invalid plate reference after reload")}
	}
	proj.Plates[plate.plateIdx].Status = "in-progress"
	proj.Plates[plate.plateIdx].Printer = printerName
	proj.Plates[plate.plateIdx].StartedAt = time.Now().Format(time.RFC3339)
	if proj.Status == "todo" {
		proj.Status = "in-progress"
	}
	if PlanOps == nil {
		return tuiNextDoneMsg{err: fmt.Errorf("plan operations not configured")}
	}
	if err := PlanOps.SaveAll(context.Background(), planFileName(*dp), dp.Plan); err != nil {
		return tuiNextDoneMsg{err: fmt.Errorf("failed to save plan: %w", err)}
	}

	return tuiNextDoneMsg{
		projectName: plate.projectName,
		plateName:   plate.plateName,
	}
}

// renderNextModal renders the next modal content.
func (m tuiModel) renderNextModal() string {
	nm := m.nextModal
	if nm == nil {
		return ""
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("34"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var b strings.Builder

	switch nm.stage {
	case stagePrinterPicker:
		b.WriteString(titleStyle.Render("Which printer?"))
		b.WriteString("\n\n")
		for i, name := range nm.printerNames {
			if i == nm.printerCursor {
				b.WriteString(selStyle.Render("› " + name))
			} else {
				b.WriteString("  " + name)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(hintStyle.Render("[↑/↓/j/k] navigate  [enter] select  [esc] cancel"))

	case stageLoading:
		msg := nm.loadingMsg
		if msg == "" {
			msg = "Loading..."
		}
		b.WriteString(titleStyle.Render(msg))
		b.WriteString("\n")

	case stagePicker:
		b.WriteString(titleStyle.Render(fmt.Sprintf("Select plate for %s (%d todo)", nm.selectedPrinter, len(nm.plates))))
		b.WriteString("\n\n")
		if len(nm.plates) == 0 {
			b.WriteString("  " + dimStyle.Render("No todo plates found") + "\n")
		}
		for i, p := range nm.plates {
			line := p.displayLine()
			if i == nm.plateCursor {
				b.WriteString(selStyle.Render("› " + line))
			} else {
				b.WriteString("  " + line)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(hintStyle.Render("[↑/↓/j/k] navigate  [enter] select  [esc] cancel"))

	case stagePreview:
		if nm.selectedPlate == nil {
			return ""
		}
		b.WriteString(titleStyle.Render("Ready to start — preview"))
		b.WriteString("\n\n")
		b.WriteString("  Printer: " + models.Sanitize(nm.selectedPrinter) + "\n")
		b.WriteString("  Plate:   " + nm.selectedPlate.displayLine() + "\n")
		b.WriteString("  Plan:    " + models.Sanitize(nm.selectedPlate.planName) + "\n\n")
		if len(nm.keepOps) > 0 {
			b.WriteString(okStyle.Render("Already loaded:"))
			b.WriteString("\n")
			for _, k := range nm.keepOps {
				b.WriteString(fmt.Sprintf("  ✓ %s → %s in %s (%.1fg)\n",
					models.Sanitize(k.needName), k.spoolDisplay, models.Sanitize(k.location), k.remaining))
			}
			b.WriteString("\n")
		}
		if len(nm.loadOps) > 0 {
			b.WriteString(okStyle.Render("Load:"))
			b.WriteString("\n")
			for _, l := range nm.loadOps {
				from := l.fromLocation
				if from == "" {
					from = "(unassigned)"
				}
				b.WriteString(fmt.Sprintf("  → %s: load %s from %s into %s\n",
					models.Sanitize(l.needName), l.spoolDisplay, models.Sanitize(from), models.Sanitize(l.toLocation)))
			}
			b.WriteString("\n")
		}
		b.WriteString(hintStyle.Render("Do the physical swaps, then confirm.  [y/enter] confirm  [n/esc] cancel"))

	case stageNeedsManual:
		if nm.selectedPlate == nil {
			return ""
		}
		b.WriteString(titleStyle.Render("Start — manual input required"))
		b.WriteString("\n\n")
		b.WriteString("  Printer: " + models.Sanitize(nm.selectedPrinter) + "\n")
		b.WriteString("  Plate:   " + nm.selectedPlate.displayLine() + "\n\n")
		if len(nm.keepOps) > 0 {
			b.WriteString(okStyle.Render("Already loaded:"))
			b.WriteString("\n")
			for _, k := range nm.keepOps {
				b.WriteString(fmt.Sprintf("  ✓ %s → %s\n", models.Sanitize(k.needName), k.spoolDisplay))
			}
			b.WriteString("\n")
		}
		if len(nm.loadOps) > 0 {
			b.WriteString(okStyle.Render("Would load:"))
			b.WriteString("\n")
			for _, l := range nm.loadOps {
				b.WriteString(fmt.Sprintf("  → %s → %s\n", models.Sanitize(l.needName), l.spoolDisplay))
			}
			b.WriteString("\n")
		}
		b.WriteString(warnStyle.Render("Needs manual input:"))
		b.WriteString("\n")
		for _, issue := range nm.issues {
			b.WriteString(fmt.Sprintf("  %s — %s\n", models.Sanitize(issue.needName), issue.reason))
		}
		b.WriteString("\n")
		b.WriteString(hintStyle.Render("Use `fil plan next` from the shell to handle this.  [esc] close"))
	}

	return b.String()
}
