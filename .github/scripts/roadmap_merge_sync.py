#!/usr/bin/env python3
"""Move a roadmap item from its current section to ## Done.

Triggered by roadmap-merge-sync.yml when a PR with `Roadmap-Item: <slug>` in
its body is merged to main. Trims ## Done to the last 20 entries.

Usage: roadmap_merge_sync.py <slug> <pr_number>
"""
import re
import sys
from datetime import datetime, timezone

ROADMAP_PATH = "roadmap.md"
DONE_TRIM = 20


def split_sections(content):
    """Return (preamble, [(header, body), ...]) where header is '## Name'."""
    parts = re.split(r"^(## .+)$", content, flags=re.MULTILINE)
    preamble = parts[0]
    sections = []
    for i in range(1, len(parts), 2):
        header = parts[i]
        body = parts[i + 1] if i + 1 < len(parts) else ""
        sections.append([header, body])
    return preamble, sections


def assemble(preamble, sections):
    return preamble + "".join(h + b for h, b in sections)


def find_section_with_item(sections, slug):
    """Return index of the section containing `### <slug>`, or None."""
    pattern = re.compile(rf"^### {re.escape(slug)}\s*$", re.MULTILINE)
    for idx, (_, body) in enumerate(sections):
        if pattern.search(body):
            return idx
    return None


def extract_block(body, slug):
    """Pull the `### <slug> ... ` block out of body. Returns (new_body, block_text)."""
    pattern = re.compile(
        rf"(?ms)^(### {re.escape(slug)}\s*\n.*?)(?=^### |\Z)"
    )
    match = pattern.search(body)
    if not match:
        return body, None
    block = match.group(1)
    new_body = body[: match.start()] + body[match.end():]
    return new_body, block


def rewrite_block_for_done(block, pr_num, merge_date):
    """Strip Branch/PR bullets entirely; insert Merged right after Source (or H3)."""
    stripped = [
        line for line in block.split("\n")
        if not re.match(r"^- \*\*(Branch|PR):\*\*", line)
    ]

    out_lines = []
    inserted = False
    merged_line = f"- **Merged:** {merge_date} in #{pr_num}"
    for line in stripped:
        out_lines.append(line)
        if not inserted and re.match(r"^- \*\*Source:\*\*", line):
            out_lines.append(merged_line)
            inserted = True

    if not inserted:
        # No Source bullet — insert after the H3 header instead
        rebuilt = []
        for line in out_lines:
            rebuilt.append(line)
            if not inserted and line.startswith("### "):
                rebuilt.append(merged_line)
                inserted = True
        out_lines = rebuilt

    return "\n".join(out_lines).rstrip() + "\n\n"


def prepend_to_done(done_body, block):
    """Insert block at the top of Done's body (after any leading comments/blanks)."""
    lines = done_body.split("\n")
    insert_idx = 0
    for i, line in enumerate(lines):
        stripped = line.strip()
        if stripped == "" or stripped.startswith("<!--"):
            insert_idx = i + 1
        else:
            break
    head = "\n".join(lines[:insert_idx])
    tail = "\n".join(lines[insert_idx:])
    if head and not head.endswith("\n"):
        head += "\n"
    return head + block + tail


def trim_done(done_body, max_items):
    """Keep only the first max_items ### blocks; drop the rest."""
    pattern = re.compile(r"(?ms)^(### .+?)(?=^### |\Z)")
    matches = list(pattern.finditer(done_body))
    if len(matches) <= max_items:
        return done_body
    # Everything before the (max_items+1)th match stays
    cutoff = matches[max_items].start()
    return done_body[:cutoff].rstrip() + "\n"


def main():
    if len(sys.argv) != 3:
        print(f"Usage: {sys.argv[0]} <slug> <pr_number>", file=sys.stderr)
        sys.exit(2)
    slug = sys.argv[1]
    pr_num = sys.argv[2]
    merge_date = datetime.now(timezone.utc).strftime("%Y-%m-%d")

    with open(ROADMAP_PATH) as f:
        content = f.read()

    preamble, sections = split_sections(content)

    src_idx = find_section_with_item(sections, slug)
    if src_idx is None:
        print(f"Slug '{slug}' not found in roadmap.md — nothing to do.", file=sys.stderr)
        return 0

    src_header, src_body = sections[src_idx]
    new_src_body, block = extract_block(src_body, slug)
    if block is None:
        print(f"Could not extract block for '{slug}' — nothing to do.", file=sys.stderr)
        return 0
    sections[src_idx][1] = new_src_body

    # Find Done section
    done_idx = next(
        (i for i, (h, _) in enumerate(sections) if h.strip() == "## Done"),
        None,
    )
    if done_idx is None:
        print("No ## Done section in roadmap.md — bailing.", file=sys.stderr)
        return 1

    modified_block = rewrite_block_for_done(block, pr_num, merge_date)
    sections[done_idx][1] = prepend_to_done(sections[done_idx][1], modified_block)
    sections[done_idx][1] = trim_done(sections[done_idx][1], DONE_TRIM)

    with open(ROADMAP_PATH, "w") as f:
        f.write(assemble(preamble, sections))

    print(f"Moved '{slug}' from '{src_header.strip()}' to Done (PR #{pr_num}, merged {merge_date}).")
    return 0


if __name__ == "__main__":
    sys.exit(main())
