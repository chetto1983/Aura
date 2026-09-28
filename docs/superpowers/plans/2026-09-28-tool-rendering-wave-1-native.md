# Native Tool Rendering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Aura's native checklist, shell, patch, file and search results readable in compact tool rows, with the same trusted display live and after replay.

**Architecture:** Extend the shared Go preview projection to receive validated call arguments as well as the persisted result. Emit bounded typed payloads for each supported shape; the React router validates each slot again and renders a copied standalone Elements or Tool UI component. Any unsupported, malformed, partial or oversized result keeps the existing escaped `ToolResultPanel`.

**Tech Stack:** Go `internal/agent/display`, AG-UI live/snapshot projection, React 19, TypeScript, Vitest, Elements standalone registry, Tool UI copyable source.

**Spec:** `docs/superpowers/specs/2026-09-28-tool-rendering-tool-ui-elements-design.md` (Wave 1). Wave 2 media, Wave 3 MCP result rendering, and message-draft authorization are separate implementation plans because each ships and tests independently.

## Global Constraints

- Preserve `ToolActivityCard` / `ToolGroup` and `ToolResultPanel` escaped fallback. Backend projection is the only rich-render authority; client parsing cannot promote a raw result.
- Use `@assistant-ui` and `@tool-ui` registries already in `web/components.json`; copy only selected standalone sources, record their upstream URL/revision/license in a source comment, and do not initialize another assistant runtime.
- Keep `web/tokens/tokens.json`'s shipped blue palette, English/Italian copy, accessible disclosure/focus, reduced motion, and narrow width. New files stay under 600 lines.
- Shell output is **combined** stdout/stderr. A stderr tail may be embedded in the preview. Never display invented separate streams, a fake exit code, or fake streaming.
- Keep code/result bytes capped. A display parse failure, error result, background-in-progress result, and unknown kind all retain escaped raw details.
- Each implementation task commits only its own files, includes a `Co-Authored-By` trailer, and leaves unrelated video-studio worktree edits alone. PRD amendments follow measured stack behavior, not this plan alone.

## Review Focus

1. A valid-looking todo argument paired with an error result must stay raw; Task 2 pins this.
2. A background shell with no final exit code must not appear successful; Task 3 pins this.
3. A diff line that starts with `+++` inside file content must remain content, not a header; Task 4 pins this.
4. A search path containing `:` or a match containing HTML must not corrupt columns or create DOM; Task 5 pins this.
5. A replayed tool call must render exactly the same typed payload as its live event; Task 1 and the final smoke pin this.

---

### Task 1: Shared call-input projection and fallback contract

**Files:**
- Modify: `internal/agent/display/preview.go`, `preview_test.go`, `normalize.go`, `payload.go`
- Modify: `internal/agent/llm_agent_display.go`, `llm_agent_events.go`
- Modify: `internal/agui/server_display.go`, `translator_tool_display.go`, their focused tests
- Modify: `web/src/chat/displays/types.ts`, `DisplayRouter.tsx`, `__tests__/DisplayRouter.test.tsx`

**Interfaces:**
- Consumes: `toolRunResult.Arguments`, `ResultPreview`, assistant `ToolCalls[].Function.Arguments`, `ToolCallID`.
- Produces: `display.PreviewInput{ToolCallID, ToolName, Arguments, ResultPreview}` and `NormalizeToolPreview(input PreviewInput, reg *Registry) (Payload, bool)`; later tasks add exact-name decode cases. The Go `Payload` and TS `DisplayPayload` slots remain wire mirrors.

- [ ] **Step 1: Write failing Go parity and TS fallback tests.** Put a literal call with arguments in `internal/agent/display/preview_test.go` and a paired assistant/tool history fixture in `internal/agui/server_display_test.go`; require equal JSON for live and `rederiveDisplays`. Add a router test that gives a recognized kind with a missing slot and expects escaped raw text.

```go
in := display.PreviewInput{ToolCallID: "c1", ToolName: "web_search", Arguments: `{"q":"rome"}`, ResultPreview: `{"results":[{"title":"A","url":"https://a.test"}]}`}
live, ok := display.NormalizeToolPreview(in, display.NewRegistry())
if !ok || live.ToolCallID != "c1" { t.Fatalf("live display = %+v, %v", live, ok) }
// The replay fixture's assistant ToolCall uses the same ID, name and arguments;
// its RoleTool content is in.ResultPreview. Assert JSON-equivalent payloads.
```

