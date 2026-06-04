---
phase: 16-mcp-sidecar-manager-third-party-trust
plan: 01
subsystem: docs
tags: [mcp, trust, profiles, streamable-http, policy]
requires:
  - phase: 09-swarm-minimal
    provides: MCP boot mount, fail-soft behavior, mail/whatsapp recipes
provides:
  - Phase 16 CAP-09 / MCP-V2-01 requirement promotion
  - MCP manager trust-class decision contract
  - Standalone Aura MCP Manager design spec
affects: [mcp, mcptools, cli, roadmap, requirements]
tech-stack:
  added: []
  patterns: [doc-amendment-gate, trust-classes, manager-control-plane]
key-files:
  created:
    - docs/superpowers/specs/2026-06-04-aura-mcp-manager-design.md
  modified:
    - prd.md
    - .planning/REQUIREMENTS.md
    - .planning/ROADMAP.md
    - .planning/DECISIONS.md
key-decisions:
  - "CAP-09 / MCP-V2-01 is promoted into v1 as Phase 16."
  - "Trust classes are trusted_recipe, trusted_local, sandboxed_local, remote_http, and blocked."
  - "OpenClaw plugin hosting, marketplace auto-install, and restart supervision stay out of Phase 16."
patterns-established:
  - "MCP manager is a control plane over managed config; stdio/HTTP clients and mcptools remain the data plane."
  - "Third-party local MCP commands default to blocked until explicitly trusted or sandboxed."
requirements-completed: [MCP-V2-01, CAP-09]
duration: 20 min
completed: 2026-06-04
---

# Phase 16 Plan 01: MCP Manager Amendment Summary

**CAP-09 / MCP-V2-01 promoted into a Phase 16 MCP manager contract with trust classes, OpenClaw separation, and a standalone design spec**

## Performance

- **Duration:** 20 min
- **Started:** 2026-06-04T13:45:00Z
- **Completed:** 2026-06-04T14:05:28Z
- **Tasks:** 2
- **Files modified:** 5

## Accomplishments

- Promoted the deferred generic MCP manager into v1 as `CAP-09 / MCP-V2-01`.
- Added concrete Phase 16 roadmap success criteria for profiles, trust, Streamable HTTP, sandboxed local runtime, doctor/status/logs, and tool policy.
- Recorded the trust classes and safety boundaries in `.planning/DECISIONS.md`.
- Created the standalone Aura MCP Manager design contract for the implementation waves.

## Task Commits

Each task was committed atomically:

1. **Task 1: Promote MCP-V2-01/CAP-09 into Phase 16 scope** - `6aaf4bc6` (docs)
2. **Task 2: Write the MCP manager design spec** - `b667d85a` (docs)

**Plan metadata:** this summary commit.

## Files Created/Modified

- `prd.md` - Adds Amendment #45 for the Phase 16 MCP Manager + Third-Party Trust scope.
- `.planning/REQUIREMENTS.md` - Promotes `MCP-V2-01` into v1 as `CAP-09`.
- `.planning/ROADMAP.md` - Adds concrete Phase 16 success criteria.
- `.planning/DECISIONS.md` - Records Phase 16 trust, OpenClaw, no-supervisor, and no-marketplace decisions.
- `docs/superpowers/specs/2026-06-04-aura-mcp-manager-design.md` - Defines architecture, config model, trust classes, profiles/catalogs, Docker runtime isolation, Streamable HTTP, status/doctor/logs, risk labels, command surface, and validation.

## Decisions Made

- The MCP manager is now a v1 requirement, not a v2 placeholder.
- `trusted_recipe`, `trusted_local`, `sandboxed_local`, `remote_http`, and `blocked` are the canonical trust classes.
- OpenClaw plugin-host work remains separate from MCP server management.
- Phase 16 keeps the Phase 9 lifecycle posture: fail-soft and on-demand diagnostics, no restart supervisor.

## Deviations from Plan

None - plan executed exactly as written.

**Total deviations:** 0 auto-fixed.
**Impact on plan:** No scope creep; the design contract matches the expanded Phase 16 context.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

Wave 1 can implement managed config v2 against the documented `CAP-09 / MCP-V2-01` contract. The code waves should preserve backwards compatibility with existing `~/.aura/mcp/servers.json` while adding trust/profile/runtime metadata.

---
*Phase: 16-mcp-sidecar-manager-third-party-trust*
*Completed: 2026-06-04*
