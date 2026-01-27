package cmd

import (
	"testing"
)

func TestMapToAlias(t *testing.T) {
	// Setup Cfg for testing
	oldCfg := Cfg
	defer func() { Cfg = oldCfg }()

	Cfg = &Config{
		LocationAliases: map[string]string{
			"A": "AMS A",
			"B": "AMS B",
		},
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"A", "AMS A"},
		{"a", "AMS A"},
		{"B", "AMS B"},
		{"b", "AMS B"},
		{"C", "C"},
		{"", ""},
		{"AMS A", "AMS A"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			actual := MapToAlias(tt.input)
			if actual != tt.expected {
				t.Errorf("MapToAlias(%q) = %q, want %q", tt.input, actual, tt.expected)
			}
		})
	}
}

func TestParseDestSpec(t *testing.T) {
	// Setup Cfg for testing
	oldCfg := Cfg
	defer func() { Cfg = oldCfg }()

	Cfg = &Config{
		LocationAliases: map[string]string{
			"A": "AMS A",
		},
	}

	tests := []struct {
		input    string
		expected DestSpec
	}{
		{"A", DestSpec{Location: "AMS A", pos: 0, hasPos: false}},
		{"A:1", DestSpec{Location: "AMS A", pos: 1, hasPos: true}},
		{"AMS A:2", DestSpec{Location: "AMS A", pos: 2, hasPos: true}},
		{"B:3", DestSpec{Location: "B", pos: 3, hasPos: true}},
		{"Shelf 6B", DestSpec{Location: "Shelf 6B", pos: 0, hasPos: false}},
		{"<empty>", DestSpec{Location: "", pos: 0, hasPos: false}},
		{"", DestSpec{Location: "", pos: 0, hasPos: false}},
		{"A@4", DestSpec{Location: "AMS A", pos: 4, hasPos: true}},
		{"Location With Space:5", DestSpec{Location: "Location With Space", pos: 5, hasPos: true}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			actual, err := ParseDestSpec(tt.input)
			if err != nil {
				t.Errorf("ParseDestSpec(%q) returned error: %v", tt.input, err)
			}
			if actual.Location != tt.expected.Location || actual.pos != tt.expected.pos || actual.hasPos != tt.expected.hasPos {
				t.Errorf("ParseDestSpec(%q) = %+v, want %+v", tt.input, actual, tt.expected)
			}
		})
	}
}

func TestInsertAt(t *testing.T) {
	tests := []struct {
		name     string
		s        []int
		i        int
		val      int
		expected []int
	}{
		{"insert at beginning", []int{1, 2, 3}, 0, 0, []int{0, 1, 2, 3}},
		{"insert in middle", []int{1, 2, 3}, 1, 0, []int{1, 0, 2, 3}},
		{"insert at end", []int{1, 2, 3}, 3, 0, []int{1, 2, 3, 0}},
		{"insert out of bounds high", []int{1, 2, 3}, 10, 0, []int{1, 2, 3, 0}},
		{"insert out of bounds low", []int{1, 2, 3}, -1, 0, []int{0, 1, 2, 3}},
		{"insert into empty", []int{}, 0, 1, []int{1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := InsertAt(tt.s, tt.i, tt.val)
			if len(actual) != len(tt.expected) {
				t.Fatalf("InsertAt() length = %d, want %d", len(actual), len(tt.expected))
			}
			for i := range actual {
				if actual[i] != tt.expected[i] {
					t.Errorf("InsertAt() at index %d = %d, want %d", i, actual[i], tt.expected[i])
				}
			}
		})
	}
}

func TestRemoveFromAllOrders(t *testing.T) {
	orders := map[string][]int{
		"loc1": {1, 2, 3},
		"loc2": {2, 4},
		"loc3": {5},
	}
	expected := map[string][]int{
		"loc1": {1, 3},
		"loc2": {4},
		"loc3": {5},
	}

	actual := RemoveFromAllOrders(orders, 2)
	for loc, ids := range expected {
		if len(actual[loc]) != len(ids) {
			t.Fatalf("RemoveFromAllOrders() for %s length = %d, want %d", loc, len(actual[loc]), len(ids))
		}
		for i := range ids {
			if actual[loc][i] != ids[i] {
				t.Errorf("RemoveFromAllOrders() for %s at index %d = %d, want %d", loc, i, actual[loc][i], ids[i])
			}
		}
	}
}

func TestRoundAmount(t *testing.T) {
	tests := []struct {
		input    float64
		expected float64
	}{
		{10.12, 10.1},
		{10.15, 10.2}, // RoundToEven: 1.5 rounds to 2
		{10.25, 10.2}, // RoundToEven: 2.5 rounds to 2
		{10.04, 10.0},
		{10.05, 10.0}, // RoundToEven: 0.5 rounds to 0
		{10.06, 10.1},
	}

	for _, tt := range tests {
		got := RoundAmount(tt.input)
		if got != tt.expected {
			t.Errorf("RoundAmount(%f) = %f, want %f", tt.input, got, tt.expected)
		}
	}
}

func TestTruncateFront(t *testing.T) {
	tests := []struct {
		s        string
		maxLen   int
		expected string
	}{
		{"Hello World", 20, "Hello World"},
		{"Hello World", 11, "Hello World"},
		{"Hello World", 10, "...o World"},
		{"Hello World", 5, "...ld"},
		{"Hello World", 3, "rld"},
		{"Hello World", 2, "ld"},
	}

	for _, tt := range tests {
		got := TruncateFront(tt.s, tt.maxLen)
		if got != tt.expected {
			t.Errorf("TruncateFront(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.expected)
		}
	}
}
