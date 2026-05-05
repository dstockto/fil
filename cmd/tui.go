package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Interactive full-screen dashboard",
	Long:  "Live-updating dashboard showing printer status, current prints, and upcoming plates.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || len(Cfg.Printers) == 0 {
			return fmt.Errorf("no printers configured")
		}

		refresh, _ := cmd.Flags().GetDuration("refresh")

		m := newTUIModel(refresh)
		p := tea.NewProgram(m, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			return err
		}
		return nil
	},
}

//nolint:gochecknoinits
func init() {
	rootCmd.AddCommand(tuiCmd)
	tuiCmd.Flags().Duration("refresh", 5*time.Second, "data refresh interval")
}

// ─────────────────────────────────────────────────────────────────────────────
// Model
// ─────────────────────────────────────────────────────────────────────────────

type tuiViewMode int

const (
	viewDashboard tuiViewMode = iota
	viewPlans
)

type tuiModel struct {
	// data
	printerStatuses map[string]api.PrinterStatus
	liveStatuses    []api.PrinterStatus
	printerMap      map[string][]tuiPrintingInfo
	activePrinters  []string
	idlePrinters    []string
	todoPlates      []tuiTodoPlate
	mismatches      []TrayMismatch
	totalTodo       int
	activePlanCount int
	plans           []tuiPlanSummary

	// ui
	view            tuiViewMode
	viewport        viewport.Model
	width           int
	height          int
	ready           bool // viewport initialized
	lastRefresh     time.Time
	refreshInterval time.Duration
	err             error
	quitting        bool

	// plans view state
	planCursor   int          // which plan the cursor is on
	planExpanded map[int]bool // which plans are expanded

	// filter mode
	filtering        bool
	filterInput      textinput.Model
	filteredIdxs     []int // indices into m.plans matching the filter
	filteredTodoIdxs []int // indices into m.todoPlates matching the filter

	// transient status message (shown in footer briefly)
	statusMsg   string
	statusError bool

	// modal state (nil when no modal is active)
	modal         tuiModalKind
	stopModal     *tuiStopModal
	completeModal *tuiCompleteModal
	nextModal     *tuiNextModal
}

// tuiPlanSummary holds the display data for a single plan in the plans view.
type tuiPlanSummary struct {
	Name        string
	RemoteName  string // server-side filename (empty for local-only plans)
	Path        string // filesystem path (empty for remote-only plans)
	Remote      bool
	HasAssembly bool
	Projects    []tuiProjectSummary
}

type tuiProjectSummary struct {
	Name   string
	Status string
	Plates []tuiPlateSummary
}

type tuiPlateSummary struct {
	Name    string
	Status  string   // "todo", "in-progress", "completed"
	Printer string   // set when in-progress
	Colors  []string // hex colors from plate needs
}

type tuiPrintingInfo struct {
	Project           string
	Plate             string
	StartedAt         string
	EstimatedDuration string
}

type tuiTodoPlate struct {
	PlanName    string
	ProjectName string
	PlateName   string
	BestPrinter string   // printer with lowest swap cost
	SwapCost    int      // swaps needed on BestPrinter
	IsReady     bool     // sufficient filament inventory
	Colors      []string // hex colors from plate needs (for swatches)
}

