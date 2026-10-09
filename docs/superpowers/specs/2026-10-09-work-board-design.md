# Work board: one Kanban the identity and its agent share

Proposed on 2026-10-09 at `1c872a1`, after reading OpenDots' Spaces and Pages and StickyFlow
(`chetto1983/stickyflow` at `e36c4eb`) against Aura's tree (prd.md §16, "A work board for the
identity and its agent"). Not yet agreed with the operator beyond one choice they made: the
board, not the wiki. Every other decision below is a proposal for review, and the PRD
paragraph records a reading of the tree, not a live run.

OpenDots keeps a Dot's work as pages in a Space, drafted by the Dot and approved by the owner.
StickyFlow keeps it as cards on a board, and its copilot acts on the board through tools:
`create_task`, `move_task`, `edit_task`, `delete_task`, `schedule_task`, `search_tasks` and
more, reading a bounded snapshot of the project and naming only ids the snapshot carries.
Aura has the second kind of agent and no board.

## The problem, measured in the tree

- There is no durable unit of work the operator and the agent share. `internal/agent/tools/todo.go`
  is a per-session scratchpad the model rewrites whole; nothing persists it and the cockpit never
  shows it. `task` (`task.go`) is the scheduler's verb: schedule, list, cancel, run_now, pause,
  resume. A swarm delegation ends as a record card written to `aura.conversation_turns`
  (`internal/swarm/delegation_card.go`). The governance `SchedulerBoard` is a master-detail list
  of scheduler rows over `BoardLayout`, not a board of work.
- StickyFlow's board is hand-written (`components/Board/BoardView.tsx`) over Supabase with
  realtime sync (`hooks/boardSync`); its SVAR dependency is `@svar-ui/react-gantt`, not the
  Kanban. Its model: `Note` with `columnId`, `projectId`, `priority`, `tags`, `dueDate`,
  `endDate`, `progress`, `parentNoteId`, `linkedDocumentId`; `Project` with `columns[]` of
  `{id, title, wipLimit}`; default columns `todo`, `in-progress` (WIP 5), `done`. Its copilot
  reads a snapshot capped at 32 notes of 160 characters (`supabase/functions/ai-chat/project/snapshot.ts`),
  validates every id against the snapshot's sets, and is told never to show raw ids.
- `@svar-ui/react-kanban` 2.6.0: MIT, `react >= 18`, depends on `@svar-ui/react-core` 2.6.0
  and thirteen more `@svar-ui` packages, among them the Excel import, the export popup and
  `xlsx-writer-lite`, which ship although export is the paid edition. The cockpit pins
  `@svar-ui/react-core` 2.6.1 through an `overrides` entry for the file manager (2.6.0), so
  the Kanban resolves to the same core. Free: board, columns, cards, editor, context menu,
  filter, sort, search, custom card rendering, virtualization, `RestDataProvider`. Paid:
  `history` (undo/redo), `dynamicData`, `export-data`. Properties (16): `card`, `cardContent`,
  `cardCss`, `cardPopup`, `cards`, `columnAccessor`, `columnCss`, `columns`, `dynamicData`,
  `filters`, `history`, `init`, `readonly`, `render`, `sort`, `tooltip`. Actions (14) include
  `add-card`, `update-card`, `move-card`, `delete-card`, `duplicate-card`, `update-column`,
  `select-card`, `filter-cards`, `sort-cards`, `provide-data`, `request-data`. A card is `id`
  plus any fields; column membership is the `column` field by default. The provider maps
  `add-card` to `POST /cards`, `update-card` to `PUT /cards/:id` debounced 500 ms, `move-card`
  to `PUT /cards/:id/move`, `delete-card` to `DELETE /cards/:id`, `duplicate-card` to
  `POST /cards/:id/duplicate`, and loads with `GET /cards`. The documentation names no
  realtime channel, no header customisation and no id reconciliation after `POST`.
