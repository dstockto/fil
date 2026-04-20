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
			name: "plain well-formed 8 fields",
			in:   "scan001,PolyTerra,PLA,Cotton White,2.47,EAD9D4,Yes,abc-123",
			want: ScanResult{
				UID: "scan001", Brand: "PolyTerra", Type: "PLA", Name: "Cotton White",
				TD: 2.47, HasTD: true, Color: "#ead9d4", Owned: "Yes", UUID: "abc-123",
				RawCSV: "scan001,PolyTerra,PLA,Cotton White,2.47,EAD9D4,Yes,abc-123",
			},
		},
		{
			// Real-world format observed from the TD-1 for an unrecognized
			// filament: 6 fields, Brand/Type/Name blank, no Owned/Uuid.
			name: "six-field unrecognized filament",
			in:   "17253504890,,,,0.3,83EAFA",
			want: ScanResult{
				UID: "17253504890",
				TD:  0.3, HasTD: true, Color: "#83eafa",
				RawCSV: "17253504890,,,,0.3,83EAFA",
			},
		},
		{
			// Device drives its own LCD with "display, ..." commands on the
			// same serial stream — these must be silently drained, not
			// surfaced as broken scans.
			name:    "display command line is not a scan",
			in:      "display, Insert Filament, 18, 16",
			wantErr: ErrNotCSV,
		},
		{
			name:    "clearScreen command is not a scan",
			in:      "clearScreen",
			wantErr: ErrNotCSV,
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
			// Seven-field scan: Owned present, Uuid absent — valid, parsed.
			name: "seven-field scan",
			in:   "a,b,c,d,1.0,ffffff,Yes",
			want: ScanResult{
				UID: "a", Brand: "b", Type: "c", Name: "d",
				TD: 1.0, HasTD: true, Color: "#ffffff", Owned: "Yes",
				RawCSV: "a,b,c,d,1.0,ffffff,Yes",
			},
		},
		{
			name:    "fewer than six fields",
			in:      "a,b,c,d,1.0",
			wantErr: ErrNotCSV,
		},
		{
			// TD-shaped field is numeric but color is malformed → not a scan
			// (rather than ErrBadColor) because the color field is how we
			// distinguish scan lines from other CSV-shaped noise on the line.
			name:    "malformed color classified as not-a-scan",
			in:      "scan,X,PLA,Y,1.0,fff,Yes,u",
			wantErr: ErrNotCSV,
		},
		{
			name:    "bad TD surfaces on an otherwise valid scan",
			in:      "scan,X,PLA,Y,notanum,ffffff,Yes,u",
			wantErr: ErrBadTD,
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
	// A line that parses as a scan (valid 6-hex color) but has a non-numeric
	// TD should be surfaced to the caller rather than drained silently — bad
	// measurement data is worth showing the user.
	p := &scriptedPort{lines: []string{
		"ready",
		"scan1,X,PLA,Y,notanum,abcdef,Yes,u",
		"scan2,X,PLA,Y,1.5,abcdef,Yes,u",
	}}
	_, err := ReadScan(context.Background(), p, 10)
	if !errors.Is(err, ErrBadTD) {
		t.Fatalf("expected ErrBadTD to surface, got %v", err)
	}
}

func TestReadScan_DrainsDisplayCommands(t *testing.T) {
	// Real-world stream from the TD-1: "display" UI commands interleaved
	// with a single scan line. The drain should eat every "display" line
	// and return the scan.
	p := &scriptedPort{lines: []string{
		"clearScreen",
		"display, Insert Filament, 18, 16",
		"display, TD:, 50, 5",
		"display, Scanning, 74, 20",
		"17253504890,,,,0.3,83EAFA",
	}}
	res, err := ReadScan(context.Background(), p, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Color != "#83eafa" || res.TD != 0.3 {
		t.Fatalf("wrong scan parsed: %+v", res)
	}
}
