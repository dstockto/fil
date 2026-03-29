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
	// Non-printer locations: collapse (remove element)
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

func TestRemoveFromAllOrders_PrinterLocation(t *testing.T) {
	oldCfg := Cfg
	defer func() { Cfg = oldCfg }()

	Cfg = &Config{
		Printers: map[string][]string{
			"Bambu X1C": {"AMS A", "AMS B"},
		},
	}

	orders := map[string][]int{
		"AMS A":    {101, 102, 103, 104},
		"AMS B":    {201, 202, 203, 204},
		"Shelf 1A": {102, 301},
	}

	// Remove spool 102 — it appears in AMS A (printer) and Shelf 1A (non-printer)
	actual := RemoveFromAllOrders(orders, 102)

	// AMS A: should replace with -1 (preserve position)
	expectedAMS := []int{101, EmptySlot, 103, 104}
	if len(actual["AMS A"]) != len(expectedAMS) {
		t.Fatalf("AMS A length = %d, want %d", len(actual["AMS A"]), len(expectedAMS))
	}
	for i, v := range expectedAMS {
		if actual["AMS A"][i] != v {
			t.Errorf("AMS A[%d] = %d, want %d", i, actual["AMS A"][i], v)
		}
	}

	// Shelf 1A: should collapse (remove element)
	expectedShelf := []int{301}
	if len(actual["Shelf 1A"]) != len(expectedShelf) {
		t.Fatalf("Shelf 1A length = %d, want %d", len(actual["Shelf 1A"]), len(expectedShelf))
	}
	for i, v := range expectedShelf {
		if actual["Shelf 1A"][i] != v {
			t.Errorf("Shelf 1A[%d] = %d, want %d", i, actual["Shelf 1A"][i], v)
		}
	}

	// AMS B: should be unchanged
	expectedB := []int{201, 202, 203, 204}
	if len(actual["AMS B"]) != len(expectedB) {
		t.Fatalf("AMS B length = %d, want %d", len(actual["AMS B"]), len(expectedB))
	}
}

func TestIsPrinterLocation(t *testing.T) {
	oldCfg := Cfg
	defer func() { Cfg = oldCfg }()

	Cfg = &Config{
		Printers: map[string][]string{
			"Bambu X1C": {"AMS A", "AMS B", "AMS C"},
			"Prusa":     {"Prusa"},
		},
	}

	tests := []struct {
		location string
		expected bool
	}{
		{"AMS A", true},
		{"AMS B", true},
		{"AMS C", true},
		{"Prusa", true},
		{"Shelf 1A", false},
		{"", false},
		{"AMS D", false},
	}

	for _, tt := range tests {
		t.Run(tt.location, func(t *testing.T) {
			if got := IsPrinterLocation(tt.location); got != tt.expected {
				t.Errorf("IsPrinterLocation(%q) = %v, want %v", tt.location, got, tt.expected)
			}
		})
	}
}

func TestIsPrinterLocation_NilConfig(t *testing.T) {
	oldCfg := Cfg
	defer func() { Cfg = oldCfg }()

	Cfg = nil
	if IsPrinterLocation("AMS A") {
		t.Error("IsPrinterLocation should return false when Cfg is nil")
	}
}

func TestPadToCapacity(t *testing.T) {
	oldCfg := Cfg
	defer func() { Cfg = oldCfg }()

	Cfg = &Config{
		Printers: map[string][]string{
			"Bambu X1C": {"AMS A"},
		},
		LocationCapacity: map[string]LocationCapacity{
			"AMS A":    {Capacity: 4},
			"Shelf 1A": {Capacity: 5},
		},
	}

	tests := []struct {
		name     string
		location string
		ids      []int
		expected []int
	}{
		{
			"printer location under capacity",
			"AMS A", []int{101, 102}, []int{101, 102, EmptySlot, EmptySlot},
		},
		{
			"printer location at capacity",
			"AMS A", []int{101, 102, 103, 104}, []int{101, 102, 103, 104},
		},
		{
			"printer location over capacity",
			"AMS A", []int{101, 102, 103, 104, 105}, []int{101, 102, 103, 104, 105},
		},
		{
			"non-printer location unchanged",
			"Shelf 1A", []int{201, 202}, []int{201, 202},
		},
		{
			"unknown location unchanged",
			"Unknown", []int{301}, []int{301},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := PadToCapacity(tt.location, tt.ids)
			if len(actual) != len(tt.expected) {
				t.Fatalf("PadToCapacity(%q) length = %d, want %d", tt.location, len(actual), len(tt.expected))
			}
			for i, v := range tt.expected {
				if actual[i] != v {
					t.Errorf("PadToCapacity(%q)[%d] = %d, want %d", tt.location, i, actual[i], v)
				}
			}
		})
	}
}

func TestCountSpools(t *testing.T) {
	tests := []struct {
		name     string
		ids      []int
		expected int
	}{
		{"all real spools", []int{101, 102, 103}, 3},
		{"with sentinels", []int{101, EmptySlot, 103, EmptySlot}, 2},
		{"all sentinels", []int{EmptySlot, EmptySlot}, 0},
		{"empty", []int{}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CountSpools(tt.ids); got != tt.expected {
				t.Errorf("CountSpools() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestFirstEmptySlot(t *testing.T) {
	tests := []struct {
		name     string
		ids      []int
		expected int
	}{
		{"first slot empty", []int{EmptySlot, 102, 103}, 0},
		{"middle slot empty", []int{101, EmptySlot, 103}, 1},
		{"last slot empty", []int{101, 102, EmptySlot}, 2},
		{"no empty slots", []int{101, 102, 103}, -1},
		{"empty slice", []int{}, -1},
		{"multiple empties returns first", []int{101, EmptySlot, EmptySlot}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FirstEmptySlot(tt.ids); got != tt.expected {
				t.Errorf("FirstEmptySlot() = %d, want %d", got, tt.expected)
			}
		})
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

func TestToProjectName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"new-head-jim-halpert", "New Head Jim Halpert"},
		{"new_head_jim_halpert", "New Head Jim Halpert"},
		{"new head jim halpert", "New Head Jim Halpert"},
		{"New-Head-Jim-Halpert", "New Head Jim Halpert"},
		{"NEW_HEAD_JIM_HALPERT", "New Head Jim Halpert"},
		{"file.yaml", "File.yaml"}, // It's expected to handle what's passed to it
		{"", ""},
		{"   ", ""},
		{"a-b_c  d", "A B C D"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			actual := ToProjectName(tt.input)
			if actual != tt.expected {
				t.Errorf("ToProjectName(%q) = %q, want %q", tt.input, actual, tt.expected)
			}
		})
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

func TestResolveLowThreshold(t *testing.T) {
	oldCfg := Cfg
	defer func() { Cfg = oldCfg }()

	Cfg = &Config{
		LowThresholds: map[string]float64{
			"PLA":           100.0,
			"Poly::PLA":     200.0,
			"Generic::PETG": 150.0,
		},
	}

	tests := []struct {
		vendor string
		name   string
		expect float64
	}{
		{"Generic", "PLA", 100.0},             // matches "PLA"
		{"PolyMaker", "PolyTerra PLA", 200.0}, // matches "Poly::PLA"
		{"Generic", "PETG", 150.0},            // matches "Generic::PETG"
		{"Unknown", "ABS", 0.0},               // no match
	}

	for _, test := range tests {
		got := ResolveLowThreshold(test.vendor, test.name)
		if got != test.expect {
			t.Errorf("ResolveLowThreshold(%q, %q) = %f; expect %f", test.vendor, test.name, got, test.expect)
		}
	}
}
