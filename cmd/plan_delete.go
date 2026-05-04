package cmd

import (
	"errors"
	"fmt"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var planDeleteCmd = &cobra.Command{
	Use:     "delete",
	Aliases: []string{"del"},
	Short:   "Delete an active plan",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil {
			return fmt.Errorf("config not loaded")
		}
		if PlanOps == nil {
			return fmt.Errorf("plan operations not configured (need either plans_server or api_base+plans_dir)")
		}

		plans, err := discoverPlans()
		if err != nil {
			return err
		}
		dp, err := selectPlan("Select plan to delete", plans)
		if err != nil {
			return err
		}

		confirmPrompt := promptui.Prompt{
			Label:     fmt.Sprintf("Are you sure you want to delete plan %s", dp.DisplayName),
			IsConfirm: true,
			Stdout:    NoBellStdout,
		}
		if _, err := confirmPrompt.Run(); err != nil {
			if errors.Is(err, promptui.ErrAbort) {
				fmt.Println("Deletion aborted.")
				return nil
			}
			return err
		}

		if err := PlanOps.Delete(cmd.Context(), planFileName(*dp)); err != nil {
			return fmt.Errorf("delete: %w", err)
		}

		fmt.Printf("Plan %s deleted successfully.\n", dp.DisplayName)
		return nil
	},
}

func init() {
	planCmd.AddCommand(planDeleteCmd)
}
