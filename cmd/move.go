/*
Copyright © 2025 David Stockton <dave@davidstockton.com>
*/
package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// moveCmd represents the move command.
var moveCmd = &cobra.Command{
	Use:     "move",
	Short:   "Moves a spool to a new Location",
	Long:    `Moves a spool to a new Location.`,
	RunE:    runMove,
	Aliases: []string{"mv", "m", "mov"},
}

type move struct {
	spoolId int
	spool   models.FindSpool
	from    string
	to      string // may include slot shorthand like "A:2"; resolve later
	dest    DestSpec
	err     error
}

type DestSpec struct {
	Location string
	pos      int  // 1-based desired position
	hasPos   bool // whether a position was provided
}

func (m move) String() string {
	if m.err != nil {
		return fmt.Sprintf("not moving %d: %s", m.spoolId, m.err)
	}

	return fmt.Sprintf("Moving #%d from %s to %s", m.spoolId, m.from, m.to)
}

func runMove(cmd *cobra.Command, args []string) error {
	if Cfg == nil || Cfg.ApiBase == "" {
		return errors.New("apiClient endpoint not configured")
	}

	apiClient := api.NewClient(Cfg.ApiBase)

	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return err
	}
	// Extra debug info is only shown when both --dry-run and --debug are provided
	debugMode, _ := cmd.Flags().GetBool("debug")

	// optional limiter for searching by name
	allFrom, err := cmd.Flags().GetString("from")
	if err != nil {
		return err
	}

	// interactive mode control
	nonInteractive, err := cmd.Flags().GetBool("non-interactive")
	if err != nil {
		return err
	}
	simpleSelect, _ := cmd.Flags().GetBool("simple-select")
	allowInteractive := isInteractiveAllowed(nonInteractive)

	// Optional default destination (can include :pos or @pos)
	allTo, err := cmd.Flags().GetString("destination")
	if err != nil {
		return err
	}

	var (
		errs  error
		moves []move
	)

	// Parse args into move specs (spool selector then destination token if --destination not given)
	for i := 0; i < len(args); i++ {
		spoolSelector := args[i]

		destination := allTo
		if destination == "" {
			if i+1 < len(args) {
				destination = args[i+1]
				i++
			} else {
				errs = errors.Join(errs, errors.New("destination must be specified if not using --destination/-d"))
				break
			}
		}

		dspec, derr := ParseDestSpec(destination)
		if derr != nil {
			// mark this move as errored but continue parsing others
			moves = append(moves, move{spoolId: -1, to: destination, dest: dspec, err: derr})
			continue
		}

		// Resolve the spool ID from selector (ID or name)
		spoolId := -1
		if id, iderr := strconv.Atoi(spoolSelector); iderr == nil {
			spoolId = id
		} else {
			query := make(map[string]string)
			if allFrom != "" {
				query["location"] = allFrom
			}
			spools, lookupErr := apiClient.FindSpoolsByName(spoolSelector, nil, query)
			if lookupErr != nil {
				errs = errors.Join(errs, fmt.Errorf("error looking up spool '%s': %w", spoolSelector, lookupErr))
				moves = append(moves, move{spoolId: -1, to: destination, dest: dspec, err: lookupErr})
				continue
			}
			if len(spools) == 0 {
				theErr := fmt.Errorf("spool not found: %s", spoolSelector)
				errs = errors.Join(errs, theErr)
				moves = append(moves, move{spoolId: -1, to: destination, dest: dspec, err: theErr})
				continue
			}
			if len(spools) != 1 {
				if allowInteractive {
					// let the user pick from a broad list honoring any filters (e.g., Location)
					chosen, canceled, selErr := selectSpoolInteractively(apiClient, spoolSelector, query, spools, simpleSelect)
					if selErr != nil {
						errs = errors.Join(errs, fmt.Errorf("selection error: %w", selErr))
						moves = append(moves, move{spoolId: -1, to: destination, dest: dspec, err: selErr})
						continue
					}
					if canceled {
						// abort entire operation before executing any changes
						return errors.New("selection canceled; no moves executed")
					}
					spoolId = chosen.Id
				} else {
					theErr := fmt.Errorf("multiple spools found (%d): %s", len(spools), spoolSelector)
					errs = errors.Join(errs, theErr)
					moves = append(moves, move{spoolId: -1, to: destination, dest: dspec, err: theErr})
					continue
				}
			} else {
				spoolId = spools[0].Id
			}
		}

		moves = append(moves, move{spoolId: spoolId, to: destination, dest: dspec})
	}

	// Resolve current spools and fill from/source
	for i, m := range moves {
		if m.err != nil || m.spoolId <= 0 {
			continue
		}
		spool, findErr := apiClient.FindSpoolsById(m.spoolId)
		if errors.Is(findErr, api.ErrSpoolNotFound) {
			theErr := fmt.Errorf("spool #%d not found", m.spoolId)
			errs = errors.Join(errs, theErr)
			moves[i].err = theErr
			continue
		}
		if findErr != nil {
			theErr := fmt.Errorf("error finding spool: %w", findErr)
			errs = errors.Join(errs, theErr)
			moves[i].err = theErr
			continue
		}
		moves[i].from = spool.Location
		moves[i].spool = *spool
	}

	// Load current locations_spoolorders
	orders, loadErr := LoadLocationOrders(apiClient)
	if loadErr != nil {
		return loadErr
	}

	// Track before/after for touched destinations for dry-run output
	before := map[string][]int{}
	touched := map[string]struct{}{}

	// Apply moves to orders in-memory (remove from any existing list; insert/append at destination)
	for _, m := range moves {
		if m.err != nil || m.spoolId <= 0 {
			continue
		}

		// Snapshot before state once per touched destination Location
		if _, ok := touched[m.dest.Location]; !ok {
			before[m.dest.Location] = append([]int(nil), orders[m.dest.Location]...)
			touched[m.dest.Location] = struct{}{}
		}

		// Remove ID from all lists to avoid duplicates in settings
		orders = RemoveFromAllOrders(orders, m.spoolId)

		// Insert/append into destination
		list := orders[m.dest.Location]
		if m.dest.hasPos {
			p := m.dest.pos
			if p < 1 {
				p = 1
			}
			if p > len(list)+1 {
				p = len(list) + 1
			}
			idx := p - 1
			list = InsertAt(list, idx, m.spoolId)
		} else {
			list = append(list, m.spoolId)
		}
		orders[m.dest.Location] = list
	}

	// Execute
	if dryRun {
		fmt.Println("Dry run:")
		for _, m := range moves {
			if m.err != nil {
				fmt.Printf("Skipping due to error - %s\n", m)
				continue
			}
			to := m.dest.Location
			label := to
			if to == "" {
				label = "<empty>"
			}
			fmt.Printf("Moving %s to %s", m.spool, label)
			if m.dest.hasPos {
				fmt.Printf(" (slot %d)", m.dest.pos)
			}
			fmt.Println()

			// Extra debug to clarify index math and source removal
			// Show where the spool was in the destination list before removal (if present),
			// and what index (0-based) we will insert at after clamping.
			if debugMode {
				beforeList := before[to]
				srcIdx := indexOf(beforeList, m.spoolId)
				clampedPos := m.dest.pos
				if !m.dest.hasPos {
					clampedPos = len(beforeList) + 1 // append semantics
				}
				if clampedPos < 1 {
					clampedPos = 1
				}
				if clampedPos > len(beforeList)+1 {
					clampedPos = len(beforeList) + 1
				}
				insIdx := clampedPos - 1
				fmt.Printf("  debug: dest before=%v, contains #%d at slot=%d (idx=%d); requested slot=%d -> clamped slot=%d (idx=%d)\n",
					beforeList, m.spoolId, srcIdx+1, srcIdx, m.dest.pos, clampedPos, insIdx)
			}
		}
		// Show per-Location before/after
		for loc := range touched {
			label := loc
			if label == "" {
				label = "<empty>"
			}
			fmt.Printf("%s before: %v\n", label, before[loc])
			fmt.Printf("%s after:  %v\n", label, orders[loc])
		}

		cmd.SilenceUsage = true
		return errs
	}

	// Persist settings first so UI order reflects immediately
	if err := apiClient.PostSettingObject("locations_spoolorders", orders); err != nil {
		return fmt.Errorf("failed to update locations_spoolorders: %w", err)
	}

	// Then update each spool's Location
	for _, m := range moves {
		if m.err != nil || m.spoolId <= 0 {
			continue
		}
		to := m.dest.Location
		if moveErr := apiClient.MoveSpool(m.spoolId, to); moveErr != nil {
			color.Red("Error moving spool %s: %v\n", m.spool, moveErr)
			errs = errors.Join(errs, fmt.Errorf("error moving spool %s: %w", m.spool, moveErr))
			continue
		}
		label := to
		if label == "" {
			label = "nowhere"
		}
		if m.dest.hasPos {
			fmt.Printf("Moved %s to %s slot %d\n", m.spool, label, m.dest.pos)
		} else {
			fmt.Printf("Moved %s to %s\n", m.spool, label)
		}
	}

	cmd.SilenceUsage = true
	return errs
}