- [ ] **Step 2: Run the focused tests and see the missing input API / fallback fail.** Run `go test ./internal/agent/display ./internal/agui -run 'Test.*(Preview|Display)' -count=1` and `npm --prefix web test -- src/chat/displays/__tests__/DisplayRouter.test.tsx`.
- [ ] **Step 3: Implement the shared input and wiring.** Replace positional preview arguments at every call site and test. Build a `map[toolCallID]PreviewInput` from assistant ToolCalls for replay; fill `ResultPreview` from each RoleTool turn. `eventDisplay` cancellation re-projection receives `ti.Arguments`. Check that the live emitter passes `run.Arguments`. Keep old web/swarm output byte-identical. In the React router, each new case checks that its slot is present and valid before mounting a rich card.

```go
type PreviewInput struct { ToolCallID, ToolName, Arguments, ResultPreview string }
func NormalizeToolPreview(in PreviewInput, reg *Registry) (Payload, bool) {
    if in.ResultPreview == "" || reg == nil { return Payload{}, false }
    value, ok := decodeToolPreview(in.ToolName, in.Arguments, in.ResultPreview)
    if !ok { return Payload{}, false }
    return NormalizeWithRegistry(in.ToolCallID, in.ToolName, value, reg)
}
```

- [ ] **Step 4: Run parity and fallback tests green.** Run the two commands from Step 2, then `npm --prefix web run typecheck`. Check existing web/swarm snapshots have not changed.
- [ ] **Step 5: Commit this seam.** Stage only the listed files and commit `refactor(display): carry tool arguments through live and replay projection`.

### Task 2: `todo_write` as Elements Todo list

**Files:**
- Modify: `internal/agent/display/preview.go`, `normalize.go`, `payload.go`, tests
- Create: `internal/agent/display/todo.go`, `todo_test.go`
- Create: `web/src/chat/displays/TodoDisplay.tsx`, `__tests__/TodoDisplay.test.tsx`
- Modify: `web/src/chat/displays/types.ts`, `DisplayRouter.tsx`, router tests, `web/src/i18n/resources.display.ts`
- Add only the copied standalone `@assistant-ui/elements-todo-list` source and its required dependencies.

**Interfaces:**
- Consumes: Task 1 `PreviewInput`; `todo_write` arguments `{todos:[{content,status,activeForm?}]}` and successful `renderTodos` preview.
- Produces: `KindTodo = "todo"`, `Payload.Todo *Todo` with ordered `{content,status,active_form}` items. `TodoDisplay` adapts these to Elements `{id,text,status,description}` without changing Aura's tool schema.

- [ ] **Step 1: Write failing tests.** Cover pending/in-progress/completed, the `activeForm` label, cleared list, malformed argument JSON, invalid status, second active item, and error preview. The last five must return `ok=false` or a true empty state only for `[todo list cleared]`.

```go
in := PreviewInput{ToolCallID:"todo-1", ToolName:"todo_write", Arguments:`{"todos":[{"content":"Build","status":"in_progress","activeForm":"Building"}]}`, ResultPreview:"[~] Build"}
p, ok := NormalizeToolPreview(in, NewRegistry())
if !ok || p.Todo == nil || p.Todo.Items[0].Status != "in_progress" { t.Fatalf("todo = %+v, %v", p, ok) }
in.ResultPreview = "todo_write: invalid status"
if _, ok := NormalizeToolPreview(in, NewRegistry()); ok { t.Fatal("error preview promoted") }
```

- [ ] **Step 2: Run `go test ./internal/agent/display -run TestTodo -count=1` and the new Vitest file; confirm failures.**
- [ ] **Step 3: Implement strict Go projection and the card.** Parse bounded arguments, require exact recognized statuses, at most one active item, and compare the successful result to the tool's plain-text status lines so an error cannot borrow valid args. Empty list is valid only with the clear marker. Port Elements Todo list standalone, map status `in_progress -> active`, `completed -> done`, use index as a revision-local ID, and put `activeForm` in the active description. Add localized heading and screen-reader status.

```tsx
const items = todo.items.map((item, index) => ({
  id: `${payload.tool_call_id}:${index}`, text: item.content,
  status: item.status === 'in_progress' ? 'active' : item.status === 'completed' ? 'done' : 'pending',
  description: item.status === 'in_progress' ? item.active_form : undefined,
}));
return <TodoList title={t('display.todo.title')} items={items} />;
```

- [ ] **Step 4: Run focused Go/Vitest, `npm --prefix web run typecheck`, and verify an empty list and narrow viewport.**
- [ ] **Step 5: Commit only todo projection/card/source/i18n files.** Use `feat(chat): render trusted todo updates as a checklist`.

### Task 3: Completed shell output as Elements Terminal block

**Files:**
- Modify: `internal/agent/display/preview.go`, `code.go`, `payload.go`, `normalize.go`, shell tests
- Create: `web/src/chat/displays/TerminalDisplay.tsx`, `__tests__/TerminalDisplay.test.tsx`
- Modify: TS payload/router/i18n and router tests
- Add only copied `@assistant-ui/elements-terminal-block` standalone source.

