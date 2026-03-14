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
	Short:   "Reprint an archived project",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || (Cfg.ArchiveDir == "" && Cfg.PlansServer == "") || (Cfg.PlansDir == "" && Cfg.PlansServer == "") {
			return fmt.Errorf("archive_dir and plans_dir (or plans_server) must be configured in config.json")
		}

		type archiveEntry struct {
			path        string // local path (empty for remote)
			remoteName  string // remote filename (empty for local)
			displayName string
			remote      bool
		}
		var entries []archiveEntry

		// Local archived plans
		if Cfg.ArchiveDir != "" {
			if _, err := os.Stat(Cfg.ArchiveDir); err == nil {
				files, _ := filepath.Glob(filepath.Join(Cfg.ArchiveDir, "*.yaml"))
				files2, _ := filepath.Glob(filepath.Join(Cfg.ArchiveDir, "*.yml"))
				files = append(files, files2...)
				for _, f := range files {
					entries = append(entries, archiveEntry{
						path:        f,
						displayName: FormatPlanPath(f),
					})
				}
			}
		}

		// Remote archived plans
		if Cfg.PlansServer != "" {
			client := api.NewPlanServerClient(Cfg.PlansServer)
			summaries, err := client.ListPlans(context.Background(), "archived")
			if err != nil {
				fmt.Printf("Warning: could not fetch archived plans from server: %v\n", err)
			} else {
				for _, s := range summaries {
					entries = append(entries, archiveEntry{
						remoteName:  s.Name,
						displayName: "<server:archive>/" + s.Name,
						remote:      true,
					})
				}
			}
		}

		if len(entries) == 0 {
			return fmt.Errorf("no archived plans found")
		}

		var selected archiveEntry
		if len(entries) == 1 {
			selected = entries[0]
		} else {
			var displayNames []string
			for _, e := range entries {
				displayNames = append(displayNames, e.displayName)
			}
			prompt := promptui.Select{
				Label:             "Select archived plan to reprint",
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
			client := api.NewPlanServerClient(Cfg.PlansServer)
			data, err = client.GetPlan(context.Background(), selected.remoteName, "archived")
		} else {
			data, err = os.ReadFile(selected.path)
		}
		if err != nil {
			return fmt.Errorf("failed to read archived plan: %w", err)
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

		// Save to server or local plans_dir
		updatedData, err := yaml.Marshal(plan)
		if err != nil {
			return fmt.Errorf("failed to marshal plan: %w", err)
		}

		if Cfg.PlansServer != "" {
			client := api.NewPlanServerClient(Cfg.PlansServer)
			if err := client.PutPlan(context.Background(), newFilename, updatedData); err != nil {
				return fmt.Errorf("failed to upload reprinted plan: %w", err)
			}
			fmt.Printf("Successfully reprinted plan to <server>/%s\n", newFilename)
		} else {
			destPath := filepath.Join(Cfg.PlansDir, newFilename)

			// Check if destination already exists and find a unique name
			counter := 1
			for {
				if _, err := os.Stat(destPath); os.IsNotExist(err) {
					break
				}
				destPath = filepath.Join(Cfg.PlansDir, fmt.Sprintf("%s-%d%s", base, counter, ext))
				counter++
			}

			if err := os.WriteFile(destPath, updatedData, 0644); err != nil {
				return fmt.Errorf("failed to write plan file: %w", err)
			}
			fmt.Printf("Successfully reprinted plan to %s\n", FormatPlanPath(destPath))
		}

		return nil
	},
}

func init() {
	planCmd.AddCommand(planReprintCmd)
	planReprintCmd.Flags().IntP("number", "n", 1, "Number of reprints")
}