// MapToAlias maps a Location alias to a Location name. If it's not found in the map, it returns the original string.
func MapToAlias(to string) string {
	aliasMap := Cfg.LocationAliases
	if aliasMap == nil {
		return to
	}

	if val, ok := aliasMap[strings.ToUpper(to)]; ok {
		return val
	}

	return to
}

// ParseDestSpec parses a destination token that may include a slot, e.g.,
// "A:2", "AMS A@3", or "Shelf 6B" (no slot). Aliases apply only to the
// Location part (before the separator). Positions are 1-based.
func ParseDestSpec(input string) (DestSpec, error) {
	in := strings.TrimSpace(input)
	if in == "" {
		return DestSpec{Location: ""}, nil
	}
	if strings.EqualFold(in, "<empty>") {
		return DestSpec{Location: ""}, nil
	}
	// Find last occurrence of either ':' or '@'
	idx := strings.LastIndexAny(in, "@:")
	if idx <= 0 || idx == len(in)-1 { // no separator or nothing after it
		return DestSpec{Location: MapToAlias(in)}, nil
	}

	locPart := strings.TrimSpace(in[:idx])
	posPart := strings.TrimSpace(in[idx+1:])
	if strings.EqualFold(locPart, "<empty>") {
		locPart = ""
	}

	// If the pos part isn't a valid int, treat as pure Location
	p, err := strconv.Atoi(posPart)
	if err != nil {
		return DestSpec{Location: MapToAlias(in)}, nil
	}

	return DestSpec{Location: MapToAlias(locPart), pos: p, hasPos: true}, nil
}

