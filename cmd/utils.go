package cmd

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/dstockto/fil/api"
)

// MapToAlias maps a Location alias to a Location name. If it's not found in the map, it returns the original string.
func MapToAlias(to string) string {
	if Cfg == nil {
		return to
	}
	aliasMap := Cfg.LocationAliases
	if aliasMap == nil {
		return to
	}

	if val, ok := aliasMap[strings.ToUpper(to)]; ok {
		return val
	}

	return to
}

type DestSpec struct {
	Location string
	pos      int
	hasPos   bool
}

func (d DestSpec) String() string {
	if !d.hasPos {
		return d.Location
	}
	return fmt.Sprintf("%s:%d", d.Location, d.pos)
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

// RoundAmount rounds a float64 to one decimal place using RoundToEven.
func RoundAmount(amount float64) float64 {
	return math.RoundToEven(amount*10) / 10
}

// ToProjectName converts a filename or string to a project name by replacing dashes
// and underscores with spaces and capitalizing each word.
func ToProjectName(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
		}
	}
	return strings.Join(words, " ")
}

// TruncateFront truncates a string from the front if it exceeds maxLen.
func TruncateFront(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[len(s)-maxLen:]
	}
	return "..." + s[len(s)-maxLen+3:]
}

// ResolveLowThreshold resolves the custom threshold for a filament.
func ResolveLowThreshold(vendor string, filamentName string) float64 {
	// Default to 0 if not configured.
	thr := 0.0

	if Cfg != nil && Cfg.LowThresholds != nil {
		lvendor := strings.ToLower(strings.TrimSpace(vendor))
		lname := strings.ToLower(strings.TrimSpace(filamentName))

		// First pass: check vendor::name patterns (more specific)
		for k, v := range Cfg.LowThresholds {
			if k == "" || v <= 0 {
				continue
			}

			lk := strings.ToLower(strings.TrimSpace(k))
			if !strings.Contains(lk, "::") {
				continue
			}

			parts := strings.SplitN(lk, "::", 2)
			vendPart := strings.TrimSpace(parts[0])
			namePart := strings.TrimSpace(parts[1])
			if vendPart == "" || namePart == "" {
				continue
			}

			if strings.Contains(lvendor, vendPart) && strings.Contains(lname, namePart) {
				return v
			}
		}

		// Second pass: name-only fallback
		for k, v := range Cfg.LowThresholds {
			if k == "" || v <= 0 {
				continue
			}

			lk := strings.ToLower(strings.TrimSpace(k))
			if strings.Contains(lk, "::") {
				continue
			}

			if strings.Contains(lname, lk) {
				return v
			}
		}
	}

	return thr
}
