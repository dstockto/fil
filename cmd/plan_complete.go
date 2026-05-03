package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/dstockto/fil/plan"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var planCompleteCmd = &cobra.Command{
	Use:     "complete",
	Aliases: []string{"done", "c"},
	Short:   "Mark a plate as completed",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil {
			return fmt.Errorf("config not loaded")
		}
		if PlanOps == nil {
			return fmt.Errorf("plan operations not configured (need either plans_server or api_base+plans_dir)")
		}
		if Cfg.ApiBase == "" {
			return fmt.Errorf("api_base not configured (needed for spool queries)")
		}
		ctx := cmd.Context()
		apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)

		plans, err := discoverPlans()
		if err != nil {
			return fmt.Errorf("discover plans: %w", err)
		}
		dp, err := selectPlan("Select plan file", plans)
		if err != nil {
			return err
		}
		planFile := dp.Plan

		projIdx, plateIdx, err := selectPlateToComplete(planFile)
		if err != nil {
			return err
		}
		if projIdx < 0 {
			fmt.Println("Nothing to complete.")
			return nil
		}
		plate := planFile.Projects[projIdx].Plates[plateIdx]
		project := planFile.Projects[projIdx]

		printerName := pickPrinterForComplete(plate)
		var printerLocations []string
		if printerName != "" {
			printerLocations = Cfg.Printers[printerName].Locations
		}

		fmt.Printf("Updating filament usage for %s...\n", models.Sanitize(plate.Name))
		deductions, err := collectDeductions(ctx, apiClient, plate, printerLocations, printerName)
		if err != nil {
			return err
		}

		req := plan.CompleteRequest{
			Plan:              planFileName(*dp),
			Project:           project.Name,
			Plate:             plate.Name,
			Printer:           printerName,
			StartedAt:         plate.StartedAt,
			EstimatedDuration: plate.EstimatedDuration,
			FinishedAt:        time.Now().UTC(),
			Deductions:        deductions,
			Filament:          plate.Needs,
		}

		result, err := PlanOps.Complete(ctx, req)
		if err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if result.ProjectCascaded {
			fmt.Printf("Project %s now fully complete.\n", models.Sanitize(project.Name))
		}
		fmt.Println("Plan updated.")
		return nil
	},
}

// selectPlateToComplete prompts the user to pick a plate from the plan,
// preferring in-progress plates at the top. Returns -1 indices when there's
// nothing to complete.
func selectPlateToComplete(planFile models.PlanFile) (int, int, error) {
	type opt struct {
		projIdx  int
		plateIdx int
	}
	var inProgressOptions, otherOptions []string
	var inProgressOptMap, otherOptMap []opt

	for i, proj := range planFile.Projects {
		if proj.Status == "completed" {
			continue
		}
		for j, plate := range proj.Plates {
			if plate.Status == "completed" {
				continue
			}
			label := fmt.Sprintf("%s / %s", proj.Name, plate.Name)
			if plate.Status == "in-progress" && plate.Printer != "" {
				label = fmt.Sprintf("%s / %s (printing on %s)", proj.Name, plate.Name, plate.Printer)
				inProgressOptions = append(inProgressOptions, label)
				inProgressOptMap = append(inProgressOptMap, opt{projIdx: i, plateIdx: j})
			} else {
				otherOptions = append(otherOptions, label)
				otherOptMap = append(otherOptMap, opt{projIdx: i, plateIdx: j})
			}
		}
	}

	options := append(inProgressOptions, otherOptions...)
	optMap := append(inProgressOptMap, otherOptMap...)
	if len(options) == 0 {
		return -1, -1, nil
	}

	prompt := promptui.Select{
		Label:             "Which plate did you complete?",
		Items:             options,
		Size:              10,
		Stdout:            NoBellStdout,
		StartInSearchMode: true,
		Searcher: func(input string, index int) bool {
			return strings.Contains(strings.ToLower(options[index]), strings.ToLower(input))
		},
	}
	idx, _, err := prompt.Run()
	if err != nil {
		return 0, 0, err
	}
	return optMap[idx].projIdx, optMap[idx].plateIdx, nil
}

// pickPrinterForComplete returns the printer to use for filament-usage
// tracking. Pre-fills from in-progress; falls back to the configured printer
// list (auto-selecting the only one or prompting). Returns "" when no printer
// is selected.
func pickPrinterForComplete(plate models.Plate) string {
	if plate.Printer != "" {
		fmt.Printf("Printer: %s (from in-progress tracking)\n", plate.Printer)
		return plate.Printer
	}
	if len(Cfg.Printers) == 0 {
		return ""
	}
	var printerNames []string
	for name := range Cfg.Printers {
		printerNames = append(printerNames, name)
	}
	if len(printerNames) == 1 {
		return printerNames[0]
	}
	items := append([]string{"None/Other"}, printerNames...)
	prompt := promptui.Select{
		Label:             "Which printer was used?",
		Items:             items,
		Stdout:            NoBellStdout,
		StartInSearchMode: true,
		Searcher: func(input string, index int) bool {
			return strings.Contains(strings.ToLower(items[index]), strings.ToLower(input))
		},
	}
	_, result, err := prompt.Run()
	if err != nil || result == "None/Other" {
		return ""
	}
	return result
}