- The widget's own samples (`svar-widgets/react-kanban`, `demos/cases`, read 2026-10-09 at the
  operator's request, `docs.svar.dev/react/kanban/samples/#/base/willow`): columns are
  `{id, label, cardLimit, addCard}` (the sample's `doing` has `cardLimit: 2`); a card carries
  `label`, `description`, `column`, `cover`, `priority` 1..3, `progress` 0..1, `deadline` (a
  `Date`), `tags`, `users`, and the counts `attachments`, `comments`, `tasks`; the `card` prop
  is the set of booleans that says which of those sections a card shows. The editor is a
  separate `Editor` component fed `items` (`text`, `textarea`, the stock `priorityItem` and
  `progressItem`, `comments` with `users`, `tasks` as a checklist), placed as `modal` or
  sidebar, with `autoSave` and a custom `bottomBar`; `getEditorItems(card)` derives the
  default items from the `card` flags. `SaveToBackend` creates `new RestDataProvider(url)`
  once, calls `api.setNext(provider)` in `init`, intercepts `add-card` to default a field,
  and loads with `provider.getData()` in an effect; it shows no id reconciliation. `Locales`
  wraps `Kanban` and `Editor` in `Locale` with the spread of `@svar-ui/core-locales` and
  `@svar-ui/kanban-locales` (`it` exists in both). `Layout` exposes `render={{columnScroll,
  fixedColumnWidth}}` and nothing for a phone. The other cases are `CardMenu`, `CardPopup`,
  `Excel`, `Filter`, `GroupBy`, `Performance`, `Styling`, `Templates`, `Toolbar`, `Tooltip`.
- The cockpit already hosts one SVAR widget the same way (`web/src/files/FilesWorkspace.tsx`):
  `Willow`/`WillowDark` picked from `useThemeMode`, `fonts={false}` so nothing preconnects
  to `cdn.svar.dev`, the self-hosted icon font in `web/src/styles/svar.css`, `Locale` words
  per language, and a Go mount that speaks the widget's REST dialect
  (`internal/agui/files_api.go`, `files_api_write.go`).

Not measured: the board on a phone, the provider on a failed write or a stale card, how the
id reconciliation after `POST /cards` behaves, and whether a model keeps a board tidy over a
week. All four are acceptance items below.

## Decisions (proposed)

- **One board per identity, created on first use.** The identity owns its columns and their
  WIP limits. Defaults, from StickyFlow: `todo`, `doing` (WIP 5), `done`. Several boards per
  identity and boards shared between identities are not in this release; the table carries
  a `board_id` so neither needs a migration later.
- **A card is the operator's unit of work, not a projection of a job.** It has a label, a
  description, a priority, tags, a due date, a column and a position. It may name the
  conversation it came from and the scheduler task it waits on; those are links, read at
  render time, never copies of the job's state. Rejected: a board that projects scheduler
  rows, delegations and document jobs into columns by status. That is the governance
  boards again under a different layout, and it is not what StickyFlow is.
- **The agent works the board through one deferred, action-multiplexed tool, `board`.**
  Actions `add`, `update`, `move`, `list`, `search`, `delete`. The shape follows `task`:
  one `action` enum, per-action requirements in field descriptions, no root `oneOf`, an
  `ActionRouter`. `list` is bounded like StickyFlow's snapshot: at most 32 cards, 160
  characters of label and description each, columns first, and the model may name only ids
  that a `list` or `search` of this turn returned; an id it did not see is refused.
- **Tiers.** `add`, `update`, `move` are Mutate: reversible, reserved and recorded, never a
  prompt, like `task schedule`. `delete` is Destructive and stops the turn, and the tool's
  description says so, as `skill_manage delete` does. `list` and `search` are reads. The
  tool-policy design of the same day applies: an operator who wants to be asked before any
  card moves sets `ask` on `board move`.
- **The board is not injected into every turn.** The model reaches it through `tool_search`
  (`board`, `kanban`, `card`, `backlog`, `todo list`, the words an operator uses), as it
  reaches `task`. When a conversation is opened from a card ("discuss"), the card is the
  turn's context block, the way the knowledge catalog block rides the message. Rejected:
  a standing board block, which costs every turn for a board most turns do not touch; the
  2026-07-30 measurement that deferred `task` (795 tokens a turn) is the precedent.
- **The cockpit renders SVAR React Kanban through its own `RestDataProvider`** against a
  mount that speaks its dialect, as the file manager does. No request-building code in the
  cockpit. `readonly` is set for an identity without the board capability (none in this
  release: the board is own-identity).
- **Freshness without a channel.** The board query refetches on mount and on window focus,
  and `useRunSignals` invalidates it on a run's `aura.board` frame, which the `board` tool
  emits once per turn that changed the board. That is the shape the artifact panel uses.
  Rejected for now: an SSE stream of board changes; the cockpit's surfaces are mutually
  exclusive, so a run and the board are never on screen together in one tab.
- **No undo in this release.** `history` is the paid edition. A delete from the cockpit is
  confirmed by the existing `ConfirmDialog`; a delete by the agent is Destructive and
  approved. The audit is the reservation ledger and the card's `updated_by`.
- **The operator's own writes are not gated.** The REST mount is the operator acting on
  their own board; the gateway sees tool calls, not cockpit requests, as today for files.

## Shape

### Migration and queries

The slot is the next free number when the task runs: `ls internal/db/migrations/ | tail -1`
printed `0139_scheduler_task_transient_retries.up.sql` on 2026-10-09, so the slot is `0140`
today, and `0141` if the tool-policy design lands first. Re-run the command first.

```sql
CREATE TABLE aura.boards (
    id          uuid        PRIMARY KEY,
    identity_id uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    name        text        NOT NULL,
    columns     jsonb       NOT NULL,   -- [{id, label, cardLimit}], order is the array order
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (identity_id, name)
);

CREATE TABLE aura.board_cards (
    id              uuid        PRIMARY KEY,
    board_id        uuid        NOT NULL REFERENCES aura.boards (id) ON DELETE CASCADE,
    identity_id     uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    column_id       text        NOT NULL,
    position        double precision NOT NULL,
    label           text        NOT NULL CHECK (char_length(label) BETWEEN 1 AND 200),
    description     text        NOT NULL DEFAULT '' CHECK (char_length(description) <= 4000),
    priority        smallint    NOT NULL DEFAULT 2 CHECK (priority BETWEEN 1 AND 3),
    tags            text[]      NOT NULL DEFAULT '{}',
    due_at          timestamptz,
    conversation_id uuid        REFERENCES aura.conversations (id) ON DELETE SET NULL,
    task_id         uuid        REFERENCES aura.scheduler_tasks (id) ON DELETE SET NULL,
    created_by      text        NOT NULL CHECK (created_by IN ('operator', 'agent')),
    updated_by      text        NOT NULL CHECK (updated_by IN ('operator', 'agent')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    done_at         timestamptz
);
CREATE INDEX board_cards_board_column ON aura.board_cards (board_id, column_id, position);
```

Both tables take the 0087 fail-closed RLS pair, copied from migration 0099 with the same
reasoning. `identity_id` is on the card as well as the board so the policy is one predicate,
not a join. `position` is a float so a move writes one row (the midpoint), with a renumbering
sweep when two positions come within `1e-6`. Queries in
`internal/db/queries/board.sql`: `GetOrCreateBoard`, `UpdateBoardColumns`, `ListCards`,
`GetCard`, `InsertCard`, `UpdateCard`, `MoveCard`, `DeleteCard`, `SearchCards` (a `tsvector`
over label, description and tags, as the document catalog does). `sqlc generate` and commit
`internal/db/sqlc/`.

### Store, `internal/board`

`Store{pool, q}` in the `internal/identity` shape with `withIdentity` binding
`app.current_identity`. One file for the board and columns, one for cards, one for the
search and the snapshot (`Snapshot(ctx, identityID, limit, chars)`), each under 600 lines.
The WIP limit is advisory here and in the widget: a move past the limit succeeds and the
column shows red, as in SVAR, because a limit that refuses a move is a prompt by another
name. The store validates `column_id` against the board's columns on every write.

### Tool, `internal/agent/tools/board.go`

`BoardTool{Store boardStore}` with a consumer-declared `boardStore` seam the live
`*board.Store` satisfies through an adapter at the composition root, as `task` is wired.
Spec: `Name: "board"`, `Deferred: true`, `Multiplexed: true`, Summary and Description
carrying every word an operator uses for a board. Actions:

| action | args | tier |
|---|---|---|
| `list` | `column` (optional) | read, bounded snapshot |
| `search` | `query` | read, bounded |
| `add` | `label`, `description`, `column`, `priority`, `tags`, `due_at` | Mutate |
| `update` | `id`, any of the add fields | Mutate |
| `move` | `id`, `column`, `before` (optional id) | Mutate |
| `delete` | `id` | Destructive |

The seen-id rule lives in the tool: `list` and `search` record the ids they returned in the
tool's own per-session map, keyed by `shellSessionKey(ctx)` as `TodoTool.byID` is, and
cleared when a new turn's first `list` runs; `update`, `move` and `delete` refuse an id not
in that set with an error that says to `list` first. The gateway gets `classifyBoard` in
`internal/gateway/classify.go`'s `multiplexedClassifiers`, keyed `"board"`: `delete` is
Destructive, `add`, `update`, `move` Normal, reads Safe, an unparseable action Risky. A turn
that changed the board emits one `aura.board` custom frame at its end.

The `todo` tool stays what it is: a scratchpad for the turn. The description of `board` says
when to use which: `todo` for the steps of this turn, `board` for work that outlives it.

### REST mount, `internal/agui/board_api.go`

`const boardBase = "/api/board"`, own-identity through `scopedIdentityID`, same-origin
cookie as the file manager's mount:

| widget action | route |
|---|---|
| load | `GET /api/board/cards` returns `{board: {id, name, columns}, cards: [...]}` |
| `add-card` | `POST /api/board/cards` returns the stored card with its server id |
| `update-card` | `PUT /api/board/cards/{id}` |
| `move-card` | `PUT /api/board/cards/{id}/move` with `{column, before}` |
| `delete-card` | `DELETE /api/board/cards/{id}` |
| `duplicate-card` | `POST /api/board/cards/{id}/duplicate` |
| columns | `PUT /api/board/columns` with the full array |

The card on the wire is the widget's shape: `id`, `label`, `description`, `column`,
`priority`, `tags`, `deadline` (ISO, parsed to a `Date` by the provider), plus
`conversation_id`, `task_id`, `created_by`, `updated_by`, `updated_at` as custom fields the
card template reads. The `card` flags turn on `priority`, `description`, `deadline` and
`tags` and leave `cover`, `progress`, `users`, `attachments` and `comments` off: an
identity's board has one user, and a cover image is an asset the board does not own. The provider's
`update-card` is debounced 500 ms in the widget; the server treats a `PUT` of an unchanged
row as a no-op. The id the server returns on `POST` replaces the widget's temporary id
through `api.intercept("add-card")`: acceptance item 3 measures what the provider does on
its own first.

### Cockpit, `web/src/board`

A new mode `board` in `MODES` (`web/src/shell/modes.ts`), not admin, between `chat` and
`studio`, lazy-loaded as the others. `BoardWorkspace.tsx` mirrors `FilesWorkspace.tsx`:
`Willow`/`WillowDark` from `useThemeMode`, `fonts={false}`, `@svar-ui/react-kanban/all.css`
after `@/styles/svar.css`, `Locale` with `boardWords(lang)`, `RestDataProvider(boardBase)`
set in `init` through `api.setNext`. `cardContent` renders the Aura card: label, priority
glyph, tags, due date, and two small links, "discuss" (opens or starts the conversation)
and the scheduler task's status chip when `task_id` is set. The editor is the widget's
`Editor` with `items` of `text` (label, required), `textarea` (description), the stock
`priorityItem`, a `tags` item and a `date` item for `deadline`, `placement="modal"`,
`autoSave={false}` and a `bottomBar` whose delete goes through the cockpit's
`ConfirmDialog` before `delete-card`, as the `Editor` sample does. The column editor is the
widget's own `update-column`. `boardWords(lang)` spreads `@svar-ui/core-locales` and
`@svar-ui/kanban-locales` for the language, as the `Locales` sample does. `useBoardWords.ts` holds the locale strings; keys in
`resources.board.ts`, both languages. Mobile: the widget's `render` scroll mode, measured in
acceptance item 1 before any custom layout.

`useRunSignals` gains one line: `aura.board` invalidates `BOARD_QUERY_KEY`.

### Skill text

A short section in the agent's prompt family that lists `board` beside `task` under
"later / work": one sentence on when a card is right and when a scheduled task is, and the
rule that it never deletes a card it was not asked to delete.

## Errors

| Where | Condition | Answer |
|---|---|---|
| tool | id not returned by a `list`/`search` of this turn | `card <id> is not on the board you listed: run board list first` |
| tool | unknown column | `column "x" is not on the board; columns are: ...` |
| tool | `delete` | approval, as any Destructive call |
| mount | card of another identity | `404`, by RLS, never `403` |
| mount | column not on the board | `400 invalid_column` |
| mount | label over 200 or description over 4000 characters | `400 too_long` |
| store | board missing | created on first `GetOrCreateBoard`, never an error |

## Observability

- Every `board` tool call is a reservation like any Mutate call; `Meta` carries `action`.
- `created_by` and `updated_by` on every card.
- One counter in the obs catalog, `board_cards_changed`, attributes `action` and `actor`
  (`operator | agent`).
- The mount logs at `Info` on column changes only; card writes are the ledger's business.

## Testing

Per prd.md §18 and CLAUDE.md: realistic fixtures, race detector, `db_integration` tier that
`t.Fatal`s under `$CI` when its env is unset, coverage floor 85% across the tag matrix,
mutation ≥70% on the critical files.

- `internal/board` `db_integration`: RLS fail-closed (no identity, no rows; another
  identity, no rows), first use creates the board with the three defaults, move writes one
  row and the sweep renumbers at `1e-6`, column validation on every write, cascade on
  identity delete, `SET NULL` on a deleted conversation or task. Property test: any
  sequence of moves keeps positions strictly increasing within a column.
- `internal/agent/tools`: the action router, the seen-id rule (ids from a previous turn are
  refused), the bounded snapshot (33 cards in, 32 out, 161 characters cut to 160), the
  `aura.board` frame emitted once. Mutation on `board.go` and the snapshot.
- `internal/gateway`: `classifyBoard` table, `delete` gated, `move` not.
- `internal/agui`: the seven routes, own-identity, the no-op `PUT`, the `400`s.
- `web`: vitest for `BoardWorkspace` mounting the widget with a fake provider, the card
  template, the run-signal invalidation; the i18n test that every `board.*` key exists in
  both languages.
- `web/e2e`: a Playwright run that adds a card, drags it to `doing`, edits it, and sees the
  same state after a reload; and one that sets a column limit and sees the overflow mark.
- Lab-VM acceptance, the Definition of Done (score > 9.8 on the real scenario):
  1. the board on a phone (390x844): columns scroll, a card drags, the editor opens; what
     the widget cannot do on touch is written down before any custom layout is;
  2. "add a card to call the supplier tomorrow and move the invoice card to done": two
     reservations, no prompt, the board shows both within one focus change, three runs out
     of three on two models of the release's matrix;
  3. a `POST /cards` that fails (the mount returns 500): what the widget shows, whether the
     temporary card stays, and what the provider does with the server id on success; the
     `intercept` is written to the measurement;
  4. "delete the card about the old server": an approval card, the delete after accept, the
     card gone from the board on the next focus;
  5. one week of the operator's real use on the lab VM: the count of cards the agent added
     that the operator deleted unchanged, as the measure of a board kept tidy; the number
     decides whether `add` stays Mutate or becomes `ask` by default in the policy design.

## Open questions, answered 2026-10-09

Answered by the engineer at the operator's request; each stands unless the operator objects.

1. **Is `add` by the agent prompted by default?** No. A card is reversible, `delete` is the
   one gated verb, and `task schedule` sets the precedent that the expected write is not a
   prompt. An operator who wants to be asked sets `ask` on `board add` in the policy design;
   acceptance 5 measures how many agent cards the operator deletes unchanged, and that
   number, not a guess, decides whether the default flips.
2. **Several boards per identity, now or later?** Later. `board_id` is on every card, so a
   second board is a migration-free change; what is missing is the evidence that one board
   overflows. The week of real use in acceptance 5 counts cards and columns first.
3. **What does "discuss" open?** The conversation the card names when it has one, else a new
   one with the card as its context block. A card the agent creates names the conversation
   of its turn automatically; a card the operator creates names none until "discuss".
4. **A due date that passes?** The board colours it, nothing else, in this release. A card
   that must remind names a scheduler task (`task_id`), created through `task` when the
   operator says "remind me": one reminder path, not two.
5. **A card thread through the editor's `comments` item?** No. "Discuss" is the thread, and
   it is the conversation history the agent already reads. A second place to talk to the
   agent is a second history that memory, recall and export would all have to learn.

## Out of scope

OpenDots' page editor, revisions and "save a conversation as a page" (the export exists in
`conversation_export.go`); StickyFlow's calendar and Gantt views, sharing, presence and
encryption; the paid SVAR edition's undo, dynamic loading and export; a realtime channel
for the board; the `todo` tool's replacement.
