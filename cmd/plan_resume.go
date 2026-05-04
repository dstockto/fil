package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var planResumeCmd = &cobra.Command{
	Use:     "resume",
	Aliases: []string{"res"},
	Short:   "Move a plan file from the pause directory back to the active plans directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil {
			return fmt.Errorf("config not loaded")
		}
		if PlanOps == nil {
			return fmt.Errorf("plan operations not configured (need either plans_server or api_base+plans_dir)")
		}

		plans, err := discoverPlansWithFilter(false, true)
		if err != nil {
			return err
		}
		dp, err := selectPlan("Select plan file to resume", plans)
		if err != nil {
			return err
		}

		if err := PlanOps.Resume(cmd.Context(), planFileName(*dp)); err != nil {
			return fmt.Errorf("resume: %w", err)
		}
		fmt.Printf("Resumed %s\n", dp.DisplayName)
		return nil
	},
}

func init() {
	planCmd.AddCommand(planResumeCmd)
}
