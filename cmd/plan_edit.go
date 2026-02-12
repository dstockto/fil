package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var planEditCmd = &cobra.Command{
	Use:     "edit",
	Aliases: []string{"ed", "e"},
	Short:   "Edit an active plan file",
	RunE: func(cmd *cobra.Command, args []string) error {
		var path string
		if len(args) > 0 {
			path = args[0]
		} else {
			plans, err := discoverPlans()
			if err != nil {
				return err
			}
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
					Label:             "Select plan file to edit",
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

		editor := os.Getenv("VISUAL")
		if editor == "" {
			editor = os.Getenv("EDITOR")
		}
		if editor == "" {
			// Fallback to common editors
			for _, e := range []string{"vim", "vi", "nano"} {
				if _, err := os.Stat("/usr/bin/" + e); err == nil {
					editor = e
					break
				}
				if _, err := os.Stat("/usr/local/bin/" + e); err == nil {
					editor = e
					break
				}
			}
		}
		if editor == "" {
			return fmt.Errorf("no editor found. Please set $VISUAL or $EDITOR environment variable")
		}

		// Handle editor with arguments (e.g. "code --wait")
		parts := strings.Fields(editor)
		editorCmd := parts[0]
		editorArgs := append(parts[1:], path)

		c := exec.Command(editorCmd, editorArgs...)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr

		return c.Run()
	},
}

func init() {
	planCmd.AddCommand(planEditCmd)
}