// LoadLocationOrders reads the settings entry 'locations_spoolorders' and
// returns it as a map[Location][]spoolID. If not set or empty, returns an
// empty map.
func LoadLocationOrders(apiClient *api.Client) (map[string][]int, error) {
	settings, err := apiClient.GetSettings()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch settings: %w", err)
	}
	entry, ok := settings["locations_spoolorders"]
	if !ok {
		return map[string][]int{}, nil
	}

	var rawString string
	if err := json.Unmarshal(entry.Value, &rawString); err != nil {
		return nil, fmt.Errorf("failed to decode settings value wrapper: %w", err)
	}
	if rawString == "" {
		return map[string][]int{}, nil
	}
	var orders map[string][]int
	if err := json.Unmarshal([]byte(rawString), &orders); err != nil {
		return nil, fmt.Errorf("failed to parse locations_spoolorders JSON: %w", err)
	}
	if orders == nil {
		orders = map[string][]int{}
	}
	return orders, nil
}

// RemoveFromAllOrders removes id from every Location list to avoid duplicates.
func RemoveFromAllOrders(orders map[string][]int, id int) map[string][]int {
	for loc, ids := range orders {
		kept := make([]int, 0, len(ids))
		for _, v := range ids {
			if v != id {
				kept = append(kept, v)
			}
		}
		orders[loc] = kept
	}
	return orders
}

// InsertAt inserts val at index i (0-based) into slice s, shifting elements to the right.
func InsertAt(s []int, i int, val int) []int {
	if i < 0 {
		i = 0
	}
	if i > len(s) {
		i = len(s)
	}
	s = append(s, 0)
	copy(s[i+1:], s[i:])
	s[i] = val
	return s
}

// indexOf returns the 0-based index of val in s, or -1 if not found.
func indexOf(s []int, val int) int {
	for i, v := range s {
		if v == val {
			return i
		}
	}
	return -1
}

func init() {
	rootCmd.AddCommand(moveCmd)

	moveCmd.Flags().Bool("dry-run", false, "show what would be moved, but don't actually move anything")
	moveCmd.Flags().StringP("destination", "d", "", "destination for all spools (supports alias and optional :slot or @slot)")
	moveCmd.Flags().StringP("from", "f", "", "source Location for all spools (limits name lookups)")
	// Note: slot is specified inline as part of the destination token (e.g., A:2); no separate --slot flag.
	moveCmd.Flags().Bool("debug", false, "show extra debug details in --dry-run output")
	moveCmd.Flags().BoolP("non-interactive", "n", false, "do not prompt; if multiple spools match, behave as current non-interactive error behavior")
	moveCmd.Flags().Bool("simple-select", false, "use a basic numbered selector instead of interactive menu (fallback for limited terminals)")
}
