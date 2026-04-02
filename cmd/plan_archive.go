package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dstockto/fil/api"
	"github.com/manifoldco/promptui"
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

		pick, _ := cmd.Flags().GetBool("pick")

		var discovered []DiscoveredPlan
		if len(args) > 0 {
			discovered = append(discovered, DiscoveredPlan{Path: args[0], DisplayName: FormatPlanPath(args[0])})
		} else {
			plans, _ := discoverPlans()
			discovered = plans
		}

		// Build list of completed plans
		var completed []DiscoveredPlan
		for _, dp := range discovered {
			plan := dp.Plan
			if len(args) > 0 && !dp.Remote {
				data, err := os.ReadFile(dp.Path)
				if err != nil {
					continue
				}
				if err := loadPlanYAML(data, &plan); err != nil {
					continue
				}
				dp.Plan = plan
			}

			allDone := true
			for _, proj := range plan.Projects {
				if proj.Status != "completed" {
					allDone = false
					break
				}
			}

			if allDone {
				completed = append(completed, dp)
			} else {
				fmt.Printf("Skipping %s (not all projects are completed)\n", dp.DisplayName)
			}
		}

		if len(completed) == 0 {
			fmt.Println("No completed plans to archive.")
			return nil
		}

		// If --pick, let the user select which plan to archive
		if pick && len(args) == 0 {
			selected, err := selectPlanToArchive(completed)
			if err != nil {
				return err
			}
			if selected == nil {
				fmt.Println("No plan selected.")
				return nil
			}
			completed = []DiscoveredPlan{*selected}
		}

		for _, dp := range completed {
			archivePlan(dp)
		}

		return nil
	},
}

func archivePlan(dp DiscoveredPlan) {
	if dp.Remote {
		client := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
		if err := client.ArchivePlan(context.Background(), dp.RemoteName); err != nil {
			fmt.Printf("  Error archiving remote plan %s: %v\n", dp.DisplayName, err)
		} else {
			fmt.Printf("Archived %s\n", dp.DisplayName)
		}
	} else {
		if Cfg.ArchiveDir == "" {
			fmt.Printf("Skipping %s (archive_dir not configured for local archiving)\n", dp.DisplayName)
			return
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
}

func selectPlanToArchive(plans []DiscoveredPlan) (*DiscoveredPlan, error) {
	if len(plans) == 1 {
		return &plans[0], nil
	}

	items := make([]string, len(plans))
	for i, dp := range plans {
		items[i] = dp.DisplayName
	}

	prompt := promptui.Select{
		Label: "Select a plan to archive",
		Items: items,
		Size:  10,
	}

	idx, _, err := prompt.Run()
	if err != nil {
		if err == promptui.ErrInterrupt || err == promptui.ErrEOF {
			return nil, nil
		}
		return nil, err
	}

	return &plans[idx], nil
}

func init() {
	planArchiveCmd.Flags().BoolP("pick", "p", false, "interactively select which completed plan to archive")
	planCmd.AddCommand(planArchiveCmd)
}
