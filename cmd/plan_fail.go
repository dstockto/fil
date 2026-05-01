package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

// failCauses is the closed enum displayed in the cause picker. Order matches
// what David tends to see most often in practice (first-layer issues first).
var failCauses = []string{
	"bed_adhesion",
	"bad_first_layer",
	"warping",
	"spaghetti",
	"layer_shift",
	"blob_of_death",
	"other",
}

// failPlateRef identifies an in-progress plate the user can fail.
type failPlateRef struct {
	dpIdx     int // index into the discoveredPlans slice
	projIdx   int
	plateIdx  int
	planName  string // "<plans>/foo.yaml" style display
	project   string
	plate     string
	printer   string
	startedAt string
	estDur    string
	needs     []models.PlateRequirement
}

// shareAllocation is one filament-deduction the command should perform after
// the user supplies a total grams figure. Computed by allocateShares.
type shareAllocation struct {
	plateRef     int     // index into the failPlateRef slice this came from
	needIdx      int     // index into the plate's Needs slice
	plannedGrams float64 // the plate need's planned amount
	shareGrams   float64 // grams to deduct from the matching spool
}

// allocateShares splits totalUsedGrams across each (plate, need) pair in
// proportion to its planned amount. Returns one allocation per Need plus
// the per-plate share total used in the JSONL log.
func allocateShares(plates []failPlateRef, totalUsedGrams float64) (allocations []shareAllocation, perPlate []float64) {
	perPlate = make([]float64, len(plates))
	if len(plates) == 0 {
		return nil, perPlate
	}

	totalPlanned := 0.0
	for _, p := range plates {
		for _, n := range p.needs {
			totalPlanned += n.Amount
		}
	}
	if totalPlanned <= 0 || totalUsedGrams <= 0 {
		return nil, perPlate
	}

	ratio := totalUsedGrams / totalPlanned
	for i, p := range plates {
		for j, n := range p.needs {
			share := n.Amount * ratio
			if share <= 0 {
				continue
			}
			allocations = append(allocations, shareAllocation{
				plateRef:     i,
				needIdx:      j,
				plannedGrams: n.Amount,
				shareGrams:   share,
			})
			perPlate[i] += share
		}
	}
	return allocations, perPlate
}