func newTUIModel(refresh time.Duration) tuiModel {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.CharLimit = 64

	return tuiModel{
		refreshInterval: refresh,
		printerStatuses: make(map[string]api.PrinterStatus),
		printerMap:      make(map[string][]tuiPrintingInfo),
		planExpanded:    make(map[int]bool),
		filterInput:     ti,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Messages
// ─────────────────────────────────────────────────────────────────────────────

type tuiTickMsg time.Time

type tuiDataMsg struct {
	printerStatuses map[string]api.PrinterStatus
	liveStatuses    []api.PrinterStatus
	printerMap      map[string][]tuiPrintingInfo
	activePrinters  []string
	idlePrinters    []string
	todoPlates      []tuiTodoPlate
	mismatches      []TrayMismatch
	totalTodo       int
	activePlanCount int
	plans           []tuiPlanSummary
}

type tuiErrMsg error

type tuiStatusMsg struct {
	text    string
	isError bool
}

type tuiClearStatusMsg struct{}

// ─────────────────────────────────────────────────────────────────────────────
// Init / Update
// ─────────────────────────────────────────────────────────────────────────────

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(
		fetchTUIData,
		tickCmd(m.refreshInterval),
	)
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Modal mode: route keys to the active modal before anything else
		if m.modal != modalNone {
			return m.updateModal(msg)
		}
		// Filter mode: send keys to text input
		if m.filtering {
			switch msg.String() {
			case "esc":
				// Cancel filter, clear text, show all
				m.filtering = false
				m.filterInput.Reset()
				m.filteredIdxs = nil
				m.filteredTodoIdxs = nil
				m.planCursor = 0
				m.filterInput.Blur()
				m = resizeViewport(m)
				m.viewport.SetContent(m.renderScrollable())
				m.viewport.GotoTop()
				return m, nil
			case "enter":
				// Accept filter and return to normal navigation
				m.filtering = false
				m.filterInput.Blur()
				m = resizeViewport(m)
				m.viewport.SetContent(m.renderScrollable())
				return m, nil
			case "ctrl+c":
				m.quitting = true
				return m, tea.Quit
			default:
				var cmd tea.Cmd
				m.filterInput, cmd = m.filterInput.Update(msg)
				m.applyFilter()
				m.viewport.SetContent(m.renderScrollable())
				m.viewport.GotoTop()
				return m, cmd
			}
		}

		visible := m.visiblePlans()

		switch msg.String() {
		case "q", "ctrl+c":
			if m.view == viewPlans {
				m.view = viewDashboard
				m.filteredIdxs = nil
				m.filteredTodoIdxs = nil
				m.filterInput.Reset()
				m = resizeViewport(m)
				m.viewport.SetContent(m.renderScrollable())
				m.viewport.GotoTop()
				return m, nil
			}
			m.quitting = true
			return m, tea.Quit
		case "r":
			return m, fetchTUIData
		case "p":
			if m.view == viewDashboard {
				m.view = viewPlans
			} else {
				m.view = viewDashboard
			}
			m.filteredIdxs = nil
			m.filteredTodoIdxs = nil
			m.filterInput.Reset()
			m.planCursor = 0
			m = resizeViewport(m)
			m.viewport.SetContent(m.renderScrollable())
			m.viewport.GotoTop()
			return m, nil
		case "esc":
			// Clear active filter first, then navigate back
			if m.filteredIdxs != nil || m.filteredTodoIdxs != nil {
				m.filteredIdxs = nil
				m.filteredTodoIdxs = nil
				m.filterInput.Reset()
				m.planCursor = 0
				m.viewport.SetContent(m.renderScrollable())
				m.viewport.GotoTop()
				return m, nil
			}
			if m.view == viewPlans {
				m.view = viewDashboard
				m = resizeViewport(m)
				m.viewport.SetContent(m.renderScrollable())
				m.viewport.GotoTop()
				return m, nil
			}
		case "/":
			if m.view == viewPlans || m.view == viewDashboard {
				m.filtering = true
				m.filterInput.Focus()
				m.planCursor = 0
				m = resizeViewport(m)
				return m, textinput.Blink
			}
		case "enter":
			if m.view == viewPlans && len(visible) > 0 {
				idx := m.selectedPlanIdx()
				if idx >= 0 {
					m.planExpanded[idx] = !m.planExpanded[idx]
				}
				m.viewport.SetContent(m.renderScrollable())
				m.ensureCursorVisible()
				return m, nil
			}
		case "right", "l":
			if m.view == viewPlans && len(visible) > 0 {
				idx := m.selectedPlanIdx()
				if idx >= 0 {
					m.planExpanded[idx] = true
				}
				m.viewport.SetContent(m.renderScrollable())
				m.ensureCursorVisible()
				return m, nil
			}
		case "left", "h":
			if m.view == viewPlans && len(visible) > 0 {
				idx := m.selectedPlanIdx()
				if idx >= 0 {
					m.planExpanded[idx] = false
				}
				m.viewport.SetContent(m.renderScrollable())
				m.ensureCursorVisible()
				return m, nil
			}
		case "j", "down":
			if m.view == viewPlans && len(visible) > 0 {
				if m.planCursor < len(visible)-1 {
					m.planCursor++
				}
				m.viewport.SetContent(m.renderScrollable())
				m.ensureCursorVisible()
				return m, nil
			}
		case "k", "up":
			if m.view == viewPlans && len(visible) > 0 {
				if m.planCursor > 0 {
					m.planCursor--
				}
				m.viewport.SetContent(m.renderScrollable())
				m.ensureCursorVisible()
				return m, nil
			}
		case "i":
			if m.view == viewPlans {
				plan := m.selectedPlan()
				if plan == nil {
					return m, nil
				}
				if !plan.HasAssembly {
					m.statusMsg = "No assembly instructions for this plan"
					m.statusError = false
					return m, clearStatusAfter(3 * time.Second)
				}
				m.statusMsg = "Opening instructions..."
				m.statusError = false
				return m, openInstructions(*plan)
			}
		case "a":
			if m.view == viewPlans {
				plan := m.selectedPlan()
				if plan == nil {
					return m, nil
				}
				allDone := true
				for _, proj := range plan.Projects {
					for _, plate := range proj.Plates {
						if plate.Status != "completed" {
							allDone = false
							break
						}
					}
					if !allDone {
						break
					}
				}
				if !allDone {
					m.statusMsg = "Cannot archive — not all plates are completed"
					m.statusError = true
					return m, clearStatusAfter(5 * time.Second)
				}
				m.statusMsg = "Archiving..."
				m.statusError = false
				return m, archivePlanTUI(*plan)
			}
		case "s":
			// Stop in-progress plate(s)
			refs, _, err := collectInProgressPlates()
			if err != nil {
				m.statusMsg = fmt.Sprintf("Error loading plans: %v", err)
				m.statusError = true
				return m, clearStatusAfter(5 * time.Second)
			}
			if len(refs) == 0 {
				m.statusMsg = "No in-progress plates to stop"
				m.statusError = false
				return m, clearStatusAfter(3 * time.Second)
			}
			stage := stagePicker
			if len(refs) == 1 {
				stage = stageConfirm
			}
			m.modal = modalStop
			m.stopModal = &tuiStopModal{plates: refs, cursor: 0, stage: stage}
			m.viewport.SetContent(m.renderStopModal())
			m.viewport.GotoTop()
			return m, nil
		case "c":
			// Complete a plate
			refs, _, err := collectCompletablePlates()
			if err != nil {
				m.statusMsg = fmt.Sprintf("Error loading plans: %v", err)
				m.statusError = true
				return m, clearStatusAfter(5 * time.Second)
			}
			if len(refs) == 0 {
				m.statusMsg = "Nothing to complete"
				m.statusError = false
				return m, clearStatusAfter(3 * time.Second)
			}
			m.modal = modalComplete
			m.completeModal = &tuiCompleteModal{plates: refs, cursor: 0, stage: stagePicker}
			// If only one plate, jump straight to preview
			if len(refs) == 1 {
				ref := refs[0]
				m.completeModal.selected = &ref
				m.completeModal.stage = stageLoading
				m.viewport.SetContent(m.renderCompleteModal())
				m.viewport.GotoTop()
				return m, buildCompletePreviewCmd(ref)
			}
			m.viewport.SetContent(m.renderCompleteModal())
			m.viewport.GotoTop()
			return m, nil
		case "n":
			// Start next plate
			if len(Cfg.Printers) == 0 {
				m.statusMsg = "No printers configured"
				m.statusError = true
				return m, clearStatusAfter(5 * time.Second)
			}
			var printerNames []string
			for name := range Cfg.Printers {
				printerNames = append(printerNames, name)
			}
			sortStrings(printerNames)
			m.modal = modalNext
			m.nextModal = &tuiNextModal{printerNames: printerNames}
			if len(printerNames) == 1 {
				m.nextModal.selectedPrinter = printerNames[0]
				m.nextModal.stage = stageLoading
				m.nextModal.loadingMsg = "Loading plates..."
				m.viewport.SetContent(m.renderNextModal())
				m.viewport.GotoTop()
				return m, prepareNextPlatesCmd(printerNames[0])
			}
			m.nextModal.stage = stagePrinterPicker
			m.viewport.SetContent(m.renderNextModal())
			m.viewport.GotoTop()
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = resizeViewport(m)

	case tuiTickMsg:
		cmds = append(cmds, fetchTUIData, tickCmd(m.refreshInterval))

	case tuiDataMsg:
		m.printerStatuses = msg.printerStatuses
		m.liveStatuses = msg.liveStatuses
		m.printerMap = msg.printerMap
		m.activePrinters = msg.activePrinters
		m.idlePrinters = msg.idlePrinters
		m.todoPlates = msg.todoPlates
		m.mismatches = msg.mismatches
		m.totalTodo = msg.totalTodo
		m.activePlanCount = msg.activePlanCount
		m.plans = msg.plans
		m.lastRefresh = time.Now()
		m.err = nil
		// Re-apply filter if active
		if m.filterInput.Value() != "" {
			m.applyFilter()
		}
		m = resizeViewport(m) // header height may have changed with new data
		m.viewport.SetContent(m.renderScrollable())
		// Clamp plan cursor to visible plans
		visible := m.visiblePlans()
		if m.planCursor >= len(visible) && len(visible) > 0 {
			m.planCursor = len(visible) - 1
		}

	case tuiErrMsg:
		m.err = msg
		if m.ready {
			m.viewport.SetContent(m.renderScrollable())
		}

	case tuiStatusMsg:
		m.statusMsg = msg.text
		m.statusError = msg.isError
		if msg.isError {
			cmds = append(cmds, clearStatusAfter(5*time.Second))
		} else {
			cmds = append(cmds, clearStatusAfter(3*time.Second))
		}

	case tuiClearStatusMsg:
		m.statusMsg = ""
		m.statusError = false

	case tuiArchiveDoneMsg:
		m.statusMsg = fmt.Sprintf("Archived %s", msg.name)
		m.statusError = false
		cmds = append(cmds, clearStatusAfter(3*time.Second), fetchTUIData)
		// Clamp cursor in case the archived plan was the last one
		if m.planCursor > 0 && m.planCursor >= len(m.plans)-1 {
			m.planCursor--
		}

	case tuiStopDoneMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Stop failed: %v", msg.err)
			m.statusError = true
			cmds = append(cmds, clearStatusAfter(5*time.Second))
		} else {
			m.statusMsg = fmt.Sprintf("Stopped %s - %s", msg.projectName, msg.plateName)
			m.statusError = false
			cmds = append(cmds, clearStatusAfter(3*time.Second), fetchTUIData)
		}

	case tuiCompletePreviewReadyMsg:
		if m.completeModal != nil {
			if msg.err != nil {
				m.completeModal.issues = []completeIssue{{needName: "—", reason: msg.err.Error()}}
				m.completeModal.stage = stageNeedsManual
			} else {
				m.completeModal.deductions = msg.deductions
				m.completeModal.issues = msg.issues
				if len(msg.issues) > 0 {
					m.completeModal.stage = stageNeedsManual
				} else if len(msg.deductions) == 0 {
					m.completeModal.issues = []completeIssue{{needName: "—", reason: "No filament needs found for this plate"}}
					m.completeModal.stage = stageNeedsManual
				} else {
					m.completeModal.stage = stagePreview
				}
			}
			m.viewport.SetContent(m.renderCompleteModal())
		}

	case tuiCompleteDoneMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Complete failed: %v", msg.err)
			m.statusError = true
			cmds = append(cmds, clearStatusAfter(5*time.Second))
		} else {
			m.statusMsg = fmt.Sprintf("Completed %s - %s", msg.projectName, msg.plateName)
			m.statusError = false
			cmds = append(cmds, clearStatusAfter(3*time.Second), fetchTUIData)
		}

	case nextPreparedData:
		if m.nextModal != nil {
			if msg.err != nil {
				m.nextModal.issues = []nextIssue{{needName: "—", reason: msg.err.Error()}}
				m.nextModal.stage = stageNeedsManual
			} else if len(msg.plates) == 0 {
				m.statusMsg = "No todo plates"
				m.statusError = false
				m.modal = modalNone
				m.nextModal = nil
				m.viewport.SetContent(m.renderScrollable())
				cmds = append(cmds, clearStatusAfter(3*time.Second))
			} else {
				m.nextModal.plates = msg.plates
				m.nextModal.plateCursor = 0
				m.nextModal.stage = stagePicker
			}
			if m.nextModal != nil {
				m.viewport.SetContent(m.renderNextModal())
				m.viewport.GotoTop()
			}
		}

	case nextPreviewReady:
		if m.nextModal != nil {
			if msg.err != nil {
				m.nextModal.issues = []nextIssue{{needName: "—", reason: msg.err.Error()}}
				m.nextModal.stage = stageNeedsManual
			} else {
				m.nextModal.keepOps = msg.keepOps
				m.nextModal.loadOps = msg.loadOps
				m.nextModal.issues = msg.issues
				if len(msg.issues) > 0 {
					m.nextModal.stage = stageNeedsManual
				} else {
					m.nextModal.stage = stagePreview
				}
			}
			m.viewport.SetContent(m.renderNextModal())
		}

	case tuiNextDoneMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Start failed: %v", msg.err)
			m.statusError = true
			cmds = append(cmds, clearStatusAfter(5*time.Second))
		} else {
			m.statusMsg = fmt.Sprintf("Started %s - %s", msg.projectName, msg.plateName)
			m.statusError = false
			cmds = append(cmds, clearStatusAfter(3*time.Second), fetchTUIData)
		}
	}

	if m.ready {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tuiTickMsg(t) })
}

