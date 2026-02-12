package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var planDeleteCmd = &cobra.Command{
	Use:     "delete",
	Aliases: []string{"del"},
	Short:   "Delete an active plan",
	RunE: func(cmd *cobra.Command, args []string) error {
		var path string
		var displayName string
		if len(args) > 0 {
			path = args[0]
			displayName = FormatPlanPath(path)
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
				displayName = plans[0].DisplayName
			} else {
				var items []string
				for _, p := range plans {
					items = append(items, p.DisplayName)
				}
				prompt := promptui.Select{
					Label:             "Select plan to delete",
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
				displayName = plans[selectedIdx].DisplayName
			}
		}

		confirmPrompt := promptui.Prompt{
			Label:     fmt.Sprintf("Are you sure you want to delete plan %s", displayName),
			IsConfirm: true,
			Stdout:    NoBellStdout,
		}

		_, err := confirmPrompt.Run()
		if err != nil {
			if errors.Is(err, promptui.ErrAbort) {
				fmt.Println("Deletion aborted.")
				return nil
			}
			return err
		}

		err = os.Remove(path)
		if err != nil {
			return fmt.Errorf("failed to delete plan: %w", err)
		}

		fmt.Printf("Plan %s deleted successfully.\n", displayName)
		return nil
	},
}

func init() {
	planCmd.AddCommand(planDeleteCmd)
}
