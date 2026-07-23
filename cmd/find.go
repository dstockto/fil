/*
Copyright © 2025 David Stockton <dave@davidstockton.com>
*/
package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// spoolExport is the machine-readable shape emitted by `fil find --json`.
// It is intentionally small and stable so other tools can consume it without
// scraping the human-formatted output.
//
// Location and Slot are kept as separate fields rather than pre-joined into the
// "AMS B:4" label the text renderer prints. Spoolman has no slot model at all —
// Location is what it actually stores, and the slot is fil's own derivation from
// the locations_spoolorders setting. Exporting them apart keeps Location a
// faithful Spoolman value and lets a consumer rebuild the display label (and the
// `fil move` destination) as location + ":" + slot when it wants one.
type spoolExport struct {
	ID         int     `json:"id"`
	Name       string  `json:"name"`
	Vendor     string  `json:"vendor"`
	Material   string  `json:"material"`
	ColorHex   string  `json:"color_hex"`
	Location   string  `json:"location"`
	Slot       int     `json:"slot,omitempty"`
	RemainingG float64 `json:"remaining_g"`
}

// slotIndex maps a printer location to the 1-based slot number of each spool ID
// listed for it. Non-printer locations are absent, so lookups there yield 0.
type slotIndex map[string]map[int]int

// buildSlotIndex derives slot numbers from the locations_spoolorders setting,
// using the same rule the text renderer does: position within the location's
// ordered ID list, 1-based.
//
// The index is keyed by location rather than flattened to a global ID -> slot
// map, because Spoolman does not reliably remove a spool's ID from its old
// location's order list when it moves. A flattened map would let a stale entry
// under one location decide the slot for a spool that actually lives under
// another, and since Go randomizes map iteration the winner would differ
// between runs. Keying by location makes slotOf consult only the list belonging
// to the spool's real location — the same cross-check the text renderer does
// when it looks the spool up in spoolsByLoc.
//
// PadToCapacity is deliberately not applied here. It only appends EmptySlot
// entries to reach the configured capacity, so it cannot shift the position of
// any real ID — and the padding exists to render trailing "(empty)" rows, which
// have no counterpart in the export.
func buildSlotIndex(orders map[string][]int) slotIndex {
	idx := make(slotIndex)
	for loc, ids := range orders {
		if !IsPrinterLocation(loc) {
			continue
		}
		m := make(map[int]int, len(ids))
		for i, id := range ids {
			if id == EmptySlot || id == 0 {
				continue
			}
			// A duplicated ID within one list is drift too; take the first
			// position so the result does not depend on iteration order.
			if _, dup := m[id]; !dup {
				m[id] = i + 1
			}
		}
		idx[loc] = m
	}
	return idx
}

// slotOf returns the 1-based slot for the spool at loc, or 0 when loc is not a
// printer location or the spool is not listed under it.
func (s slotIndex) slotOf(loc string, id int) int {
	return s[loc][id]
}

