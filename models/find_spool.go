package models

import (
	"fmt"
	"strconv"
	"strings"
	"time"
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
		archived = " \x1b[38;2;200;0;0m(archived)\x1b[0m"
	}
	colorBlock := ""
	if s.Filament.ColorHex != "" {
		r, g, b := convertFromHex(s.Filament.ColorHex)
		blockChars := "████"
		if len(s.Filament.ColorHex) > 6 {
			blockChars = "▓▓▓▓"
		}
		colorBlock = fmt.Sprintf("\x1b[48;2;255;255;255m\x1b[38;2;%d;%d;%dm%s\x1b[0m ", r, g, b, blockChars)
	}
	if s.Filament.MultiColorHexes != "" {
		// multicolor is represented by comma-separated hex values
		colors := strings.SplitN(s.Filament.MultiColorHexes, ",", 2)
		r1, g1, b1 := convertFromHex(colors[0])
		r2, g2, b2 := convertFromHex(colors[1])
		colorBlock1 := fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", r1, g1, b1, "██")
		colorBlock2 := fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m ", r2, g2, b2, "██")
		colorBlock = colorBlock1 + colorBlock2
	}
	// Default to not showing the diameter if it's 1.75
	diameter := ""
	if s.Filament.Diameter != 1.75 {
		diameter = fmt.Sprintf(" \x1b[38;2;200;128;0m(%.2fmm)\x1b[0m", s.Filament.Diameter)
	}

	format := "%s%s - #%d %s%s (%s%s) - %.1fg remaining, last used %s%s"
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
	location = fmt.Sprintf("\033[1m%s\033[0m", location)

	return fmt.Sprintf(format, colorBlock, location, s.Id, s.Filament.Name, diameter, s.Filament.Material, colorHex, s.RemainingWeight, lastUsedDuration, archived)
}

func convertFromHex(hex string) (int, int, int) {
	// convert the hex color like 45FFE0 to rgb integers
	r, _ := strconv.ParseInt(hex[0:2], 16, 8)
	g, _ := strconv.ParseInt(hex[2:4], 16, 8)
	b, _ := strconv.ParseInt(hex[4:6], 16, 8)
	return int(r), int(g), int(b)
}