// collectDeductions runs the per-need interactive flow: ask the user how much
// filament they used, find a matching spool in the printer, deduct, and loop
// for overruns until the user is satisfied. Returns a SpoolDeduction list
// that LocalPlanOps applies via Spoolman writes.
//
// Note: this performs Spoolman *reads* (FindSpoolsByName, FindSpoolsById) to
// resolve the deduction list. In Remote Mode the writes still go through the
// plan-server. May revisit later to route reads through the server too.
func collectDeductions(ctx context.Context, apiClient *api.Client, plate models.Plate, printerLocations []string, printerName string) ([]plan.SpoolDeduction, error) {
	var out []plan.SpoolDeduction
	for _, req := range plate.Needs {
		fmt.Printf("Filament: %s. Amount used (default %.1fg): ", models.Sanitize(req.Name), req.Amount)
		var input string
		_, _ = fmt.Scanln(&input)
		used := req.Amount
		if input != "" {
			_, _ = fmt.Sscanf(input, "%f", &used)
		}

		for used > 0 {
			matched, manualID, skipped := findCompleteSpool(ctx, apiClient, req, printerLocations, printerName, used)
			if skipped {
				break
			}
			if matched != nil {
				amount := used
				if used > matched.RemainingWeight && matched.RemainingWeight > 0 {
					fmt.Printf("Spool #%d only has %.1fg remaining. Deduct all of it and pick another spool for the rest? [Y/n] ", matched.Id, matched.RemainingWeight)
					var confirm string
					_, _ = fmt.Scanln(&confirm)
					if confirm == "" || strings.ToLower(confirm) == "y" {
						amount = matched.RemainingWeight
					}
				}
				out = append(out, plan.SpoolDeduction{SpoolID: matched.Id, Amount: amount})
				used -= amount
				continue
			}
			// Manual ID path: user typed an ID we couldn't verify against
			// Spoolman. Record the deduction at face value; LocalPlanOps
			// will surface any Spoolman rejection as an error.
			out = append(out, plan.SpoolDeduction{SpoolID: manualID, Amount: used})
			used = 0
		}
	}
	return out, nil
}

// findCompleteSpool resolves a single deduction step interactively. Returns:
//   - (spool, 0, false) when a spool was successfully matched
//   - (nil, manualID, false) when the user typed an unverified spool ID
//   - (nil, 0, true) when the user opted to skip (blank input)
func findCompleteSpool(ctx context.Context, apiClient *api.Client, req models.PlateRequirement, printerLocations []string, printerName string, used float64) (*models.FindSpool, int, bool) {
	if len(printerLocations) > 0 {
		allSpools, _ := apiClient.FindSpoolsByName(ctx, "*", onlyStandardFilament, nil)
		var candidates []models.FindSpool
		for _, s := range allSpools {
			inPrinter := false
			for _, loc := range printerLocations {
				if s.Location == loc {
					inPrinter = true
					break
				}
			}
			if !inPrinter {
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
			fmt.Printf("Using spool #%d (%s) from %s (%.1fg -> %.1fg remaining)\n",
				candidates[0].Id, models.Sanitize(candidates[0].Filament.Name),
				models.Sanitize(candidates[0].Location),
				candidates[0].RemainingWeight, candidates[0].RemainingWeight-used)
			return &candidates[0], 0, false
		}
		if len(candidates) > 1 {
			items := make([]string, 0, len(candidates)+1)
			for _, c := range candidates {
				items = append(items, fmt.Sprintf("#%d: %s (%s) - %.1fg -> %.1fg remaining",
					c.Id, models.Sanitize(c.Filament.Name), models.Sanitize(c.Location),
					c.RemainingWeight, c.RemainingWeight-used))
			}
			items = append(items, "Other/Manual")
			prompt := promptui.Select{
				Label:             fmt.Sprintf("Multiple matching spools found in %s. Select one:", printerName),
				Items:             items,
				Stdout:            NoBellStdout,
				StartInSearchMode: true,
				Searcher: func(input string, index int) bool {
					return strings.Contains(strings.ToLower(items[index]), strings.ToLower(input))
				},
			}
			idx, _, err := prompt.Run()
			if err == nil && idx < len(candidates) {
				return &candidates[idx], 0, false
			}
		}
	}

	// Manual-ID fallback.
	fmt.Printf("Enter Spool ID to deduct from (%.1fg remaining to account for, or leave blank to skip): ", used)
	var spoolIdStr string
	_, _ = fmt.Scanln(&spoolIdStr)
	if spoolIdStr == "" {
		return nil, 0, true
	}
	var sid int
	_, _ = fmt.Sscanf(spoolIdStr, "%d", &sid)
	if spool, err := apiClient.FindSpoolsById(ctx, sid); err == nil && spool != nil {
		return spool, 0, false
	} else if err != nil {
		fmt.Printf("Could not verify spool #%d (%v). Recording deduction anyway.\n", sid, err)
	}
	return nil, sid, false
}

func init() {
	planCmd.AddCommand(planCompleteCmd)
}
