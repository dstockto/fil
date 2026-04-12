package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var planNextCmd = &cobra.Command{
	Use:     "next [file]",
	Aliases: []string{"n"},
	Short:   "Suggest the next plate to print and manage swaps",
	RunE: func(cmd *cobra.Command, args []string) error {
		if Cfg == nil || Cfg.ApiBase == "" {
			return fmt.Errorf("api endpoint not configured")
		}
		apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
		ctx := cmd.Context()

		// 1. Select Printer
		if len(Cfg.Printers) == 0 {
			return fmt.Errorf("no printers configured in config.json")
		}
		var printerNames []string
		for name := range Cfg.Printers {
			printerNames = append(printerNames, name)
		}
		promptPrinter := promptui.Select{
			Label:             "Which printer are you using?",
			Items:             printerNames,
			Stdout:            NoBellStdout,
			StartInSearchMode: true,
			Searcher: func(input string, index int) bool {
				return strings.Contains(strings.ToLower(printerNames[index]), strings.ToLower(input))
			},
		}
		_, printerName, err := promptPrinter.Run()
		if err != nil {
			return err
		}
		printerLocations := Cfg.Printers[printerName].Locations

		// 2. Discover and Load Plans
		var discovered []DiscoveredPlan
		if len(args) > 0 {
			data, err := os.ReadFile(args[0])
			if err == nil {
				var p models.PlanFile
				_ = yaml.Unmarshal(data, &p)
				discovered = append(discovered, DiscoveredPlan{Path: args[0], Plan: p})
			}
		} else {
			discovered, _ = discoverPlans()
		}

		// 2b. Check if this printer already has a plate in-progress
		for di, dp := range discovered {
			for pi, proj := range dp.Plan.Projects {
				if proj.Status == "completed" {
					continue
				}
				for pli, plate := range proj.Plates {
					if plate.Status == "in-progress" && plate.Printer == printerName {
						fmt.Printf("\n%s - %s is currently printing on %s\n", models.Sanitize(proj.Name), models.Sanitize(plate.Name), models.Sanitize(printerName))

						actionPrompt := promptui.Select{
							Label:  "What would you like to do?",
							Items:  []string{"Mark as completed", "Cancel (back to todo)", "Keep printing (start another plate)", "Quit"},
							Stdout: NoBellStdout,
						}
						actionIdx, _, err := actionPrompt.Run()
						if err != nil {
							return err
						}

						switch actionIdx {
						case 0: // Complete
							discovered[di].Plan.Projects[pi].Plates[pli].Status = "completed"
							discovered[di].Plan.Projects[pi].Plates[pli].Printer = ""
							// Auto-complete project if all plates done
							allDone := true
							for _, pl := range discovered[di].Plan.Projects[pi].Plates {
								if pl.Status != "completed" {
									allDone = false
									break
								}
							}
							if allDone {
								discovered[di].Plan.Projects[pi].Status = "completed"
								fmt.Printf("All plates complete — project %s marked completed\n", models.Sanitize(proj.Name))
							}
							if err := savePlan(discovered[di], discovered[di].Plan); err != nil {
								return fmt.Errorf("failed to save plan: %w", err)
							}
							fmt.Printf("Marked %s as completed\n\n", models.Sanitize(plate.Name))
						case 1: // Cancel
							discovered[di].Plan.Projects[pi].Plates[pli].Status = "todo"
							discovered[di].Plan.Projects[pi].Plates[pli].Printer = ""
							discovered[di].Plan.Projects[pi].Plates[pli].StartedAt = ""
							if err := savePlan(discovered[di], discovered[di].Plan); err != nil {
								return fmt.Errorf("failed to save plan: %w", err)
							}
							fmt.Printf("Cancelled %s — set back to todo\n\n", models.Sanitize(plate.Name))
						case 2: // Keep printing
							fmt.Printf("Keeping %s in-progress\n\n", models.Sanitize(plate.Name))
						case 3: // Quit
							return nil
						}
					}
				}
			}
		}

		// 3. Collect all TODO plates
		type plateOption struct {
			discoveredIdx int
			projectIdx    int
			plateIdx      int
			plate         models.Plate
			projectName   string
			swapCost      int
			isReady       bool
		}
		var options []plateOption

		// Also track in-progress plates on other printers for display
		type inProgressElsewhere struct {
			projectName string
			plateName   string
			printer     string
		}
		var busyElsewhere []inProgressElsewhere

		// Get current inventory & loaded spools
		allSpools, _ := apiClient.FindSpoolsByName(ctx, "*", onlyStandardFilament, nil)
		loadedSpools := make(map[string]models.FindSpool)
		for _, s := range allSpools {
			if s.Location != "" {
				// Use unique key since multiple spools can be in same Location
				key := s.Location + "_" + fmt.Sprint(s.Id)
				loadedSpools[key] = s
			}
		}

		for di, dp := range discovered {
			for i, proj := range dp.Plan.Projects {
				if proj.Status == "completed" {
					continue
				}
				for j, plate := range proj.Plates {
					if plate.Status == "completed" {
						continue
					}
					if plate.Status == "in-progress" {
						// Already printing on another printer (same-printer was handled above)
						if plate.Printer != printerName {
							busyElsewhere = append(busyElsewhere, inProgressElsewhere{
								projectName: proj.Name,
								plateName:   plate.Name,
								printer:     plate.Printer,
							})
						}
						continue
					}

					// Calculate swap cost and readiness
					cost := 0
					ready := true
					for _, req := range plate.Needs {
						// Check if already loaded in any of this printer's locations
						foundInPrinter := false
						for _, loc := range printerLocations {
							// Check if the filament is loaded in this Location
							for _, s := range loadedSpools {
								if s.Location == loc && s.Filament.Id == req.FilamentID {
									foundInPrinter = true
									break
								}
							}
							if foundInPrinter {
								break
							}
						}
						if !foundInPrinter {
							cost++
						}

						// Check total inventory readiness
						totalAvailable := 0.0
						for _, s := range allSpools {
							if !s.Archived && s.Filament.Id == req.FilamentID {
								totalAvailable += s.RemainingWeight
							}
						}
						if totalAvailable < req.Amount {
							ready = false
						}
					}

					options = append(options, plateOption{
						discoveredIdx: di,
						projectIdx:    i,
						plateIdx:      j,
						plate:         plate,
						projectName:   proj.Name,
						swapCost:      cost,
						isReady:       ready,
					})
				}
			}
		}

		if len(busyElsewhere) > 0 {
			fmt.Println("\nIn-progress on other printers:")
			for _, b := range busyElsewhere {
				fmt.Printf("  %s - %s (printing on %s)\n", models.Sanitize(b.projectName), models.Sanitize(b.plateName), models.Sanitize(b.printer))
			}
			fmt.Println()
		}

		if len(options) == 0 {
			fmt.Println("No pending plates found.")
			return nil
		}

		// Sort: Ready first, then lower swap cost
		// (Simple recommendation logic: just highlight the best one)
		bestIdx := -1
		minCost := 999
		for i, o := range options {
			if o.isReady && o.swapCost < minCost {
				minCost = o.swapCost
				bestIdx = i
			}
		}

		var items []string
		for i, o := range options {
			prefix := "  "
			if i == bestIdx {
				prefix = "* [REC] "
			}
			readyStr := ""
			if !o.isReady {
				readyStr = " (INSUFFICIENT FILAMENT)"
			}
			items = append(items, fmt.Sprintf("%s%s - %s [Swaps: %d]%s", prefix, models.Sanitize(o.projectName), models.Sanitize(o.plate.Name), o.swapCost, readyStr))
		}

		promptPlate := promptui.Select{
			Label:             "Select plate to print",
			Items:             items,
			Size:              10,
			Stdout:            NoBellStdout,
			StartInSearchMode: true,
			Searcher: func(input string, index int) bool {
				return strings.Contains(strings.ToLower(items[index]), strings.ToLower(input))
			},
		}
		selectedIdx, _, err := promptPlate.Run()
		if err != nil {
			return err
		}
		choice := options[selectedIdx]

		// 4. Swap Instructions
		fmt.Printf("\nPreparing to print: %s - %s\n", models.Sanitize(choice.projectName), models.Sanitize(choice.plate.Name))

		// Pre-calculate what filaments are needed for the current and future plates in the project
		neededFilamentIDs := make(map[int]bool)
		for i := choice.projectIdx; i < len(discovered[0].Plan.Projects); i++ {
			proj := discovered[0].Plan.Projects[i]
			if proj.Status == "completed" {
				continue
			}
			startPlate := 0
			if i == choice.projectIdx {
				startPlate = choice.plateIdx
			}
			for j := startPlate; j < len(proj.Plates); j++ {
				plate := proj.Plates[j]
				if plate.Status == "completed" {
					continue
				}
				for _, req := range plate.Needs {
					neededFilamentIDs[req.FilamentID] = true
				}
			}
		}

		// Pre-collect all locations that are assigned to ANY printer
		allPrinterLocations := make(map[string]string) // Location -> printer name
		for pName, pCfg := range Cfg.Printers {
			for _, l := range pCfg.Locations {
				allPrinterLocations[l] = pName
			}
		}

		swapsPerformed := false
		for _, req := range choice.plate.Needs {
			// Collect all matching loaded spools in this printer
			var loadedMatches []models.FindSpool
			var loadedTotal float64
			for _, loc := range printerLocations {
				for _, s := range loadedSpools {
					if s.Location == loc && s.Filament.Id == req.FilamentID {
						loadedMatches = append(loadedMatches, s)
						loadedTotal += s.RemainingWeight
					}
				}
			}

			var bestSpool *models.FindSpool
			if len(loadedMatches) > 0 {
				if loadedTotal >= req.Amount {
					// Enough filament already loaded (possibly across multiple slots)
					if len(loadedMatches) == 1 {
						fmt.Printf("✓ %s is already loaded in %s (%.1fg remaining)\n",
							models.Sanitize(req.Name),
							models.Sanitize(loadedMatches[0].Location),
							loadedMatches[0].RemainingWeight)
					} else {
						var locs []string
						for _, s := range loadedMatches {
							locs = append(locs, fmt.Sprintf("%s #%d: %.1fg",
								models.Sanitize(s.Location), s.Id, s.RemainingWeight))
						}
						fmt.Printf("✓ %s is already loaded across %d slots (%.1fg total) — %s\n",
							models.Sanitize(req.Name), len(loadedMatches), loadedTotal,
							strings.Join(locs, ", "))

						// Recommend starting with the spool that has the least remaining,
						// so the (near-)empty spool finishes mid-print and the fuller one takes over.
						leastRemaining := loadedMatches[0]
						for _, s := range loadedMatches[1:] {
							if s.RemainingWeight < leastRemaining.RemainingWeight {
								leastRemaining = s
							}
						}
						fmt.Printf("  → Start with spool #%d in %s (%.1fg remaining) first so it finishes before the fuller spool takes over\n",
							leastRemaining.Id,
							models.Sanitize(leastRemaining.Location),
							leastRemaining.RemainingWeight)
					}
					continue
				}

				// Short on filament even when summing all matching loaded spools
				fmt.Printf("! WARNING: Loaded %s has %.1fg remaining across %d slot(s), but this plate requires %.1fg\n",
					models.Sanitize(req.Name), loadedTotal, len(loadedMatches), req.Amount)

				// Build set of loaded spool IDs and printer location set to exclude
				loadedIDs := make(map[int]bool, len(loadedMatches))
				for _, s := range loadedMatches {
					loadedIDs[s.Id] = true
				}
				inThisPrinter := make(map[string]bool, len(printerLocations))
				for _, loc := range printerLocations {
					inThisPrinter[loc] = true
				}

				var nextBest *models.FindSpool
				for _, s := range allSpools {
					if s.Archived || s.Filament.Id != req.FilamentID {
						continue
					}
					if loadedIDs[s.Id] {
						continue
					}
					if inThisPrinter[s.Location] {
						continue // already in this printer — would have been in loadedMatches
					}
					if nextBest == nil || s.RemainingWeight > nextBest.RemainingWeight {
						sc := s
						nextBest = &sc
					}
				}

				if nextBest != nil {
					fmt.Printf("  Suggestion: Load spool #%d (%.1fg remaining) into another slot for automatic swap.\n", nextBest.Id, nextBest.RemainingWeight)
					prompt := promptui.Prompt{
						Label:     "Do you want to load this spool now?",
						IsConfirm: true,
						Stdout:    NoBellStdout,
					}
					if _, err := prompt.Run(); err == nil {
						// Proceed to find a slot and load it
						bestSpool = nextBest
					}
				}

				if bestSpool == nil {
					continue
				}
			}

			if bestSpool == nil {
				// Find best spool to load
				var candidates []models.FindSpool
				for _, s := range allSpools {
					if !s.Archived && s.Filament.Id == req.FilamentID {
						candidates = append(candidates, s)
					}
				}

				// Priority:
				// 1. Not in any printer Location
				// 2. Partially used
				// 3. Oldest (lowest ID)
				for i := range candidates {
					s := &candidates[i]
					if bestSpool == nil {
						bestSpool = s
						continue
					}

					_, curInPrinter := allPrinterLocations[bestSpool.Location]
					_, newInPrinter := allPrinterLocations[s.Location]

					// If current best is in a printer but this one isn't, this one is better
					if curInPrinter && !newInPrinter {
						bestSpool = s
						continue
					}
					// If this one is in a printer but current best isn't, current best is still better
					if !curInPrinter && newInPrinter {
						continue
					}

					// If both same in terms of "in printer", use existing priority
					// If current best is unused but this one is used
					if bestSpool.UsedWeight == 0 && s.UsedWeight > 0 {
						bestSpool = s
						continue
					}
					// If both same state, pick lowest ID
					if (bestSpool.UsedWeight > 0) == (s.UsedWeight > 0) && s.Id < bestSpool.Id {
						bestSpool = s
					}
				}

				if bestSpool == nil {
					fmt.Printf("! Error: Could not find any spool for %s\n", models.Sanitize(req.Name))
					continue
				}

				// If the best (or only) spool is in another printer, warn the user
				if otherPName, inOtherPrinter := allPrinterLocations[bestSpool.Location]; inOtherPrinter {
					fmt.Printf("! WARNING: Spool #%d (%s) is already in %s (Printer: %s)\n", bestSpool.Id, models.Sanitize(bestSpool.Filament.Name), models.Sanitize(bestSpool.Location), models.Sanitize(otherPName))
					prompt := promptui.Prompt{
						Label:     "Do you want to move it to this printer anyway?",
						IsConfirm: true,
						Stdout:    NoBellStdout,
					}
					_, err := prompt.Run()
					if err != nil {
						fmt.Println("Skipping this swap.")
						continue
					}
				}
			}

			swapsPerformed = true
			// Find an empty slot or one to swap out
			targetLoc := ""
			minLoad := 999
			for _, loc := range printerLocations {
				loadedInLoc := 0
				for _, s := range loadedSpools {
					if s.Location == loc {
						loadedInLoc++
					}
				}
				capacity := 1
				if capInfo, ok := Cfg.LocationCapacity[loc]; ok {
					capacity = capInfo.Capacity
				}
				if loadedInLoc < capacity {
					if loadedInLoc < minLoad {
						minLoad = loadedInLoc
						targetLoc = loc
					}
				}
			}

			if targetLoc == "" {
				// All locations are full. We need to find the best Location to unload from.
				// We want a Location that has a spool NOT needed for the current project.
				// And among those, the LRU spool.
				var bestUnloadLoc string
				var bestUnloadSpool models.FindSpool
				foundNonNeeded := false

				for _, loc := range printerLocations {
					var spoolsInLoc []models.FindSpool
					for _, s := range loadedSpools {
						if s.Location == loc {
							spoolsInLoc = append(spoolsInLoc, s)
						}
					}

					for _, s := range spoolsInLoc {
						isNeeded := neededFilamentIDs[s.Filament.Id]
						// Check if it's needed for other requirements of the CURRENT plate as well
						for _, otherReq := range choice.plate.Needs {
							if s.Filament.Id == otherReq.FilamentID {
								isNeeded = true
								break
							}
						}

						if !isNeeded {
							if !foundNonNeeded {
								bestUnloadLoc = loc
								bestUnloadSpool = s
								foundNonNeeded = true
							} else {
								// LRU Logic: Older LastUsed comes first. Never-used comes last.
								li, lj := bestUnloadSpool.LastUsed, s.LastUsed
								zi, zj := li.IsZero(), lj.IsZero()

								better := false
								if zi && !zj {
									better = true // s is used, bestUnloadSpool never used; s is better candidate to unload?
									// Wait, if it's never used, maybe we should keep it?
									// find.go says: "never-used appear last" for --lru.
									// "li.Before(lj) // older last-used first"
									// "zi && !zj { return false // i has never been used; place after j }"
									// So LRU order is: [Oldest Used] ... [Newest Used] [Never Used]
									// If we want to unload the LEAST recently used, we want the one at the start of that list.
								} else if !zi && !zj {
									if lj.Before(li) {
										better = true
									}
								}

								if better {
									bestUnloadLoc = loc
									bestUnloadSpool = s
								}
							}
						} else if !foundNonNeeded {
							// If we haven't found any non-needed spool yet, keep track of the LRU needed one as fallback
							if bestUnloadLoc == "" {
								bestUnloadLoc = loc
								bestUnloadSpool = s
							} else {
								li, lj := bestUnloadSpool.LastUsed, s.LastUsed
								zi, zj := li.IsZero(), lj.IsZero()
								if (!zi && !zj && lj.Before(li)) || (zi && !zj) {
									bestUnloadLoc = loc
									bestUnloadSpool = s
								}
							}
						}
					}
				}
				targetLoc = bestUnloadLoc
			}

			// If target Location is full, we need to unload something
			var spoolToUnload *models.FindSpool
			var unloadIdx = -1
			loadedInTarget := []models.FindSpool{}
			for _, s := range loadedSpools {
				if s.Location == targetLoc {
					loadedInTarget = append(loadedInTarget, s)
				}
			}

			capacity := 1
			if capInfo, ok := Cfg.LocationCapacity[targetLoc]; ok {
				capacity = capInfo.Capacity
			}

			if len(loadedInTarget) >= capacity {
				// Choose which one in this Location to unload.
				// Same logic: prioritize non-needed, then LRU.
				var candidate models.FindSpool
				foundNonNeeded := false
				for _, s := range loadedInTarget {
					isNeeded := neededFilamentIDs[s.Filament.Id]
					for _, otherReq := range choice.plate.Needs {
						if s.Filament.Id == otherReq.FilamentID {
							isNeeded = true
							break
						}
					}

					if !isNeeded {
						if !foundNonNeeded {
							candidate = s
							foundNonNeeded = true
						} else {
							li, lj := candidate.LastUsed, s.LastUsed
							zi, zj := li.IsZero(), lj.IsZero()
							if (!zi && !zj && lj.Before(li)) || (zi && !zj) {
								candidate = s
							}
						}
					} else if !foundNonNeeded {
						if candidate.Id == 0 {
							candidate = s
						} else {
							li, lj := candidate.LastUsed, s.LastUsed
							zi, zj := li.IsZero(), lj.IsZero()
							if (!zi && !zj && lj.Before(li)) || (zi && !zj) {
								candidate = s
							}
						}
					}
				}
				spoolToUnload = &candidate

				// Find the index of the spool being unloaded in its current location
				orders, _ := LoadLocationOrders(ctx, apiClient)
				if list, ok := orders[targetLoc]; ok {
					unloadIdx = indexOf(list, spoolToUnload.Id)
				}

				fmt.Printf("→ UNLOAD #%d (%s) from %s\n", spoolToUnload.Id, models.Sanitize(spoolToUnload.Filament.Name), models.Sanitize(targetLoc))

				// Interactive location picker for unload destination
				orders, _ = LoadLocationOrders(ctx, apiClient)
				allSpools, _ := apiClient.FindSpoolsByName(ctx, "*", nil, nil)
				spoolCounts := map[string]int{}
				for _, s := range allSpools {
					if s.Location != "" {
						spoolCounts[s.Location]++
					}
				}

				fmt.Printf("  Select where to put it (or press Ctrl+C to keep as-is):\n")
				newLoc, canceled, selErr := selectLocationInteractively(orders, spoolCounts, false)
				if selErr != nil || canceled {
					fmt.Println("  Keeping spool location as-is in Spoolman.")
				} else {
					_ = apiClient.MoveSpool(ctx, spoolToUnload.Id, newLoc)

					// Update locations_spoolorders
					orders = RemoveFromAllOrders(orders, spoolToUnload.Id)
					list := orders[newLoc]
					if IsPrinterLocation(newLoc) {
						emptyIdx := FirstEmptySlot(list)
						if emptyIdx >= 0 {
							list[emptyIdx] = spoolToUnload.Id
						} else {
							list = append(list, spoolToUnload.Id)
						}
					} else {
						list = append(list, spoolToUnload.Id)
					}
					orders[newLoc] = list
					_ = apiClient.PostSettingObject(ctx, "locations_spoolorders", orders)
					fmt.Printf("  Moved to %s\n", newLoc)
				}
				// Remove from our local tracking of what's loaded
				for loc, s := range loadedSpools {
					if s.Id == spoolToUnload.Id {
						delete(loadedSpools, loc)
					}
				}
			}

			fmt.Printf("→ LOAD #%d (%s) into %s (currently at %s)\n", bestSpool.Id, models.Sanitize(bestSpool.Filament.Name), models.Sanitize(targetLoc), models.Sanitize(bestSpool.Location))
			fmt.Printf("Press Enter once the swap is complete...")
			var confirm string
			_, _ = fmt.Scanln(&confirm)

			_ = apiClient.MoveSpool(ctx, bestSpool.Id, targetLoc)

			// Update locations_spoolorders for LOAD
			orders, err := LoadLocationOrders(ctx, apiClient)
			if err == nil {
				orders = RemoveFromAllOrders(orders, bestSpool.Id)
				list := orders[targetLoc]
				if IsPrinterLocation(targetLoc) {
					if unloadIdx >= 0 && unloadIdx < len(list) && list[unloadIdx] == EmptySlot {
						// Place into the slot vacated by the unloaded spool
						list[unloadIdx] = bestSpool.Id
					} else {
						emptyIdx := FirstEmptySlot(list)
						if emptyIdx >= 0 {
							list[emptyIdx] = bestSpool.Id
						} else {
							list = append(list, bestSpool.Id)
						}
					}
				} else {
					if unloadIdx != -1 {
						list = InsertAt(list, unloadIdx, bestSpool.Id)
					} else {
						list = append(list, bestSpool.Id)
					}
				}
				orders[targetLoc] = list
				_ = apiClient.PostSettingObject(ctx, "locations_spoolorders", orders)
			}

			// Push tray update to printer if this is a printer location
			if IsPrinterLocation(targetLoc) && Cfg.PlansServer != "" {
				// Find the slot position of the spool we just placed
				slotPos := 0
				for i, id := range orders[targetLoc] {
					if id == bestSpool.Id {
						slotPos = i + 1
						break
					}
				}
				if slotPos > 0 {
					if mapping := MapLocationToTray(targetLoc, slotPos); mapping.SupportsTrayPush() {
						colorHex := strings.TrimPrefix(bestSpool.Filament.ColorHex, "#")
						if len(colorHex) == 6 {
							colorHex += "FF"
						}
						trayType := bestSpool.Filament.Material
						infoIdx := ""
						if profile := LookupFilamentProfile(bestSpool.Filament.Vendor.Name, bestSpool.Filament.Name, bestSpool.Filament.Material); profile != nil {
							trayType = profile.TrayType
							infoIdx = profile.InfoIdx
						}
						planClient := api.NewPlanServerClient(Cfg.PlansServer, version, Cfg.TLSSkipVerify)
						if err := planClient.PushTray(ctx, mapping.PrinterName, api.TrayPushRequest{
							AmsID:   mapping.AmsID,
							TrayID:  mapping.TrayID,
							Color:   strings.ToUpper(colorHex),
							Type:    trayType,
							TempMin: 190,
							TempMax: 240,
							InfoIdx: infoIdx,
						}); err != nil {
							fmt.Printf("  Note: could not update printer tray: %v\n", err)
						} else {
							fmt.Printf("  Updated %s tray\n", mapping.PrinterName)
						}
					}
				}
			}

			// Update our local tracking
			bestSpool.Location = targetLoc
			loadedSpools[targetLoc+"_"+fmt.Sprint(bestSpool.Id)] = *bestSpool
		}

		// Mark the plate as in-progress with the printer name
		dp := &discovered[choice.discoveredIdx]
		dp.Plan.Projects[choice.projectIdx].Plates[choice.plateIdx].Status = "in-progress"
		dp.Plan.Projects[choice.projectIdx].Plates[choice.plateIdx].Printer = printerName
		dp.Plan.Projects[choice.projectIdx].Plates[choice.plateIdx].StartedAt = time.Now().Format(time.RFC3339)
		// Also mark the project as in-progress if it was todo
		if dp.Plan.Projects[choice.projectIdx].Status == "todo" {
			dp.Plan.Projects[choice.projectIdx].Status = "in-progress"
		}
		if err := savePlan(*dp, dp.Plan); err != nil {
			fmt.Printf("Warning: failed to save in-progress state: %v\n", err)
		}

		if swapsPerformed {
			fmt.Println("\nSwaps complete. Happy printing!")
		} else {
			fmt.Println("\nEverything ready. Happy printing!")
		}
		return nil
	},
}

func init() {
	planCmd.AddCommand(planNextCmd)
}