**Interfaces:**
- Consumes: Task 1 `PreviewInput`; existing `[aura_shell {...}]` and `[aura_shell_bg {...}]` footer parsing.
- Produces: `KindTerminal = "terminal"`, `Payload.Terminal{command,output,cwd,exit_code,duration_ms,truncated}` for completed calls with an actual exit code. Existing `KindCode` remains for unsupported/legacy shell results.

- [ ] **Step 1: Write failing parser and card tests.** Use a foreground exit 7 with combined output, a legacy cancelled footer, a running background shell ID, a final `shell_poll` status, a malformed footer, and a malicious command string. Running/legacy/malformed examples must keep the row/raw fallback, not show a green terminal.

```go
in := PreviewInput{ToolCallID:"s1", ToolName:"shell_exec", Arguments:`{"command":"make test"}`, ResultPreview:"failed\n[aura_shell {\"exit_code\":7,\"cwd\":\"/workspace\",\"duration_ms\":34}]"}
p, ok := NormalizeToolPreview(in, NewRegistry())
if !ok || p.Terminal == nil || p.Terminal.ExitCode != 7 { t.Fatalf("terminal = %+v, %v", p, ok) }
```

- [ ] **Step 2: Run `go test ./internal/agent/display -run 'Test.*(Shell|Terminal)' -count=1` and TerminalDisplay Vitest; confirm failures.**
- [ ] **Step 3: Implement the terminal projection and adapter.** Parse command from validated `shell_exec` args; for `shell_poll`, label the operation `shell_poll <shell_id>` because its args do not contain the original command. Accept only a complete foreground footer or a final background status with numeric exit. Preserve combined output and any actual stderr-tail text as lines in order. Carry real cwd/duration/truncation when present; bound displayed lines and disclose truncation. Pass `done={true}`, `visibleCount={lines.length}`, `exitCode={terminal.exit_code}` to standalone TerminalBlock. A running job uses the existing row until it has final output.

```tsx
const lines = terminal.output.split('\n');
return <TerminalBlock command={terminal.command} lines={lines} visibleCount={lines.length}
  done exitCode={terminal.exit_code} cwd={terminal.cwd} durationMs={terminal.duration_ms}
  truncated={terminal.truncated} maxCollapsedLines={12} />;
```

- [ ] **Step 4: Run focused tests, web typecheck, and verify live/replay output and keyboard expansion.**
- [ ] **Step 5: Commit only shell display files.** Use `feat(chat): show completed shell output in terminal card`.

### Task 4: Applied patch and file text

**Files:**
- Create: `internal/agent/display/filetext.go`, `filetext_test.go`, `diff.go`, `diff_test.go`
- Modify: display preview/normalize/payload and their tests
- Create: `web/src/chat/displays/DiffDisplay.tsx`, `__tests__/DiffDisplay.test.tsx`
- Modify: existing `CodeDisplay.tsx` and test, TS payload/router/i18n
- Add only copied standalone `@assistant-ui/elements-code-diff` and Tool UI `code-block` sources. Pin Tool UI source to the archived repository revision `49a870286facdbf28160cd647f0d337ebdc9b275` recorded in the spec; the old `tool-ui.com/docs/code-block` URL now redirects.

**Interfaces:**
- Consumes: Task 1 `PreviewInput`; `patch` successful unified diff; `read_file` numbered text; `write_file` success receipt plus bounded content from call args.
- Produces: `KindDiff = "diff"`, `Payload.Diff` with filename, counts and ordered context/added/removed lines; existing `KindCode` gains filename and first line number where verified.

- [ ] **Step 1: Write failing fixtures.** Include `patch` replace and patch mode, no-op success, `---/+++` content lines, malformed hunk counts, multi-file input, `read_file` offset/pagination hint, extracted document text, and `write_file` with a mismatching receipt. Malformed/no-op/binary/oversized results retain raw text.

```go
in := PreviewInput{ToolCallID:"p1", ToolName:"patch", Arguments:`{"path":"a.txt","mode":"replace","old_string":"a","new_string":"b"}`, ResultPreview:"--- a/a.txt\n+++ b/a.txt\n@@ -1,1 +1,1 @@\n-a\n+b\n"}
p, ok := NormalizeToolPreview(in, NewRegistry())
if !ok || p.Diff == nil || p.Diff.Additions != 1 || p.Diff.Deletions != 1 { t.Fatalf("diff = %+v, %v", p, ok) }
```

- [ ] **Step 2: Run focused Go tests and the new Vitest file; confirm failures.**
- [ ] **Step 3: Implement strict projection and cards.** Parse unified headers/hunks with byte and line caps, verify hunk counts, retain real filenames, and reject ambiguous input. For `read_file`, separate only verified `N|` gutters and its exact pagination marker; for `write_file`, require its verified success receipt before projecting capped arg content. Adapt Diff lines to Elements `{kind,text}` and Code text to Tool UI Code block. Keep copy/download/raw access, no apply/reject controls.

