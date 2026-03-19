package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dstockto/fil/api"
	"github.com/spf13/cobra"
)

var planResumeCmd = &cobra.Command{
	Use:     "resume [file]",
	Aliases: []string{"res"},
	Short:   "Move a plan file from the pause directory back to the active plans directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || (Cfg.PlansServer == "" && (Cfg.PauseDir == "" || Cfg.PlansDir == "")) {
			return fmt.Errorf("pause_dir and plans_dir (or plans_server) must be configured in config.json")
		}

		var dp *DiscoveredPlan
		if len(args) > 0 {
			dp = &DiscoveredPlan{Path: args[0], DisplayName: FormatPlanPath(args[0])}
		} else {
			plans, err := discoverPlansWithFilter(false, true)
			if err != nil {
				return err
			}
			dp, err = selectPlan("Select plan file to resume", plans)
			if err != nil {
				return err
			}
		}

		if dp.Remote {
			client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
			if err := client.ResumePlan(context.Background(), dp.RemoteName); err != nil {
				return fmt.Errorf("failed to resume remote plan: %w", err)
			}
			fmt.Printf("Resumed %s\n", dp.DisplayName)
			return nil
		}

		dest := filepath.Join(Cfg.PlansDir, filepath.Base(dp.Path))
		err := os.Rename(dp.Path, dest)
		if err != nil {
			return fmt.Errorf("failed to move file: %w", err)
		}

		fmt.Printf("Moved %s to %s\n", dp.DisplayName, FormatPlanPath(dest))
		return nil
	},
}

func init() {
	planCmd.AddCommand(planResumeCmd)
}
