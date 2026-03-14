package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var planResolveCmd = &cobra.Command{
	Use:     "resolve [file]",
	Aliases: []string{"r", "link"},
	Short:   "Interactively link filament names to IDs in a plan file",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.ApiBase == "" {
			return fmt.Errorf("api endpoint not configured")
		}
		apiClient := api.NewClient(Cfg.ApiBase)
		ctx := cmd.Context()

		var dp *DiscoveredPlan
		if len(args) > 0 {
			dp = &DiscoveredPlan{Path: args[0]}
			data, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			var plan models.PlanFile
			if err := yaml.Unmarshal(data, &plan); err != nil {
				return err
			}
			plan.DefaultStatus()
			dp.Plan = plan
			dp.DisplayName = FormatPlanPath(args[0])
		} else {
			plans, err := discoverPlans()
			if err != nil {
				return err
			}
			dp, err = selectPlan("Select plan file to resolve", plans)
			if err != nil {
				return err
			}
		}

		plan := dp.Plan
		path := dp.DisplayName

		modified := false
		for i := range plan.Projects {
			for j := range plan.Projects[i].Plates {
				for k := range plan.Projects[i].Plates[j].Needs {
					need := &plan.Projects[i].Plates[j].Needs[k]
					if need.FilamentID == 0 && (need.Name != "" || need.Material != "") {
						// Search Spoolman
						query := make(map[string]string)
						if need.Material != "" {
							query["material"] = need.Material
						}
						spools, err := apiClient.FindSpoolsByName(ctx, need.Name, nil, query)
						if err != nil {
							fmt.Printf("Resolving filament for: %s %s (%s)\n", models.Sanitize(need.Name), models.Sanitize(need.Material), path)
							fmt.Printf("  Error searching Spoolman: %v\n", err)
							continue
						}

						if len(spools) == 0 {
							fmt.Printf("Resolving filament for: %s %s (%s)\n", models.Sanitize(need.Name), models.Sanitize(need.Material), path)
							fmt.Printf("  No matches found for '%s' '%s'. Choosing from full list...\n", models.Sanitize(need.Name), models.Sanitize(need.Material))
							spools, err = apiClient.FindSpoolsByName(ctx, "*", nil, query)
							if err != nil {
								fmt.Printf("  Error fetching all filaments: %v\n", err)
								continue
							}
							if len(spools) == 0 {
								fmt.Printf("  Still no matches found in full list with type filtering.\n")
								continue
							}
						}

						// Group by filament ID to avoid picking individual spools
						type filMatch struct {
							id     int
							name   string
							mat    string
							vendor string
						}
						matches := make(map[int]filMatch)
						var matchIds []int
						for _, s := range spools {
							if _, ok := matches[s.Filament.Id]; !ok {
								matches[s.Filament.Id] = filMatch{
									id:     s.Filament.Id,
									name:   s.Filament.Name,
									mat:    s.Filament.Material,
									vendor: s.Filament.Vendor.Name,
								}
								matchIds = append(matchIds, s.Filament.Id)
							}
						}

						var selectedId int
						if len(matchIds) == 1 && need.Name != "" {
							// If we found exactly one match by name, use it.
							// But if we are in the "full list" fallback, we should probably still ask if need.Name was empty.
							// Actually, if it was found by FindSpoolsByName(need.Name), and it's unique, it's safe.
							selectedId = matchIds[0]
						} else {
							fmt.Printf("Resolving filament for: %s %s (%s)\n", models.Sanitize(need.Name), models.Sanitize(need.Material), path)
							var items []string
							for _, id := range matchIds {
								m := matches[id]
								items = append(items, fmt.Sprintf("%s - %s (%s) [#%d]", models.Sanitize(m.vendor), models.Sanitize(m.name), models.Sanitize(m.mat), id))
							}
							prompt := promptui.Select{
								Label:             "Select matching filament",
								Items:             items,
								Stdout:            NoBellStdout,
								Size:              10,
								StartInSearchMode: true,
								Searcher: func(input string, index int) bool {
									m := matches[matchIds[index]]
									needle := strings.ToLower(strings.TrimSpace(input))
									if needle == "" {
										return true
									}
									fields := []string{
										fmt.Sprintf("%d", m.id),
										m.name,
										m.mat,
										m.vendor,
									}
									joined := strings.ToLower(strings.Join(fields, " "))
									return strings.Contains(joined, needle)
								},
							}
							idx, _, err := prompt.Run()
							if err != nil {
								return err
							}
							selectedId = matchIds[idx]
						}

						need.FilamentID = selectedId
						need.Name = matches[selectedId].name
						need.Material = matches[selectedId].mat
						modified = true
					} else if need.FilamentID != 0 && (need.Name == "" || need.Material == "") {
						// Reverse sync
						// We need a way to get filament info by ID.
						// Spoolman API has /api/v1/filament/{id}
						filament, err := apiClient.GetFilamentById(ctx, need.FilamentID)
						if err == nil && filament != nil {
							need.Name = filament.Filament.Name
							need.Material = filament.Filament.Material
							modified = true
						}
					}
				}
			}
		}

		if modified {
			return savePlan(*dp, plan)
		}

		fmt.Println("No changes needed.")
		return nil
	},
}

func init() {
	planCmd.AddCommand(planResolveCmd)
}
