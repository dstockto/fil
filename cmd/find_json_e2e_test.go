package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// spoolFixture renders one Spoolman-shaped spool record. The diameter is 1.75
// so it survives find's default onlyStandardFilament filter.
func spoolFixture(id int, name, vendor, material, hex, location string, remaining float64) string {
	b, err := json.Marshal(map[string]any{
		"id": id,
		"filament": map[string]any{
			"id":        id * 10,
			"name":      name,
			"vendor":    map[string]any{"id": 1, "name": vendor},
			"material":  material,
			"color_hex": hex,
			"diameter":  1.75,
		},
		"remaining_weight": remaining,
		"used_weight":      1000 - remaining,
		"location":         location,
	})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// newFindTestServer serves the endpoints runFind touches: the settings endpoint
// (empty, so no location ordering is applied), the spool search, and the
// single-spool lookup used when the argument parses as an ID.
//
// The two spool routes must be matched exactly, not by prefix: Spoolman's
// /api/v1/spool returns an array while /api/v1/spool/{id} returns one object,
// and a prefix match would hand the array to FindSpoolsById's object decoder.
func newFindTestServer(t *testing.T, spools []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/setting"):
			_, _ = w.Write([]byte(`{}`))
		case r.URL.Path == "/api/v1/spool":
			_, _ = w.Write([]byte("[" + strings.Join(spools, ",") + "]"))
		case strings.HasPrefix(r.URL.Path, "/api/v1/spool/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/spool/")
			for _, s := range spools {
				var probe struct {
					ID int `json:"id"`
				}
				if err := json.Unmarshal([]byte(s), &probe); err != nil {
					t.Errorf("bad fixture %q: %v", s, err)
					continue
				}
				if strconv.Itoa(probe.ID) == id {
					_, _ = w.Write([]byte(s))
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runFindForTest executes find against a stub Spoolman with a fresh command so
// flag state doesn't leak between cases. Returns stdout and stderr separately.
func runFindForTest(t *testing.T, spools []string, args ...string) (stdout, stderr string) {
	t.Helper()

	srv := newFindTestServer(t, spools)

	oldCfg := Cfg
	t.Cleanup(func() { Cfg = oldCfg })
	Cfg = &Config{ApiBase: srv.URL}

	c := &cobra.Command{Use: "find", RunE: runFind}
	addFindFlags(c)

	var outBuf, errBuf bytes.Buffer
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	c.SetArgs(args)

	if err := c.Execute(); err != nil {
		t.Fatalf("find failed: %v (stderr: %s)", err, errBuf.String())
	}
	return outBuf.String(), errBuf.String()
}

// TestFindJSONStdoutIsPureJSON is the regression test for the bug where
// "Filtering by location: ..." was printed to stdout ahead of the JSON
// document, making `fil find -l ams --json | jq` fail on line 1.
//
// This only holds because runFind writes every byte of stdout through
// cmd.OutOrStdout(); a stray fmt.Printf would bypass the buffer and escape
// this assertion. TestFindWritesNothingToProcessStdout is the backstop.
func TestFindJSONStdoutIsPureJSON(t *testing.T) {
	fixtures := []string{
		spoolFixture(262, "PolyTerra™ Cotton White", "Polymaker", "Matte PLA", "e6dddb", "AMS B:4", 419.9),
	}

	stdout, stderr := runFindForTest(t, fixtures, "*", "-l", "ams", "--json")

	var got []spoolExport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout was:\n%s", err, stdout)
	}
	if len(got) != 1 || got[0].ID != 262 {
		t.Errorf("got %+v; want a single spool with id 262", got)
	}
	if !strings.Contains(stderr, "Filtering by location") {
		t.Errorf("expected the location notice on stderr; stderr was: %q", stderr)
	}
}

// TestFindTextOutputGoesToTheBuffer proves cmd.SetOut captures the human
// rendering too, not just the JSON encoder — i.e. that the buffer is
// authoritative for what lands on stdout.
func TestFindTextOutputGoesToTheBuffer(t *testing.T) {
	fixtures := []string{
		spoolFixture(1, "Matte White", "Polymaker", "Matte PLA", "ffffff", "Shelf 1", 500),
	}

	stdout, _ := runFindForTest(t, fixtures, "*")

	for _, want := range []string{"Found 1 spools", "Matte White", "Summary"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in the captured stdout buffer; buffer was:\n%s", want, stdout)
		}
	}
}

// TestFindJSONEmptyResultIsEmptyArray locks the documented promise that no
// matches produces [] rather than null, so consumers can range over it directly.
func TestFindJSONEmptyResultIsEmptyArray(t *testing.T) {
	stdout, _ := runFindForTest(t, nil, "nothing-matches-this", "--json")

	if strings.TrimSpace(stdout) != "[]" {
		t.Errorf("stdout = %q; want []", strings.TrimSpace(stdout))
	}

	var got []spoolExport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("got %v; want a non-nil empty slice", got)
	}
}

// TestFindJSONRespectsFilters checks that the JSON path applies the same
// in-process filters as the text path rather than dumping everything.
func TestFindJSONRespectsFilters(t *testing.T) {
	fixtures := []string{
		spoolFixture(1, "Matte White", "Polymaker", "Matte PLA", "ffffff", "Shelf 1", 500),
		spoolFixture(2, "Tough Black", "Acme", "PETG", "000000", "Shelf 2", 600),
	}

	stdout, _ := runFindForTest(t, fixtures, "*", "--material", "petg", "--json")

	var got []spoolExport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout was:\n%s", err, stdout)
	}
	if len(got) != 1 {
		t.Fatalf("got %d spools; want 1 (--material petg): %+v", len(got), got)
	}
	if got[0].ID != 2 || got[0].Material != "PETG" {
		t.Errorf("got %+v; want the PETG spool (id 2)", got[0])
	}
}