// updateModal handles key input when a modal is active.
func (m tuiModel) updateModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.modal {
	case modalStop:
		return m.updateStopModal(msg)
	case modalComplete:
		return m.updateCompleteModal(msg)
	case modalNext:
		return m.updateNextModal(msg)
	}
	return m, nil
}

// scrollPickerToCursor adjusts the viewport's Y-offset so the row at `cursor`
// stays visible. Picker renders are: `headerLines` lines of header, then one
// line per row — so the cursor's absolute line is `headerLines + cursor`.
// Caller is expected to have just SetContent'd the picker render.
func scrollPickerToCursor(vp *viewport.Model, cursor, headerLines int) {
	if vp.Height <= 0 {
		return
	}
	target := headerLines + cursor
	top := vp.YOffset
	bottom := top + vp.Height - 1
	if target < top {
		vp.SetYOffset(target)
	} else if target > bottom {
		vp.SetYOffset(target - vp.Height + 1)
	}
}

// pickerHeaderLines is the number of lines all picker stages render before the
// first row (title + blank line). Kept as a named constant so the scroll math
// stays in sync if the picker chrome ever grows.
const pickerHeaderLines = 2

// updateNextModal handles key input for the next modal.
func (m tuiModel) updateNextModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	nm := m.nextModal
	if nm == nil {
		m.modal = modalNone
		return m, nil
	}
	closeModal := func() tuiModel {
		m.modal = modalNone
		m.nextModal = nil
		m.viewport.SetContent(m.renderScrollable())
		return m
	}

	switch nm.stage {
	case stagePrinterPicker:
		switch msg.String() {
		case "esc", "q":
			return closeModal(), nil
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "j", "down":
			if nm.printerCursor < len(nm.printerNames)-1 {
				nm.printerCursor++
			}
			m.viewport.SetContent(m.renderNextModal())
			scrollPickerToCursor(&m.viewport, nm.printerCursor, pickerHeaderLines)
			return m, nil
		case "k", "up":
			if nm.printerCursor > 0 {
				nm.printerCursor--
			}
			m.viewport.SetContent(m.renderNextModal())
			scrollPickerToCursor(&m.viewport, nm.printerCursor, pickerHeaderLines)
			return m, nil
		case "enter":
			nm.selectedPrinter = nm.printerNames[nm.printerCursor]
			nm.stage = stageLoading
			nm.loadingMsg = "Loading plates..."
			m.viewport.SetContent(m.renderNextModal())
			return m, prepareNextPlatesCmd(nm.selectedPrinter)
		}

	case stageLoading:
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}

	case stagePicker:
		switch msg.String() {
		case "esc", "q":
			if len(nm.printerNames) > 1 {
				nm.stage = stagePrinterPicker
				nm.plates = nil
				m.viewport.SetContent(m.renderNextModal())
				return m, nil
			}
			return closeModal(), nil
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "j", "down":
			if nm.plateCursor < len(nm.plates)-1 {
				nm.plateCursor++
			}
			m.viewport.SetContent(m.renderNextModal())
			scrollPickerToCursor(&m.viewport, nm.plateCursor, pickerHeaderLines)
			return m, nil
		case "k", "up":
			if nm.plateCursor > 0 {
				nm.plateCursor--
			}
			m.viewport.SetContent(m.renderNextModal())
			scrollPickerToCursor(&m.viewport, nm.plateCursor, pickerHeaderLines)
			return m, nil
		case "enter":
			if len(nm.plates) == 0 {
				return m, nil
			}
			plate := nm.plates[nm.plateCursor]
			if !plate.isReady {
				m.statusMsg = "That plate has insufficient filament"
				m.statusError = true
				return m, clearStatusAfter(5 * time.Second)
			}
			nm.selectedPlate = &plate
			nm.stage = stageLoading
			nm.loadingMsg = "Building preview..."
			m.viewport.SetContent(m.renderNextModal())
			return m, buildNextPreviewCmd(nm.selectedPrinter, plate)
		}

	case stagePreview:
		switch msg.String() {
		case "esc", "n", "q":
			nm.stage = stagePicker
			nm.selectedPlate = nil
			nm.keepOps = nil
			nm.loadOps = nil
			nm.issues = nil
			m.viewport.SetContent(m.renderNextModal())
			return m, nil
		case "y", "enter":
			if nm.selectedPlate == nil {
				return closeModal(), nil
			}
			plate := *nm.selectedPlate
			printer := nm.selectedPrinter
			loadOps := nm.loadOps
			return closeModal(), executeNextCmd(printer, plate, loadOps)
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}

	case stageNeedsManual:
		switch msg.String() {
		case "esc", "q", "enter":
			return closeModal(), nil
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func prepareNextPlatesCmd(printerName string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
		return preparePlates(ctx, apiClient, printerName)
	}
}

