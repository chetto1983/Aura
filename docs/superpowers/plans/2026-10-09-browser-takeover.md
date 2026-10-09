# Browser takeover: one hand on the box browser at a time

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** While the operator drives a box browser session in the live view, the agent's writes on that session are refused at once with an error it understands; after the operator lets go, the agent's first element action by `@eN` reference is refused until it has re-read the page.

**Architecture:** A leaf registry (`internal/browsercontrol`) holds who drives each (identity, session) and which sessions went stale. The live-view routes write it: a viewer's input takes the hold, a `control` route releases or re-takes it, the end of the viewer's stream releases it. The MCP bridge reads it through `MountOptions.Browser`, the same per-mount seam `Files` uses, and refuses or refreshes per call. The cockpit shows the state and a release button.

**Tech Stack:** Go 1.27, React 19 + vitest.

**Spec:** `docs/superpowers/specs/2026-10-09-browser-takeover-design.md`, answered questions included. PRD §12, "The operator takes the browser over".

## Global Constraints

- Files at or under 600 lines; `browser_live.go` is at 295, so the control route goes in `browser_control.go`.
- After every Go edit: vet, build, `go test -race` of the package. No database tier is involved.
- The existing live-view behaviour (frames, input, a second viewer replacing the first, the ChatGPT login view) stays as it is; the new state only adds refusals on the agent's side.
- Commit per task, with the why.

## Review Focus

1. **The agent's writes are refused while the operator drives, its reads are not.** Pinned by `TestBrowserRefusalTable` (all 29 recipe tools × held × stale).
2. **A stale `@eN` selector is refused until a result carrying `[ref=eN]` references comes back.** Pinned by `TestBrowserExecuteRefreshesOnReferences`.
3. **Only the holder releases.** A replaced viewer's late stream end does not free the newer viewer's hold. Pinned by `TestOnlyTheHolderReleases` and `TestBrowserControlFollowsTheViewer`.
4. **No registry wired is no refusal**, for the CLI, `aura toolpipe` and every test double.

## Decisions taken while writing this plan

- **A viewer's first input takes the hold; there is no separate "take over" click.** The spec proposed watching by default and an explicit take-over. The same live view serves the ChatGPT sign-in and every site login, where no agent is driving and an extra click would be a regression with no safety gained. The property the spec exists for is kept whole: while the operator's hand is on the page, the agent cannot write to it. The cockpit shows "You are driving" and a "Let Aura drive" button that releases; closing the view releases too.
- **The stale check reads the selector, not the tool.** A call is refused as stale only when its `selector` is an `@eN` reference; a CSS selector does not depend on a snapshot's numbering.
- **Refresh fires on any result whose text carries `[ref=eN]`**, the snapshot format measured in `spikes/agent-browser-auth/FINDINGS.md` (A1). That answers the spec's open question on `read` by measurement of the output, not by a tool list.
- **The control route is exempt from the idempotency inventory**, like `input`: nothing durable is written, and a replayed release is a release.

## File structure

| File | Task | Responsibility |
|---|---|---|
| `internal/browsercontrol/registry.go` (+ test) | 1 | Hold, Release by holder, Held, HeldBy, Stale, Refresh |
| `internal/agent/mcptools/bridge_browser_control.go` (+ test) | 2 | `BrowserControl` seam, refusal, refresh |
| `internal/agent/mcptools/mount.go`, `bridge_supervisor.go`, `bridge_call.go` | 2 | `MountOptions.Browser` → `MountedServer.browser` → `Execute` |
| `internal/agui/browser_control.go` (+ test), `browser_live.go`, `server.go`, `mutation_coverage_test.go` | 3 | Hold on input, release on stream end, `POST .../control` |
| `cmd/aura/serve_agui.go`, `mcp_tools.go`, runtime handles | 3 | One registry for both sides |
| `web/src/browserLive/useBrowserLive.ts`, `BrowserLiveView.tsx`, `resources.browserLive.ts` (+ tests) | 4 | Driving state, release button |
| `internal/skills/embed/browser-aura/SKILL.md` | 5 | The two refusals in the skill's words |

### Task 1: Registry — [x] done with 100% statement coverage and a property test.
### Task 2: Bridge
- [x] Failing tests, then `BrowserControl{Held, Stale, Refresh}`, `browserGuard`, `refreshesReferences`; wire through `MountOptions` and `Execute`.
### Task 3: Live view routes and wiring
- [x] Failing tests, then hold on input, release on stream end, the control route, the exemption, one registry from the composition root.
### Task 4: Cockpit
- [x] Failing tests, then the hook's `driving` state and `release()`, the header chip and button, keys in en and it.
### Task 5: Skill and closing gates
- [x] Skill text; vet, build, race on touched packages; CI linter; web typecheck, oxlint, vitest.
- [ ] Lab-VM acceptance (spec, Testing, items 1-5): open; each run leaves its file in `docs/superpowers/verification/`.
