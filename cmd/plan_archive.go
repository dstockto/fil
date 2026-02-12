package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dstockto/fil/models"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var planArchiveCmd = &cobra.Command{
	Use:     "archive [file]",
	Aliases: []string{"a"},
	Short:   "Move completed plan files to the archive directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.ArchiveDir == "" {
			return fmt.Errorf("archive_dir not configured in config.json")
		}

		// Ensure archive dir exists
		if _, err := os.Stat(Cfg.ArchiveDir); os.IsNotExist(err) {
			_ = os.MkdirAll(Cfg.ArchiveDir, 0755)
		}

		var paths []string
		if len(args) > 0 {
			paths = append(paths, args[0])
		} else {
			plans, _ := discoverPlans()
			for _, p := range plans {
				paths = append(paths, p.Path)
			}
		}

		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var plan models.PlanFile
			_ = yaml.Unmarshal(data, &plan)
			plan.DefaultStatus()

			allDone := true
			for _, proj := range plan.Projects {
				if proj.Status != "completed" {
					allDone = false
					break
				}
			}

			if allDone {
				ext := filepath.Ext(path)
				base := strings.TrimSuffix(filepath.Base(path), ext)
				timestamp := time.Now().Format("20060102150405")
				newFilename := fmt.Sprintf("%s-%s%s", base, timestamp, ext)

				dest := filepath.Join(Cfg.ArchiveDir, newFilename)
				fmt.Printf("Archiving %s to %s\n", FormatPlanPath(path), FormatPlanPath(dest))
				err := os.Rename(path, dest)
				if err != nil {
					fmt.Printf("  Error moving file: %v\n", err)
				}
			} else {
				fmt.Printf("Skipping %s (not all projects are completed)\n", FormatPlanPath(path))
			}
		}

		return nil
	},
}

func init() {
	planCmd.AddCommand(planArchiveCmd)
}