func buildNextPreviewCmd(printerName string, plate nextPlateRef) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
		return buildNextPreview(ctx, apiClient, printerName, plate)
	}
}

func executeNextCmd(printerName string, plate nextPlateRef, loadOps []nextLoadOp) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
		return executeNext(ctx, apiClient, printerName, plate, loadOps)
	}
}

// updateCompleteModal handles key input for the complete modal.
func (m tuiModel) updateCompleteModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cm := m.completeModal
	if cm == nil {
		m.modal = modalNone
		return m, nil
	}

	closeModal := func() tuiModel {
		m.modal = modalNone
		m.completeModal = nil
		m.viewport.SetContent(m.renderScrollable())
		return m
	}

	switch cm.stage {
	case stagePicker:
		switch msg.String() {
		case "esc", "q":
			return closeModal(), nil
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "j", "down":
			if cm.cursor < len(cm.plates)-1 {
				cm.cursor++
			}
			m.viewport.SetContent(m.renderCompleteModal())
			scrollPickerToCursor(&m.viewport, cm.cursor, pickerHeaderLines)
			return m, nil
		case "k", "up":
			if cm.cursor > 0 {
				cm.cursor--
			}
			m.viewport.SetContent(m.renderCompleteModal())
			scrollPickerToCursor(&m.viewport, cm.cursor, pickerHeaderLines)
			return m, nil
		case "enter":
			ref := cm.plates[cm.cursor]
			cm.selected = &ref
			cm.stage = stageLoading
			m.viewport.SetContent(m.renderCompleteModal())
			return m, buildCompletePreviewCmd(ref)
		}

	case stageLoading:
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}

	case stagePreview:
		switch msg.String() {
		case "esc", "n", "q":
			if len(cm.plates) > 1 {
				cm.stage = stagePicker
				cm.selected = nil
				cm.deductions = nil
				cm.issues = nil
				m.viewport.SetContent(m.renderCompleteModal())
				return m, nil
			}
			return closeModal(), nil
		case "y", "enter":
			if cm.selected == nil {
				return closeModal(), nil
			}
			ref := *cm.selected
			deductions := cm.deductions
			return closeModal(), executeCompleteCmd(ref, deductions)
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}

	case stageNeedsManual:
		switch msg.String() {
		case "esc", "q", "enter":
			return closeModal(), nil
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func buildCompletePreviewCmd(ref completePlateRef) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
		deductions, issues, err := buildCompletePreview(ctx, apiClient, ref)
		return tuiCompletePreviewReadyMsg{deductions: deductions, issues: issues, err: err}
	}
}

func executeCompleteCmd(ref completePlateRef, deductions []completeDeduction) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
		return executeComplete(ctx, apiClient, ref, deductions)
	}
}

