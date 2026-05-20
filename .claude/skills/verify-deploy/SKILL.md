---
name: verify-deploy
description: Post-deploy sanity check for fil on the Pi — hits the plan-server health endpoint, tails recent fil-serve journal, and confirms deployed binary version matches the expected tag. Use after a deploy completes (or after the auto-deploy on push) to confirm the new binary is running cleanly.
disable-model-invocation: true
---

# verify-deploy

Run after a fil deploy to confirm fil-serve is healthy on the Pi. This is a read-only check — never redeploy, roll back, or restart automatically. If something is off, stop and report.

## Steps

1. **Health endpoint** — hit the plan-server `/health` (port and host live in `config.json`; default below):

   ```bash
   curl -sS -w '\n[HTTP %{http_code}]\n' http://raspberrypi4.local:8080/api/v1/health
   ```

2. **Recent journal** — tail the last 30 lines of fil-serve, scan for ERROR/WARN:

   ```bash
   ssh pi@raspberrypi4.local 'sudo journalctl -u fil-serve -n 30 --no-pager'
   ```

3. **Version match** — compare deployed version against the expected tag:

   ```bash
   expected=$(git describe --tags --abbrev=0)
   deployed=$(ssh pi@raspberrypi4.local '/usr/local/bin/fil version')
   echo "expected=$expected  deployed=$deployed"
   ```

## Reporting

Report exactly three lines:
- `health:   <status_code> <body or PASS/FAIL>`
- `journal:  <count> ERROR / <count> WARN in last 30 lines (highlights inline if any)`
- `version:  <expected> vs <deployed> — <MATCH or MISMATCH>`

If any line is FAIL/MISMATCH or there are ERROR/WARN highlights, stop and surface the raw output. Do not attempt to fix.
