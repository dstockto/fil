package cmd

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/devices"
	"github.com/dstockto/fil/models"
	"github.com/fatih/color"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var scanCmd = &cobra.Command{
	Use:          "scan",
	Short:        "Scan a filament sample with the TD-1 and update Spoolman",
	Long:         `Connect to an attached TD-1 USB scanner, read scans, and apply measured color and transmission distance (TD) to the matching Spoolman filament. The picker auto-ranks spools by ΔE against the scanned color so the right spool is almost always the top choice.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runScan,
}

//nolint:gochecknoinits
func init() {
	rootCmd.AddCommand(scanCmd)
	scanCmd.Flags().BoolP("dry-run", "d", false, "scan and display but never write to Spoolman")
}

// scanSession holds state that persists across scan iterations.
type scanSession struct {
	ctx          context.Context
	port         devices.Port
	deviceInfo   devices.PortInfo
	apiClient    *api.Client
	planClient   *api.PlanServerClient
	allSpools    []models.FindSpool
	sticky       *models.FindSpool // current target spool for incoming scans
	lastUsed     *models.FindSpool // offered at top of picker when dropping stickiness
	dryRun       bool
	clientHost   string
}

func runScan(cmd *cobra.Command, _ []string) error {
	if Cfg == nil || Cfg.ApiBase == "" {
		return errors.New("api_base must be configured")
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	ctx := cmd.Context()

	info, err := devices.Probe(nil)
	if err != nil {
		if errors.Is(err, devices.ErrNoDevice) {
			return errors.New("no TD-1 detected — plug it in and try again")
		}
		return fmt.Errorf("probe TD-1: %w", err)
	}

	port, err := devices.Open(info.Path)
	if err != nil {
		return fmt.Errorf("open TD-1 on %s: %w (check System Settings → Privacy & Security for USB access on macOS)", info.Path, err)
	}
	defer func() { _ = port.Close() }()

	// Optional handshake: device may require "connect\n" before scanning. Ignore
	// errors — the user's setup may not need it, and the scan read below will
	// fail with a clear message if something's truly wrong.
	_ = port.WriteLine("connect")
	handshakeCtx, handshakeCancel := context.WithTimeout(ctx, 2*time.Second)
	for i := 0; i < 5; i++ {
		line, rerr := port.ReadLine(handshakeCtx)
		if rerr != nil {
			break
		}
		if strings.Contains(strings.ToLower(line), "ready") {
			break
		}
	}
	handshakeCancel()

	apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
	var planClient *api.PlanServerClient
	if Cfg.PlansServer != "" {
		planClient = api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
	}

	all, err := apiClient.FindSpoolsByName(ctx, "*", nil, nil)
	if err != nil {
		return fmt.Errorf("fetch spools: %w", err)
	}
	// Filter out archived — scans are for live inventory.
	active := all[:0]
	for _, s := range all {
		if !s.Archived {
			active = append(active, s)
		}
	}

	host, _ := os.Hostname()

	s := &scanSession{
		ctx:        ctx,
		port:       port,
		deviceInfo: info,
		apiClient:  apiClient,
		planClient: planClient,
		allSpools:  active,
		dryRun:     dryRun,
		clientHost: host,
	}

	fmt.Printf("Connected to TD-1 on %s\n", info.Path)
	if dryRun {
		color.HiRed("Dry-run mode — nothing will be written to Spoolman.")
	}
	fmt.Println("Press P on the device to scan. Ctrl+C to exit.")
	fmt.Println()

	return s.loop()
}

// loop reads scans in a loop, processing each until the user quits or the
// context is cancelled.
func (s *scanSession) loop() error {
	for {
		result, err := devices.ReadScan(s.ctx, s.port, 10)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			if errors.Is(err, devices.ErrBadColor) || errors.Is(err, devices.ErrBadTD) {
				color.Yellow("scan rejected: %v", err)
				s.logEvent(api.ScanEvent{
					Action:     "error",
					Error:      err.Error(),
					RawCSV:     strings.TrimSpace(result.RawCSV),
					DeviceUID:  result.UID,
					DeviceUUID: result.UUID,
				})
				continue
			}
			return fmt.Errorf("read scan: %w", err)
		}

		if done, err := s.handleScan(result); err != nil {
			return err
		} else if done {
			return nil
		}
	}
}

// handleScan processes a single successfully-parsed scan: picks a target spool
// (if none is sticky), presents the action menu, and applies the user's choice.
// Returns done=true when the user chose to quit.
func (s *scanSession) handleScan(r devices.ScanResult) (bool, error) {
	// Display the scanned values.
	fmt.Println()
	fmt.Printf("Scan: %s  %s", r.Color, models.GetColorBlock(r.Color, ""))
	if r.HasTD {
		fmt.Printf("  TD %.2fmm", r.TD)
	} else {
		fmt.Printf("  TD (not measured)")
	}
	fmt.Println()

	// If we don't have a sticky target, pick one.
	if s.sticky == nil {
		target, canceled, err := s.pickTarget(r.Color)
		if err != nil {
			return false, err
		}
		if canceled {
			s.logEvent(s.buildEvent(r, nil, "skip", "", false, false, ""))
			return false, nil
		}
		s.sticky = &target
	} else {
		fmt.Printf("Scanning for spool #%d: %s %s\n",
			s.sticky.Id, s.sticky.Filament.Vendor.Name, s.sticky.Filament.Name)
	}

	// Display diff against the current filament values.
	s.printDiff(r)

	// Action menu.
	action, err := s.promptAction(r)
	if err != nil {
		return false, err
	}
	switch action {
	case "write":
		return false, s.commit(r)
	case "skip":
		s.logEvent(s.buildEvent(r, s.sticky, "skip", "", false, false, ""))
		return false, nil
	case "rescan":
		s.logEvent(s.buildEvent(r, s.sticky, "rescan", "", false, false, ""))
		return false, nil
	case "new":
		s.lastUsed = s.sticky
		s.sticky = nil
		s.logEvent(s.buildEvent(r, nil, "skip", "", false, false, ""))
		return false, nil
	case "quit":
		s.logEvent(s.buildEvent(r, s.sticky, "skip", "", false, false, ""))
		return true, nil
	}
	return false, nil
}

// pickTarget prompts the user to pick a spool for the given scanned hex.
// Spools are pre-ranked by CIEDE2000 ΔE against the scan so the most likely
// match appears first. The previously-sticky spool (if any) is also floated.
func (s *scanSession) pickTarget(scannedHex string) (models.FindSpool, bool, error) {
	target, _ := parseHexColor(scannedHex)

	ranked := rankSpoolsByDistance(s.allSpools, target)
	if s.lastUsed != nil {
		ranked = floatSpoolToFront(ranked, s.lastUsed.Id)
	}

	sp, canceled, err := selectSpoolInteractively(s.ctx, s.apiClient, "", nil, ranked, false)
	if err != nil {
		return models.FindSpool{}, false, err
	}
	return sp, canceled, nil
}

// rankSpoolsByDistance returns a copy of spools sorted by ΔE to target. Spools
// without a parseable color end up at the end in original relative order.
func rankSpoolsByDistance(spools []models.FindSpool, target colorful.Color) []models.FindSpool {
	out := make([]models.FindSpool, len(spools))
	copy(out, spools)
	sort.SliceStable(out, func(i, j int) bool {
		di := spoolColorDistance(out[i], target)
		dj := spoolColorDistance(out[j], target)
		if math.IsInf(di, 1) && math.IsInf(dj, 1) {
			return false
		}
		return di < dj
	})
	return out
}

// floatSpoolToFront returns a copy of spools with the given spool ID moved to
// the front if present.
func floatSpoolToFront(spools []models.FindSpool, id int) []models.FindSpool {
	out := make([]models.FindSpool, 0, len(spools))
	var front *models.FindSpool
	for i := range spools {
		if spools[i].Id == id {
			s := spools[i]
			front = &s
			continue
		}
		out = append(out, spools[i])
	}
	if front != nil {
		out = append([]models.FindSpool{*front}, out...)
	}
	return out
}

// printDiff shows current filament color/td alongside the scanned values.
func (s *scanSession) printDiff(r devices.ScanResult) {
	f := s.sticky.Filament
	currentHex := canonScanHex(f.ColorHex)
	scannedHex := canonScanHex(r.Color)

	fmt.Printf("  Current filament: %s %s  #%d: %s %s (%s)\n",
		models.GetColorBlock(f.ColorHex, f.MultiColorHexes),
		firstNonEmpty(f.ColorHex, "(no color set)"),
		s.sticky.Id, f.Vendor.Name, f.Name, f.Material)

	if f.MultiColorHexes != "" {
		color.Yellow("  Multi-color filament — only TD will be written; color untouched.")
	} else if currentHex != "" && currentHex != scannedHex {
		fmt.Printf("  Scanned:          %s %s\n", models.GetColorBlock(r.Color, ""), r.Color)
	}
}

// promptAction displays the action menu and returns the chosen action name.
func (s *scanSession) promptAction(r devices.ScanResult) (string, error) {
	label := "Action"
	items := []string{
		"write — commit to Spoolman",
		"skip — discard this scan",
		"rescan — try again",
		"new — pick a different spool",
		"quit — exit",
	}
	if s.dryRun {
		items[0] = "write (dry-run — no Spoolman change)"
	}
	prompt := promptui.Select{
		Label:  label,
		Items:  items,
		Stdout: NoBellStdout,
	}
	idx, _, err := prompt.Run()
	if err != nil {
		if errors.Is(err, promptui.ErrInterrupt) || errors.Is(err, promptui.ErrAbort) {
			return "quit", nil
		}
		return "", err
	}
	_ = r
	switch idx {
	case 0:
		return "write", nil
	case 1:
		return "skip", nil
	case 2:
		return "rescan", nil
	case 3:
		return "new", nil
	default:
		return "quit", nil
	}
}

// commit writes the scan to Spoolman and logs the event. Returns nil on
// success so the loop continues; returns an error only on context cancel or
// something truly broken (the caller surfaces it to the user).
func (s *scanSession) commit(r devices.ScanResult) error {
	f := s.sticky.Filament
	currentHex := canonScanHex(f.ColorHex)
	scannedHex := canonScanHex(r.Color)
	multiColor := f.MultiColorHexes != ""

	writeColor := !multiColor && scannedHex != ""
	writeTD := r.HasTD

	// If color already has a value, confirm overwrite. Empty → silent write.
	if writeColor && currentHex != "" && currentHex != scannedHex {
		if !confirmOverwrite(currentHex, scannedHex) {
			writeColor = false
		}
	}

	if !writeColor && !writeTD {
		color.Yellow("Nothing to write.")
		s.logEvent(s.buildEvent(r, s.sticky, "skip", "nothing to write", false, false, ""))
		return nil
	}

	updates := map[string]any{}
	if writeColor {
		updates["color_hex"] = strings.TrimPrefix(scannedHex, "#")
	}
	if writeTD {
		// Spoolman custom fields: values in `extra` are JSON-encoded strings.
		// For a numeric custom field like `td`, send the number formatted as a string.
		// If Spoolman returns 422 we'll see it in the error path and the user can
		// adjust the encoding once a real write is attempted against the live instance.
		updates["extra"] = map[string]any{
			"td": strconv.FormatFloat(r.TD, 'f', 2, 64),
		}
	}

	errStr := ""
	var patchErr error
	if s.dryRun {
		fmt.Printf("  → would PATCH filament #%d: %v\n", f.Id, updates)
	} else {
		patchErr = s.apiClient.PatchFilament(s.ctx, f.Id, updates)
		if patchErr != nil {
			color.Red("PATCH failed: %v", patchErr)
			errStr = patchErr.Error()
		} else {
			color.Green("Wrote filament #%d.", f.Id)
		}
	}

	action := "commit"
	if patchErr != nil || s.dryRun {
		if patchErr != nil {
			action = "error"
		}
	}

	ev := s.buildEvent(r, s.sticky, action, errStr, writeColor && patchErr == nil && !s.dryRun, writeTD && patchErr == nil && !s.dryRun, "")
	s.logEvent(ev)

	return nil
}

// buildEvent constructs a ScanEvent from session + scan state.
func (s *scanSession) buildEvent(r devices.ScanResult, target *models.FindSpool, action, errStr string, colorWritten, tdWritten bool, _ string) api.ScanEvent {
	ev := api.ScanEvent{
		Timestamp:    time.Now().UTC(),
		ClientHost:   s.clientHost,
		DeviceUID:    r.UID,
		DeviceUUID:   r.UUID,
		ScannedHex:   r.Color,
		ScannedTD:    r.TD,
		HasTD:        r.HasTD,
		Action:       action,
		Error:        errStr,
		RawCSV:       r.RawCSV,
		ColorWritten: colorWritten,
		TDWritten:    tdWritten,
	}
	if target != nil {
		ev.SpoolID = target.Id
		ev.FilamentID = target.Filament.Id
		ev.PreviousHex = target.Filament.ColorHex
		// PreviousTD: if the filament had a TD custom field, we'd read it here.
		// We don't parse Extra today — leave nil. The raw CSV + current scan is
		// enough to reconstruct drift analysis from the log.
	}
	return ev
}

// logEvent POSTs the event to the plan server best-effort. A missing plan
// server (or server error) is logged to stderr but never fails the scan.
func (s *scanSession) logEvent(ev api.ScanEvent) {
	if s.planClient == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	if err := s.planClient.PostScanEvent(ctx, ev); err != nil {
		color.HiBlack("(scan-history not logged: %v)", err)
	}
}

// canonScanHex lowercases and prefixes with # for display/comparison.
func canonScanHex(h string) string {
	h = strings.TrimSpace(strings.ToLower(h))
	if h == "" {
		return ""
	}
	if !strings.HasPrefix(h, "#") {
		h = "#" + h
	}
	return h
}

// firstNonEmpty returns a if non-empty, else b.
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// confirmOverwrite asks the user to confirm overwriting an existing color value.
// Uses the same promptui style as other confirmations in fil for consistency.
func confirmOverwrite(currentHex, scannedHex string) bool {
	fmt.Printf("  Current:  %s %s\n", models.GetColorBlock(currentHex, ""), currentHex)
	fmt.Printf("  Scanned:  %s %s\n", models.GetColorBlock(scannedHex, ""), scannedHex)
	prompt := promptui.Prompt{
		Label:     "Overwrite current color",
		IsConfirm: true,
		Stdout:    NoBellStdout,
	}
	res, err := prompt.Run()
	if err != nil {
		return false
	}
	res = strings.ToLower(strings.TrimSpace(res))
	return res == "" || res == "y" || res == "yes"
}