// updateStopModal handles key input for the stop modal.
func (m tuiModel) updateStopModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	sm := m.stopModal
	if sm == nil {
		m.modal = modalNone
		return m, nil
	}

	switch sm.stage {
	case stagePicker:
		switch msg.String() {
		case "esc", "q", "ctrl+c":
			m.modal = modalNone
			m.stopModal = nil
			m.viewport.SetContent(m.renderScrollable())
			return m, nil
		case "j", "down":
			if sm.cursor < len(sm.plates)-1 {
				sm.cursor++
			}
			m.viewport.SetContent(m.renderStopModal())
			scrollPickerToCursor(&m.viewport, sm.cursor, pickerHeaderLines)
			return m, nil
		case "k", "up":
			if sm.cursor > 0 {
				sm.cursor--
			}
			m.viewport.SetContent(m.renderStopModal())
			scrollPickerToCursor(&m.viewport, sm.cursor, pickerHeaderLines)
			return m, nil
		case "enter":
			sm.stage = stageConfirm
			m.viewport.SetContent(m.renderStopModal())
			return m, nil
		}
	case stageConfirm:
		switch msg.String() {
		case "esc", "n", "q":
			// Cancel — return to picker if we had one, otherwise close
			if len(sm.plates) > 1 {
				sm.stage = stagePicker
				m.viewport.SetContent(m.renderStopModal())
				return m, nil
			}
			m.modal = modalNone
			m.stopModal = nil
			m.viewport.SetContent(m.renderScrollable())
			return m, nil
		case "y", "enter":
			// Execute the stop
			ref := sm.plates[sm.cursor]
			m.modal = modalNone
			m.stopModal = nil
			m.viewport.SetContent(m.renderScrollable())
			return m, func() tea.Msg {
				_, plans, err := collectInProgressPlates()
				if err != nil {
					return tuiStopDoneMsg{err: err}
				}
				return stopSelectedPlate(ref, plans)
			}
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// resizeViewport recalculates the viewport height based on the current header
// and footer sizes, then updates or creates the viewport.
func resizeViewport(m tuiModel) tuiModel {
	vpHeight := m.height - m.headerHeight() - m.footerHeight()
	if vpHeight < 1 {
		vpHeight = 1
	}
	if !m.ready {
		m.viewport = viewport.New(m.width, vpHeight)
		m.viewport.SetContent(m.renderScrollable())
		m.ready = true
	} else {
		m.viewport.Width = m.width
		m.viewport.Height = vpHeight
	}
	return m
}

// ─────────────────────────────────────────────────────────────────────────────
// Data fetching
// ─────────────────────────────────────────────────────────────────────────────

func fetchTUIData() tea.Msg {
	ctx := context.Background()
	data := tuiDataMsg{
		printerStatuses: make(map[string]api.PrinterStatus),
		printerMap:      make(map[string][]tuiPrintingInfo),
	}

	// Discover plans and build printer map + collect todo plates
	plans, err := discoverPlans()
	if err != nil {
		return tuiErrMsg(err)
	}

	type rawTodo struct {
		planName    string
		projectName string
		plate       models.Plate
	}
	var rawTodos []rawTodo

	for _, p := range plans {
		planDisplay := p.DisplayName
		hasIncomplete := false

		planSummary := tuiPlanSummary{
			Name:        planDisplay,
			RemoteName:  p.RemoteName,
			Path:        p.Path,
			Remote:      p.Remote,
			HasAssembly: p.Plan.Assembly != "",
		}

		for _, proj := range p.Plan.Projects {
			projSummary := tuiProjectSummary{
				Name:   proj.Name,
				Status: proj.Status,
			}

			for _, plate := range proj.Plates {
				if plate.Status == "in-progress" && plate.Printer != "" {
					data.printerMap[plate.Printer] = append(data.printerMap[plate.Printer], tuiPrintingInfo{
						Project:           proj.Name,
						Plate:             plate.Name,
						StartedAt:         plate.StartedAt,
						EstimatedDuration: plate.EstimatedDuration,
					})
				}
				if plate.Status == "todo" {
					rawTodos = append(rawTodos, rawTodo{
						planName:    planDisplay,
						projectName: proj.Name,
						plate:       plate,
					})
					data.totalTodo++
				}
				if plate.Status != "completed" {
					hasIncomplete = true
				}

				var plateColors []string
				for _, need := range plate.Needs {
					if need.Color != "" {
						plateColors = append(plateColors, need.Color)
					}
				}
				projSummary.Plates = append(projSummary.Plates, tuiPlateSummary{
					Name:    plate.Name,
					Status:  plate.Status,
					Printer: plate.Printer,
					Colors:  plateColors,
				})
			}
			planSummary.Projects = append(planSummary.Projects, projSummary)
		}
		data.plans = append(data.plans, planSummary)

		if hasIncomplete {
			data.activePlanCount++
		}
	}

	// Compute swap cost per plate per printer.
	// Load spools from Spoolman (best-effort; if it fails, show plates without cost).
	var allSpools []models.FindSpool
	if Cfg.ApiBase != "" {
		apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
		allSpools, _ = apiClient.FindSpoolsByName(ctx, "*", onlyStandardFilament, nil)
	}

	// Build loaded-spool lookup by location
	type spoolByLoc struct {
		Location   string
		FilamentID int
	}
	loadedSet := make(map[spoolByLoc]bool)
	for _, s := range allSpools {
		if s.Location != "" {
			loadedSet[spoolByLoc{s.Location, s.Filament.Id}] = true
		}
	}

	// Inventory totals by filament ID (for readiness check)
	inventoryByFilament := make(map[int]float64)
	for _, s := range allSpools {
		if !s.Archived {
			inventoryByFilament[s.Filament.Id] += s.RemainingWeight
		}
	}

	for _, rt := range rawTodos {
		var colors []string
		for _, need := range rt.plate.Needs {
			if need.Color != "" {
				colors = append(colors, need.Color)
			}
		}
		tp := tuiTodoPlate{
			PlanName:    rt.planName,
			ProjectName: rt.projectName,
			PlateName:   rt.plate.Name,
			SwapCost:    -1, // unknown
			IsReady:     true,
			Colors:      colors,
		}

		// Check readiness
		for _, req := range rt.plate.Needs {
			if inventoryByFilament[req.FilamentID] < req.Amount {
				tp.IsReady = false
			}
		}

		// Find best printer(s) (lowest swap cost)
		if len(allSpools) > 0 {
			bestCost := 999
			var bestPrinters []string
			for printerName, pCfg := range Cfg.Printers {
				cost := 0
				for _, req := range rt.plate.Needs {
					found := false
					for _, loc := range pCfg.Locations {
						if loadedSet[spoolByLoc{loc, req.FilamentID}] {
							found = true
							break
						}
					}
					if !found {
						cost++
					}
				}
				if cost < bestCost {
					bestCost = cost
					bestPrinters = []string{printerName}
				} else if cost == bestCost {
					bestPrinters = append(bestPrinters, printerName)
				}
			}
			sortStrings(bestPrinters)
			tp.SwapCost = bestCost
			if len(bestPrinters) == len(Cfg.Printers) {
				tp.BestPrinter = "any printer"
			} else {
				tp.BestPrinter = strings.Join(bestPrinters, " or ")
			}
		}

		data.todoPlates = append(data.todoPlates, tp)
	}

	// Sort todo plates: ready first, then by swap cost
	sortTodoPlates(data.todoPlates)

	// Fetch live printer status
	if Cfg.PlansServer != "" {
		client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
		if statuses, err := client.GetPrinterStatus(ctx); err == nil {
			data.liveStatuses = statuses
			for _, s := range statuses {
				data.printerStatuses[s.Name] = s
			}
		}
	}

	// Split printers into active/idle
	for name := range Cfg.Printers {
		if infos, ok := data.printerMap[name]; ok && len(infos) > 0 {
			data.activePrinters = append(data.activePrinters, name)
		} else {
			data.idlePrinters = append(data.idlePrinters, name)
		}
	}
	sortStrings(data.activePrinters)
	sortStrings(data.idlePrinters)

	// Detect mismatches
	if len(data.liveStatuses) > 0 {
		data.mismatches = detectMismatches(ctx, data.liveStatuses)
	}

	return data
}

// sortStrings sorts a string slice in place.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// sortTodoPlates sorts plates: ready first, then by swap cost ascending.
func sortTodoPlates(plates []tuiTodoPlate) {
	for i := 1; i < len(plates); i++ {
		for j := i; j > 0 && todoLess(plates[j], plates[j-1]); j-- {
			plates[j], plates[j-1] = plates[j-1], plates[j]
		}
	}
}

func todoLess(a, b tuiTodoPlate) bool {
	if a.IsReady != b.IsReady {
		return a.IsReady // ready plates sort first
	}
	return a.SwapCost < b.SwapCost
}

// ensureCursorVisible scrolls the viewport so the cursor line is visible.
func (m *tuiModel) ensureCursorVisible() {
	if m.view != viewPlans {
		return
	}
	visible := m.visiblePlans()
	if len(visible) == 0 {
		return
	}

	// Count lines before the cursor position
	line := 0
	for vi, planIdx := range visible {
		if vi == m.planCursor {
			break
		}
		line++ // the plan line itself
		if m.planExpanded[planIdx] {
			for _, proj := range m.plans[planIdx].Projects {
				line++ // project line
				line += len(proj.Plates)
			}
			line++ // blank line after expanded plan
		}
	}

	// Also count lines the cursor's own expanded content takes
	cursorEnd := line + 1 // at minimum the plan header line
	if m.planCursor < len(visible) {
		planIdx := visible[m.planCursor]
		if m.planExpanded[planIdx] {
			for _, proj := range m.plans[planIdx].Projects {
				cursorEnd++ // project line
				cursorEnd += len(proj.Plates)
			}
			cursorEnd++ // blank line
		}
	}

	// Scroll up if cursor is above viewport
	if line < m.viewport.YOffset {
		m.viewport.SetYOffset(line)
	}
	// Scroll down if cursor's content extends below viewport
	if cursorEnd > m.viewport.YOffset+m.viewport.Height {
		m.viewport.SetYOffset(cursorEnd - m.viewport.Height)
	}
}

// visibleTodoPlates returns the todo plate indices visible after filtering.
func (m tuiModel) visibleTodoPlates() []int {
	if m.filtering || len(m.filteredTodoIdxs) > 0 {
		return m.filteredTodoIdxs
	}
	idxs := make([]int, len(m.todoPlates))
	for i := range m.todoPlates {
		idxs[i] = i
	}
	return idxs
}

// visiblePlans returns the plan indices visible after filtering.
// When no filter is active, returns all indices.
func (m tuiModel) visiblePlans() []int {
	if m.filtering || len(m.filteredIdxs) > 0 {
		return m.filteredIdxs
	}
	idxs := make([]int, len(m.plans))
	for i := range m.plans {
		idxs[i] = i
	}
	return idxs
}

// selectedPlan returns the plan at the current cursor position,
// accounting for filtering. Returns nil if no plans are visible.
func (m tuiModel) selectedPlan() *tuiPlanSummary {
	visible := m.visiblePlans()
	if len(visible) == 0 || m.planCursor >= len(visible) {
		return nil
	}
	return &m.plans[visible[m.planCursor]]
}

// selectedPlanIdx returns the real index into m.plans for the cursor position.
func (m tuiModel) selectedPlanIdx() int {
	visible := m.visiblePlans()
	if len(visible) == 0 || m.planCursor >= len(visible) {
		return -1
	}
	return visible[m.planCursor]
}

// applyFilter updates filteredIdxs and filteredTodoIdxs based on the current filter text.
func (m *tuiModel) applyFilter() {
	query := strings.ToLower(m.filterInput.Value())
	if query == "" {
		m.filteredIdxs = nil
		m.filteredTodoIdxs = nil
		return
	}
	m.filteredIdxs = nil
	for i, plan := range m.plans {
		if strings.Contains(strings.ToLower(plan.Name), query) {
			m.filteredIdxs = append(m.filteredIdxs, i)
		}
	}
	m.filteredTodoIdxs = nil
	for i, tp := range m.todoPlates {
		if strings.Contains(strings.ToLower(tp.PlanName), query) ||
			strings.Contains(strings.ToLower(tp.ProjectName), query) ||
			strings.Contains(strings.ToLower(tp.PlateName), query) {
			m.filteredTodoIdxs = append(m.filteredTodoIdxs, i)
		}
	}
	// Clamp cursor (plans view)
	visible := m.visiblePlans()
	if m.planCursor >= len(visible) {
		if len(visible) > 0 {
			m.planCursor = len(visible) - 1
		} else {
			m.planCursor = 0
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Commands (async actions)
// ─────────────────────────────────────────────────────────────────────────────

func openInstructions(plan tuiPlanSummary) tea.Cmd {
	return func() tea.Msg {
		if Cfg == nil || Cfg.PlansServer == "" {
			return tuiStatusMsg{text: "plans_server not configured", isError: true}
		}

		planName := plan.RemoteName
		if planName == "" {
			return tuiStatusMsg{text: "No remote name for plan", isError: true}
		}

		client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
		data, filename, err := client.GetAssembly(context.Background(), planName)
		if err != nil {
			return tuiStatusMsg{text: fmt.Sprintf("Failed to download: %v", err), isError: true}
		}

		if filename == "" {
			filename = planName + "-assembly.pdf"
		}

		tmpFile, err := os.CreateTemp("", "fil-assembly-*.pdf")
		if err != nil {
			return tuiStatusMsg{text: fmt.Sprintf("Failed to create temp file: %v", err), isError: true}
		}

		if _, err := tmpFile.Write(data); err != nil {
			_ = tmpFile.Close()
			return tuiStatusMsg{text: fmt.Sprintf("Failed to write PDF: %v", err), isError: true}
		}
		_ = tmpFile.Close()

		var openCmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			openCmd = exec.Command("open", tmpFile.Name())
		default:
			openCmd = exec.Command("xdg-open", tmpFile.Name())
		}

		if err := openCmd.Start(); err != nil {
			return tuiStatusMsg{text: fmt.Sprintf("Saved at: %s", tmpFile.Name()), isError: false}
		}

		return tuiStatusMsg{text: fmt.Sprintf("Opened %s", filename), isError: false}
	}
}

func archivePlanTUI(plan tuiPlanSummary) tea.Cmd {
	return func() tea.Msg {
		dp := DiscoveredPlan{
			Path:       plan.Path,
			RemoteName: plan.RemoteName,
			Remote:     plan.Remote,
		}
		if PlanOps != nil {
			_ = PlanOps.Archive(context.Background(), planFileName(dp))
		}
		return tuiArchiveDoneMsg{name: plan.Name}
	}
}

type tuiArchiveDoneMsg struct {
	name string
}

func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return tuiClearStatusMsg{} })
}

// ─────────────────────────────────────────────────────────────────────────────
// View / Rendering
// ─────────────────────────────────────────────────────────────────────────────

// Styles
var (
	tuiHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15"))

	tuiDividerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	tuiDimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	tuiWarnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))

	tuiPrinterNameStyle = lipgloss.NewStyle().
				Bold(true)

	tuiIdleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	tuiProgressFullStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("34")) // green

	tuiProgressPausedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214")) // yellow/orange

	tuiProgressFailedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("196")) // red

	tuiProgressEmptyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240"))

	tuiCompletedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("34")) // green

	tuiFooterStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
)

