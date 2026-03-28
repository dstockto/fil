package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"gopkg.in/yaml.v3"

	"github.com/spf13/cobra"
)

var planUnarchiveCmd = &cobra.Command{
	Use:     "unarchive [file]",
	Aliases: []string{"ua"},
	Short:   "Move a plan file from the archive directory back to the active plans directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || (Cfg.PlansServer == "" && (Cfg.ArchiveDir == "" || Cfg.PlansDir == "")) {
			return fmt.Errorf("archive_dir and plans_dir (or plans_server) must be configured in config.json")
		}

		var dp *DiscoveredPlan
		if len(args) > 0 {
			dp = &DiscoveredPlan{Path: args[0], DisplayName: FormatPlanPath(args[0])}
		} else {
			plans, err := discoverArchivedPlans()
			if err != nil {
				return err
			}
			dp, err = selectPlan("Select plan file to unarchive", plans)
			if err != nil {
				return err
			}
		}

		if dp.Remote {
			client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
			if err := client.UnarchivePlan(context.Background(), dp.RemoteName); err != nil {
				return fmt.Errorf("failed to unarchive remote plan: %w", err)
			}
			fmt.Printf("Unarchived %s\n", dp.DisplayName)
			return nil
		}

		// Strip the archive timestamp suffix to restore the original filename
		base := filepath.Base(dp.Path)
		restored := stripArchiveTimestamp(base)
		dest := filepath.Join(Cfg.PlansDir, restored)

		err := os.Rename(dp.Path, dest)
		if err != nil {
			return fmt.Errorf("failed to move file: %w", err)
		}

		fmt.Printf("Moved %s to %s\n", dp.DisplayName, FormatPlanPath(dest))
		return nil
	},
}

// timestampSuffix matches the -YYYYMMDDHHMMSS suffix added by plan archive.
var timestampSuffix = regexp.MustCompile(`-\d{14}$`)

// stripArchiveTimestamp removes the archive timestamp from a filename,
// e.g. "myplan-20260328150405.yaml" -> "myplan.yaml"
func stripArchiveTimestamp(filename string) string {
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filename, ext)
	name = timestampSuffix.ReplaceAllString(name, "")
	return name + ext
}

// discoverArchivedPlans finds plans in the archive directory and on the remote server.
func discoverArchivedPlans() ([]DiscoveredPlan, error) {
	var plans []DiscoveredPlan

	// Local archive directory
	if Cfg != nil && Cfg.ArchiveDir != "" {
		evalDir, err := filepath.EvalSymlinks(Cfg.ArchiveDir)
		if err == nil {
			// use evaluated path
		} else {
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
