#!/usr/bin/env bash
# SessionStart hook for fil roadmap status.
#
# Output behavior (silent when no signal):
#   A. If `## In Flight` has items: list them with PR/branch.
#   B. Else if `## Ready` has items: report counts and the next-up item.
#   C. Else: silent.
#
#   Override: if current git branch is `roadmap/<slug>` and that slug is
#   In Flight, surface that specifically.
#
# Pure local read; no network. Uses awk for parsing, no yq dependency.

set -u

ROADMAP="roadmap.md"
[ -f "$ROADMAP" ] || exit 0

current_branch=$(git branch --show-current 2>/dev/null || true)

# Section → slug list. One "section|slug" per line.
sections_and_items=$(awk '
  /^## / { section = $0; gsub(/^## /, "", section); next }
  /^### / && section { gsub(/^### /, ""); print section "|" $0 }
' "$ROADMAP")

# Item metadata bullets. One "slug|key|value" per line.
metadata=$(awk '
  /^### / { gsub(/^### /, ""); slug = $0; next }
  /^## / { slug = ""; next }
  slug && /^- \*\*[A-Za-z-]+:\*\*/ {
    match($0, /\*\*[A-Za-z-]+:\*\*/)
    key = substr($0, RSTART+2, RLENGTH-4)
    value = substr($0, RSTART+RLENGTH)
    sub(/^[ \t]+/, "", value)
    print slug "|" key "|" value
  }
' "$ROADMAP")

get_meta() {
  local slug="$1" key="$2"
  printf '%s\n' "$metadata" | awk -F '|' -v s="$slug" -v k="$key" '$1 == s && $2 == k { print $3; exit }'
}

count_lines() {
  [ -z "$1" ] && { echo 0; return; }
  printf '%s\n' "$1" | wc -l | tr -d ' '
}

in_flight=$(printf '%s\n' "$sections_and_items" | awk -F '|' '$1 == "In Flight" { print $2 }')
ready=$(printf '%s\n' "$sections_and_items" | awk -F '|' '$1 == "Ready" { print $2 }')
backlog=$(printf '%s\n' "$sections_and_items" | awk -F '|' '$1 == "Idea Backlog" { print $2 }')

# Branch-awareness override: working on a roadmap/* branch that matches an In Flight item
if [ -n "${current_branch}" ] && [[ "$current_branch" == roadmap/* ]]; then
  slug="${current_branch#roadmap/}"
  if printf '%s\n' "$in_flight" | grep -qx "$slug" 2>/dev/null; then
    pr=$(get_meta "$slug" "PR")
    echo "Roadmap: you're on $current_branch (In Flight, PR ${pr:-unknown})"
    exit 0
  fi
fi

# A: In Flight has items
if [ -n "$in_flight" ]; then
  echo "Roadmap: in flight"
  while IFS= read -r slug; do
    [ -z "$slug" ] && continue
    pr=$(get_meta "$slug" "PR")
    branch=$(get_meta "$slug" "Branch")
    echo "  - $slug   PR ${pr:-unknown}   branch ${branch:-unknown}"
  done <<< "$in_flight"
  echo "Finish in-flight work before starting new items."
  exit 0
fi

# B: No In Flight, Ready has items
if [ -n "$ready" ]; then
  ready_count=$(count_lines "$ready")
  backlog_count=$(count_lines "$backlog")
  next_up=$(printf '%s\n' "$ready" | head -1)
  echo "Roadmap: ${ready_count} ready, ${backlog_count} backlog"
  echo "Next up: ${next_up}"
  echo "Ask the roadmap-groomer agent to propose it."
  exit 0
fi

# C: silent
exit 0