```tsx
return <CodeDiff filename={diff.filename} additions={diff.additions}
  deletions={diff.deletions} lines={diff.lines} cycle={0} />;
```

- [ ] **Step 4: Run `go test ./internal/agent/display ./internal/agui -count=1`, focused Vitest, typecheck and mobile overflow check.**
- [ ] **Step 5: Commit only patch/file display files.** Use `feat(chat): render verified patch and file text`.

### Task 5: Search results, Elements table, and native end-to-end gate

**Files:**
- Create: `internal/agent/display/searchfiles.go`, `searchfiles_test.go`
- Modify: display preview/normalize/payload and tests
- Modify: `web/src/chat/displays/TableDisplay.tsx`, `__tests__/TableDisplay.test.tsx`, `tableData.ts`, TS payload/router tests, i18n
- Add only copied standalone `@assistant-ui/elements-data-table` source.
- Add a representative native tool Playwright scenario under `web/e2e/` using the existing authenticated chat fixtures.

**Interfaces:**
- Consumes: Task 1 `PreviewInput`; `search_files` args `target` / `output_mode`; exact success text from `search_files_content.go` and `search_files_names.go`.
- Produces: existing `KindTable` payload with bounded `columns`/`rows`; `TableDisplay` keeps filter, sort, pagination, TSV copy, and CSV export around Elements DataTable's responsive table/card body. Update `tableData.filterAndSort(rows, filter, sort: DataTableSort | null, columns, locale)` to use the copied element's comparison rules on the full row set, where sort keys are `c0`, `c1`, and so on. Reject unknown sort keys and reset the page on `onSortChange`.

- [ ] **Step 1: Write failing search/table tests.** Cover content `path:line: text`, context `path-line- text`, `files_only`, count, empty result, walk-truncation marker, path containing `:`, HTML-like match, and a malformed result. In Vitest, assert filter/export/sort and narrow-card labels survive the component replacement. Add a disposition fixture: `shell_kill`, `read_tool_output`, `tool_search`, and `current_time` keep their existing row/raw treatment, while `text_response` remains the assistant message. `sandbox_exec` follows the completed shell path if present.

```go
in := PreviewInput{ToolCallID:"q1", ToolName:"search_files", Arguments:`{"pattern":"needle","target":"content","output_mode":"content"}`, ResultPreview:"src/a.go:12: needle <b>"}
p, ok := NormalizeToolPreview(in, NewRegistry())
if !ok || p.Table == nil || p.Table.Rows[0][0] != "src/a.go" { t.Fatalf("table = %+v, %v", p, ok) }
```

- [ ] **Step 2: Run focused Go/Vitest and confirm failures.**
- [ ] **Step 3: Implement the bounded parser and table adapter.** Interpret output according to validated args; parse the `path:line: text` / `path-line- text` grammar and reject ambiguous numeric-delimiter lines to raw fallback instead of guessing a path. Preserve context/match distinction and truncation notice. Convert `columns` and rows into indexed record keys for standalone DataTable; retain `TableDisplay`'s filter, copy, export and page controls. Use one controlled sort state: filter and sort the full dataset with the copied DataTable comparator before slicing the page, then pass the same sort state to DataTable. Its page-local sort is idempotent and cannot alter page membership. Render cell values as React text, never HTML.

```tsx
const { t, i18n } = useTranslation();
const [sort, setSort] = useState<DataTableSort | null>(null);
const columns = table.columns.map((label, index) => ({ key: `c${index}`, label, priority: index === 0 ? 'primary' as const : 'secondary' as const }));
const filtered = filterAndSort(table.rows, filter, sort, table.columns, i18n.language);
const visible = filtered.slice(current * perPage, (current + 1) * perPage);
const rows = visible.map((cells) => Object.fromEntries(cells.map((value, index) => [`c${index}`, value])));
return <DataTable columns={columns} rows={rows} sort={sort}
  onSortChange={(next) => { setSort(next); setPage(0); }}
  caption={t('display.type.table')} locale={i18n.language} />;
```

- [ ] **Step 4: Run `go test ./internal/agent/display ./internal/agui -count=1`, `npm --prefix web run typecheck`, focused Vitest, and the native Playwright scenario.** The scenario opens todo, shell, patch, read and search rows; reloads; compares their displays; and checks one malformed fallback. Record the measured result before any PRD amendment.
- [ ] **Step 5: Commit only search/table/E2E files, then perform the wave review.** Use `feat(chat): show structured file search results` and include the measured smoke in the commit body.
