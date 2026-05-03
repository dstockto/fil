# Fil Context

Fil is a CLI for managing 3D-printer filament inventory backed by Spoolman, with a plan-server mode that coordinates multi-machine print **Plans**. This file defines the domain language used in code and conversation.

## Language

**Spool**:
A physical filament reel tracked in Spoolman. Identified by Spoolman's spool ID; carries a filament profile, a remaining-weight value, and a current location.
_Avoid_: roll, reel, cartridge.

**Spoolman**:
The external REST service (open-source, third-party) that owns the truth about Spools, locations, and usage history. Fil is a client of Spoolman, never its replacement.
_Avoid_: inventory service, backend.

**Plan**:
A YAML file describing one or more print **Projects** along with their **Plates** and filament requirements. Plans are the unit of work the CLI and TUI operate on.
_Avoid_: job, build, print run.

**Project**:
A printable thing inside a Plan, composed of one or more **Plates**. A Plan can contain multiple Projects (e.g., a multi-part assembly).
_Avoid_: model, item.

**Plate**:
One physical print-bed run of one Project — the smallest schedulable unit. Plates carry filament needs and a lifecycle status (e.g., pending, printing, complete).
_Avoid_: bed, layer, print, session.

**plan-server**:
The HTTP server (in `server/`) that hosts shared Plans for multi-machine access. One canonical instance runs centrally; CLIs on other machines treat plans hosted there as **remote Plans**.
_Avoid_: API, backend, daemon.

**Local Mode**:
The CLI is configured without a plan-server URL. Plans are discovered from CWD and `plans_dir`; the CLI mutates Plan files and calls Spoolman directly. This is one of two mutually-exclusive deployment modes.

**Remote Mode**:
The CLI is configured with a plan-server URL. All Plan operations are performed by the plan-server over HTTP; the CLI does not touch Plan files or Spoolman directly. This is the other of two mutually-exclusive deployment modes.

**Fail (a Plate)**:
A *logged event* recording wasted filament and notes against a Plate, plus the corresponding Spoolman deduction. It is **not** a Plate lifecycle status — failing a Plate does not transition it to a "failed" state.
_Avoid_: treating "failed" as a status value.

**TD-1**:
A handheld colorimeter device (`devices/td1.go`) used to read filament colors. Readings are unreliable for dark and opaque filaments — a physical limitation, not a software bug.

## Relationships

- A **Plan** contains one or more **Projects**; a **Project** contains one or more **Plates**.
- A **Plate** declares filament needs that resolve to specific **Spools** in Spoolman.
- A fil installation runs in either **Local Mode** or **Remote Mode** — not both. The mode is determined by config at startup.
- In **Remote Mode**, the **plan-server** mutates Spoolman on behalf of the CLI; in **Local Mode**, the CLI mutates Spoolman directly. Spoolman is only ever called by one side.
- **Fail** events deduct filament from **Spools** in Spoolman and append a history record, but leave the **Plate**'s lifecycle status untouched.

## Example dialogue

> **Dev:** "When we **fail** a **Plate**, do we set its status to `failed`?"
> **Domain expert:** "No — `fail` is a log event, not a state. The **Plate** keeps whatever lifecycle status it already had. We record the wasted grams against the **Spools** and write a history entry, that's it."
> **Dev:** "And if we're in **Remote Mode**, the deduction happens on the plan-server?"
> **Domain expert:** "Right. In **Local Mode** the CLI deducts; in **Remote Mode** the plan-server deducts. A single fil install is one or the other — never both. Spoolman is only ever called once per event."

## Flagged ambiguities

- "fail" was previously ambiguous: was it a Plate status or a log event? Resolved: log event only.
