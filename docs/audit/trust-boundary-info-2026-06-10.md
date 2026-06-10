# Trust Boundary INFO: Bundled Skill Scripts

Date: 2026-06-10

## Decision

The audit INFO finding for self-installed skill bundled scripts is accepted as a deliberate trust boundary, not treated as a defect. No bundled-script scanner is added.

## Boundary

`internal/skills/loader.go` scans `SKILL.md` body text and frontmatter description against the configured blocklist. Bundled helper scripts that live beside `SKILL.md` are not blocklist-scanned by the loader.

That is intentional under the full-host-terminal trust model from PRD amendment #50 / D-15c. A self-installed skill's bundled script is equivalent to the model writing code and running it through the deliberate host execution surface. The host is the capability boundary; scanning those bundled files would not materially constrain an actor that can already author and execute host-side code.

## Audit Link

The source audit line is `docs/audit/deep-correctness-audit-2026-06-10.md` INFO: self-installed skill bundled scripts are not blocklist-scanned; `loader.go:213-220` scans only `SKILL.md` body and description.

## What Remains Enforced

The existing loader blocklist still applies to the `SKILL.md` body and description before a skill enters model context. Writer-side validation and name/path guards remain the control points for skill structure and filesystem placement.

## What Is Not Added

No scanner is added for bundled scripts in this phase. The explicit decision is to document the trust boundary and leave `internal/skills/loader.go` unchanged.
