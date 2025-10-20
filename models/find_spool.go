package models

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
)

type FindSpool struct {
	Id         int       `json:"id"`
	Registered time.Time `json:"registered"`
	FirstUsed  time.Time `json:"first_used"`
	LastUsed   time.Time `json:"last_used"`
	Filament   struct {
		Id         int       `json:"id"`
		Registered time.Time `json:"registered"`
		Name       string    `json:"name"`
		Vendor     struct {
			Id         int       `json:"id"`
			Registered time.Time `json:"registered"`
			Name       string    `json:"name"`
			Extra      struct {
			} `json:"extra"`
		} `json:"vendor"`
		Material            string  `json:"material"`
		Price               float64 `json:"price"`
		Density             float64 `json:"density"`
		Diameter            float64 `json:"diameter"`
		Weight              float64 `json:"weight"`
		SpoolWeight         float64 `json:"spool_weight"`
		ColorHex            string  `json:"color_hex"`
		MultiColorHexes     string  `json:"multi_color_hexes"`
		MultiColorDirection string  `json:"multi_color_direction"`
		Extra               struct {
		} `json:"extra"`
	} `json:"filament"`
	RemainingWeight float64 `json:"remaining_weight"`
	InitialWeight   float64 `json:"initial_weight"`
	SpoolWeight     float64 `json:"spool_weight"`
	UsedWeight      float64 `json:"used_weight"`
	RemainingLength float64 `json:"remaining_length"`
	UsedLength      float64 `json:"used_length"`
	Location        string  `json:"location"`
	Comment         string  `json:"comment"`
	Archived        bool    `json:"archived"`
	Extra           struct {
	} `json:"extra"`
}

func (s FindSpool) String() string {
	//  - AMS B - #127 PolyTerra™ Cotton White (Matte PLA #E6DDDB) - 91.5g remaining, last used 2 days ago (archived)
	archived := ""
	if s.Archived {
		// print archived in red
		archived = color.RGB(200, 0, 0).Sprintf(" (archived)")
	}

	colorBlock := ""

	// Only show color swatches when color output is enabled; otherwise omit them entirely
	if !color.NoColor {
		if s.Filament.ColorHex != "" {
			r, g, b := convertFromHex(s.Filament.ColorHex)
			customColor := color.RGB(r, g, b)

			semiTransparent := len(s.Filament.ColorHex) > 6

			blockChars := "████"
			if semiTransparent {
				blockChars = "▓▓▓▓"
			}

			if semiTransparent {
				customColor.AddBgRGB(255, 255, 255)
				colorBlock = customColor.Sprintf("%s", blockChars) + " "
			} else {
				colorBlock = customColor.Sprintf("%s", blockChars) + " "
			}
		}

		if s.Filament.MultiColorHexes != "" {
			// multicolor is represented by comma-separated hex values
			colors := strings.SplitN(s.Filament.MultiColorHexes, ",", 2)
			r1, g1, b1 := convertFromHex(colors[0])
			r2, g2, b2 := convertFromHex(colors[1])
			colorBlock1 := color.RGB(r1, g1, b1).Sprintf("██")
			colorBlock2 := color.RGB(r2, g2, b2).Sprintf("██")
			colorBlock = colorBlock1 + colorBlock2 + " "
		}
	}
	// Default to not showing the diameter if it's 1.75
	diameter := ""
	if s.Filament.Diameter != 1.75 {
		diameter = color.RGB(200, 128, 0).Sprintf(" %.2fmm", s.Filament.Diameter)
	}

	format := "%s%s - #%d %s %s%s (%s%s) - %.1fg remaining, last used %s%s"

	var lastUsedDuration string
	if s.LastUsed.IsZero() {
		lastUsedDuration = "never"
	} else {
		duration := time.Since(s.LastUsed)
		if duration.Hours() > 24 {
			lastUsedDuration = fmt.Sprintf("%d days ago", int(duration.Truncate(24*time.Hour).Hours())/24)
		} else if duration.Hours() > 1 {
			lastUsedDuration = fmt.Sprintf("%d hours ago", int(duration.Truncate(time.Hour).Hours()))
		} else if duration.Minutes() > 1 {
			lastUsedDuration = fmt.Sprintf("%d minutes ago", int(duration.Truncate(time.Minute).Minutes()))
		} else if duration.Seconds() > 1 {
			lastUsedDuration = fmt.Sprintf("%d seconds ago", int(duration.Truncate(time.Second).Seconds()))
		} else {
			lastUsedDuration = time.Since(s.LastUsed).String() + " ago"
		}
	}

	colorHex := ""
	if s.Filament.ColorHex != "" {
		colorHex = " #" + s.Filament.ColorHex
	}

	location := s.Location
	if location == "" {
		location = "N/A"
	}

	location = color.New(color.Bold).Sprintf(location)

	return fmt.Sprintf(
		format,
		colorBlock,
		location,
		s.Id,
		s.Filament.Vendor.Name,
		s.Filament.Name,
		diameter,
		s.Filament.Material,
		colorHex,
		s.RemainingWeight,
		lastUsedDuration,
		archived,
	)
}

func convertFromHex(hex string) (int, int, int) {
	// convert the hex color like 45FFE0 to rgb integers
	r, _ := strconv.ParseInt(hex[0:2], 16, 16)
	g, _ := strconv.ParseInt(hex[2:4], 16, 16)
	b, _ := strconv.ParseInt(hex[4:6], 16, 16)

	return int(r), int(g), int(b)
}
