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

var planMoveBackCmd = &cobra.Command{
	Use:     "move-back",
	Aliases: []string{"mb"},
	Short:   "Move a plan file back to its original location",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.PlansDir == "" {
			return fmt.Errorf("plans_dir not configured in config.json")
		}

		// Find yaml files in plans directory
		files, _ := filepath.Glob(filepath.Join(Cfg.PlansDir, "*.yaml"))
		files2, _ := filepath.Glob(filepath.Join(Cfg.PlansDir, "*.yml"))
		files = append(files, files2...)

		if len(files) == 0 {
			return fmt.Errorf("no yaml files found in central plans directory")
		}

		var path string
		if len(files) == 1 {
			path = files[0]
		} else {
			prompt := promptui.Select{
				Label:             "Select plan file to move back",
				Items:             files,
				Stdout:            NoBellStdout,
				StartInSearchMode: true,
				Searcher: func(input string, index int) bool {
					return strings.Contains(strings.ToLower(files[index]), strings.ToLower(input))
				},
			}
			_, result, err := prompt.Run()
			if err != nil {
				return err
			}
			path = result
		}

		// Read the plan to find the original location
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read plan file: %w", err)
		}

		var plan models.PlanFile
		if err := yaml.Unmarshal(data, &plan); err != nil {
			return fmt.Errorf("failed to unmarshal plan: %w", err)
		}
		plan.DefaultStatus()

		if plan.OriginalLocation == "" {
			return fmt.Errorf("plan file does not have an original location recorded")
		}

		// Ensure the directory for the original location exists
		destDir := filepath.Dir(plan.OriginalLocation)
		if _, err := os.Stat(destDir); os.IsNotExist(err) {
			if err := os.MkdirAll(destDir, 0755); err != nil {
				return fmt.Errorf("failed to create destination directory: %w", err)
			}
		}

		if _, err := os.Stat(plan.OriginalLocation); err == nil {
			return fmt.Errorf("file %s already exists at original location", plan.OriginalLocation)
		}

		// Clear OriginalLocation before moving back
		originalDest := plan.OriginalLocation
		plan.OriginalLocation = ""
		updatedData, err := yaml.Marshal(plan)
		if err != nil {
			return fmt.Errorf("failed to marshal plan: %w", err)
		}
		if err := os.WriteFile(path, updatedData, 0644); err != nil {
			return fmt.Errorf("failed to update plan file: %w", err)
		}

		err = os.Rename(path, originalDest)
		if err != nil {
			return fmt.Errorf("failed to move file back: %w", err)
		}
		fmt.Printf("Moved %s back to %s\n", path, originalDest)
		return nil
	},
}

func init() {
	planCmd.AddCommand(planMoveBackCmd)
}
