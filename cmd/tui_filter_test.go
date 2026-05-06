package cmd

import "testing"

func TestPickerFilterIdxs(t *testing.T) {
	lines := []string{"Polymaker PLA white", "Bambu PETG black", "Polymaker PLA red"}

	tests := []struct {
		name      string
		query     string
		wantNil   bool
		wantIdxs  []int
		wantEmpty bool // non-nil but length 0
	}{
		{name: "empty query returns nil (no filter)", query: "", wantNil: true},
		{name: "whitespace query returns nil (no filter)", query: "   ", wantNil: true},
		{name: "single match", query: "petg", wantIdxs: []int{1}},
		{name: "multiple matches case-insensitive", query: "POLYMAKER", wantIdxs: []int{0, 2}},
		{
			// Regression: a non-empty query that matches nothing must NOT return nil,
			// because nil is the sentinel for "no filter active". If it returned nil,
			// the picker would collapse back to showing every plate.
			name:      "no matches returns non-nil empty slice",
			query:     "definitely-not-there",
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pickerFilterIdxs(tt.query, lines)
			switch {
			case tt.wantNil:
				if got != nil {
					t.Errorf("got %v, want nil", got)
				}
			case tt.wantEmpty:
				if got == nil {
					t.Error("got nil, want non-nil empty slice (else picker shows everything on zero matches)")
				}
				if len(got) != 0 {
					t.Errorf("got %v, want empty slice", got)
				}
			default:
				if len(got) != len(tt.wantIdxs) {
					t.Fatalf("got %v, want %v", got, tt.wantIdxs)
				}
				for i := range got {
					if got[i] != tt.wantIdxs[i] {
						t.Errorf("got %v, want %v", got, tt.wantIdxs)
						break
					}
				}
			}
		})
	}
}
