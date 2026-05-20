---
name: tui-screenshot
description: Captures a Bubbletea TUI view as a snapshot or terminal capture so Claude can visually verify changes to cmd/tui.go and related views before reporting complete. Use whenever a TUI change has been made and visual confirmation is needed.
---

# tui-screenshot

Bubbletea views render to a terminal, which makes them invisible to me unless I can capture output. Pick the right approach for the change:

## Option A: teatest snapshot (preferred for tests / deterministic checks)

For repeatable snapshots inside Go tests, use [`github.com/charmbracelet/x/exp/teatest`](https://github.com/charmbracelet/x/tree/main/exp/teatest):

```go
import (
    "io"
    "testing"

    "github.com/charmbracelet/x/exp/teatest"
)

func TestTUIInitialView(t *testing.T) {
    tm := teatest.NewTestModel(t, newTUIModel(/* fixtures */))
    out, _ := io.ReadAll(tm.FinalOutput(t))
    teatest.RequireEqualOutput(t, out)
}
```

Snapshots land in `testdata/` and diff cleanly in PRs. Use this for model/update logic, key bindings, and filter behavior.

## Option B: scripted terminal capture (for ad-hoc visual check)

For a one-off look at the live TUI in a non-interactive mode, capture through `script(1)`:

```bash
TERM=xterm-256color script -q /tmp/tui.cap -c './fil tui --non-interactive 2>/dev/null'
```

Then read `/tmp/tui.cap` with `Read` — it contains ANSI escapes; you can still see structure, colors, and text content. Use this for lipgloss style tweaks where the question is "what does it look like."

## When to use which

| Change | Approach |
|---|---|
| Model/update logic | teatest snapshot |
| Key bindings, filter behavior | teatest with simulated input |
| Color or lipgloss style tweak | scripted capture, read raw |
| New view/component | teatest snapshot + scripted capture |

## Reporting

Always check the captured output (snapshot diff or raw file) before reporting the TUI change as complete. If you cannot get a capture, say so explicitly rather than claiming the change works.
