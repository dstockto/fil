package api

import "testing"

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int // >0 means a>b, <0 means a<b, 0 means equal
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"v1.2.3", "v1.2.3", 0},
		{"v2.0.0", "v1.0.0", 1},
		{"v1.0.0", "v2.0.0", -1},
		{"v10.0.0", "v2.0.0", 1},  // the bug this fixes
		{"v2.0.0", "v10.0.0", -1}, // the bug this fixes
		{"v1.10.0", "v1.2.0", 1},
		{"v1.2.0", "v1.10.0", -1},
		{"v1.0.10", "v1.0.2", 1},
		{"v1.0.2", "v1.0.10", -1},
		{"v1.2.0", "v1.1.9", 1},
		{"1.2.3", "1.2.3", 0},  // no v prefix
		{"v1.2.3", "1.2.3", 0}, // mixed prefix
		{"v1.0", "v1.0.0", 0},  // missing patch
		{"v1", "v1.0.0", 0},    // major only
		{"dev", "v1.0.0", -1},  // non-numeric treated as 0
	}

	for _, tt := range tests {
		got := compareSemver(tt.a, tt.b)
		// Normalize to sign
		switch {
		case tt.want > 0 && got <= 0:
			t.Errorf("compareSemver(%q, %q) = %d, want >0", tt.a, tt.b, got)
		case tt.want < 0 && got >= 0:
			t.Errorf("compareSemver(%q, %q) = %d, want <0", tt.a, tt.b, got)
		case tt.want == 0 && got != 0:
			t.Errorf("compareSemver(%q, %q) = %d, want 0", tt.a, tt.b, got)
		}
	}
}