// TestFindJSONDoesNotEscapeHTML covers the deliberate SetEscapeHTML(false):
// filament names containing & or < must survive verbatim rather than being
// emitted as the & / < escapes Go's encoder defaults to.
func TestFindJSONDoesNotEscapeHTML(t *testing.T) {
	fixtures := []string{
		spoolFixture(7, "Black & Blue <Silk>", "Acme", "PLA", "112233", "Shelf 3", 250),
	}

	stdout, _ := runFindForTest(t, fixtures, "*", "--json")

	// The literal six-character sequences the encoder would emit if HTML
	// escaping were left on.
	for _, escape := range []string{"\\u0026", "\\u003c", "\\u003e"} {
		if strings.Contains(stdout, escape) {
			t.Errorf("found HTML escape %s; expected verbatim characters. stdout was:\n%s", escape, stdout)
		}
	}
	if !strings.Contains(stdout, "Black & Blue <Silk>") {
		t.Errorf("name not emitted verbatim; stdout was:\n%s", stdout)
	}

	var got []spoolExport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Black & Blue <Silk>" {
		t.Errorf("got %+v; want the name round-tripped intact", got)
	}
}

// TestFindWritesNothingToProcessStdout is the backstop for every other
// assertion in this file. Capturing via cmd.SetOut only proves anything if
// runFind has no path that writes to os.Stdout directly — a stray fmt.Printf
// would sail past the buffer while still corrupting `fil find --json | jq` for
// real. So: redirect the process's stdout to a pipe, run find both ways, and
// require that the pipe stays empty.
//
// Swapping os.Stdout alone is not enough. fmt.Printf reads os.Stdout at call
// time, so the swap catches it — but fatih/color's package-level Output var is
// initialized once at import to colorable.NewColorableStdout(), which on
// unix holds the *original* os.Stdout. A regression that reintroduced
// color.Green(foundMsg) — exactly the call this change replaced — would write
// through color.Output, miss the pipe entirely, and leave this test green while
// `fil find --json | jq` broke. So redirect color.Output/color.Error too.
func TestFindWritesNothingToProcessStdout(t *testing.T) {
	fixtures := []string{
		spoolFixture(1, "Matte White", "Polymaker", "Matte PLA", "ffffff", "Shelf 1", 500),
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	// The restore must be deferred: runFindForTest calls t.Fatalf on any command
	// error, which unwinds via runtime.Goexit and would skip a plain restore
	// statement, leaving os.Stdout pointing at this pipe for the rest of the
	// package run. Closing w here too unblocks the drain goroutine on that path.
	orig := os.Stdout
	origColorOut, origColorErr := color.Output, color.Error
	defer func() {
		os.Stdout = orig
		color.Output, color.Error = origColorOut, origColorErr
		_ = w.Close() // no-op if the happy path below already closed it
		_ = r.Close()
	}()
	os.Stdout = w
	// color.Error is redirected as well: it too defaults to a wrapper captured at
	// import, and a stray color print aimed at "stderr" that actually landed on
	// fd 1 is precisely the confusion worth catching.
	color.Output, color.Error = w, w

	// Drain concurrently. A leak larger than the pipe buffer (64KB) would
	// otherwise block the writer inside runFindForTest, hanging the test until
	// the package timeout instead of reporting the leak this test exists to find.
	leakedCh := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(r) // a closed write end yields EOF, not an error
		leakedCh <- b
	}()

	// Exercise the JSON path and the human path, with and without the flags
	// that emit progress chatter.
	runFindForTest(t, fixtures, "*", "-l", "shelf", "--json")
	runFindForTest(t, fixtures, "*", "-l", "shelf")
	runFindForTest(t, fixtures, "*", "--purchase")

	// Restore and close before asserting, so t.Errorf output is safe and the
	// drain goroutine sees EOF.
	os.Stdout = orig
	color.Output, color.Error = origColorOut, origColorErr
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close pipe: %v", err)
	}

	leaked := <-leakedCh
	if len(leaked) > 0 {
		t.Errorf("find wrote %d bytes to the process stdout (os.Stdout or color.Output), bypassing cmd.OutOrStdout():\n%s", len(leaked), leaked)
	}
}

