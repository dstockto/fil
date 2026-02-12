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

		var path string
		if len(args) > 0 {
			path = args[0]
		} else {
			plans, err := discoverPlans()
			if err != nil {
				return err
			}
			if len(plans) == 0 {
				return fmt.Errorf("no plans found")
			}
			if len(plans) == 1 {
				path = plans[0].Path
			} else {
				var items []string
				for _, p := range plans {
					items = append(items, p.DisplayName)
				}
				prompt := promptui.Select{
					Label:             "Select plan file to resolve",
					Items:             items,
					Stdout:            NoBellStdout,
					StartInSearchMode: true,
					Searcher: func(input string, index int) bool {
						return strings.Contains(strings.ToLower(items[index]), strings.ToLower(input))
					},
				}
				selectedIdx, _, err := prompt.Run()
				if err != nil {
					return err
				}
				path = plans[selectedIdx].Path
			}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var plan models.PlanFile
		if err := yaml.Unmarshal(data, &plan); err != nil {
			return err
		}
		plan.DefaultStatus()

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
						spools, err := apiClient.FindSpoolsByName(need.Name, nil, query)
						if err != nil {
							fmt.Printf("Resolving filament for: %s %s (%s)\n", need.Name, need.Material, path)
							fmt.Printf("  Error searching Spoolman: %v\n", err)
							continue
						}

						if len(spools) == 0 {
							fmt.Printf("Resolving filament for: %s %s (%s)\n", need.Name, need.Material, path)
							fmt.Printf("  No matches found for '%s' '%s'. Choosing from full list...\n", need.Name, need.Material)
							spools, err = apiClient.FindSpoolsByName("*", nil, query)
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
							fmt.Printf("Resolving filament for: %s %s (%s)\n", need.Name, need.Material, path)
							var items []string
							for _, id := range matchIds {
								m := matches[id]
								items = append(items, fmt.Sprintf("%s - %s (%s) [#%d]", m.vendor, m.name, m.mat, id))
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
						filament, err := apiClient.GetFilamentById(need.FilamentID)
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
			out, err := yaml.Marshal(plan)
			if err != nil {
				return err
			}
			return os.WriteFile(path, out, 0644)
		}

		fmt.Println("No changes needed.")
		return nil
	},
}

func init() {
	planCmd.AddCommand(planResolveCmd)
}
