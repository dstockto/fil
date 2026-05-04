package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var planUnarchiveCmd = &cobra.Command{
	Use:     "unarchive",
	Aliases: []string{"ua"},
	Short:   "Move a plan file from the archive directory back to the active plans directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil {
			return fmt.Errorf("config not loaded")
		}
		if PlanOps == nil {
			return fmt.Errorf("plan operations not configured (need either plans_server or api_base+plans_dir)")
		}

		plans, err := discoverArchivedPlans()
		if err != nil {
			return err
		}
		dp, err := selectPlan("Select plan file to unarchive", plans)
		if err != nil {
			return err
		}

		if err := PlanOps.Unarchive(cmd.Context(), planFileName(*dp)); err != nil {
			return fmt.Errorf("unarchive: %w", err)
		}
		fmt.Printf("Unarchived %s\n", dp.DisplayName)
		return nil
	},
}

// discoverArchivedPlans finds plans in the archive directory and on the remote server.
func discoverArchivedPlans() ([]DiscoveredPlan, error) {
	var plans []DiscoveredPlan

	// Local archive directory
	if Cfg != nil && Cfg.ArchiveDir != "" {
		evalDir, err := filepath.EvalSymlinks(Cfg.ArchiveDir)
		if err != nil {
			evalDir = Cfg.ArchiveDir
		}

		entries, err := os.ReadDir(evalDir)
		if err == nil {
			for _, d := range entries {
				if d.IsDir() {
					continue
				}
				ext := strings.ToLower(filepath.Ext(d.Name()))
				if ext != ".yaml" && ext != ".yml" {
					continue
				}

				path := filepath.Join(evalDir, d.Name())
				absPath, err := filepath.Abs(path)
				if err != nil {
					absPath = path
				}

				data, err := os.ReadFile(path)
				if err != nil {
					continue
				}
				var plan models.PlanFile
				if err := yaml.Unmarshal(data, &plan); err != nil {
					continue
				}
				plan.DefaultStatus()
				if len(plan.Projects) > 0 {
					plans = append(plans, DiscoveredPlan{
						Path:        absPath,
						DisplayName: FormatPlanPath(absPath),
						Plan:        plan,
					})
				}
			}
		}
	}

	// Remote archived plans
	if Cfg != nil && Cfg.PlansServer != "" {
		client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
		ctx := context.Background()

		summaries, err := client.ListPlans(ctx, "archived")
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Warning: could not reach plan server: %v\n", err)
		} else {
			for _, summary := range summaries {
				data, err := client.GetPlan(ctx, summary.Name, "archived")
				if err != nil {
					continue
				}
				var plan models.PlanFile
				if err := yaml.Unmarshal(data, &plan); err != nil {
					continue
				}
				plan.DefaultStatus()
				if len(plan.Projects) > 0 {
					plans = append(plans, DiscoveredPlan{
						RemoteName:  summary.Name,
						DisplayName: "<server>/" + summary.Name,
						Plan:        plan,
						Remote:      true,
					})
				}
			}
		}
	}

	return plans, nil
}

func init() {
	planCmd.AddCommand(planUnarchiveCmd)
}
