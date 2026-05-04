package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var planPauseCmd = &cobra.Command{
	Use:     "pause",
	Aliases: []string{"p"},
	Short:   "Move a plan file to the pause directory",
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
		dp, err := selectPlan("Select plan file to pause", plans)
		if err != nil {
			return err
		}

		if err := PlanOps.Pause(cmd.Context(), planFileName(*dp)); err != nil {
			return fmt.Errorf("pause: %w", err)
		}
		fmt.Printf("Paused %s\n", dp.DisplayName)
		return nil
	},
}

func init() {
	planCmd.AddCommand(planPauseCmd)
}
