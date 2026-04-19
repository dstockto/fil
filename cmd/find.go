/*
Copyright © 2025 David Stockton <dave@davidstockton.com>
*/
package cmd

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/devices"
	"github.com/dstockto/fil/models"
	"github.com/fatih/color"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/spf13/cobra"
)

// findCmd represents the find command.
var findCmd = &cobra.Command{
	Use:   "find <name or id...>",
	Short: "find a spool based on name or id",
	Long: `Find a spool based on name or id. You can provide multiple names or ids. For multi-word names, enclose in 
	quotes. To show all spools, use the wildcard character '*'.`,
	RunE:    runFind,
	Aliases: []string{"f"},
}

func buildFindQuery(cmd *cobra.Command) (map[string]string, []api.SpoolFilter, error) {
	var filters []api.SpoolFilter
	// API doesn't support diameter, so we have to filter manually
	diameter, err := cmd.Flags().GetString("diameter")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get diameter flag: %w", err)
	}

	switch diameter {
	case "*":
		filters = append(filters, noFilter)
	case "2.85":
		filters = append(filters, ultimakerFilament)
	default:
		filters = append(filters, onlyStandardFilament)
	}

	query := make(map[string]string)

	if manufacturer, err := cmd.Flags().GetString("manufacturer"); err == nil && manufacturer != "" {
		query["manufacturer"] = manufacturer
	}

	if allowedArchived, err := cmd.Flags().GetBool("allowed-archived"); err == nil && allowedArchived {
		query["allow_archived"] = "true"
	}

	if onlyArchived, err := cmd.Flags().GetBool("archived-only"); err == nil && onlyArchived {
		query["allow_archived"] = "true" // allow archived is needed to get archived spools from the API

		// the API doesn't support only returning archived spools, so we have to filter manually
		filters = append(filters, archivedOnly)
	}

	if hasComment, err := cmd.Flags().GetBool("has-comment"); err == nil && hasComment {
		filters = append(filters, getCommentFilter("*"))
	}

	if comment, err := cmd.Flags().GetString("comment"); err == nil && comment != "" {
		filters = append(filters, getCommentFilter(comment))
	}

	if used, err := cmd.Flags().GetBool("used"); err == nil && used {
		filters = append(filters, func(s models.FindSpool) bool {
			return s.UsedWeight != 0.0
		})
	}

	if pristine, err := cmd.Flags().GetBool("pristine"); err == nil && pristine {
		filters = append(filters, func(s models.FindSpool) bool {
			return s.UsedWeight == 0.0
		})
	}

	if location, err := cmd.Flags().GetString("location"); err == nil && location != "" {
		location = MapToAlias(location)
		query["location"] = location
	}

	if material, err := cmd.Flags().GetString("material"); err == nil && material != "" {
		// Support comma-separated list of material substrings, case-insensitive
		var needles []string
		for _, m := range strings.Split(material, ",") {
			if trimmed := strings.ToLower(strings.TrimSpace(m)); trimmed != "" {
				needles = append(needles, trimmed)
			}
		}
		filters = append(filters, func(s models.FindSpool) bool {
			haystack := strings.ToLower(s.Filament.Material)
			for _, n := range needles {
				if strings.Contains(haystack, n) {
					return true
				}
			}
			return false
		})
	}

	if needed, err := cmd.Flags().GetBool("needed"); err == nil && needed {
		apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)
		neededIDs, err := GetNeededFilamentIDs(cmd.Context(), apiClient)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get needed filament IDs: %w", err)
		}
		filters = append(filters, func(s models.FindSpool) bool {
			return neededIDs[s.Filament.Id]
		})
	}

	return query, filters, nil
}

