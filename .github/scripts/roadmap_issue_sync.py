#!/usr/bin/env python3
"""Append a roadmap-labeled GitHub issue to the ## Idea Backlog section.

Triggered by roadmap-issue-sync.yml when an issue is labeled `roadmap`.
Idempotent: re-running for the same gh#N is a no-op.

Usage: roadmap_issue_sync.py <issue_number> <title> <body_file>

The issue body is read from a file (passed via env to avoid shell-escaping
problems with multi-line content).
"""
import re
import sys

ROADMAP_PATH = "roadmap.md"
BODY_TRUNCATE = 500


def slugify(title):
    s = title.lower()
    s = re.sub(r"[^a-z0-9]+", "-", s)
    s = re.sub(r"-+", "-", s)
    return s.strip("-") or "untitled"


def unique_slug(content, base_slug):
    if not re.search(rf"^### {re.escape(base_slug)}\s*$", content, re.MULTILINE):
        return base_slug
    n = 2
    while True:
        candidate = f"{base_slug}-{n}"
        if not re.search(rf"^### {re.escape(candidate)}\s*$", content, re.MULTILINE):
            return candidate
        n += 1


def already_ingested(content, issue_num):
    return bool(re.search(rf"^- \*\*Source:\*\* gh#{issue_num}\b", content, re.MULTILINE))


def main():
    if len(sys.argv) != 4:
        print(f"Usage: {sys.argv[0]} <issue_number> <title> <body_file>", file=sys.stderr)
        sys.exit(2)
    issue_num = sys.argv[1]
    title = sys.argv[2]
    body_file = sys.argv[3]

    with open(body_file) as f:
        body = f.read().strip()
    if not body:
        body = "(no description)"
    if len(body) > BODY_TRUNCATE:
        body = body[:BODY_TRUNCATE].rstrip() + "..."

    with open(ROADMAP_PATH) as f:
        content = f.read()

    if already_ingested(content, issue_num):
        print(f"gh#{issue_num} already in roadmap.md — no-op.")
        return 0

    base_slug = slugify(title)
    slug = unique_slug(content, base_slug)

    new_entry = (
        f"\n### {slug}\n"
        f"- **Source:** gh#{issue_num}\n"
        f"\n"
        f"{body}\n"
    )

    # Append after the last item in ## Idea Backlog (before the next ## section or EOF)
    backlog_pattern = re.compile(
        r"(?ms)^(## Idea Backlog\s*\n)(.*?)(?=^## |\Z)"
    )
    match = backlog_pattern.search(content)
    if not match:
        print("No ## Idea Backlog section in roadmap.md — bailing.", file=sys.stderr)
        return 1

    header = match.group(1)
    body_section = match.group(2).rstrip()
    new_body_section = body_section + "\n" + new_entry + "\n"
    new_content = content[: match.start()] + header + new_body_section + content[match.end():]

    with open(ROADMAP_PATH, "w") as f:
        f.write(new_content)

    print(f"Appended gh#{issue_num} '{title}' as `{slug}` in Idea Backlog.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