var planFailCmd = &cobra.Command{
	Use:     "fail",
	Aliases: []string{"f"},
	Short:   "Log a print failure (no plate state changes)",
	Long: `Logs an in-progress print failure to the print history without changing plate state.
Run plan stop afterward if you also want the plate moved back to todo.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.ApiBase == "" {
			return fmt.Errorf("api endpoint not configured")
		}
		if Cfg.PlansServer == "" {
			return fmt.Errorf("plans_server must be configured")
		}
		ctx := cmd.Context()
		apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
		planClient := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)

		discovered, err := discoverPlans()
		if err != nil {
			return fmt.Errorf("discover plans: %w", err)
		}
		refs := collectFailableInProgress(discovered)
		if len(refs) == 0 {
			fmt.Println("No in-progress plates to fail.")
			return nil
		}

		// Group by printer; if multiple printers have in-progress plates, pick one.
		printer, err := pickFailPrinter(refs)
		if err != nil {
			return err
		}
		var onPrinter []failPlateRef
		for _, r := range refs {
			if r.printer == printer {
				onPrinter = append(onPrinter, r)
			}
		}

		selected, err := selectFailPlates(onPrinter)
		if err != nil {
			return err
		}
		if len(selected) == 0 {
			fmt.Println("Nothing selected.")
			return nil
		}

		cause, err := selectFailCause()
		if err != nil {
			return err
		}

		reason, err := readLine("Reason (optional, press enter to skip): ")
		if err != nil {
			return err
		}
		reason = strings.TrimSpace(reason)

		totalGrams, err := readGrams("Total grams used (default 0): ")
		if err != nil {
			return err
		}

		// Deduct filament.
		var printerLocations []string
		if pc, ok := Cfg.Printers[printer]; ok {
			printerLocations = pc.Locations
		}
		allocations, perPlate := allocateShares(selected, totalGrams)
		if err := deductShares(ctx, apiClient, selected, allocations, printerLocations, printer); err != nil {
			fmt.Printf("Warning: filament deduction had errors: %v\n", err)
		}

		// Build the request.
		req := api.PlanFailRequest{
			Printer:  printer,
			Cause:    cause,
			Reason:   reason,
			FailedAt: time.Now().UTC(),
		}
		for i, p := range selected {
			pp := api.PlanFailPlate{
				Plan:              planFileName(discovered[p.dpIdx]),
				Project:           p.project,
				Plate:             p.plate,
				StartedAt:         p.startedAt,
				EstimatedDuration: p.estDur,
				UsedGrams:         perPlate[i],
			}
			for _, n := range p.needs {
				pp.Filament = append(pp.Filament, api.HistoryFilament{
					Name:       n.Name,
					FilamentID: n.FilamentID,
					Material:   n.Material,
					Amount:     n.Amount,
				})
			}
			req.Plates = append(req.Plates, pp)
		}

		if err := planClient.PostPlanFail(ctx, req); err != nil {
			return fmt.Errorf("log failure: %w", err)
		}

		fmt.Printf("Logged failure: %d plate(s) on %s — cause=%s, used=%.1fg\n", len(selected), printer, cause, totalGrams)
		fmt.Println("Plate state unchanged. Run `fil plan stop` if you want them back in todo.")
		return nil
	},
}

// collectFailableInProgress flattens every in-progress plate across all
// discovered plans into failPlateRefs.
func collectFailableInProgress(plans []DiscoveredPlan) []failPlateRef {
	var out []failPlateRef
	for i, dp := range plans {
		for pi, proj := range dp.Plan.Projects {
			for plIdx, plate := range proj.Plates {
				if plate.Status != "in-progress" {
					continue
				}
				if plate.Printer == "" {
					continue
				}
				out = append(out, failPlateRef{
					dpIdx:     i,
					projIdx:   pi,
					plateIdx:  plIdx,
					planName:  dp.DisplayName,
					project:   proj.Name,
					plate:     plate.Name,
					printer:   plate.Printer,
					startedAt: plate.StartedAt,
					estDur:    plate.EstimatedDuration,
					needs:     append([]models.PlateRequirement(nil), plate.Needs...),
				})
			}
		}
	}
	return out
}

// pickFailPrinter returns the printer to scope the failure to. When only one
// printer has in-progress plates the answer is auto-selected.
func pickFailPrinter(refs []failPlateRef) (string, error) {
	seen := map[string]struct{}{}
	var printers []string
	for _, r := range refs {
		if _, ok := seen[r.printer]; ok {
			continue
		}
		seen[r.printer] = struct{}{}
		printers = append(printers, r.printer)
	}
	sort.Strings(printers)
	if len(printers) == 1 {
		return printers[0], nil
	}

	prompt := promptui.Select{
		Label:             "Which printer failed?",
		Items:             printers,
		Stdout:            NoBellStdout,
		StartInSearchMode: true,
		Searcher: func(input string, idx int) bool {
			return strings.Contains(strings.ToLower(printers[idx]), strings.ToLower(input))
		},
	}
	_, val, err := prompt.Run()
	return val, err
}

// selectFailPlates lets the user pick one or more plates from the printer's
// in-progress list. Single-plate case auto-selects; otherwise an index-list
// prompt is used (e.g. "1,3" or "all").
func selectFailPlates(refs []failPlateRef) ([]failPlateRef, error) {
	if len(refs) == 1 {
		return refs, nil
	}

	fmt.Println("In-progress plates:")
	for i, r := range refs {
		started := ""
		if r.startedAt != "" {
			if t, err := time.Parse(time.RFC3339, r.startedAt); err == nil {
				elapsed := time.Since(t).Round(time.Minute)
				started = fmt.Sprintf("  (%s elapsed)", elapsed)
			}
		}
		fmt.Printf("  [%d] %s / %s — %s%s\n", i+1, models.Sanitize(r.project), models.Sanitize(r.plate), models.Sanitize(r.planName), started)
	}

	for {
		input, err := readLine("Which failed? (e.g. 1,2 or all): ")
		if err != nil {
			return nil, err
		}
		idxs, err := parsePlateSelection(strings.TrimSpace(input), len(refs))
		if err != nil {
			fmt.Printf("  %v\n", err)
			continue
		}
		var picked []failPlateRef
		for _, i := range idxs {
			picked = append(picked, refs[i])
		}
		return picked, nil
	}
}

// parsePlateSelection turns "1,3" / "all" / "1-3" into 0-based indices into a
// slice of length n. Returns an error on out-of-range or unparseable input.
func parsePlateSelection(input string, n int) ([]int, error) {
	if input == "" {
		return nil, fmt.Errorf("no selection")
	}
	if strings.EqualFold(input, "all") {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out, nil
	}
	seen := map[int]struct{}{}
	var out []int
	for _, part := range strings.Split(input, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			lo, err1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
			hi, err2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err1 != nil || err2 != nil || lo < 1 || hi > n || lo > hi {
				return nil, fmt.Errorf("invalid range %q", part)
			}
			for i := lo; i <= hi; i++ {
				if _, ok := seen[i-1]; ok {
					continue
				}
				seen[i-1] = struct{}{}
				out = append(out, i-1)
			}
			continue
		}
		v, err := strconv.Atoi(part)
		if err != nil || v < 1 || v > n {
			return nil, fmt.Errorf("invalid index %q (1..%d)", part, n)
		}
		if _, ok := seen[v-1]; ok {
			continue
		}
		seen[v-1] = struct{}{}
		out = append(out, v-1)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid indices")
	}
	sort.Ints(out)
	return out, nil
}

func selectFailCause() (string, error) {
	prompt := promptui.Select{
		Label:             "Cause",
		Items:             failCauses,
		Stdout:            NoBellStdout,
		StartInSearchMode: true,
		Searcher: func(input string, idx int) bool {
			return strings.Contains(failCauses[idx], strings.ToLower(input))
		},
	}
	_, val, err := prompt.Run()
	return val, err
}

// readLine reads a full line (including spaces) from stdin. Returns empty
// string on EOF or empty input.
func readLine(prompt string) (string, error) {
	fmt.Print(prompt)
	rd := bufio.NewReader(os.Stdin)
	line, err := rd.ReadString('\n')
	if err != nil && line == "" {
		// EOF with no data — treat as empty input rather than an error
		// so a non-interactive run with no stdin pipes through cleanly.
		return "", nil
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func readGrams(prompt string) (float64, error) {
	for {
		s, err := readLine(prompt)
		if err != nil {
			return 0, err
		}
		s = strings.TrimSpace(strings.TrimSuffix(s, "g"))
		if s == "" {
			return 0, nil
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || v < 0 {
			fmt.Println("  enter a non-negative number (or blank for 0)")
			continue
		}
		return v, nil
	}
}

// deductShares groups allocations by spool and calls UseFilamentSafely once
// per spool with the summed share. Falls back to a manual prompt when no
// matching spool can be found in the printer's locations.
func deductShares(ctx context.Context, apiClient *api.Client, plates []failPlateRef, allocs []shareAllocation, printerLocations []string, printerName string) error {
	if len(allocs) == 0 {
		return nil
	}

	// Find candidate spools for each requirement once and group share totals
	// by the chosen spool ID. We sum first, deduct second, so a single spool
	// powering two slots only triggers one Spoolman update.
	type pending struct {
		spool *models.FindSpool
		grams float64
		label string
	}
	var allSpools []models.FindSpool
	loaded := false
	loadSpools := func() []models.FindSpool {
		if loaded {
			return allSpools
		}
		s, err := apiClient.FindSpoolsByName(ctx, "*", onlyStandardFilament, nil)
		if err == nil {
			allSpools = s
		}
		loaded = true
		return allSpools
	}

	bySpool := map[int]*pending{}
	var unmatched []shareAllocation

	for _, a := range allocs {
		need := plates[a.plateRef].needs[a.needIdx]
		spool := findPrinterSpool(loadSpools(), printerLocations, need)
		if spool == nil {
			unmatched = append(unmatched, a)
			continue
		}
		if cur, ok := bySpool[spool.Id]; ok {
			cur.grams += a.shareGrams
		} else {
			label := fmt.Sprintf("#%d %s @ %s", spool.Id, models.Sanitize(spool.Filament.Name), models.Sanitize(spool.Location))
			cp := *spool
			bySpool[spool.Id] = &pending{spool: &cp, grams: a.shareGrams, label: label}
		}
	}

	for _, p := range bySpool {
		fmt.Printf("Deducting %.1fg from %s\n", p.grams, p.label)
		if err := UseFilamentSafely(ctx, apiClient, p.spool, p.grams); err != nil {
			return fmt.Errorf("deduct %s: %w", p.label, err)
		}
	}

	if len(unmatched) > 0 {
		fmt.Println("Could not auto-resolve spool for these requirements; deduct manually with `fil use`:")
		for _, a := range unmatched {
			need := plates[a.plateRef].needs[a.needIdx]
			fmt.Printf("  - %s / %s: %.1fg of %s\n",
				models.Sanitize(plates[a.plateRef].project), models.Sanitize(plates[a.plateRef].plate),
				a.shareGrams, models.Sanitize(need.Name))
		}
	}
	return nil
}

// findPrinterSpool picks a single spool in the printer's locations matching
// the requirement. Returns nil when there are zero matches or multiple
// candidates without a clear winner (caller falls back to manual deduction).
func findPrinterSpool(spools []models.FindSpool, printerLocations []string, req models.PlateRequirement) *models.FindSpool {
	if len(printerLocations) == 0 {
		return nil
	}
	locSet := map[string]struct{}{}
	for _, l := range printerLocations {
		locSet[l] = struct{}{}
	}

	var candidates []models.FindSpool
	for _, s := range spools {
		if _, ok := locSet[s.Location]; !ok {
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
	if len(candidates) == 1 {
		c := candidates[0]
		return &c
	}
	return nil
}

// planFileName returns the basename used as the plan key in history entries.
func planFileName(dp DiscoveredPlan) string {
	if dp.Remote {
		return dp.RemoteName
	}
	if dp.Path != "" {
		// keep this consistent with logCompletions which uses the plan name as posted to the server
		return baseName(dp.Path)
	}
	return dp.DisplayName
}

func baseName(p string) string {
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func init() {
	planCmd.AddCommand(planFailCmd)
}
