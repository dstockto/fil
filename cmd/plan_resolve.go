package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/dstockto/fil/plan"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var planResolveCmd = &cobra.Command{
	Use:     "resolve",
	Aliases: []string{"r", "link"},
	Short:   "Interactively link filament names to IDs in a plan file",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.ApiBase == "" {
			return fmt.Errorf("api endpoint not configured")
		}
		if PlanOps == nil {
			return fmt.Errorf("plan operations not configured (need either plans_server or api_base+plans_dir)")
		}
		apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
		ctx := cmd.Context()

		plans, err := discoverPlans()
		if err != nil {
			return err
		}
		dp, err := selectPlan("Select plan file to resolve", plans)
		if err != nil {
			return err
		}

		resolutions, err := collectResolutions(ctx, apiClient, dp)
		if err != nil {
			return err
		}

		if len(resolutions) == 0 {
			fmt.Println("No changes needed.")
			return nil
		}

		req := plan.ResolveRequest{
			Plan:        planFileName(*dp),
			Resolutions: resolutions,
		}
		if err := PlanOps.Resolve(ctx, req); err != nil {
			return fmt.Errorf("resolve: %w", err)
		}
		fmt.Printf("Resolved %d need(s) in %s.\n", len(resolutions), dp.DisplayName)
		return nil
	},
}

// collectResolutions walks the plan's needs and produces a NeedResolution
// for each one that needs work — either an unresolved filament (no
// FilamentID but has name/material) or a partially-populated entry that
// needs reverse-sync from Spoolman by ID.
func collectResolutions(ctx context.Context, apiClient *api.Client, dp *DiscoveredPlan) ([]plan.NeedResolution, error) {
	var resolutions []plan.NeedResolution
	displayName := dp.DisplayName

	for _, proj := range dp.Plan.Projects {
		for _, plate := range proj.Plates {
			for needIdx, need := range plate.Needs {
				switch {
				case need.FilamentID == 0 && (need.Name != "" || need.Material != ""):
					res, err := pickFilamentForNeed(ctx, apiClient, displayName, need)
					if err != nil {
						return nil, err
					}
					if res == nil {
						continue
					}
					resolutions = append(resolutions, plan.NeedResolution{
						Project:    proj.Name,
						Plate:      plate.Name,
						NeedIndex:  needIdx,
						FilamentID: res.id,
						Name:       res.name,
						Material:   res.mat,
					})

				case need.FilamentID != 0 && (need.Name == "" || need.Material == ""):
					filament, err := apiClient.GetFilamentById(ctx, need.FilamentID)
					if err != nil || filament == nil {
						continue
					}
					resolutions = append(resolutions, plan.NeedResolution{
						Project:    proj.Name,
						Plate:      plate.Name,
						NeedIndex:  needIdx,
						FilamentID: need.FilamentID,
						Name:       filament.Filament.Name,
						Material:   filament.Filament.Material,
					})
				}
			}
		}
	}
	return resolutions, nil
}

type filamentMatch struct {
	id     int
	name   string
	mat    string
	vendor string
}

// pickFilamentForNeed runs the interactive disambiguation: queries Spoolman
// by name+material, falls back to "*" if nothing matches, then auto-selects
// or prompts. Returns nil when nothing usable is available (caller skips).
func pickFilamentForNeed(ctx context.Context, apiClient *api.Client, displayName string, need models.PlateRequirement) (*filamentMatch, error) {
	query := map[string]string{}
	if need.Material != "" {
		query["material"] = need.Material
	}

	spools, err := apiClient.FindSpoolsByName(ctx, need.Name, onlyStandardFilament, query)
	if err != nil {
		fmt.Printf("Resolving filament for: %s %s (%s)\n", models.Sanitize(need.Name), models.Sanitize(need.Material), displayName)
		fmt.Printf("  Error searching Spoolman: %v\n", err)
		return nil, nil
	}

	if len(spools) == 0 {
		fmt.Printf("Resolving filament for: %s %s (%s)\n", models.Sanitize(need.Name), models.Sanitize(need.Material), displayName)
		fmt.Printf("  No matches found for '%s' '%s'. Choosing from full list...\n", models.Sanitize(need.Name), models.Sanitize(need.Material))
		spools, err = apiClient.FindSpoolsByName(ctx, "*", onlyStandardFilament, query)
		if err != nil {
			fmt.Printf("  Error fetching all filaments: %v\n", err)
			return nil, nil
		}
		if len(spools) == 0 {
			fmt.Printf("  Still no matches found in full list with type filtering.\n")
			return nil, nil
		}
	}

	matches := map[int]filamentMatch{}
	var matchIDs []int
	for _, s := range spools {
		if _, ok := matches[s.Filament.Id]; ok {
			continue
		}
		matches[s.Filament.Id] = filamentMatch{
			id:     s.Filament.Id,
			name:   s.Filament.Name,
			mat:    s.Filament.Material,
			vendor: s.Filament.Vendor.Name,
		}
		matchIDs = append(matchIDs, s.Filament.Id)
	}

	if len(matchIDs) == 1 && need.Name != "" {
		// Unique match by name (not via the "*" fallback) — safe to auto-select.
		m := matches[matchIDs[0]]
		return &m, nil
	}

	fmt.Printf("Resolving filament for: %s %s (%s)\n", models.Sanitize(need.Name), models.Sanitize(need.Material), displayName)
	items := make([]string, 0, len(matchIDs))
	for _, id := range matchIDs {
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
			m := matches[matchIDs[index]]
			needle := strings.ToLower(strings.TrimSpace(input))
			if needle == "" {
				return true
			}
			fields := []string{
				fmt.Sprintf("%d", m.id), m.name, m.mat, m.vendor,
			}
			return strings.Contains(strings.ToLower(strings.Join(fields, " ")), needle)
		},
	}
	idx, _, err := prompt.Run()
	if err != nil {
		return nil, err
	}
	m := matches[matchIDs[idx]]
	return &m, nil
}

func init() {
	planCmd.AddCommand(planResolveCmd)
}
