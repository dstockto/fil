package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		if Cfg == nil || Cfg.ArchiveDir == "" || Cfg.PlansDir == "" {
			return fmt.Errorf("archive_dir and plans_dir must be configured in config.json")
		}

		// Ensure archive dir exists
		if _, err := os.Stat(Cfg.ArchiveDir); os.IsNotExist(err) {
			return fmt.Errorf("archive directory %s does not exist", FormatPlanPath(Cfg.ArchiveDir))
		}

		// Find yaml files in archive directory
		files, _ := filepath.Glob(filepath.Join(Cfg.ArchiveDir, "*.yaml"))
		files2, _ := filepath.Glob(filepath.Join(Cfg.ArchiveDir, "*.yml"))
		files = append(files, files2...)

		if len(files) == 0 {
			return fmt.Errorf("no archived plans found in %s", FormatPlanPath(Cfg.ArchiveDir))
		}

		var selectedPath string
		if len(files) == 1 {
			selectedPath = files[0]
		} else {
			var displayNames []string
			for _, f := range files {
				displayNames = append(displayNames, FormatPlanPath(f))
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
			selectedPath = files[idx]
		}

		// Read the plan
		data, err := os.ReadFile(selectedPath)
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
		ext := filepath.Ext(selectedPath)
		base := strings.TrimSuffix(filepath.Base(selectedPath), ext)

		// Remove timestamp suffix if present (Format: 20060102150405, length 14)
		// Usually appended as -YYYYMMDDHHMMSS
		if len(base) >= 15 && base[len(base)-15] == '-' {
			timestampPart := base[len(base)-14:]
			// Check if it's all digits
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

		// Save the reset plan to the new location
		updatedData, err := yaml.Marshal(plan)
		if err != nil {
			return fmt.Errorf("failed to marshal plan: %w", err)
		}

		if err := os.WriteFile(destPath, updatedData, 0644); err != nil {
			return fmt.Errorf("failed to write plan file: %w", err)
		}

		fmt.Printf("Successfully reprinted plan to %s\n", FormatPlanPath(destPath))
		return nil
	},
}

func init() {
	planCmd.AddCommand(planReprintCmd)
	planReprintCmd.Flags().IntP("number", "n", 1, "Number of reprints")
}
