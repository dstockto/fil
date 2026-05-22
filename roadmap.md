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

### add-grep-based-regression-test-for-plan-server-client-url-prefixes
- **Acceptance:**
  - New test `TestNoPlanServerClientV1URLs` lives in `api/url_prefix_test.go` (new file, package `api`).
  - Test parses these files via `go/parser.ParseFile`: `api/plans_client.go`, `api/health.go`, and every `plan/remote_*.go` (located by `filepath.Glob`, so future `remote_*.go` files are picked up automatically).
  - Test walks each file's AST via `ast.Inspect`, collecting `*ast.BasicLit` nodes with `token.STRING`, and asserts none contain the substring `/api/v1`.
  - Failure message names the file and line: e.g. `plans_client.go:97: string literal contains /api/v1: "/api/v1/plans"`.
  - Source files are located relative to the test file via `runtime.Caller(0)`, so the test works regardless of `go test` invocation directory (including CI).
  - Out of scope: Spoolman URLs in `api/client.go` and outbound Prusa URLs in `server/prusa.go` are NOT scanned — both legitimately use `/api/v1`.
  - No production code changes; test is green on current `main`.
  - Stdlib only — no new dependencies (`go/parser`, `go/ast`, `go/token`, `runtime`, `path/filepath`).
- **Source:** gh#21
- **Branch:** roadmap/add-grep-based-regression-test-for-plan-server-client-url-prefixes
- **PR:** #22

Closes the loop on the production miss described in gh#21: PR #17 listed specific files instead of stating the invariant; `api/health.go`'s `GetHealth` was missed and PR #19 then broke `fil doctor` in production. A structural enumeration test prevents the same shape of miss in the future. Complements existing runtime probes `TestGetHealth_UsesFilPrefix` and `TestPlanServerClientUsesFilPrefix` in `api/plans_client_test.go`.

## Ready

<!-- No items currently Ready. Add new items here with **Acceptance:** to mark them shippable. -->

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
### api-fil-prefix-migration-pr3-remove-v1
- **Acceptance:**
  - `server/handler.go` `apiPrefixes` contains only `"/api/fil"`; `"/api/v1"` is removed.
  - Migration-progress comment on `apiPrefixes` is rewritten to describe the canonical prefix (no more "during the migration" language).
  - `GET /api/v1/version` (and every other formerly-routed `/api/v1/*` path) returns 404; `GET /api/fil/version` continues to work.
  - `TestBothPrefixesRoute` in `server/handler_test.go` is rewritten as `TestOnlyFilPrefixRoutes` asserting `/api/fil/*` works AND `/api/v1/*` returns 404.
  - All other server tests that hit `/api/v1/...` paths are flipped to `/api/fil/...`. Out of scope (must NOT change): Spoolman stub paths in `server/health_test.go:50`, outbound Prusa-printer paths in `server/prusa.go`, Spoolman-client refs in `api/client.go`/`api/health.go`, and the `cmd/clean.go` comment about Spoolman's `/api/v1/setting/...`.
  - Stale `/api/v1/` references in our plan-server doc comments removed (`server/plan_fail.go:11`, `cmd/doctor.go:121`).
- **Source:** memory:2026-04-30
- **Merged:** 2026-05-21 in #19

