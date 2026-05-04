package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var planReprintCmd = &cobra.Command{
	Use:     "reprint",
	Aliases: []string{"rp"},
	Short:   "Create a fresh copy of a plan for reprinting",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || (Cfg.PlansDir == "" && Cfg.PlansServer == "") {
			return fmt.Errorf("plans_dir or plans_server must be configured in config.json")
		}

		type reprintEntry struct {
			path        string // local path (empty for remote)
			remoteName  string // remote filename (empty for local)
			displayName string
			remote      bool
			status      string // "active", "paused", or "archived"
		}
		var entries []reprintEntry

		// Active and paused plans (from discoverPlansWithFilter)
		activePlans, _ := discoverPlansWithFilter(true, false)
		for _, dp := range activePlans {
			label := "active"
			if Cfg.PauseDir != "" {
				absPauseDir, _ := filepath.Abs(Cfg.PauseDir)
				if dp.Path != "" && strings.HasPrefix(dp.Path, absPauseDir) {
					label = "paused"
				}
				if dp.Remote && strings.Contains(dp.DisplayName, "<paused>") {
					label = "paused"
				}
			}
			e := reprintEntry{
				path:        dp.Path,
				remoteName:  dp.RemoteName,
				displayName: fmt.Sprintf("%s (%s)", FormatDiscoveredPlan(dp), label),
				remote:      dp.Remote,
				status:      label,
			}
			entries = append(entries, e)
		}

		// Local archived plans
		if Cfg.ArchiveDir != "" {
			if _, err := os.Stat(Cfg.ArchiveDir); err == nil {
				files, _ := filepath.Glob(filepath.Join(Cfg.ArchiveDir, "*.yaml"))
				files2, _ := filepath.Glob(filepath.Join(Cfg.ArchiveDir, "*.yml"))
				files = append(files, files2...)
				for _, f := range files {
					entries = append(entries, reprintEntry{
						path:        f,
						displayName: fmt.Sprintf("%s (archived)", FormatPlanPath(f)),
						status:      "archived",
					})
				}
			}
		}

		// Remote archived plans
		if Cfg.PlansServer != "" {
			client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
			summaries, err := client.ListPlans(context.Background(), "archived")
			if err != nil {
				fmt.Printf("Warning: could not fetch archived plans from server: %v\n", err)
			} else {
				for _, s := range summaries {
					entries = append(entries, reprintEntry{
						remoteName:  s.Name,
						displayName: fmt.Sprintf("<server:archive>/%s (archived)", s.Name),
						remote:      true,
						status:      "archived",
					})
				}
			}
		}

		if len(entries) == 0 {
			return fmt.Errorf("no plans found to reprint")
		}

		var selected reprintEntry
		if len(entries) == 1 {
			selected = entries[0]
		} else {
			var displayNames []string
			for _, e := range entries {
				displayNames = append(displayNames, e.displayName)
			}
			prompt := promptui.Select{
				Label:             "Select plan to reprint",
				Items:             displayNames,
				Stdout:            NoBellStdout,
				StartInSearchMode: true,
				Searcher: func(input string, index int) bool {
					name := strings.ToLower(displayNames[index])
					input = strings.ToLower(input)
					return strings.Contains(name, input)
				},
			}
			idx, _, err := prompt.Run()
			if err != nil {
				return err
			}
			selected = entries[idx]
		}

		// Read the plan
		var data []byte
		var err error
		if selected.remote {
			client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
			switch selected.status {
			case "archived":
				data, err = client.GetPlan(context.Background(), selected.remoteName, "archived")
			case "paused":
				data, err = client.GetPlan(context.Background(), selected.remoteName, "paused")
			default:
				data, err = client.GetPlan(context.Background(), selected.remoteName)
			}
		} else {
			data, err = os.ReadFile(selected.path)
		}
		if err != nil {
			return fmt.Errorf("failed to read plan: %w", err)
		}

		var plan models.PlanFile
		if err := yaml.Unmarshal(data, &plan); err != nil {
			return fmt.Errorf("failed to unmarshal plan: %w", err)
		}
		plan.DefaultStatus()

		num, _ := cmd.Flags().GetInt("number")
		if num < 1 {
			num = 1
		}

		// Reset all plates and projects to todo
		for i := range plan.Projects {
			plan.Projects[i].Status = "todo"
			for j := range plan.Projects[i].Plates {
				plan.Projects[i].Plates[j].Status = "todo"
				plan.Projects[i].Plates[j].Printer = ""
				plan.Projects[i].Plates[j].StartedAt = ""
			}

			if num > 1 {
				originalPlates := plan.Projects[i].Plates
				for n := 1; n < num; n++ {
					plan.Projects[i].Plates = append(plan.Projects[i].Plates, originalPlates...)
				}
			}
		}

		// Determine new filename
		var nameSource string
		if selected.remote {
			nameSource = selected.remoteName
		} else {
			nameSource = filepath.Base(selected.path)
		}
		ext := filepath.Ext(nameSource)
		base := strings.TrimSuffix(nameSource, ext)

		// Remove timestamp suffix if present (Format: 20060102150405, length 14)
		if len(base) >= 15 && base[len(base)-15] == '-' {
			timestampPart := base[len(base)-14:]
			isDigits := true
			for _, r := range timestampPart {
				if r < '0' || r > '9' {
					isDigits = false
					break
				}
			}
			if isDigits {
				base = base[:len(base)-15]
			}
		}

		newFilename := base + ext

		// Disambiguate the new filename so we don't overwrite an existing
		// plan. Both modes do this differently — server lists via API, local
		// stats the filesystem — but the rule (append -N until free) is the
		// same. Resolved filename then flows into PlanOps.SaveAll.
		newFilename = uniqueReprintName(newFilename, base, ext)

		if PlanOps == nil {
			return fmt.Errorf("plan operations not configured")
		}
		if err := PlanOps.SaveAll(cmd.Context(), newFilename, plan); err != nil {
			return fmt.Errorf("failed to save reprinted plan: %w", err)
		}
		if Cfg.PlansServer != "" {
			fmt.Printf("Successfully reprinted plan to <server>/%s\n", newFilename)
		} else {
			fmt.Printf("Successfully reprinted plan to %s\n", FormatPlanPath(filepath.Join(Cfg.PlansDir, newFilename)))
		}
		return nil
	},
}

// uniqueReprintName returns a name that doesn't collide with an existing
// plan. Falls back to the candidate as-is on errors (caller will hit the
// real conflict during SaveAll).
func uniqueReprintName(candidate, base, ext string) string {
	if Cfg.PlansServer != "" {
		client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
		existing, err := client.ListPlans(context.Background(), "")
		if err != nil {
			return candidate
		}
		taken := make(map[string]bool, len(existing))
		for _, s := range existing {
			taken[s.Name] = true
		}
		counter := 1
		for taken[candidate] {
			candidate = fmt.Sprintf("%s-%d%s", base, counter, ext)
			counter++
		}
		return candidate
	}
	if Cfg.PlansDir == "" {
		return candidate
	}
	dest := filepath.Join(Cfg.PlansDir, candidate)
	counter := 1
	for {
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d%s", base, counter, ext)
		dest = filepath.Join(Cfg.PlansDir, candidate)
		counter++
	}
}

func init() {
	planCmd.AddCommand(planReprintCmd)
	planReprintCmd.Flags().IntP("number", "n", 1, "Number of reprints")
}
