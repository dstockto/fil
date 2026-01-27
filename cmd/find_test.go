package cmd

import (
	"testing"
	"time"

	"github.com/dstockto/fil/models"
	"github.com/spf13/cobra"
)

func TestFindFilters(t *testing.T) {
	tests := []struct {
		name     string
		filter   func(models.FindSpool) bool
		spool    models.FindSpool
		expected bool
	}{
		{
			"standard filament - match",
			onlyStandardFilament,
			models.FindSpool{Filament: struct {
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
			}{Diameter: 1.75}},
			true,
		},
		{
			"standard filament - no match",
			onlyStandardFilament,
			models.FindSpool{Filament: struct {
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
			}{Diameter: 2.85}},
			false,
		},
		{
			"ultimaker filament - match",
			ultimakerFilament,
			models.FindSpool{Filament: struct {
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
			}{Diameter: 2.85}},
			true,
		},
		{
			"archived only - match",
			archivedOnly,
			models.FindSpool{Archived: true},
			true,
		},
		{
			"archived only - no match",
			archivedOnly,
			models.FindSpool{Archived: false},
			false,
		},
		{
			"comment filter - wildcard match",
			getCommentFilter("*"),
			models.FindSpool{Comment: "some comment"},
			true,
		},
		{
			"comment filter - wildcard no match",
			getCommentFilter("*"),
			models.FindSpool{Comment: ""},
			false,
		},
		{
			"comment filter - specific match",
			getCommentFilter("test"),
			models.FindSpool{Comment: "This is a Test comment"},
			true,
		},
		{
			"comment filter - specific no match",
			getCommentFilter("test"),
			models.FindSpool{Comment: "Something else"},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.filter(tt.spool) != tt.expected {
				t.Errorf("%s: filter failed, expected %v", tt.name, tt.expected)
			}
		})
	}
}

func TestAggregateFilter(t *testing.T) {
	f1 := func(s models.FindSpool) bool { return s.Id > 10 }
	f2 := func(s models.FindSpool) bool { return s.Archived == false }

	agg := aggregateFilter(f1, f2)

	if !agg(models.FindSpool{Id: 11, Archived: false}) {
		t.Error("aggregateFilter should have returned true for Id:11, Archived:false")
	}
	if agg(models.FindSpool{Id: 9, Archived: false}) {
		t.Error("aggregateFilter should have returned false for Id:9")
	}
	if agg(models.FindSpool{Id: 11, Archived: true}) {
		t.Error("aggregateFilter should have returned false for Archived:true")
	}
}

func TestBuildFindQueryFilters(t *testing.T) {
	tests := []struct {
		name     string
		flags    map[string]string
		spool    models.FindSpool
		expected bool
	}{
		{
			"archived only - filter match",
			map[string]string{"archived-only": "true"},
			models.FindSpool{Archived: true, Filament: struct {
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
			}{Diameter: 1.75}},
			true,
		},
		{
			"archived only - filter no match",
			map[string]string{"archived-only": "true"},
			models.FindSpool{Archived: false, Filament: struct {
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
			}{Diameter: 1.75}},
			false,
		},
		{
			"used - filter match",
			map[string]string{"used": "true"},
			models.FindSpool{UsedWeight: 10.0, Filament: struct {
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
			}{Diameter: 1.75}},
			true,
		},
		{
			"used - filter no match",
			map[string]string{"used": "true"},
			models.FindSpool{UsedWeight: 0.0, Filament: struct {
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
			}{Diameter: 1.75}},
			false,
		},
		{
			"pristine - filter match",
			map[string]string{"pristine": "true"},
			models.FindSpool{UsedWeight: 0.0, Filament: struct {
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
			}{Diameter: 1.75}},
			true,
		},
		{
			"pristine - filter no match",
			map[string]string{"pristine": "true"},
			models.FindSpool{UsedWeight: 5.0, Filament: struct {
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
			}{Diameter: 1.75}},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.Flags().String("diameter", "1.75", "")
			cmd.Flags().String("manufacturer", "", "")
			cmd.Flags().Bool("allowed-archived", false, "")
			cmd.Flags().Bool("archived-only", false, "")
			cmd.Flags().Bool("has-comment", false, "")
			cmd.Flags().String("comment", "", "")
			cmd.Flags().Bool("used", false, "")
			cmd.Flags().Bool("pristine", false, "")
			cmd.Flags().String("location", "", "")

			for k, v := range tt.flags {
				_ = cmd.Flags().Set(k, v)
			}

			_, filters, _ := buildFindQuery(cmd)
			agg := aggregateFilter(filters...)
			got := agg(tt.spool)
			if got != tt.expected {
				t.Errorf("%s: expected %v, got %v (spool: %+v)", tt.name, tt.expected, got, tt.spool)
			}
		})
	}
}