func (m tuiModel) View() string {
	if m.quitting {
		return ""
	}
	if !m.ready {
		return "Loading..."
	}
	// Three-zone layout:
	// 1. Top (pinned): printers + mismatches
	// 2. Middle (scrollable): up-next plate list
	// 3. Bottom (pinned): summary + keybinds
	return m.renderHeader() + m.viewport.View() + "\n" + m.renderFooter()
}

// renderHeader returns the pinned top section, adapting to the current view.
func (m tuiModel) renderHeader() string {
	if m.view == viewPlans {
		return m.renderPlansHeader()
	}
	return m.renderDashboardHeader()
}

func (m tuiModel) renderDashboardHeader() string {
	w := m.width
	if w == 0 {
		w = 80
	}

	var b strings.Builder

	// Error banner
	if m.err != nil {
		b.WriteString(tuiWarnStyle.Render(fmt.Sprintf("  Error: %v", m.err)))
		b.WriteString("\n")
	}

	b.WriteString(tuiHeaderStyle.Render("Printers"))
	b.WriteString("\n")
	b.WriteString(tuiDividerStyle.Render(strings.Repeat("─", w)))
	b.WriteString("\n")

	hasData := len(m.activePrinters) > 0 || len(m.idlePrinters) > 0
	if !hasData && m.lastRefresh.IsZero() {
		b.WriteString(tuiDimStyle.Render("  Loading..."))
		b.WriteString("\n")
	} else if !hasData {
		b.WriteString(tuiDimStyle.Render("  No printers configured"))
		b.WriteString("\n")
	}

	for _, name := range m.activePrinters {
		m.renderActivePrinter(&b, name, w)
		b.WriteString("\n")
	}

	for _, name := range m.idlePrinters {
		m.renderIdlePrinter(&b, name)
	}

	if len(m.mismatches) > 0 {
		b.WriteString(tuiWarnStyle.Render(fmt.Sprintf("  ⚠ %d tray mismatch(es) — run: fil verify", len(m.mismatches))))
		b.WriteString("\n")
	}

	// Up next section header (pinned with printers, list scrolls below)
	b.WriteString("\n")
	b.WriteString(tuiHeaderStyle.Render("Up next"))
	b.WriteString("\n")
	b.WriteString(tuiDividerStyle.Render(strings.Repeat("─", w)))
	b.WriteString("\n")

	return b.String()
}