func runFind(cmd *cobra.Command, args []string) error {
	if Cfg == nil || Cfg.ApiBase == "" {
		return errors.New("apiClient endpoint not configured")
	}

	if len(args) == 0 {
		args = append(args, "*")
	}

	apiClient := api.NewClient(Cfg.ApiBase, Cfg.TLSSkipVerify)

	var (
		spools  []models.FindSpool
		filters []api.SpoolFilter
	)

	ctx := cmd.Context()

	// Preload settings-based Location orders to sort results accordingly.
	// The settings key 'locations_spoolorders' stores, per Location, the ordered list of spool IDs.
	orders, err := LoadLocationOrders(ctx, apiClient)
	if err != nil {
		// Non-fatal: if settings cannot be loaded, continue without settings-based ordering.
		orders = map[string][]int{}
	}
	// Build quick lookup of ranks per Location for O(1) index lookups.
	ranks := make(map[string]map[int]int, len(orders))
	for loc, ids := range orders {
		m := make(map[int]int, len(ids))
		for i, id := range ids {
			m[id] = i
		}
		ranks[loc] = m
	}

	query, filters, err := buildFindQuery(cmd)
	if err != nil {
		return err
	}

	if location, _ := cmd.Flags().GetString("location"); location != "" {
		fmt.Printf("Filtering by location: %s\n", query["location"])
	}

	if needed, _ := cmd.Flags().GetBool("needed"); needed {
		fmt.Println("Filtering by spools needed by projects")
	}

	// Allow additional filters later, for now, just default to 1.75mm filament
	aggFilter := aggregateFilter(filters...)

	// determine if we should sort by least or most recently used
	lruSort, _ := cmd.Flags().GetBool("lru")
	mruSort, _ := cmd.Flags().GetBool("mru")
	if lruSort && mruSort {
		return fmt.Errorf("flags --lru and --mru are mutually exclusive; please specify only one")
	}

	nearHex, _ := cmd.Flags().GetString("near")
	useScan, _ := cmd.Flags().GetBool("scan")
	limit, _ := cmd.Flags().GetInt("limit")
	var target colorful.Color
	useNear := nearHex != ""
	if useNear && useScan {
		return fmt.Errorf("--near and --scan are mutually exclusive")
	}
	if useScan {
		if lruSort || mruSort {
			return fmt.Errorf("--scan cannot be combined with --lru or --mru")
		}
		if limit <= 0 {
			return fmt.Errorf("--limit must be positive, got %d", limit)
		}
		scanned, err := readOneScanForFind(ctx)
		if err != nil {
			return err
		}
		c, ok := parseHexColor(scanned.Color)
		if !ok {
			return fmt.Errorf("scanned color %q is not parseable", scanned.Color)
		}
		target = c
		fmt.Printf("Scanned color: %s %s\n", scanned.Color, models.GetColorBlock(scanned.Color, ""))
		if scanned.HasTD {
			fmt.Printf("Scanned TD:    %.2fmm\n", scanned.TD)
		}
		fmt.Println()
		// Re-use the --near code path by marking useNear = true.
		useNear = true
	} else if useNear {
		c, ok := parseHexColor(nearHex)
		if !ok {
			return fmt.Errorf("invalid --near color %q; expected hex like #ff5500", nearHex)
		}
		if lruSort || mruSort {
			return fmt.Errorf("--near cannot be combined with --lru or --mru")
		}
		if limit <= 0 {
			return fmt.Errorf("--limit must be positive, got %d", limit)
		}
		target = c
	} else if cmd.Flags().Changed("limit") {
		return fmt.Errorf("--limit only applies with --near or --scan")
	}

	for _, a := range args {
		foundFmt := "Found %d spools matching '%s':\n"
		name := a
		// figure out if the argument is an id (int)
		id, err := strconv.Atoi(a)
		if err == nil {
			name = "#" + name
			foundFmt = "Found %d spool with ID %s:\n"

			spool, err := apiClient.FindSpoolsById(ctx, id)
			if errors.Is(err, api.ErrSpoolNotFound) {
				spools = []models.FindSpool{}
			} else if err != nil {
				return fmt.Errorf("error finding spools: %w", err)
			} else {
				spools = []models.FindSpool{*spool}
			}
		} else {
			spools, err = apiClient.FindSpoolsByName(ctx, a, aggFilter, query)
			if err != nil {
				return fmt.Errorf("error finding spools: %w", err)
			}
		}

		// When --near is set, sort by color distance and truncate to the limit.
		var deltas map[int]float64
		if useNear {
			deltas = make(map[int]float64, len(spools))
			withColor := spools[:0]
			for _, sp := range spools {
				d := spoolColorDistance(sp, target)
				if math.IsInf(d, 1) {
					continue
				}
				deltas[sp.Id] = d
				withColor = append(withColor, sp)
			}
			spools = withColor
			sort.SliceStable(spools, func(i, j int) bool {
				return deltas[spools[i].Id] < deltas[spools[j].Id]
			})
			if len(spools) > limit {
				spools = spools[:limit]
			}
		}

		// If requested, sort by least- or most-recently used; never-used at the end
		if !useNear && (lruSort || mruSort) && len(spools) > 1 {
			sort.Slice(spools, func(i, j int) bool {
				li, lj := spools[i].LastUsed, spools[j].LastUsed
				zi, zj := li.IsZero(), lj.IsZero()
				if zi && !zj {
					return false // i has never been used; place after j
				}
				if !zi && zj {
					return true // i used, j never used; i comes first
				}
				if zi && zj {
					return false // keep relative order for never-used
				}
				if lruSort {
					return li.Before(lj) // older last-used first
				}
				return li.After(lj) // newer last-used first
			})
		} else if !useNear && len(spools) > 1 && len(ranks) > 0 {
			// Default behavior: group by location, then sort by settings-defined order within each location.
			// Items with a known rank (present in settings for their Location) come before unknowns.
			sort.SliceStable(spools, func(i, j int) bool {
				ai := spools[i]
				aj := spools[j]
				locI := MapToAlias(ai.Location)
				locJ := MapToAlias(aj.Location)

				// Group by location first
				if locI != locJ {
					return locI < locJ
				}

				// Within same location, sort by rank
				rI, okI := ranks[locI][ai.Id]
				rJ, okJ := ranks[locJ][aj.Id]
				if okI && !okJ {
					return true
				}
				if !okI && okJ {
					return false
				}
				if okI && okJ {
					if rI != rJ {
						return rI < rJ
					}
					return false
				}
				// Neither has a rank; keep current relative order (stable sort preserves input order).
				return false
			})
		}

		var foundMsg string
		if useNear {
			if name == "*" {
				foundMsg = fmt.Sprintf("Found %d spools near #%s:\n", len(spools), strings.TrimPrefix(nearHex, "#"))
			} else {
				foundMsg = fmt.Sprintf("Found %d spools matching '%s' near #%s:\n", len(spools), name, strings.TrimPrefix(nearHex, "#"))
			}
		} else {
			foundMsg = fmt.Sprintf(foundFmt, len(spools), name)
		}
		if len(spools) == 0 {
			// print in red
			color.HiRed(foundMsg)
		} else {
			// print in green
			color.Green(foundMsg)
		}

		totalRemaining := 0.0
		totalUsed := 0.0

		showPurchase, _ := cmd.Flags().GetBool("purchase")

		// Check if any results are in printer locations — if so, render those
		// locations with slot numbers and empty indicators.
		hasPrinterLoc := false
		for _, s := range spools {
			if IsPrinterLocation(s.Location) {
				hasPrinterLoc = true
				break
			}
		}

		if useNear && len(spools) > 0 {
			maxLabel := 0
			for _, s := range spools {
				loc := models.Sanitize(s.Location)
				if loc == "" {
					loc = "N/A"
				}
				if len(loc) > maxLabel {
					maxLabel = len(loc)
				}
			}
			for _, s := range spools {
				loc := models.Sanitize(s.Location)
				if loc == "" {
					loc = "N/A"
				}
				deltaStr := color.New(color.Faint).Sprintf("ΔE %5.1f", deltas[s.Id])
				boldLabel := color.New(color.Bold).Sprintf("%-*s", maxLabel, loc)
				fmt.Printf(" %s  %s %s\n", deltaStr, boldLabel, s.StringNoLocation())
				if showPurchase {
					fmt.Printf("    %s\n", amazonLink(s.Filament.Vendor.Name, s.Filament.Name))
				}
				totalRemaining += s.RemainingWeight
				totalUsed += s.UsedWeight
			}
		} else if hasPrinterLoc && len(spools) > 0 && !lruSort && !mruSort {
			// Group spools by location
			spoolsByLoc := map[string]map[int]models.FindSpool{}
			locOrder := []string{}
			locSeen := map[string]struct{}{}
			for _, s := range spools {
				loc := s.Location
				if _, ok := spoolsByLoc[loc]; !ok {
					spoolsByLoc[loc] = map[int]models.FindSpool{}
				}
				spoolsByLoc[loc][s.Id] = s
				if _, ok := locSeen[loc]; !ok {
					locOrder = append(locOrder, loc)
					locSeen[loc] = struct{}{}
				}
			}

			// Pre-compute all location labels to find max width for alignment
			type labeledSpool struct {
				label string
				spool models.FindSpool
			}
			var labeled []labeledSpool

			for _, loc := range locOrder {
				locSpools := spoolsByLoc[loc]
				if IsPrinterLocation(loc) {
					slotList := orders[loc]
					slotList = PadToCapacity(loc, slotList)
					showEmpties := a == "*"
					for i, id := range slotList {
						slotNum := i + 1
						if id == EmptySlot {
							if showEmpties {
								labeled = append(labeled, labeledSpool{
									label: fmt.Sprintf("%s:%d", loc, slotNum),
								})
							}
						} else if s, ok := locSpools[id]; ok {
							labeled = append(labeled, labeledSpool{
								label: fmt.Sprintf("%s:%d", loc, slotNum),
								spool: s,
							})
							delete(locSpools, id)
						}
					}
					for _, s := range locSpools {
						labeled = append(labeled, labeledSpool{
							label: fmt.Sprintf("%s:?", loc),
							spool: s,
						})
					}
				} else {
					for _, s := range locSpools {
						label := models.Sanitize(s.Location)
						if label == "" {
							label = "N/A"
						}
						labeled = append(labeled, labeledSpool{
							label: label,
							spool: s,
						})
					}
				}
			}

			// Find max label width
			maxLabel := 0
			for _, ls := range labeled {
				if len(ls.label) > maxLabel {
					maxLabel = len(ls.label)
				}
			}

			// Render with aligned columns
			for _, ls := range labeled {
				boldLabel := color.New(color.Bold).Sprintf("%-*s", maxLabel, ls.label)
				if ls.spool.Id == 0 {
					// Empty slot
					dimmed := color.New(color.Faint).SprintFunc()
					fmt.Printf(" %s %s\n", boldLabel, dimmed("(empty)"))
				} else {
					fmt.Printf(" %s %s\n", boldLabel, ls.spool.StringNoLocation())
					if showPurchase {
						fmt.Printf("    %s\n", amazonLink(ls.spool.Filament.Vendor.Name, ls.spool.Filament.Name))
					}
					totalRemaining += ls.spool.RemainingWeight
					totalUsed += ls.spool.UsedWeight
				}
			}
		} else {
			for _, s := range spools {
				loc := models.Sanitize(s.Location)
				if loc == "" {
					loc = "N/A"
				}
				boldLabel := color.New(color.Bold).Sprintf("%s", loc)
				fmt.Printf(" %s %s\n", boldLabel, s.StringNoLocation())
				if showPurchase {
					fmt.Printf("%s\n", amazonLink(s.Filament.Vendor.Name, s.Filament.Name))
				}
				totalRemaining += s.RemainingWeight
				totalUsed += s.UsedWeight
			}
		}

		if len(spools) > 0 {
			bold := color.New(color.Bold).SprintFunc()
			spoolPlural := "spools"

			if len(spools) == 1 {
				spoolPlural = "spool"
			}

			fmt.Printf(
				"%s: %d %s, %s: %.1fg, %s: %.1fg\n\n",
				bold("Summary"),
				len(spools),
				spoolPlural,
				bold("Remaining"),
				totalRemaining,
				bold("Used"),
				totalUsed,
			)
		}
	}

	return nil
}

