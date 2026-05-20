#!/usr/bin/env bash
# Print roadmap-labeled GitHub issues that aren't yet in roadmap.md.
#
# Catches sync drift — e.g., an issue was labeled `roadmap` while the
# issue-sync workflow was offline, or before that workflow shipped to main.
#
# Usage: .github/scripts/roadmap_drift.sh
# Requires: gh (authenticated), python3

set -euo pipefail
ROADMAP="${ROADMAP:-roadmap.md}"
export ROADMAP
[ -f "$ROADMAP" ] || { echo "No $ROADMAP found in cwd" >&2; exit 1; }

gh issue list -l roadmap --state open --limit 100 --json number,title | \
python3 -c "
import json, os, re, sys
with open(os.environ['ROADMAP']) as f:
    content = f.read()
issues = json.load(sys.stdin)
missing = [i for i in issues if not re.search(rf'gh#{i[\"number\"]}\b', content)]
if not missing:
    print('No drift — every open roadmap-labeled issue is already in roadmap.md.')
    sys.exit(0)
print(f'{len(missing)} issue(s) missing from roadmap.md:')
for i in missing:
    print(f'  gh#{i[\"number\"]} - {i[\"title\"]}')
print()
print('To ingest manually:')
print('  python3 .github/scripts/roadmap_issue_sync.py <num> \"<title>\" <body-file>')
print('Or re-apply the roadmap label on the issue to trigger roadmap-issue-sync.yml')
print('(once that workflow has shipped to main).')
"
