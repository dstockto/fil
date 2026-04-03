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
		remaining, _ := cmd.Flags().GetBool("remaining")

		plans, err := discoverPlansWithFilter(all, paused)
		if err != nil {
			return err
		}

		if len(plans) == 0 {
			fmt.Println("No plans found.")
			return nil
		}

		for _, p := range plans {
			if remaining {
				// Check if this plan has any todo plates
				hasTodo := false
				for _, proj := range p.Plan.Projects {
					if proj.Status == "completed" {
						continue
					}
					for _, plate := range proj.Plates {
						if plate.Status == "todo" {
							hasTodo = true
							break
						}
					}
					if hasTodo {
						break
					}
				}
				if !hasTodo {
					continue
				}
			}

			pdfIndicator := ""
			if p.Plan.Assembly != "" {
				pdfIndicator = " [PDF]"
			}
			fmt.Printf("Plan: %s%s\n", p.DisplayName, pdfIndicator)
			for _, proj := range p.Plan.Projects {
				if proj.Status == "completed" {
					continue
				}
				remainingCount := 0
				total := len(proj.Plates)
				var todoPlates []string
				var printing []string
				for _, plate := range proj.Plates {
					if plate.Status != "completed" {
						remainingCount++
					}
					if plate.Status == "todo" {
						todoPlates = append(todoPlates, plate.Name)
					}
					if plate.Status == "in-progress" && plate.Printer != "" {
						printing = append(printing, plate.Printer)
					}
				}

				if remaining && len(todoPlates) == 0 {
					continue
				}

				line := fmt.Sprintf("  Project: %s [%s] (%d/%d plates remaining", models.Sanitize(proj.Name), models.Sanitize(proj.Status), remainingCount, total)
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

				if remaining {
					for _, name := range todoPlates {
						fmt.Printf("    - %s\n", models.Sanitize(name))
					}
				}
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
	planListCmd.Flags().BoolP("remaining", "r", false, "Show remaining (todo) plates for each plan")
}
