package cmd

import (
	"reflect"
	"testing"
)

func TestSortedPrinterNames(t *testing.T) {
	cases := []struct {
		name     string
		printers map[string]PrinterConfig
		want     []string
	}{
		{"empty", nil, nil},
		{"single", map[string]PrinterConfig{"Prusa XL": {}}, []string{"Prusa XL"}},
		{
			"several",
			map[string]PrinterConfig{"Prusa XL": {}, "Bambu X1C": {}, "Voron 2.4": {}, "Ender 3": {}},
			[]string{"Bambu X1C", "Ender 3", "Prusa XL", "Voron 2.4"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Go randomizes map iteration, so repeat to catch an unsorted
			// result that happens to come out right once.
			for i := 0; i < 20; i++ {
				got := sortedPrinterNames(tc.printers)
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("run %d: got %v, want %v", i, got, tc.want)
				}
			}
		})
	}
}