func (m tuiModel) renderPlansHeader() string {
	w := m.width
	if w == 0 {
		w = 80
	}

	var b strings.Builder
	b.WriteString(tuiHeaderStyle.Render("Plans"))
	b.WriteString(tuiDimStyle.Render(fmt.Sprintf("  (%d)", len(m.plans))))
	b.WriteString("\n")
	b.WriteString(tuiDividerStyle.Render(strings.Repeat("─", w)))
	b.WriteString("\n")
	return b.String()
}

// headerHeight counts the lines in the header so the viewport gets the remaining space.
func (m tuiModel) headerHeight() int {
	return strings.Count(m.renderHeader(), "\n")
}

// footerHeight returns the number of terminal lines the pinned footer occupies,
// including the newline separator between the viewport and footer.
func (m tuiModel) footerHeight() int {
	return 4 // separator + divider + summary + keybinds
}

// renderScrollable returns the viewport content for the current view.
func (m tuiModel) renderScrollable() string {
	switch m.modal {
	case modalStop:
		return m.renderStopModal()
	case modalComplete:
		return m.renderCompleteModal()
	case modalNext:
		return m.renderNextModal()
	}
	if m.view == viewPlans {
		return m.renderPlansScrollable()
	}
	return m.renderDashboardScrollable()
}

