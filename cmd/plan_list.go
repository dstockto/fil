package cmd

import (
	"fmt"
	"strings"

	"github.com/dstockto/fil/models"
	"github.com/spf13/cobra"
)

var planListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all discovered plans and their status",
	RunE: func(cmd *cobra.Command, args []string) error {
		paused, _ := cmd.Flags().GetBool("paused")
		all, _ := cmd.Flags().GetBool("all")

		plans, err := discoverPlansWithFilter(all, paused)
		if err != nil {
			return err
		}

		if len(plans) == 0 {
			fmt.Println("No plans found.")
			return nil
		}

		for _, p := range plans {
			pdfIndicator := ""
			if p.Plan.Assembly != "" {
				pdfIndicator = " [PDF]"
			}
			fmt.Printf("Plan: %s%s\n", p.DisplayName, pdfIndicator)
			for _, proj := range p.Plan.Projects {
				remaining := 0
				total := len(proj.Plates)
				var printing []string
				for _, plate := range proj.Plates {
					if plate.Status != "completed" {
						remaining++
					}
					if plate.Status == "in-progress" && plate.Printer != "" {
						printing = append(printing, plate.Printer)
					}
				}
				line := fmt.Sprintf("  Project: %s [%s] (%d/%d plates remaining", models.Sanitize(proj.Name), models.Sanitize(proj.Status), remaining, total)
				if len(printing) > 0 {
					// Deduplicate printer names in case multiple plates on same printer (shouldn't happen but safe)
					seen := make(map[string]int)
					for _, p := range printing {
						seen[p]++
					}
					var printerInfo []string
					for name, count := range seen {
						if count == 1 {
							printerInfo = append(printerInfo, fmt.Sprintf("1 on %s", name))
						} else {
							printerInfo = append(printerInfo, fmt.Sprintf("%d on %s", count, name))
						}
					}
					line += fmt.Sprintf(", %s printing", strings.Join(printerInfo, ", "))
				}
				line += ")"
				fmt.Println(line)
			}
			fmt.Println()
		}
		return nil
	},
}

func init() {
	planCmd.AddCommand(planListCmd)
	planListCmd.Flags().BoolP("paused", "p", false, "Show only paused plans")
	planListCmd.Flags().BoolP("all", "a", false, "Show all plans, including paused ones")
}
