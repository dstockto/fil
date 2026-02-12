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

var planMoveCmd = &cobra.Command{
	Use:     "move [file]",
	Aliases: []string{"mv", "m"},
	Short:   "Move a plan file to the central plans directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.PlansDir == "" {
			return fmt.Errorf("plans_dir not configured in config.json")
		}

		var path string
		if len(args) > 0 {
			path = args[0]
		} else {
			// Find yaml files in current directory
			files, _ := filepath.Glob("*.yaml")
			files2, _ := filepath.Glob("*.yml")
			files = append(files, files2...)

			if len(files) == 0 {
				return fmt.Errorf("no yaml files found in current directory")
			}
			if len(files) == 1 {
				path = files[0]
			} else {
				prompt := promptui.Select{
					Label:             "Select plan file to move",
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
		}

		// Ensure plans dir exists
		if _, err := os.Stat(Cfg.PlansDir); os.IsNotExist(err) {
			_ = os.MkdirAll(Cfg.PlansDir, 0755)
		}

		// Load the plan to update OriginalLocation
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read plan file: %w", err)
		}

		var plan models.PlanFile
		if err := yaml.Unmarshal(data, &plan); err != nil {
			return fmt.Errorf("failed to unmarshal plan: %w", err)
		}
		plan.DefaultStatus()

		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("failed to get absolute path: %w", err)
		}
		plan.OriginalLocation = absPath

		updatedData, err := yaml.Marshal(plan)
		if err != nil {
			return fmt.Errorf("failed to marshal plan: %w", err)
		}
		if err := os.WriteFile(path, updatedData, 0644); err != nil {
			return fmt.Errorf("failed to update plan file with original location: %w", err)
		}

		dest := filepath.Join(Cfg.PlansDir, filepath.Base(path))
		if _, err := os.Stat(dest); err == nil {
			return fmt.Errorf("file %s already exists in central Location", dest)
		}

		err = os.Rename(path, dest)
		if err != nil {
			return fmt.Errorf("failed to move file: %w", err)
		}
		fmt.Printf("Moved %s to %s\n", path, dest)
		return nil
	},
}

func init() {
	planCmd.AddCommand(planMoveCmd)
}