func (m tuiModel) renderDashboardScrollable() string {
	visible := m.visibleTodoPlates()
	if len(visible) == 0 {
		if len(m.filteredTodoIdxs) == 0 && len(m.todoPlates) == 0 {
			return tuiDimStyle.Render("  No plates remaining") + "\n"
		}
		return tuiDimStyle.Render("  No matching plates") + "\n"
	}

	var b strings.Builder
	for _, idx := range visible {
		tp := m.todoPlates[idx]
		// Color swatches
		swatch := tuiColorSwatches(tp.Colors)
		if swatch != "" {
			swatch += " "
		}

		line := fmt.Sprintf("  %s%s / %s",
			swatch,
			models.Sanitize(tp.ProjectName),
			models.Sanitize(tp.PlateName))

		if !tp.IsReady {
			line += tuiWarnStyle.Render("  (insufficient filament)")
		} else if tp.SwapCost >= 0 && tp.BestPrinter != "" {
			line += tuiDimStyle.Render(fmt.Sprintf("  %d swaps on %s", tp.SwapCost, tp.BestPrinter))
		}

		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func (m tuiModel) renderPlansScrollable() string {
	visible := m.visiblePlans()
	if len(visible) == 0 {
		if len(m.filteredIdxs) == 0 && len(m.plans) == 0 {
			return tuiDimStyle.Render("  No plans found") + "\n"
		}
		return tuiDimStyle.Render("  No matching plans") + "\n"
	}

	var b strings.Builder
	for vi, planIdx := range visible {
		plan := m.plans[planIdx]

		// Cursor indicator
		cursor := "  "
		if vi == m.planCursor {
			cursor = "> "
		}

		// Expand/collapse indicator
		expandIcon := "▶"
		if m.planExpanded[planIdx] {
			expandIcon = "▼"
		}

		// Count plates by status
		done, total := 0, 0
		for _, proj := range plan.Projects {
			for _, plate := range proj.Plates {
				total++
				if plate.Status == "completed" {
					done++
				}
			}
		}

		planLine := fmt.Sprintf("%s%s %s", cursor, expandIcon, models.Sanitize(plan.Name))
		progress := tuiDimStyle.Render(fmt.Sprintf("  %d/%d plates done", done, total))

		if vi == m.planCursor {
			b.WriteString(tuiPrinterNameStyle.Render(planLine))
		} else {
			b.WriteString(planLine)
		}
		b.WriteString(progress)
		b.WriteString("\n")

		// Expanded detail
		if m.planExpanded[planIdx] {
			for _, proj := range plan.Projects {
				b.WriteString(tuiDimStyle.Render(fmt.Sprintf("    %s", models.Sanitize(proj.Name))))
				b.WriteString("\n")

				for _, plate := range proj.Plates {
					icon := "○"
					style := tuiDimStyle
					switch plate.Status {
					case "completed":
						icon = "✓"
						style = tuiCompletedStyle
					case "in-progress":
						icon = "●"
						style = lipgloss.NewStyle().Foreground(lipgloss.Color("34"))
					}

					// Color swatches for this plate's filament needs
					swatch := tuiColorSwatches(plate.Colors)
					if swatch != "" {
						swatch += " "
					}

					plateLine := fmt.Sprintf("      %s %s%s", icon, swatch, models.Sanitize(plate.Name))
					b.WriteString(style.Render(plateLine))

					if plate.Printer != "" && plate.Status == "in-progress" {
						b.WriteString(tuiDimStyle.Render(fmt.Sprintf("  on %s", plate.Printer)))
					}
					b.WriteString("\n")
				}
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// renderFooter returns the always-visible pinned footer (summary + keybinds).
func (m tuiModel) renderFooter() string {
	w := m.width
	if w == 0 {
		w = 80
	}

	var b strings.Builder
	b.WriteString(tuiDividerStyle.Render(strings.Repeat("─", w)))
	b.WriteString("\n")

	summary := ""
	if !m.lastRefresh.IsZero() {
		summary = fmt.Sprintf("%d active plan(s) · %d plates remaining", m.activePlanCount, m.totalTodo)
	}
	refreshInfo := ""
	if !m.lastRefresh.IsZero() {
		refreshInfo = fmt.Sprintf("Updated at %s", m.lastRefresh.Format("3:04:05pm"))
	}
	gap := w - len(summary) - len(refreshInfo)
	if gap < 2 {
		gap = 2
	}
	b.WriteString(tuiDimStyle.Render(summary + strings.Repeat(" ", gap) + refreshInfo))
	b.WriteString("\n")
	if m.filtering {
		b.WriteString(m.filterInput.View())
		b.WriteString(tuiFooterStyle.Render("  [enter]accept  [esc]cancel"))
	} else if m.statusMsg != "" {
		style := tuiDimStyle
		if m.statusError {
			style = tuiWarnStyle
		}
		b.WriteString(style.Render(m.statusMsg))
	} else if m.modal == modalStop {
		b.WriteString(tuiFooterStyle.Render("Stop plate — [esc]cancel"))
	} else if m.modal == modalComplete {
		b.WriteString(tuiFooterStyle.Render("Complete plate — [esc]cancel"))
	} else if m.modal == modalNext {
		b.WriteString(tuiFooterStyle.Render("Start next plate — [esc]cancel"))
	} else {
		switch m.view {
		case viewPlans:
			b.WriteString(tuiFooterStyle.Render("[p/esc]dashboard  [↑/↓/j/k]navigate  [→/enter]expand  [←]collapse  [/]filter  [a]rchive  [i]nstructions  [n]ext  [c]omplete  [s]top  [r]efresh  [q]uit"))
		default:
			b.WriteString(tuiFooterStyle.Render("[p]lans  [/]filter  [↑/↓]scroll  [n]ext  [c]omplete  [s]top  [r]efresh  [q]uit"))
		}
	}

	return b.String()
}

func (m tuiModel) renderActivePrinter(b *strings.Builder, name string, width int) {
	infos := m.printerMap[name]
	live, hasLive := m.printerStatuses[name]

	// Progress bar when we have live data with progress
	if hasLive && live.Progress > 0 && (live.State == "printing" || live.State == "paused" || live.State == "failed") {
		b.WriteString(m.renderProgressLine(name, live, width))
		switch live.State {
		case "paused":
			b.WriteString("  " + tuiProgressPausedStyle.Render("PAUSED"))
		case "failed":
			b.WriteString("  " + tuiProgressFailedStyle.Render("FAILED"))
		}
		b.WriteString("\n")
	} else {
		b.WriteString(tuiPrinterNameStyle.Render(name))
		if hasLive && live.State != "" && live.State != "idle" && live.State != "printing" {
			stateStyle := tuiDimStyle
			switch live.State {
			case "paused":
				stateStyle = tuiProgressPausedStyle
			case "failed":
				stateStyle = tuiProgressFailedStyle
			}
			b.WriteString(stateStyle.Render(fmt.Sprintf("  %s", live.State)))
		}
		b.WriteString("\n")
	}

	// Plate info lines
	for _, info := range infos {
		line := fmt.Sprintf("  %s / %s", models.Sanitize(info.Project), models.Sanitize(info.Plate))

		if hasLive && live.State == "printing" {
			line += m.formatTUILiveInfo(live)
		} else {
			line += formatTimeInfo(info.StartedAt, info.EstimatedDuration)
		}

		// Color swatch
		if hasLive {
			if swatch := tuiActiveTrayColor(live); swatch != "" {
				line = swatch + " " + line
			}
		}

		b.WriteString(line)
		b.WriteString("\n")
	}
}

func (m tuiModel) renderProgressLine(name string, live api.PrinterStatus, width int) string {
	label := tuiPrinterNameStyle.Render(name)
	labelLen := len(name)

	// Build the right side: percentage + ETA
	right := fmt.Sprintf(" %d%%", live.Progress)
	if live.RemainingMins > 0 {
		eta := time.Now().Add(time.Duration(live.RemainingMins) * time.Minute)
		right += fmt.Sprintf("  ~%s", eta.Format("3:04pm"))
	}

	// Bar fills the space between label and right side
	barSpace := width - labelLen - len(right) - 4 // 4 = padding/spaces
	if barSpace < 10 {
		barSpace = 10
	}

	filled := barSpace * live.Progress / 100
	empty := barSpace - filled

	// Color based on printer state
	fillStyle := tuiProgressFullStyle
	switch live.State {
	case "paused":
		fillStyle = tuiProgressPausedStyle
	case "failed":
		fillStyle = tuiProgressFailedStyle
	}

	bar := fillStyle.Render(strings.Repeat("█", filled)) +
		tuiProgressEmptyStyle.Render(strings.Repeat("░", empty))

	return fmt.Sprintf("%s  %s%s", label, bar, tuiDimStyle.Render(right))
}

func (m tuiModel) formatTUILiveInfo(status api.PrinterStatus) string {
	var parts []string
	if status.Layer > 0 && status.TotalLayers > 0 {
		parts = append(parts, fmt.Sprintf("layer %d/%d", status.Layer, status.TotalLayers))
	}
	if len(parts) == 0 {
		return ""
	}
	return tuiDimStyle.Render(fmt.Sprintf("  (%s)", strings.Join(parts, ", ")))
}

func (m tuiModel) renderIdlePrinter(b *strings.Builder, name string) {
	live, hasLive := m.printerStatuses[name]
	if hasLive && live.State != "idle" && live.State != "offline" && live.State != "" {
		_, _ = fmt.Fprintf(b, "%s  %s\n",
			tuiPrinterNameStyle.Render(name),
			tuiDimStyle.Render(fmt.Sprintf("(%s)", live.State)))
	} else {
		_, _ = fmt.Fprintf(b, "%s  %s\n",
			tuiPrinterNameStyle.Render(name),
			tuiIdleStyle.Render("(idle)"))
	}
}

// tuiColorSwatches renders a row of ██ blocks from a list of hex color strings.
// Each color gets one block, separated by no space (they visually merge into a stripe).
func tuiColorSwatches(colors []string) string {
	if len(colors) == 0 {
		return ""
	}
	var out strings.Builder
	for _, hex := range colors {
		hex = strings.TrimPrefix(hex, "#")
		if len(hex) < 6 {
			continue
		}
		r, _ := strconv.ParseInt(hex[0:2], 16, 16)
		g, _ := strconv.ParseInt(hex[2:4], 16, 16)
		b, _ := strconv.ParseInt(hex[4:6], 16, 16)
		style := lipgloss.NewStyle().
			Foreground(lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, b)))
		out.WriteString(style.Render("█"))
	}
	return out.String()
}

// tuiActiveTrayColor returns a lipgloss-styled color swatch for the active tray.
func tuiActiveTrayColor(status api.PrinterStatus) string {
	if len(status.Trays) == 0 || status.ActiveTray < 0 {
		return ""
	}

	amsID := status.ActiveTray / 4
	trayID := status.ActiveTray % 4

	for _, tray := range status.Trays {
		if tray.AmsID == amsID && tray.TrayID == trayID {
			if tray.Color == "" {
				return ""
			}
			hex := strings.TrimPrefix(tray.Color, "#")
			if len(hex) < 6 {
				return ""
			}
			r, _ := strconv.ParseInt(hex[0:2], 16, 16)
			g, _ := strconv.ParseInt(hex[2:4], 16, 16)
			b, _ := strconv.ParseInt(hex[4:6], 16, 16)
			style := lipgloss.NewStyle().
				Foreground(lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, b)))
			return style.Render("██")
		}
	}
	return ""
}

