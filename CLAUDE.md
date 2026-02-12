# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Fil is a CLI tool for managing 3D printer filament inventory via Spoolman (an open-source spool management system). It tracks spools, locations, usage, and multi-plate print projects.

## Build & Development Commands

```bash
go build -o fil ./              # Build the binary
go test ./...                   # Run all tests
go test -v ./cmd/...            # Run command tests (verbose)
go test -run TestFindFilters ./cmd  # Run a single test
golangci-lint run ./...         # Lint (wsl linter is disabled)
gofmt -w ./...                  # Format
```

No Makefile; standard Go toolchain with `go.mod` (Go 1.23.1). Pure Go SQLite via `modernc.org/sqlite` — no CGO required.

## Architecture

- **`main.go`** — Entry point, calls `cmd.Execute()`
- **`cmd/`** — Cobra command implementations. Each file defines a command (`find.go`, `use.go`, `move.go`, `low.go`, `archive.go`, `plan.go`, `clean.go`). Commands register via `init()` functions. `root.go` handles config loading and global flags. `interactive.go` provides TTY-aware spool selection with promptui. `utils.go` has shared helpers (aliases, parsing).
- **`models/`** — Data structures: `Spool` (with colored terminal display), `Project`/`Plan`/`Plate` (YAML-based project files)
- **`api/`** — `SpoolmanAPI` interface and HTTP client for the Spoolman REST API (`FindSpoolsByName`, `UseFilament`, `MoveSpool`, etc.)
- **`db/`** — SQLite wrapper (`db.Client`) with `MaxOpenConns=1` for CLI use

### Key Patterns

- **Config merging**: Configs load in order HOME → XDG → CWD, later overrides earlier. `--config` flag bypasses merging.
- **Spool filtering**: `SpoolFilter` function type applied in-process after API queries (see `find.go` for filter implementations like `onlyStandardFilament`, `ultimakerFilament`).
- **Interactive selection**: `promptui.Select` with live filtering; auto-disabled when not a TTY or `--non-interactive` is passed.
- **Plan system** (`cmd/plan.go`): YAML project files with projects containing plates, each with filament needs. Supports lifecycle: new → resolve → check → next → complete → archive. Plans discovered from CWD and configured `plans_dir`.
- **Dry-run support**: `--dry-run` flag on mutating commands (`use`, `move`, `archive`).

### Test Patterns

Table-driven tests using `testing` stdlib. Config tests use `os.MkdirTemp` for isolation. No test framework dependencies.
