package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/dstockto/fil/api"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var planDeleteCmd = &cobra.Command{
	Use:     "delete",
	Aliases: []string{"del"},
	Short:   "Delete an active plan",
	RunE: func(cmd *cobra.Command, args []string) error {
		var dp *DiscoveredPlan
		if len(args) > 0 {
			dp = &DiscoveredPlan{Path: args[0], DisplayName: FormatPlanPath(args[0])}
		} else {
			plans, err := discoverPlans()
			if err != nil {
				return err
			}
			dp, err = selectPlan("Select plan to delete", plans)
			if err != nil {
				return err
			}
		}

		confirmPrompt := promptui.Prompt{
			Label:     fmt.Sprintf("Are you sure you want to delete plan %s", dp.DisplayName),
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

		if dp.Remote {
			client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
			if err := client.DeletePlan(context.Background(), dp.RemoteName); err != nil {
				return fmt.Errorf("failed to delete remote plan: %w", err)
			}
		} else {
			if err := os.Remove(dp.Path); err != nil {
				return fmt.Errorf("failed to delete plan: %w", err)
			}
		}

		fmt.Printf("Plan %s deleted successfully.\n", dp.DisplayName)
		return nil
	},
}

func init() {
	planCmd.AddCommand(planDeleteCmd)
}
