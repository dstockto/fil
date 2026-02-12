package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var planPauseCmd = &cobra.Command{
	Use:     "pause [file]",
	Aliases: []string{"p"},
	Short:   "Move a plan file to the pause directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.PauseDir == "" {
			return fmt.Errorf("pause_dir not configured in config.json")
		}

		// Ensure pause dir exists
		if _, err := os.Stat(Cfg.PauseDir); os.IsNotExist(err) {
			_ = os.MkdirAll(Cfg.PauseDir, 0755)
		}

		var path string
		if len(args) > 0 {
			path = args[0]
		} else {
			plans, _ := discoverPlans()
			if len(plans) == 0 {
				return fmt.Errorf("no plans found")
			}
			if len(plans) == 1 {
				path = plans[0].Path
			} else {
				var items []string
				for _, p := range plans {
					items = append(items, p.DisplayName)
				}
				prompt := promptui.Select{
					Label:             "Select plan file to pause",
					Items:             items,
					Stdout:            NoBellStdout,
					StartInSearchMode: true,
					Searcher: func(input string, index int) bool {
						return strings.Contains(strings.ToLower(items[index]), strings.ToLower(input))
					},
				}
				selectedIdx, _, err := prompt.Run()
				if err != nil {
					return err
				}
				path = plans[selectedIdx].Path
			}
		}

		dest := filepath.Join(Cfg.PauseDir, filepath.Base(path))
		err := os.Rename(path, dest)
		if err != nil {
			return fmt.Errorf("failed to move file: %w", err)
		}

		fmt.Printf("Moved %s to %s\n", FormatPlanPath(path), FormatPlanPath(dest))
		return nil
	},
}

func init() {
	planCmd.AddCommand(planPauseCmd)
}
