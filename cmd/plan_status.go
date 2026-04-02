package cmd

import (
	"fmt"
	"sort"

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

		// Build map of printer → (project name, plate name)
		type printingInfo struct {
			Project string
			Plate   string
		}
		printerMap := make(map[string]printingInfo)

		for _, p := range plans {
			for _, proj := range p.Plan.Projects {
				for _, plate := range proj.Plates {
					if plate.Status == "in-progress" && plate.Printer != "" {
						printerMap[plate.Printer] = printingInfo{
							Project: proj.Name,
							Plate:   plate.Name,
						}
					}
				}
			}
		}

		// Sort printer names for consistent output
		printerNames := make([]string, 0, len(Cfg.Printers))
		for name := range Cfg.Printers {
			printerNames = append(printerNames, name)
		}
		sort.Strings(printerNames)

		for _, name := range printerNames {
			if info, ok := printerMap[name]; ok {
				fmt.Printf("%s: %s / %s\n", name, models.Sanitize(info.Project), models.Sanitize(info.Plate))
			} else {
				fmt.Printf("%s: (idle)\n", name)
			}
		}

		return nil
	},
}

func init() {
	planCmd.AddCommand(planStatusCmd)
}