// TestFindJSONDedupesAcrossSearchTerms covers a spool matching more than one
// argument. The text path prints it once per term under that term's header,
// where the repeat reads as intentional; a flat JSON array has no headers, so
// emitting it twice would silently inflate any consumer that counts spools or
// sums remaining_g.
func TestFindJSONDedupesAcrossSearchTerms(t *testing.T) {
	fixtures := []string{
		spoolFixture(1, "Matte Blue", "Polymaker", "Matte PLA", "0000ff", "Shelf 1", 500),
	}

	// The stub ignores the name filter, so both terms match the same spool —
	// which is the situation being guarded, just reached deterministically.
	stdout, _ := runFindForTest(t, fixtures, "blue", "matte", "--json")

	var got []spoolExport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout was:\n%s", err, stdout)
	}
	if len(got) != 1 {
		t.Fatalf("got %d records for one spool matching two terms; want 1: %+v", len(got), got)
	}
	if got[0].ID != 1 {
		t.Errorf("got %+v; want spool 1", got[0])
	}
}

// TestFindJSONByID covers the numeric-argument branch, which fetches through
// FindSpoolsById rather than the search endpoint and so never reaches the
// in-process filters the other JSON tests exercise.
func TestFindJSONByID(t *testing.T) {
	fixtures := []string{
		spoolFixture(1, "Matte White", "Polymaker", "Matte PLA", "ffffff", "Shelf 1", 500),
		spoolFixture(2, "Tough Black", "Acme", "PETG", "000000", "Shelf 2", 600),
	}

	stdout, _ := runFindForTest(t, fixtures, "2", "--json")

	var got []spoolExport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout was:\n%s", err, stdout)
	}
	if len(got) != 1 || got[0].ID != 2 || got[0].Name != "Tough Black" {
		t.Fatalf("got %+v; want just spool 2", got)
	}
}

// TestFindJSONMissingIDIsEmptyArray pins the 404 branch: a missing ID is not an
// error, and must still produce [] rather than null or a bare newline.
func TestFindJSONMissingIDIsEmptyArray(t *testing.T) {
	fixtures := []string{
		spoolFixture(1, "Matte White", "Polymaker", "Matte PLA", "ffffff", "Shelf 1", 500),
	}

	stdout, _ := runFindForTest(t, fixtures, "999", "--json")

	if strings.TrimSpace(stdout) != "[]" {
		t.Errorf("stdout = %q; want []", strings.TrimSpace(stdout))
	}
}

// TestFindJSONNearSortsAndLimits covers --near feeding the JSON path. That path
// rewrites the spools slice in place (spools[:0] aliasing) and truncates to
// --limit before the export runs, so it's the ordering most worth pinning — and
// JSON is the cheapest place to assert on order exactly.
func TestFindJSONNearSortsAndLimits(t *testing.T) {
	fixtures := []string{
		spoolFixture(1, "White", "Acme", "PLA", "ffffff", "Shelf 1", 500),
		spoolFixture(2, "Near Red", "Acme", "PLA", "fe0000", "Shelf 2", 500),
		spoolFixture(3, "Black", "Acme", "PLA", "000000", "Shelf 3", 500),
	}

	stdout, _ := runFindForTest(t, fixtures, "*", "--near", "#ff0000", "--limit", "2", "--json")

	var got []spoolExport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout was:\n%s", err, stdout)
	}
	if len(got) != 2 {
		t.Fatalf("got %d spools; want 2 (--limit 2): %+v", len(got), got)
	}
	if got[0].ID != 2 {
		t.Errorf("nearest to #ff0000 is %+v; want spool 2 (#fe0000)", got[0])
	}
}

// TestFindTextModeStillPrintsNoticeToStderr guards against the stderr move
// silently dropping the notice for interactive (non-JSON) use.
func TestFindTextModeStillPrintsNoticeToStderr(t *testing.T) {
	fixtures := []string{
		spoolFixture(1, "Matte White", "Polymaker", "Matte PLA", "ffffff", "Shelf 1", 500),
	}

	_, stderr := runFindForTest(t, fixtures, "*", "-l", "shelf")

	if !strings.Contains(stderr, "Filtering by location: shelf") {
		t.Errorf("expected the location notice on stderr; stderr was: %q", stderr)
	}
}
