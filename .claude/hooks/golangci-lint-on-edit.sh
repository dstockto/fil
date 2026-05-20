#!/usr/bin/env bash
# PostToolUse hook: runs golangci-lint on the package containing the edited file.
# Scoped to files inside the fil repo so other Go projects aren't linted from here.
# Input: JSON payload on stdin from Claude Code (tool_input.file_path).
set -uo pipefail

REPO_ROOT="/Users/davidstockton/Projects/go/fil"

file=$(jq -r '.tool_input.file_path // empty')

if [[ "$file" != *.go || "$file" != "$REPO_ROOT"/* || ! -f "$file" ]]; then
  exit 0
fi

cd "$(dirname "$file")"
golangci-lint run ./... 2>&1 || true
