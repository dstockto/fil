package cmd

import (
	"testing"
)

func TestMakeAmazonSearch(t *testing.T) {
	tests := []struct {
		vendor   string
		name     string
		expected string
	}{
		{"Bambu Lab", "PLA Basic", "https://www.amazon.com/s?k=Bambu+Lab+PLA+Basic"},
		{" Overture ", " PETG ", "https://www.amazon.com/s?k=Overture+++PETG"},
		{"", "PLA", "https://www.amazon.com/s?k=PLA"},
		{"Sunlu", "", "https://www.amazon.com/s?k=Sunlu"},
	}

	for _, tt := range tests {
		t.Run(tt.vendor+" "+tt.name, func(t *testing.T) {
			got := makeAmazonSearch(tt.vendor, tt.name)
			if got != tt.expected {
				t.Errorf("makeAmazonSearch(%q, %q) = %q, want %q", tt.vendor, tt.name, got, tt.expected)
			}
		})
	}
}

func TestTermLink(t *testing.T) {
	text := "Click here"
	link := "http://example.com"
	expected := "\x1b]8;;http://example.com\x1b\\Click here\x1b]8;;\x1b\\"
	got := termLink(text, link)
	if got != expected {
		t.Errorf("termLink(%q, %q) = %q, want %q", text, link, got, expected)
	}
}

func TestAmazonLink(t *testing.T) {
	vendor := "eSUN"
	name := "PLA+"
	// makeAmazonSearch("eSUN", "PLA+") -> "https://www.amazon.com/s?k=eSUN+PLA%2B"
	expectedSearch := "https://www.amazon.com/s?k=eSUN+PLA%2B"
	expected := termLink(expectedSearch, expectedSearch)
	got := amazonLink(vendor, name)
	if got != expected {
		t.Errorf("amazonLink(%q, %q) = %q, want %q", vendor, name, got, expected)
	}
}
