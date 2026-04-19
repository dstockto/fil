package devices

import (
	"context"
	"errors"
	"io"
	"testing"
)

func TestParseCSV(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    ScanResult
		wantErr error
	}{
		{
			name: "plain well-formed",
			in:   "scan001,PolyTerra,PLA,Cotton White,2.47,EAD9D4,Yes,abc-123",
			want: ScanResult{
				UID: "scan001", Brand: "PolyTerra", Type: "PLA", Name: "Cotton White",
				TD: 2.47, HasTD: true, Color: "#ead9d4", Owned: "Yes", UUID: "abc-123",
				RawCSV: "scan001,PolyTerra,PLA,Cotton White,2.47,EAD9D4,Yes,abc-123",
			},
		},
		{
			name: "parens wrapped on some fields",
			in:   "scan002,(PolyTerra),(PLA),(Cotton White),2.47,(EAD9D4),(Yes),(abc-123)",
			want: ScanResult{
				UID: "scan002", Brand: "PolyTerra", Type: "PLA", Name: "Cotton White",
				TD: 2.47, HasTD: true, Color: "#ead9d4", Owned: "Yes", UUID: "abc-123",
				RawCSV: "scan002,(PolyTerra),(PLA),(Cotton White),2.47,(EAD9D4),(Yes),(abc-123)",
			},
		},
		{
			name: "leading # on color is accepted",
			in:   "scan003,Bambu,PLA,Jade,1.8,#a1b2c3,No,zzz",
			want: ScanResult{
				UID: "scan003", Brand: "Bambu", Type: "PLA", Name: "Jade",
				TD: 1.8, HasTD: true, Color: "#a1b2c3", Owned: "No", UUID: "zzz",
				RawCSV: "scan003,Bambu,PLA,Jade,1.8,#a1b2c3,No,zzz",
			},
		},
		{
			name: "empty TD leaves HasTD false",
			in:   "scan004,X,PLA,Y,,ffffff,Yes,u",
			want: ScanResult{
				UID: "scan004", Brand: "X", Type: "PLA", Name: "Y",
				HasTD: false, Color: "#ffffff", Owned: "Yes", UUID: "u",
				RawCSV: "scan004,X,PLA,Y,,ffffff,Yes,u",
			},
		},
		{
			name: "CRLF trimmed",
			in:   "scan005,X,PLA,Y,1.0,000000,Yes,u\r\n",
			want: ScanResult{
				UID: "scan005", Brand: "X", Type: "PLA", Name: "Y",
				TD: 1.0, HasTD: true, Color: "#000000", Owned: "Yes", UUID: "u",
				RawCSV: "scan005,X,PLA,Y,1.0,000000,Yes,u",
			},
		},
		{
			name:    "banner line (wrong field count)",
			in:      "ready",
			wantErr: ErrNotCSV,
		},
		{
			name:    "blank line",
			in:      "",
			wantErr: ErrNotCSV,
		},
		{
			name:    "seven fields",
			in:      "a,b,c,d,1.0,ffffff,Yes",
			wantErr: ErrNotCSV,
		},
		{
			name:    "bad TD",
			in:      "scan,X,PLA,Y,notanum,ffffff,Yes,u",
			wantErr: ErrBadTD,
		},
		{
			name:    "bad color (too short)",
			in:      "scan,X,PLA,Y,1.0,fff,Yes,u",
			wantErr: ErrBadColor,
		},
		{
			name:    "bad color (non-hex chars)",
			in:      "scan,X,PLA,Y,1.0,zzzzzz,Yes,u",
			wantErr: ErrBadColor,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCSV(tt.in)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("result mismatch:\n got: %#v\nwant: %#v", got, tt.want)
			}
		})
	}
}

func TestStripParens(t *testing.T) {
	cases := map[string]string{
		"":           "",
		"plain":      "plain",
		"(wrapped)":  "wrapped",
		"()":         "",
		"(half":      "(half",
		"half)":      "half)",
		"(nested())": "nested()",
	}
	for in, want := range cases {
		if got := stripParens(in); got != want {
			t.Errorf("stripParens(%q) = %q, want %q", in, got, want)
		}
	}
}

// scriptedPort is a Port implementation that yields pre-scripted lines.
type scriptedPort struct {
	lines []string
	idx   int
}

func (s *scriptedPort) ReadLine(_ context.Context) (string, error) {
	if s.idx >= len(s.lines) {
		return "", io.EOF
	}
	line := s.lines[s.idx]
	s.idx++
	return line, nil
}

func (s *scriptedPort) WriteLine(_ string) error { return nil }
func (s *scriptedPort) Close() error             { return nil }

func TestReadScan_DrainsBanner(t *testing.T) {
	p := &scriptedPort{lines: []string{
		"TD-1 firmware 1.2.3",
		"ready",
		"",
		"scan1,X,PLA,Y,1.5,abcdef,Yes,u",
	}}
	res, err := ReadScan(context.Background(), p, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Color != "#abcdef" {
		t.Fatalf("expected color #abcdef, got %q", res.Color)
	}
	if !res.HasTD || res.TD != 1.5 {
		t.Fatalf("expected TD 1.5, got %v (has=%v)", res.TD, res.HasTD)
	}
}

func TestReadScan_MaxDrainExceeded(t *testing.T) {
	p := &scriptedPort{lines: []string{"noise", "more noise", "still noise"}}
	_, err := ReadScan(context.Background(), p, 2)
	if err == nil {
		t.Fatal("expected error when drain limit exceeded")
	}
	if !errors.Is(err, ErrNotCSV) {
		t.Fatalf("expected ErrNotCSV, got %v", err)
	}
}

func TestReadScan_BrokenScanSurfaces(t *testing.T) {
	// If we get a line that's CSV-shaped but has a bad color, surface it
	// rather than draining it (user should see the error).
	p := &scriptedPort{lines: []string{
		"ready",
		"scan1,X,PLA,Y,1.5,notahex,Yes,u",
		"scan2,X,PLA,Y,1.5,abcdef,Yes,u",
	}}
	_, err := ReadScan(context.Background(), p, 10)
	if !errors.Is(err, ErrBadColor) {
		t.Fatalf("expected ErrBadColor to surface, got %v", err)
	}
}
