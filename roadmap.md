# Fil Roadmap

Source of truth for what's next. Edit this file directly to add items, set priorities (order = priority within section), or adjust acceptance criteria.

Conventions:
- H3 slug = canonical item ID. Kebab-case English. Used as `Roadmap-Item: <slug>` in PR bodies.
- `**Acceptance:**` bullet is the gate between Idea Backlog and Ready. No acceptance → it's an idea, not Ready.
- `**Source:**` is freeform: `memory:DATE`, `gh#N`, `direct`, `reviewer:agent-name`.
- `**Blocked:**`, `**Needs-spec:**` annotations document constraints inline.

Automation:
- `.github/workflows/roadmap-issue-sync.yml` appends `roadmap`-labeled issues to Idea Backlog.
- `.github/workflows/roadmap-merge-sync.yml` moves items from In Flight to Done on PR merge.
- `roadmap-groomer` subagent proposes the next Ready item.
- `feature-shipper` skill implements a Ready item via strict TDD and opens a draft PR.

Drift check (run on demand to verify nothing's missing): `.github/scripts/roadmap_drift.sh`.

---

## In Flight

<!-- Items with an active branch/draft PR. Populated by feature-shipper, cleared by roadmap-merge-sync. -->

---

## Ready

### fetchspoolsbyid-per-id-lookup
- **Acceptance:**
  - Add `FindSpoolByID(ctx context.Context, id int) (models.Spool, error)` to the narrow `Spoolman` interface in `api/`.
  - Implement against Spoolman's `GET /api/v1/spool/{id}` endpoint in the HTTP client.
  - Swap `plan/local_complete.go` `fetchSpoolsByID` (currently lines 33–52) to use per-ID calls instead of `FindSpoolsByName("*", nil, nil)` + filter.
  - Regression tests: interface method round-trips a spool; refactored caller makes N per-ID calls (not one catalog call) for N input IDs.
  - Deduction path is O(deductions), not O(catalog).
- **Source:** gh#9

Identified by `spoolman-quirks-reviewer` when auditing `ec4e296` (plan-complete migration). Not a correctness issue, but a wasteful catalog fetch on every plate completion (~250 spools and growing). The existing inline comment in `local_complete.go` acknowledges this:

> // Goes through FindSpoolsByName("*") because that's already in the
> // narrow Spoolman interface — adding a per-ID lookup would expand the seam.

Adding `FindSpoolByID` is the seam expansion that comment is gating against.

---

## Idea Backlog

### locations-spoolorders-refresh-on-complete
- **Source:** gh#8
- **Blocked:** latent — leave as-is until auto-unload-on-complete behavior is added

`plan/local_complete.go:88` clears `plate.Printer = ""` but doesn't refresh the `locations_spoolorders` Spoolman setting. Today this is harmless because completion doesn't physically move any spool. The footgun reopens the moment any auto-unload-on-complete behavior is added. Pin this so whoever adds that doesn't reinvent the bug. See `cmd/move.go:394`, `cmd/archive.go:160` for the pattern (`PostSettingObject(ctx, "locations_spoolorders", orders)` after the mutation).

### plan-history-zero-prints-display
- **Source:** gh#10
- **Needs-spec:** is the zero-prints showing "24h00m" the same root cause as the suspicious-duration entries (30m/41g, 23h59m/26g), or two separate bugs? Acceptance criteria depend on the answer.

`fil p h` shows `24h00m` on days with no prints, which should be `0` or omitted. Some calculated durations also look wrong on real-print days. Until the scope is decided, the groomer should refuse to ship this and file a `needs-spec` follow-up.

### api-fil-prefix-migration
- **Source:** memory:2026-04-30

Move plan-server endpoints from `/api/v1/*` to `/api/fil/*` so a single Caddy wildcard covers them and future endpoints don't need Caddyfile edits. Mechanical change across `server/handler.go` (30+ route registrations) and `api/plans_client.go`. Three-PR migration: (1) add `/api/fil/*` as a second prefix server-side (dual-routing), (2) flip client calls + update Caddy, (3) remove `/api/v1/*` server-side. Brief cutover window where old binaries 404 between (2) and binary redeploy.

### dirigera-print-completion-blink
- **Source:** memory:2026-04-16

Flash the DIRIGERA bulb near a printer when a print finishes. Data already available from printer state-change notifications. Hardware is ready (bulbs over each printer, tunable-white). Needs a DIRIGERA client (local REST API over HTTPS, OAuth-style pairing) and a printer→bulb mapping in config. Existing notification path is the integration point.

### dirigera-pick-to-light
- **Source:** memory:2026-04-16
- **Blocked:** hardware — needs bulbs/strips purchased and placed at filament storage locations

Light up the bulb at a storage location when `fil find` or `plan next` identifies a spool to grab. Once hardware exists, needs a location→bulb mapping in config.

### dirigera-physical-shortcut-buttons
- **Source:** memory:2026-04-16

10 TRÅDFRI controllers (3 connected, 7 unused). DIRIGERA hub exposes a WebSocket event stream for button events. Map button IDs to fil commands via config: e.g. single-press near a printer = `fil p c`, long-press = `fil p stop`. Needs a long-running listener (separate binary, possibly part of plan server) holding a WebSocket to the hub. Config shape: `buttons: { "<device-id>": { "single": "<cmd>", "long": "<cmd>" } }`.

### chat-bot-interface
- **Source:** memory:2026-04-12

Discord or Telegram bot that can answer status queries ("what's printing", "find silver PLA") and accept commands ("complete plate", "use 50g from spool 123"). Pairs with existing Pushover notifications — adds a reply channel. Works remotely without exposing the plan server (bot connects outbound). Decision needed: which platform.

### barcode-qr-labels
- **Source:** memory:2026-04-12

Print labels for storage bins; scan with phone to call a server endpoint that runs `fil move <spool> <bin>`. At ~250 spools the biggest physical friction is finding things. Needs label format decision and a scan-to-API endpoint.

### absorb-spoolman-functionality
- **Source:** memory:2026-04-12

Fil takes over spool/filament/vendor management entirely, dropping the Spoolman dependency. Motivated by Spoolman's stagnant upstream and its API quirks (settings double-wrapping, locations_spoolorders drift, no slot model). Significant undertaking — needs data migration, spool/filament/vendor CRUD commands, and either a replacement web UI or a decision that CLI is enough.

### iphone-caddy-ca-install
- **Source:** memory:2026-04-30
- **Blocked:** manual task, no code change

Install Caddy's root CA on the iPhone so iOS Shortcut can hit HTTPS endpoints (`raspberrypi4.local`) without cert warnings. Copy `root.crt` from the pi (under Caddy's data dir, e.g. `/var/lib/caddy/.local/share/caddy/pki/authorities/local/root.crt`) → AirDrop/email → install profile → Settings → General → About → Certificate Trust Settings → enable trust for "Caddy Local Authority". One-time setup, ~10-year lifetime.

---

## Done

<!-- Items merged within the last 20 entries; older are trimmed by roadmap-merge-sync.yml. Format: `### <slug>` + `**Merged:** YYYY-MM-DD in #<N>`. -->
