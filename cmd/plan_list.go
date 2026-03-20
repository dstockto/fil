package cmd

import (
	"fmt"

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
				todo := 0
				total := len(proj.Plates)
				for _, plate := range proj.Plates {
					if plate.Status != "completed" {
						todo++
					}
				}
				fmt.Printf("  Project: %s [%s] (%d/%d plates remaining)\n", models.Sanitize(proj.Name), models.Sanitize(proj.Status), todo, total)
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
