package cmd

import (
	"testing"

	"github.com/dstockto/fil/models"
)

func newSpool(id int, hex string) models.FindSpool {
	s := models.FindSpool{Id: id}
	s.Filament.ColorHex = hex
	return s
}

func TestRankSpoolsByDistance(t *testing.T) {
	target, ok := parseHexColor("#ff0000") // red
	if !ok {
		t.Fatal("failed to parse target hex")
	}

	spools := []models.FindSpool{
		newSpool(1, "00ff00"), // green, far
		newSpool(2, "ff0000"), // red, exact
		newSpool(3, ""),       // no color, should go last
		newSpool(4, "ff2200"), // near-red, close
	}

	got := rankSpoolsByDistance(spools, target)
	if len(got) != 4 {
		t.Fatalf("expected 4 spools, got %d", len(got))
	}
	// Exact match first, then near-red, then green, then colorless.
	wantOrder := []int{2, 4, 1, 3}
	for i, id := range wantOrder {
		if got[i].Id != id {
			t.Errorf("position %d: got id %d, want %d", i, got[i].Id, id)
		}
	}
}

func TestFloatSpoolToFront(t *testing.T) {
	spools := []models.FindSpool{
		newSpool(1, ""),
		newSpool(2, ""),
		newSpool(3, ""),
	}
	got := floatSpoolToFront(spools, 2)
	if len(got) != 3 {
		t.Fatalf("expected 3 spools, got %d", len(got))
	}
	want := []int{2, 1, 3}
	for i, id := range want {
		if got[i].Id != id {
			t.Errorf("position %d: got id %d, want %d", i, got[i].Id, id)
		}
	}
}

func TestFloatSpoolToFront_NotFound(t *testing.T) {
	spools := []models.FindSpool{newSpool(1, ""), newSpool(2, "")}
	got := floatSpoolToFront(spools, 99)
	if len(got) != 2 || got[0].Id != 1 || got[1].Id != 2 {
		t.Errorf("expected unchanged order when id missing, got %v", got)
	}
}

func TestCanonScanHex(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"  ":       "",
		"EAD9D4":   "#ead9d4",
		"#EAD9D4":  "#ead9d4",
		"#ead9d4":  "#ead9d4",
		" #ABC123": "#abc123",
	}
	for in, want := range cases {
		if got := canonScanHex(in); got != want {
			t.Errorf("canonScanHex(%q) = %q, want %q", in, got, want)
		}
	}
}
