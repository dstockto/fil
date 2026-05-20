#!/usr/bin/env bash
# PostToolUse hook: runs gofmt on the edited file if it's a .go file.
# Input: JSON payload on stdin from Claude Code (tool_input.file_path).
set -euo pipefail

file=$(jq -r '.tool_input.file_path // empty')

if [[ "$file" == *.go && -f "$file" ]]; then
  gofmt -w "$file"
fi
