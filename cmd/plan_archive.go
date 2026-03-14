package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dstockto/fil/api"
	"github.com/spf13/cobra"
)

var planArchiveCmd = &cobra.Command{
	Use:     "archive [file]",
	Aliases: []string{"a"},
	Short:   "Move completed plan files to the archive directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || (Cfg.ArchiveDir == "" && Cfg.PlansServer == "") {
			return fmt.Errorf("archive_dir (or plans_server) not configured in config.json")
		}

		// Ensure archive dir exists (if local)
		if Cfg.ArchiveDir != "" {
			if _, err := os.Stat(Cfg.ArchiveDir); os.IsNotExist(err) {
				_ = os.MkdirAll(Cfg.ArchiveDir, 0755)
			}
		}

		var discovered []DiscoveredPlan
		if len(args) > 0 {
			discovered = append(discovered, DiscoveredPlan{Path: args[0], DisplayName: FormatPlanPath(args[0])})
		} else {
			plans, _ := discoverPlans()
			discovered = plans
		}

		for _, dp := range discovered {
			plan := dp.Plan
			// If from args and not yet loaded, the Plan will be zero-value.
			// We need to load it for args case.
			if len(args) > 0 && !dp.Remote {
				data, err := os.ReadFile(dp.Path)
				if err != nil {
					continue
				}
				if err := loadPlanYAML(data, &plan); err != nil {
					continue
				}
			}

			allDone := true
			for _, proj := range plan.Projects {
				if proj.Status != "completed" {
					allDone = false
					break
				}
			}

			if allDone {
				if dp.Remote {
					client := api.NewPlanServerClient(Cfg.PlansServer)
					if err := client.ArchivePlan(context.Background(), dp.RemoteName); err != nil {
						fmt.Printf("  Error archiving remote plan %s: %v\n", dp.DisplayName, err)
					} else {
						fmt.Printf("Archived %s\n", dp.DisplayName)
					}
				} else {
					if Cfg.ArchiveDir == "" {
						fmt.Printf("Skipping %s (archive_dir not configured for local archiving)\n", dp.DisplayName)
						continue
					}
					ext := filepath.Ext(dp.Path)
					base := strings.TrimSuffix(filepath.Base(dp.Path), ext)
					timestamp := time.Now().Format("20060102150405")
					newFilename := fmt.Sprintf("%s-%s%s", base, timestamp, ext)

					dest := filepath.Join(Cfg.ArchiveDir, newFilename)
					fmt.Printf("Archiving %s to %s\n", dp.DisplayName, FormatPlanPath(dest))
					err := os.Rename(dp.Path, dest)
					if err != nil {
						fmt.Printf("  Error moving file: %v\n", err)
					}
				}
			} else {
				fmt.Printf("Skipping %s (not all projects are completed)\n", dp.DisplayName)
			}
		}

		return nil
	},
}

func init() {
	planCmd.AddCommand(planArchiveCmd)
}
