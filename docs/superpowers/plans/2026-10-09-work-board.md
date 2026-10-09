# Work board: one Kanban the identity and its agent share

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A board per identity, worked by the operator in a cockpit mode built on SVAR React Kanban and by the agent through a deferred `board` tool; every card says where it came from, and the operator can save filtered views.

**Architecture:** Three tables under the 0087 RLS pair (`aura.boards`, `aura.board_cards`, `aura.board_views`), one store package (`internal/board`), one deferred multiplexed tool (`internal/agent/tools/board.go`) with its gateway classifier, one REST mount (`internal/agui/board_api.go`) in the widget's own dialect, and one cockpit mode (`web/src/board`).

**Tech Stack:** Go 1.27, PostgreSQL 16 + sqlc 1.31.1, React 19 + `@svar-ui/react-kanban` 2.6.0 (MIT), vitest.

**Spec:** `docs/superpowers/specs/2026-10-09-work-board-design.md`, answered questions included, plus item 3a of `2026-10-09-consolidation-best-of-design.md` (card source, saved views).

## Global Constraints

- Files ≤600 lines. Slot `0141` (`ls internal/db/migrations | tail -1` printed `0140_gateway_tool_policies.up.sql`). `sqlc generate` after query edits.
- Disposable Postgres recipe as in the tool-policy plan. No live database.
- Every unsafe route is inventoried in `idempotency_http.go`; the cockpit's fetch wrapper adds the key.

## Decisions taken while writing this plan (measured on the installed widget, 2026-10-09)

- **`GET /api/board/cards` returns an array.** `RestDataProvider.getData` calls `send("cards","GET")` and `parseCards` iterates the result; the board's name and columns come from `GET /api/board`.
- **No id reconciliation code.** `ActionQueue.tryExec` maps a temporary id to `res.id` when the `POST` answers `{id}`; the mount answers the stored card, id included.
- **The provider does not check HTTP status.** `sendRequest` is `fetch(...).then(res => res.json())`: a 500 with a JSON body reads as success. The cockpit subclasses the provider and overrides the public `send` to check `res.ok`; on failure it shows the error and reloads the board from the server, and returns `{}` so the widget's queue does not hang. That is acceptance item 3 answered in code; the lab run confirms it.
- **The free edition has no add-column or delete-column action**, only `update-column` (label, limit, collapsed). Columns are added and removed through `PUT /api/board/columns` from a small editor in the board's header; the agent does not edit columns in this release.
- **No `aura.board` frame.** `useRunSignals` records the measurement: the cockpit's surfaces are mutually exclusive, so a run and the board are never on screen in the same tab, and a frame would reach no mounted board. The board query refetches on mount and on window focus, as the scheduler board does.
- **`source` replaces `created_by`.** Values: `cockpit` (the operator on the board), `chat` (the agent with a live responder), `background` (the agent in a scheduled job or a swarm child, `gateway.HasResponder` false). Who created a card follows from where; `updated_by` (`operator`|`agent`) stays, because the last hand can differ from the first.
- **Seen ids are per conversation**, kept by the tool keyed like `TodoTool`; an id returned by `list`, `search` or `add` in this conversation may be updated, moved or deleted. A stricter per-turn reset adds nothing against invented ids and costs a `list` per turn.
- **Search is `ILIKE` on label and description plus an exact tag match**, bounded to 32 rows. A personal board is small; a `tsvector` column is a later measurement.
- **Priorities follow the widget**: 1 Low, 2 Medium, 3 High (`getPriorityOptions`).

## File structure

| File | Task |
|---|---|
| `internal/db/migrations/0141_work_board.{up,down}.sql`, `internal/db/queries/board.sql`, `internal/db/sqlc/`, `internal/db/migrate_0141_integration_test.go` | 1 |
| `internal/board/{board.go,cards.go,views.go}` + unit and integration tests | 2 |
| `internal/agent/tools/board.go`, `board_actions.go` + tests; `internal/gateway/classify.go` (`classifyBoard`) + test; `cmd/aura/main.go`, `serve_adapters.go` | 3 |
| `internal/agui/board_api.go`, `board_views_api.go` + tests; `idempotency_http.go`; `cmd/aura/serve_webui*.go`, `serve_agui.go` | 4 |
| `web/src/board/*`, `web/src/shell/modes.ts`, `AppShell.tsx`, `resources.board.ts` + tests | 5 |
| skill/prompt line, closing gates | 6 |

### Task 1: Tables — [ ]
### Task 2: Store — [ ]
### Task 3: Tool and classifier — [ ]
### Task 4: REST mount — [ ]
### Task 5: Cockpit mode — [ ]
### Task 6: Closing gates — [ ]
- [ ] Lab-VM acceptance (spec, Testing, items 1-5): open.
