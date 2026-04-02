package cmd

import (
	"fmt"
	"sort"
	"time"

	"github.com/dstockto/fil/models"
	"github.com/spf13/cobra"
)

var planStatusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"s"},
	Short:   "Show what is printing on each printer",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || len(Cfg.Printers) == 0 {
			fmt.Println("No printers configured.")
			return nil
		}

		plans, err := discoverPlans()
		if err != nil {
			return err
		}

		// Build map of printer → (project name, plate name, time info)
		type printingInfo struct {
			Project           string
			Plate             string
			StartedAt         string
			EstimatedDuration string
		}
		printerMap := make(map[string]printingInfo)

		for _, p := range plans {
			for _, proj := range p.Plan.Projects {
				for _, plate := range proj.Plates {
					if plate.Status == "in-progress" && plate.Printer != "" {
						printerMap[plate.Printer] = printingInfo{
							Project:           proj.Name,
							Plate:             plate.Name,
							StartedAt:         plate.StartedAt,
							EstimatedDuration: plate.EstimatedDuration,
						}
					}
				}
			}
		}

		// Split into active and idle, each sorted alphabetically
		var active, idle []string
		for name := range Cfg.Printers {
			if _, ok := printerMap[name]; ok {
				active = append(active, name)
			} else {
				idle = append(idle, name)
			}
		}
		sort.Strings(active)
		sort.Strings(idle)

		for _, name := range active {
			info := printerMap[name]
			line := fmt.Sprintf("%s: %s / %s", name, models.Sanitize(info.Project), models.Sanitize(info.Plate))
			line += formatTimeInfo(info.StartedAt, info.EstimatedDuration)
			fmt.Println(line)
		}
		for _, name := range idle {
			fmt.Printf("%s: (idle)\n", name)
		}

		return nil
	},
}

func formatTimeInfo(startedAt, estimatedDuration string) string {
	if startedAt == "" {
		return ""
	}

	started, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		return ""
	}

	startedStr := started.Format("3:04pm")

	if estimatedDuration == "" {
		return fmt.Sprintf(" (started %s)", startedStr)
	}

	dur, err := time.ParseDuration(estimatedDuration)
	if err != nil {
		return fmt.Sprintf(" (started %s)", startedStr)
	}

	eta := started.Add(dur)
	return fmt.Sprintf(" (started %s, done ~%s)", startedStr, eta.Format("3:04pm"))
}

func init() {
	planCmd.AddCommand(planStatusCmd)
}
