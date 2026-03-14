package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dstockto/fil/api"
	"github.com/spf13/cobra"
)

var planPauseCmd = &cobra.Command{
	Use:     "pause [file]",
	Aliases: []string{"p"},
	Short:   "Move a plan file to the pause directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || (Cfg.PauseDir == "" && Cfg.PlansServer == "") {
			return fmt.Errorf("pause_dir (or plans_server) not configured in config.json")
		}

		var dp *DiscoveredPlan
		if len(args) > 0 {
			dp = &DiscoveredPlan{Path: args[0], DisplayName: FormatPlanPath(args[0])}
		} else {
			plans, _ := discoverPlans()
			var err error
			dp, err = selectPlan("Select plan file to pause", plans)
			if err != nil {
				return err
			}
		}

		if dp.Remote {
			client := api.NewPlanServerClient(Cfg.PlansServer)
			if err := client.PausePlan(context.Background(), dp.RemoteName); err != nil {
				return fmt.Errorf("failed to pause remote plan: %w", err)
			}
			fmt.Printf("Paused %s\n", dp.DisplayName)
			return nil
		}

		if Cfg.PauseDir == "" {
			return fmt.Errorf("pause_dir not configured in config.json")
		}

		// Ensure pause dir exists
		if _, err := os.Stat(Cfg.PauseDir); os.IsNotExist(err) {
			_ = os.MkdirAll(Cfg.PauseDir, 0755)
		}

		dest := filepath.Join(Cfg.PauseDir, filepath.Base(dp.Path))
		err := os.Rename(dp.Path, dest)
		if err != nil {
			return fmt.Errorf("failed to move file: %w", err)
		}

		fmt.Printf("Moved %s to %s\n", dp.DisplayName, FormatPlanPath(dest))
		return nil
	},
}

func init() {
	planCmd.AddCommand(planPauseCmd)
}
