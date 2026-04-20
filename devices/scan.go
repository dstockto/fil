package devices

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ErrNotCSV indicates the line did not parse as a valid 8-field CSV row.
// Callers (e.g. the scan driver) use this to distinguish banner / prompt lines
// from malformed-but-attempted scans so they can drain the former.
var ErrNotCSV = errors.New("not a CSV scan line")

// ErrBadColor indicates the color field is not a valid 6-digit hex.
var ErrBadColor = errors.New("invalid color hex")

// ErrBadTD indicates the TD field was present but not parseable as a float.
var ErrBadTD = errors.New("invalid TD value")

// ScanResult is one parsed scan line from the TD-1.
type ScanResult struct {
	UID    string  // "uid" column (per-scan? per-device? — see roadmap memory)
	Brand  string  // "Brand" column
	Type   string  // "Type" column
	Name   string  // "Name" column
	TD     float64 // "TD" column, in mm; check HasTD before using
	HasTD  bool    // true when the TD field was populated and parsed
	Color  string  // "Color" column, normalized to lowercase hex with leading "#"
	Owned  string  // "Owned" column (raw, Yes/No/1/0 — semantics unclear)
	UUID   string  // "Uuid" column
	RawCSV string  // the raw line as received, for history logging
}

var hexRe = regexp.MustCompile(`^[0-9A-Fa-f]{6}$`)

// ParseCSV parses one line of TD-1 CSV output. Scans look like
//
//	uid,Brand,Type,Name,TD,Color[,Owned,Uuid]
//
// — 6 to 8 fields. Owned/Uuid may be absent for unrecognized filaments.
// Returns ErrNotCSV when the line is not scan-shaped at all (boot banner,
// "display, ..." UI commands, blank lines — all common noise from the TD-1).
// Returns ErrBadColor / ErrBadTD when the line looks like a scan but a
// required field is unusable.
func ParseCSV(line string) (ScanResult, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return ScanResult{}, ErrNotCSV
	}
	r := csv.NewReader(strings.NewReader(line))
	r.FieldsPerRecord = -1 // don't fail on mismatch; we'll validate manually
	fields, err := r.Read()
	if err != nil {
		return ScanResult{}, fmt.Errorf("%w: %v", ErrNotCSV, err)
	}
	if len(fields) < 6 {
		return ScanResult{}, fmt.Errorf("%w: got %d fields, want >=6", ErrNotCSV, len(fields))
	}

	for i := range fields {
		fields[i] = stripParens(strings.TrimSpace(fields[i]))
	}

	// Guard against "display, ..." lines that happen to have 6+ fields: the
	// color field must look like hex-6, otherwise we don't treat this as a scan.
	colorStr := strings.TrimPrefix(fields[5], "#")
	if !hexRe.MatchString(colorStr) {
		return ScanResult{}, fmt.Errorf("%w: color field %q is not hex", ErrNotCSV, fields[5])
	}

	res := ScanResult{
		UID:    fields[0],
		Brand:  fields[1],
		Type:   fields[2],
		Name:   fields[3],
		RawCSV: line,
	}
	if len(fields) >= 7 {
		res.Owned = fields[6]
	}
	if len(fields) >= 8 {
		res.UUID = fields[7]
	}

	if tdStr := fields[4]; tdStr != "" {
		td, err := strconv.ParseFloat(tdStr, 64)
		if err != nil {
			return res, fmt.Errorf("%w: %q", ErrBadTD, tdStr)
		}
		res.TD = td
		res.HasTD = true
	}

	res.Color = "#" + strings.ToLower(colorStr)
	return res, nil
}

// stripParens removes a single surrounding pair of parentheses from s, if present.
// Only strips the outermost pair; nested parens inside values are preserved.
func stripParens(s string) string {
	if len(s) >= 2 && s[0] == '(' && s[len(s)-1] == ')' {
		return s[1 : len(s)-1]
	}
	return s
}

// ReadScan reads lines from the port until a parseable scan line appears or
// an unrecoverable error occurs. Non-scan noise (TD-1 "display" UI commands,
// boot banners, blank lines) is silently drained; the context is the only
// way to stop waiting for a scan. If maxDrain is > 0 it caps the number of
// non-scan lines tolerated (primarily useful for tests); 0 means unlimited.
func ReadScan(ctx context.Context, p Port, maxDrain int) (ScanResult, error) {
	drained := 0
	for {
		line, err := p.ReadLine(ctx)
		if err != nil {
			return ScanResult{}, fmt.Errorf("read line: %w", err)
		}
		res, err := ParseCSV(line)
		if errors.Is(err, ErrNotCSV) {
			drained++
			if maxDrain > 0 && drained >= maxDrain {
				return ScanResult{}, fmt.Errorf("no scan line after %d non-scan lines: %w", maxDrain, err)
			}
			continue
		}
		// Any other error (bad TD / bad color) means we got a scan-shaped line
		// but it was broken — surface to caller instead of draining further.
		return res, err
	}
}