func onlyStandardFilament(spool models.FindSpool) bool {
	return spool.Filament.Diameter == 1.75
}

func noFilter(_ models.FindSpool) bool {
	return true
}

func ultimakerFilament(spool models.FindSpool) bool {
	return spool.Filament.Diameter == 2.85
}

func archivedOnly(spool models.FindSpool) bool {
	return spool.Archived
}

func getCommentFilter(comment string) api.SpoolFilter {
	if comment == "*" {
		return func(spool models.FindSpool) bool {
			return spool.Comment != ""
		}
	}

	lowerComment := strings.ToLower(comment)

	return func(s models.FindSpool) bool {
		return strings.Contains(strings.ToLower(s.Comment), lowerComment)
	}
}

func aggregateFilter(filters ...api.SpoolFilter) api.SpoolFilter {
	return func(s models.FindSpool) bool {
		for _, f := range filters {
			if !f(s) {
				return false
			}
		}

		return true
	}
}

func init() {
	rootCmd.AddCommand(findCmd)

	findCmd.Flags().StringP("diameter", "d", "1.75", "filter by diameter, default is 1.75mm, '*' for all")
	findCmd.Flags().StringP("manufacturer", "m", "", "filter by manufacturer, default is all")
	findCmd.Flags().BoolP("allowed-archived", "a", false, "show archived spools, default is false")
	findCmd.Flags().Bool("archived-only", false, "show only archived spools, default is false")
	findCmd.Flags().Bool("has-comment", false, "show only spools with comments, default is false")
	findCmd.Flags().StringP("comment", "c", "", "find spools with a comment matching the provided value")
	findCmd.Flags().BoolP("used", "u", false, "show only spools that have been used")
	findCmd.Flags().BoolP("pristine", "p", false, "show only (pristine) spools that have not been used")
	findCmd.Flags().StringP("location", "l", "", "filter by location, default is all")
	findCmd.Flags().Bool("lru", false, "sort by least recently used first; never-used appear last")
	findCmd.Flags().Bool("mru", false, "sort by most recently used first; never-used appear last")
	findCmd.Flags().Bool("purchase", false, "show purchase link for each spool")
	findCmd.Flags().BoolP("needed", "n", false, "show only spools for filaments that are needed by plans but not loaded")
	findCmd.Flags().String("material", "", "filter by material substring (e.g. 'pla' matches 'PLA', 'Matte PLA', 'Silk PLA'). Comma-separated for multiple (case-insensitive)")
	findCmd.Flags().String("near", "", "sort results by CIEDE2000 ΔE distance from a target hex color (e.g. '#ff5500'); pairs with --limit")
	findCmd.Flags().Int("limit", 10, "when --near or --scan is set, show only the N nearest results (must be positive)")
	findCmd.Flags().Bool("scan", false, "read one color from an attached TD-1 scanner and rank spools by ΔE against it")
}

// readOneScanForFind opens the TD-1, performs the handshake, reads a single
// scan, and returns the result. Only used by `fil find --scan`.
func readOneScanForFind(ctx context.Context) (devices.ScanResult, error) {
	info, err := devices.Probe(nil)
	if err != nil {
		if errors.Is(err, devices.ErrNoDevice) {
			return devices.ScanResult{}, errors.New("no TD-1 detected — plug it in and try again")
		}
		return devices.ScanResult{}, fmt.Errorf("probe TD-1: %w", err)
	}
	port, err := devices.Open(info.Path)
	if err != nil {
		return devices.ScanResult{}, fmt.Errorf("open TD-1 on %s: %w", info.Path, err)
	}
	defer func() { _ = port.Close() }()

	if err := handshakeTD1(ctx, port); err != nil {
		return devices.ScanResult{}, fmt.Errorf("TD-1 handshake: %w", err)
	}

	fmt.Println("Insert a filament sample into the TD-1 to scan...")
	return devices.ReadScan(ctx, port, 10)
}