Final slice of the 3-PR migration. PR-1 (#16) added dual-routing server-side; PR-2 (#17) flipped clients; Caddy is wildcarded for `/api/fil/*`. All `fil` binaries have been redeployed and no caller still hits `/api/v1/*`, so the server can drop it cleanly with no 404 risk at runtime.

---

### printer-restart-fires-false-finished-notification
- **Acceptance:**
  - `server/bambu.go:292` guard changed from `if b.state.State != oldState && oldState != ""` to also exclude `oldState == "offline"`, so the synthetic startup/disconnect `"offline"` state never qualifies as a real prior state for callback firing.
  - `server/prusa.go:132` same change.
  - `LastFinishedAt = time.Now()` (server/bambu.go:176-178 and server/prusa.go:156-159) no longer stamps when the prior state is `""` or `"offline"`, so plan-history `FinishedAt` (server/history.go:112-121) isn't overwritten on every restart.
  - New regression test in `server/bambu_test.go`: construct a fresh `BambuAdapter` (initial state `"offline"`), drive `handleReport` with `gcode_state: "FINISH"`, assert (a) no state callback fires and (b) `LastFinishedAt` remains zero. Mirror test in `server/prusa_test.go` for the Prusa adapter.
  - New regression test: drive `offline → FINISH → RUNNING → FINISH` (Bambu) and `offline → FINISHED → PRINTING → FINISHED` (Prusa); assert exactly one callback fires (on the second `FINISH`) and `LastFinishedAt` is set then, not on the first.
  - Existing tests in `server/bambu_test.go`, `server/prusa_test.go`, and any callers of state-change notifications continue to pass unchanged.
- **Source:** direct
- **Merged:** 2026-05-21 in #18

Every plan-server restart triggers an Alexa announcement ("Bambu X1C finished a print") and matching Pushover/ntfy push whenever a Bambu/Prusa is sitting in `FINISH`/`FINISHED` (the natural state between prints until the next job starts). Cause: `NewBambuAdapter` / `NewPrusaAdapter` seed `state.State = "offline"` (server/bambu.go:48, server/prusa.go:36). On (re)connect the first MQTT/HTTP status report transitions `"offline" → "finished"`, which slips past the `oldState != ""` guard, fires the state-change callback, and cmd/serve.go:152 calls `notifier.Speak(...)`. `ConnectionLostHandler` also resets state to `"offline"` (server/bambu.go:67-71), so transient network blips during `FINISH` replay the announcement.

Latent since 2026-04-04 (3416846, adapters introduced); audible since 2026-04-23 (9d97558, voicemonkey wired). Confirmed 2026-05-20 via `GET /api/fil/printers` — X1C reports `state: "finished"` and a `last_finished_at` that matches the most recent redeploy timestamp, not the actual print completion.

Second-order: same flawed guard at server/bambu.go:176-178 stamps `LastFinishedAt = time.Now()` whenever `oldState != "finished" && new == "finished"`, so the timestamp is overwritten on every restart while the printer is parked at `FINISH`. That value feeds plan-history `FinishedAt` (server/history.go:112-121), so print-completion times in history are being silently corrupted to "whenever the server last restarted while the print was still parked at FINISH." Same fix shape fixes both.

### api-fil-prefix-migration-pr2-client-flip
- **Acceptance:**
  - Every plan-server URL in `api/plans_client.go` and `plan/remote_*.go` changed from `/api/v1/...` to `/api/fil/...`.
  - Existing tests in `api/plans_client_test.go` continue to pass with their httptest mock paths updated to `/api/fil/...`.
  - A new regression test asserts at least one representative endpoint actually hits `/api/fil/...` (so a future accidental revert to `/api/v1/...` would fail).
  - Spoolman client (`api/client.go`), Prusa outbound (`server/prusa.go`), and the plan-server `Routes()` registration (`server/handler.go`) are NOT touched — those are out of scope for PR-2.
- **Source:** memory:2026-04-30
- **Merged:** 2026-05-21 in #17

PR-2 of a 3-PR migration. PR-1 (#16) added server-side dual-routing; Caddy now wildcards `/api/fil/*` to the plan server. This slice flips the client side. PR-3 will remove `/api/v1/*` from the server once we're confident nothing still calls it. Safe to ship right now because both prefixes are live end-to-end: server answers both, Caddy routes both.

---

### api-fil-prefix-migration-pr1-dual-routing
- **Acceptance:**
  - `server/handler.go` `Routes()` registers every existing `/api/v1/<suffix>` route under `/api/fil/<suffix>` as well, routing to the same handler.
  - Refactor extracts a route table (slice of `{method+suffix, handler}`) and registers it under both prefixes via a loop; no handler bodies change.
  - New `TestBothPrefixesRoute` table-driven test asserts that every endpoint returns the same HTTP status under both prefixes.
  - All existing `/api/v1/*` tests in `server/handler_test.go` continue to pass unchanged.
- **Source:** memory:2026-04-30
- **Merged:** 2026-05-21 in #16

PR-1 of a 3-PR migration. Goal of this slice: dual-route `/api/v1/*` and `/api/fil/*` on the server so the binary can be redeployed independently. PR-2 will flip client calls + update Caddy; PR-3 will remove `/api/v1/*` server-side. See parent backlog item `api-fil-prefix-migration` for the full migration plan and motivation (single Caddy wildcard for plan-server endpoints).

---

### workflows-bump-actions-checkout-off-node-20-before-deprecation
- **Acceptance:**
  - `.github/workflows/roadmap-merge-sync.yml:15` changed from `actions/checkout@v4` to `actions/checkout@v6`.
  - `.github/workflows/roadmap-issue-sync.yml:15` changed from `actions/checkout@v4` to `actions/checkout@v6`.
  - No other workflow inputs or permissions blocks changed.
  - Post-merge Actions run for `roadmap-merge-sync.yml` completes without the "Node.js 20 actions are deprecated" warning.
- **Source:** gh#13
- **Merged:** 2026-05-20 in #14

Both roadmap automation workflows pin `actions/checkout@v4` (Node.js 20). GitHub forces Node.js 24 default on 2026-06-02; Node.js 20 is removed on 2026-09-16. `actions/checkout@v6` (Node.js 24, stable since 2026-01-09) is a drop-in: identical inputs, the only v6 behavioral change (token under `$RUNNER_TEMP` instead of `.git/config`) is transparent to `git push`. Audit found no other Node-20 actions in `.github/workflows/`.

---

### fetchspoolsbyid-per-id-lookup
- **Acceptance:**
  - Add `FindSpoolByID(ctx context.Context, id int) (models.Spool, error)` to the narrow `Spoolman` interface in `api/`.
  - Implement against Spoolman's `GET /api/v1/spool/{id}` endpoint in the HTTP client.
  - Swap `plan/local_complete.go` `fetchSpoolsByID` (currently lines 33–52) to use per-ID calls instead of `FindSpoolsByName("*", nil, nil)` + filter.
  - Regression tests: interface method round-trips a spool; refactored caller makes N per-ID calls (not one catalog call) for N input IDs.
  - Deduction path is O(deductions), not O(catalog).
- **Source:** gh#9
- **Merged:** 2026-05-20 in #12

Identified by `spoolman-quirks-reviewer` when auditing `ec4e296` (plan-complete migration). Not a correctness issue, but a wasteful catalog fetch on every plate completion (~250 spools and growing). The existing inline comment in `local_complete.go` acknowledges this:

> // Goes through FindSpoolsByName("*") because that's already in the
> // narrow Spoolman interface — adding a per-ID lookup would expand the seam.

Adding `FindSpoolByID` is the seam expansion that comment is gating against.

---