// toExport flattens a FindSpool into the export shape. ColorHex is normalized
// to a leading '#'. slot is the spool's 1-based printer slot, or 0 when it is
// not in a printer location.
//
// Every string field is run through models.Sanitize, not just Location: the
// JSON encoder would escape control characters either way, but a consumer that
// echoes name or vendor to a terminal would then replay whatever escape
// sequence Spoolman happened to store. Sanitize only removes control characters
// and ANSI escapes, so for the plain-text values locations actually hold it is
// an identity transform and the exported Location stays usable as a key. A
// location that did contain control characters would differ from the stored
// value — but it would also be unusable in the text renderer, which sanitizes
// the same way.
func toExport(s models.FindSpool, slot int) spoolExport {
	hex := strings.TrimSpace(models.Sanitize(s.Filament.ColorHex))
	if hex != "" && !strings.HasPrefix(hex, "#") {
		hex = "#" + hex
	}
	return spoolExport{
		ID:         s.Id,
		Name:       models.Sanitize(s.Filament.Name),
		Vendor:     models.Sanitize(s.Filament.Vendor.Name),
		Material:   models.Sanitize(s.Filament.Material),
		ColorHex:   hex,
		Location:   models.Sanitize(s.Location),
		Slot:       slot,
		RemainingG: s.RemainingWeight,
	}
}

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

	jsonOut, _ := cmd.Flags().GetBool("json")
	var jsonSpools []spoolExport
	// One spool can match several search terms (`fil find blue matte`). The text
	// path prints it once under each term's header, where the repeat is legible
	// as such; a flat JSON array has no headers, so the same spool would just
	// appear twice and silently inflate any consumer that counts spools or sums
	// remaining_g. Emit each spool at most once, keeping first-match order.
	jsonSeen := map[int]bool{}
	// ΔE per exported spool, kept across all search terms so --near can be
	// re-sorted globally at the end. Sorting per term would emit N locally
	// ranked runs in one flat array, which reads as a single ranking and is not.
	jsonDeltas := map[int]float64{}

	// All result output goes through out (stdout) and all progress chatter
	// through msgs (stderr). Nothing in this function may write to os.Stdout
	// directly, or `--json` stops being machine-parseable — and tests that
	// capture via cmd.SetOut stop being able to prove it.
	out := cmd.OutOrStdout()
	msgs := cmd.ErrOrStderr()

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
	// Slot numbers come from the same orders map the text renderer uses, so the
	// exported slot and the printed "AMS B:4" label can never disagree.
	slots := buildSlotIndex(orders)

	query, filters, err := buildFindQuery(cmd)
	if err != nil {
		return err
	}

	// Progress chatter goes to stderr, not stdout, so that `--json` leaves stdout
	// holding nothing but the JSON document.
	if location, _ := cmd.Flags().GetString("location"); location != "" {
		_, _ = fmt.Fprintf(msgs, "Filtering by location: %s\n", query["location"])
	}

	if needed, _ := cmd.Flags().GetBool("needed"); needed {
		_, _ = fmt.Fprintln(msgs, "Filtering by spools needed by projects")
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
		scanned, err := readOneScanForFind(ctx, msgs)
		if err != nil {
			return err
		}
		c, ok := parseHexColor(scanned.Color)
		if !ok {
			return fmt.Errorf("scanned color %q is not parseable", scanned.Color)
		}
		target = c
		_, _ = fmt.Fprintf(msgs, "Scanned color: %s %s\n", scanned.Color, models.GetColorBlock(scanned.Color, ""))
		if scanned.HasTD {
			_, _ = fmt.Fprintf(msgs, "Scanned TD:    %.2fmm\n", scanned.TD)
		}
		_, _ = fmt.Fprintln(msgs)
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

		if jsonOut {
			for _, s := range spools {
				if jsonSeen[s.Id] {
					continue
				}
				jsonSeen[s.Id] = true
				if useNear {
					jsonDeltas[s.Id] = deltas[s.Id]
				}
				jsonSpools = append(jsonSpools, toExport(s, slots.slotOf(s.Location, s.Id)))
			}
			continue
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
			_, _ = color.New(color.FgHiRed).Fprint(out, foundMsg)
		} else {
			// print in green
			_, _ = color.New(color.FgGreen).Fprint(out, foundMsg)
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
				_, _ = fmt.Fprintf(out, " %s  %s %s\n", deltaStr, boldLabel, s.StringNoLocation())
				if showPurchase {
					_, _ = fmt.Fprintf(out, "    %s\n", amazonLink(s.Filament.Vendor.Name, s.Filament.Name))
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
					_, _ = fmt.Fprintf(out, " %s %s\n", boldLabel, dimmed("(empty)"))
				} else {
					_, _ = fmt.Fprintf(out, " %s %s\n", boldLabel, ls.spool.StringNoLocation())
					if showPurchase {
						_, _ = fmt.Fprintf(out, "    %s\n", amazonLink(ls.spool.Filament.Vendor.Name, ls.spool.Filament.Name))
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
				_, _ = fmt.Fprintf(out, " %s %s\n", boldLabel, s.StringNoLocation())
				if showPurchase {
					_, _ = fmt.Fprintf(out, "%s\n", amazonLink(s.Filament.Vendor.Name, s.Filament.Name))
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

			_, _ = fmt.Fprintf(out,
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

	if jsonOut {
		if jsonSpools == nil {
			jsonSpools = []spoolExport{}
		}
		// --near ranks per search term inside the loop above. The text path shows
		// each term under its own header so the grouping is visible, but a flat
		// array is read as one ranking, so re-rank globally and apply --limit once
		// across the whole result rather than once per term.
		//
		// Truncating per term first and again here still yields the exact global
		// top-N, so this is not an approximation: if a spool belongs in the global
		// top-N then fewer than N spools beat it overall, so fewer than N beat it
		// within its own term too, so it cannot have been dropped by that term's
		// truncation. Don't "fix" this by deferring the per-term cut — it would
		// only grow the intermediate slice.
		if useNear {
			sort.SliceStable(jsonSpools, func(i, j int) bool {
				return jsonDeltas[jsonSpools[i].ID] < jsonDeltas[jsonSpools[j].ID]
			})
			if len(jsonSpools) > limit {
				jsonSpools = jsonSpools[:limit]
			}
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		// Deliberately propagated, unlike the text path's `_, _ = fmt.Fprintf`.
		// A truncated JSON document is worse than a truncated table: it is
		// unparseable, and a consumer piping into jq deserves a non-zero exit
		// rather than a silent partial read. Note a closed stdout pipe (`| head`)
		// raises SIGPIPE and never reaches here, so this covers real write
		// failures such as a full disk on a redirect.
		return enc.Encode(jsonSpools)
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
	addFindFlags(findCmd)
}

// addFindFlags registers the find command's flags. Split out of init() so tests
// can build a fresh command with clean flag state instead of mutating the
// package-level findCmd across cases.
func addFindFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("diameter", "d", "1.75", "filter by diameter, default is 1.75mm, '*' for all")
	cmd.Flags().StringP("manufacturer", "m", "", "filter by manufacturer, default is all")
	cmd.Flags().BoolP("allowed-archived", "a", false, "show archived spools, default is false")
	cmd.Flags().Bool("archived-only", false, "show only archived spools, default is false")
	cmd.Flags().Bool("has-comment", false, "show only spools with comments, default is false")
	cmd.Flags().StringP("comment", "c", "", "find spools with a comment matching the provided value")
	cmd.Flags().BoolP("used", "u", false, "show only spools that have been used")
	cmd.Flags().BoolP("pristine", "p", false, "show only (pristine) spools that have not been used")
	cmd.Flags().StringP("location", "l", "", "filter by location, default is all")
	cmd.Flags().Bool("lru", false, "sort by least recently used first; never-used appear last")
	cmd.Flags().Bool("mru", false, "sort by most recently used first; never-used appear last")
	cmd.Flags().Bool("purchase", false, "show purchase link for each spool")
	cmd.Flags().BoolP("needed", "n", false, "show only spools for filaments that are needed by plans but not loaded")
	cmd.Flags().String("material", "", "filter by material substring (e.g. 'pla' matches 'PLA', 'Matte PLA', 'Silk PLA'). Comma-separated for multiple (case-insensitive)")
	cmd.Flags().String("near", "", "sort results by CIEDE2000 ΔE distance from a target hex color (e.g. '#ff5500'); pairs with --limit")
	cmd.Flags().Int("limit", 10, "when --near or --scan is set, show only the N nearest results (must be positive)")
	cmd.Flags().Bool("scan", false, "read one color from an attached TD-1 scanner and rank spools by ΔE against it")
	cmd.Flags().Bool("json", false, "output matching spools as JSON (id, name, vendor, material, color_hex, location, slot, remaining_g) instead of text; a spool matching several search terms is emitted once")
}

// readOneScanForFind opens the TD-1, performs the handshake, reads a single
// scan, and returns the result. Only used by `fil find --scan`. The prompt is
// written to msgs (stderr) so it never contaminates `--json` output on stdout.
func readOneScanForFind(ctx context.Context, msgs io.Writer) (devices.ScanResult, error) {
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

	_, _ = fmt.Fprintln(msgs, "Insert a filament sample into the TD-1 to scan...")
	return devices.ReadScan(ctx, port, 0)
}
