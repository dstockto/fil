package cmd

import (
	"math"
	"strings"

	"github.com/dstockto/fil/models"
	"github.com/lucasb-eyer/go-colorful"
)

// parseHexColor accepts a hex color with or without a leading '#', in 3- or
// 6-digit form. An optional 2-character alpha suffix is ignored. Returns false
// when the input cannot be parsed as a color.
func parseHexColor(hex string) (colorful.Color, bool) {
	s := strings.TrimSpace(hex)
	if s == "" {
		return colorful.Color{}, false
	}
	if !strings.HasPrefix(s, "#") {
		s = "#" + s
	}
	if len(s) == 4 {
		s = "#" + string(s[1]) + string(s[1]) + string(s[2]) + string(s[2]) + string(s[3]) + string(s[3])
	}
	if len(s) > 7 {
		s = s[:7]
	}
	c, err := colorful.Hex(s)
	if err != nil {
		return colorful.Color{}, false
	}
	return c, true
}

// spoolColorDistance returns the minimum CIEDE2000 ΔE between target and the
// spool's primary color or any of its multi-color components. The value is
// scaled to the conventional 0–100 range (go-colorful's native output uses
// 0–1 L* and must be multiplied by 100). Returns math.Inf(1) if the spool
// has no parseable color.
func spoolColorDistance(s models.FindSpool, target colorful.Color) float64 {
	best := math.Inf(1)
	consider := func(hex string) {
		if c, ok := parseHexColor(hex); ok {
			d := target.DistanceCIEDE2000(c) * 100
			if d < best {
				best = d
			}
		}
	}
	consider(s.Filament.ColorHex)
	if s.Filament.MultiColorHexes != "" {
		for _, h := range strings.Split(s.Filament.MultiColorHexes, ",") {
			consider(h)
		}
	}
	return best
}
