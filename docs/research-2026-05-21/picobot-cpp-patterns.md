# Picobot + cli-printing-press — unlifted patterns (2026-05-21)

Reading log:
- D:/tmp/picobot — full Go source (~6273 LOC main code, 9 packages, ~22 tools across `internal/agent/tools/`).
- D:/tmp/cli-printing-press — Go source (~88486 LOC incl. tests/golden; `internal/mcpdesc`, `internal/mcpoverrides`, `internal/pipeline`, skill bundles).

Already lifted (skipped):
- `mcpdesc.Compose` compact MCP descriptions (cli-printing-press).
- `references/` directory in skill format (cli-printing-press).
- Tool registry shape — name/desc/parameters/Execute interface (picobot).
- State-machine ortogonale (cli-printing-press `internal/pipeline/state.go`) — confirmed deferred per CLAUDE.md learnings.

The list below is what is NOT yet in Aura, ranked by ROI.

---

## 1. Kitchen-sink tool with `action` enum dispatch — picobot CORE PATTERN

### WHAT
Picobot collapses what would be N tiny tools into ONE tool that takes an `action: enum` plus an action-specific payload. The LLM sees one entry in the registry; internally the tool dispatches with a `switch action`. Used uniformly for filesystem (read/write/list), cron (add/list/cancel), and conceptually for memory (the 5 memory tools are arguably 1 tool with action).

### WHERE
- D:/tmp/picobot/internal/agent/tools/filesystem.go (134 LOC, single Go file = 3 verbs)
- D:/tmp/picobot/internal/agent/tools/cron.go (146 LOC, single Go file = 3 verbs)
- D:/tmp/picobot/internal/agent/tools/exec.go (137 LOC)

### SNIPPET — filesystem.go:40-60 + 85-133
```go
func (t *FilesystemTool) Parameters() map[string]interface{} {
    return map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "action": map[string]interface{}{
                "type":        "string",
                "description": "The filesystem operation to perform",
                "enum":        []string{"read", "write", "list"},
            },
            "path":    map[string]interface{}{"type": "string", ...},
            "content": map[string]interface{}{"type": "string", "description": "Content to write (required when action is 'write')"},
        },
        "required": []string{"action", "path"},
    }
}

func (t *FilesystemTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
    action, _ := args["action"].(string)
    pathStr, _ := args["path"].(string)
    switch action {
    case "read":  b, err := t.root.ReadFile(pathStr); ...
    case "write": ... t.root.WriteFile(pathStr, []byte(content), 0o644)
    case "list":  ... f.ReadDir(-1)
    default: return "", fmt.Errorf("filesystem: unknown action %s", action)
    }
}
```

Cron is the most striking: cron.go:25-62 advertises ONE tool with `action: add | list | cancel`, then the LLM picks the action — eliminating `schedule_task` + `list_tasks` + `cancel_task` as three separate registry entries.

### WHY
This IS the surface-reduction lever Aura's mandate is asking for. The five `source_*` tools in Aura (`source.go` 282, `source_delete.go` 81, `source_list.go` 103, `source_ocr.go` 130, `source_read.go` 116, `source_store.go` 111) plus `source_unified.go` 289 are a textbook candidate: one `source` tool with `action: store | read | list | delete | ocr | ingest`. Same with `scheduler.go` (today exposes 3 separate tools) and the wiki path/subgraph/godnodes triple. The action enum is also more LLM-friendly: the model picks an action from a closed set in the same token window where it picks the tool — fewer "which of the 5 source_X tools do I call" mistakes.

The kitchen-sink shape also makes Aura's `tools/registry/source_unified.go` (which already tries to do this internally but is one of several source tools the LLM sees) the canonical exit point — keep source_unified, delete the leaf tools.

### EFFORT
~2 sessions across 2 zones (sources, scheduler). 1 zone per session (sources first, scheduler next) per the "one module per slice" memory rule.

### DELTA
Estimated **-1100 LOC** (deletion is positive):
- Collapse source_* files: ~822 LOC removed (5 leaf files), `source_unified.go` becomes the only entrypoint, gains ~80 LOC of dispatch glue. Net ~-700.
- Collapse scheduler: 3 registry entries → 1 enum-dispatch tool. ~150 LOC saved.
- Collapse wiki_path/godnodes/subgraph: 3 → 1 `wiki` tool with `action: path | godnodes | subgraph`. ~200 LOC saved.
- Net registry surface: ~22 tools → ~12-14 tools.

---

## 2. `os.Root`-anchored sandboxing (Go 1.24+)

### WHAT
Picobot opens an `os.Root` at the workspace dir at startup and routes ALL filesystem operations (filesystem tool, skill manager) through the rooted FD. Kernel-enforced via `openat()`. No userspace path-traversal regex, no symlink-escape vulnerability, no TOCTOU race.

### WHERE
- D:/tmp/picobot/internal/agent/loop.go:82-86 — `root, err := os.OpenRoot(workspace)` once at startup.
- D:/tmp/picobot/internal/agent/tools/filesystem.go:14-30 — `FilesystemTool` wraps `*os.Root`.
- D:/tmp/picobot/internal/agent/tools/skill.go:19-25 — `SkillManager` shares the same `*os.Root`.

### SNIPPET — filesystem.go:14-30
```go
// FilesystemTool provides read/write/list operations within the filesystem.
// All operations are sandboxed to the workspace directory using os.Root (Go 1.24+),
// which provides kernel-enforced path containment via openat() syscalls.
// This prevents symlink escapes, TOCTOU races, and path traversal attacks.
type FilesystemTool struct {
    root *os.Root
}

func NewFilesystemTool(workspaceDir string) (*FilesystemTool, error) {
    absDir, err := filepath.Abs(workspaceDir)
    if err != nil { return nil, ... }
    root, err := os.OpenRoot(absDir)
    if err != nil { return nil, ... }
    return &FilesystemTool{root: root}, nil
}
```

### WHY
Aura's `workspace_files.go` + `wiki_path.go` + `source_*.go` family all do manual path validation (look for `..` etc.) — fragile. Switching to `os.Root` per workspace lifetime (wiki root, sources root) deletes the whole class of CVEs and the validation code that guards against them. The skills system in Aura also walks paths manually — a single `os.Root` rooted at the skills dir would replace that.

### EFFORT
~1 session. Aura is already on Go 1.24+ (check `go.mod` — should be). Refactor 4-5 files to inject `*os.Root` instead of string paths.

### DELTA
~-150 LOC (validation helpers + duplicated path-cleaning utilities + the workspace_validation.go file mostly disappears).

---

## 3. Single consolidated system message — picobot context builder

### WHAT
Picobot concatenates ALL system instructions (base prompt, bootstrap files, channel ctx, memory ctx, ranked memories, skills manifest) into ONE `system`-role message at index 0 — rather than 4-6 system messages. Done specifically to work with strict chat templates (llama.cpp).

### WHERE
- D:/tmp/picobot/internal/agent/context.go:32-96

### SNIPPET — context.go:32-96
```go
func (cb *ContextBuilder) BuildMessages(history []string, currentMessage string, channel, chatID string, memoryContext string, memories []memory.MemoryItem) []providers.Message {
    msgs := make([]providers.Message, 0, len(history)+2)
    // Combine all system instructions into one message at position 0 to avoid errors in strict chat templates (e.g. llama.cpp)
    var sysParts []string
    sysParts = append(sysParts, "You are Picobot, a helpful assistant.")
    // Load workspace bootstrap files
    bootstrapFiles := []string{"SOUL.md", "AGENTS.md", "USER.md", "TOOLS.md"}
    for _, name := range bootstrapFiles {
        p := filepath.Join(cb.workspace, name)
        data, err := os.ReadFile(p)
        if err != nil { continue }
        ...
        sysParts = append(sysParts, fmt.Sprintf("## %s\n\n%s", name, content))
    }
    sysParts = append(sysParts, fmt.Sprintf("You are operating on channel=%q chatID=%q. ...", channel, chatID))
    if len(loadedSkills) > 0 { ... }
    if memoryContext != "" { sysParts = append(sysParts, "Memory:\n"+memoryContext) }
    if len(selected) > 0 { ... }
    msgs = append(msgs, providers.Message{Role: "system", Content: strings.Join(sysParts, "\n\n")})
    ...
}
```

### WHY
Aura's `internal/conversation/system_prompt.go` already restructured to base+overlays in EN-only (per the 2026-05-21 lessons), but the prompt-overlay assembly in Aura currently produces multiple system messages (one per overlay file in some paths). Concatenating to a single `system` message at index 0 is friendlier to local LLMs and matches the prompt-cache shape Anthropic recommends. Bonus: the `## SOUL.md\n\n...` header in each part makes section provenance visible to the model.

### EFFORT
~0.5 session. The concatenation site is one function in `internal/conversation/`.

### DELTA
~-50 LOC and ~zero risk (idempotent prompt change, all overlays in EN already).

---

## 4. Picobot MCP client — 363 LOC for both stdio + Streamable HTTP + SSE

### WHAT
Complete MCP client in one file with both transports, JSON-RPC envelope, initialize handshake, tools/list, tools/call, and SSE parsing. No external MCP SDK dependency — pure stdlib.

### WHERE
- D:/tmp/picobot/internal/mcp/client.go (363 LOC, ONE file)

### SNIPPET — client.go:27-110 — public API surface
```go
type Client struct {
    name      string
    transport transport
    nextID    atomic.Int64
    tools     []Tool
}

func NewStdioClient(name, command string, args []string) (*Client, error) { ... }
func NewHTTPClient(name, url string, headers map[string]string) (*Client, error) { ... }

func (c *Client) Name() string { return c.name }
func (c *Client) Tools() []Tool { return c.tools }
func (c *Client) CallTool(_ context.Context, toolName string, arguments map[string]interface{}) (string, error) { ... }
func (c *Client) Close() error { return c.transport.close() }

/*** transport interface ***/
type transport interface {
    roundTrip(req []byte) ([]byte, error)
    notify(req []byte) error
    close() error
}
```

The `transport` interface (only 3 methods) is the entire abstraction — stdio + http both implement it; SSE handled inline in `httpTransport.doPost` (lines 293-343). No SDK, no codegen, no plugin registry.

### WHY
Aura's `internal/mcp/` is significantly larger and harder to reason about. The picobot pattern shows you can:
- Use a single `transport` interface for both stdio and HTTP.
- Skip notifications by detecting `id != nil` JSON probe (line 246).
- Handle SSE inline with a scanner that filters `data: ` lines (lines 348-363).
- Keep the JSON-RPC types as local structs (no external rpc lib).

Aura should NOT rewrite from scratch — but porting picobot's `transport` interface boundary and the notification-skip probe trick would simplify a lot of branching in Aura's mcp/client.

### EFFORT
~1-2 sessions. Touching `internal/mcp/` is a known-risk surface (CLAUDE.md notes boot is non-fatal on MCP failures); changes need fresh probe runs.

### DELTA
Hard to quantify without measuring Aura's current `internal/mcp/` (didn't fully read). Picobot's whole MCP layer is 363 LOC; Aura's is multiple files. Plausible -300 to -500 LOC if Aura's split into stdio.go / http.go / sse.go / types.go etc.

---

## 5. mcpoverrides.json sidecar pattern (cli-printing-press)

### WHAT
A user-editable JSON sidecar (`mcp-descriptions.json`) that overrides auto-generated MCP tool descriptions per tool-name key. Loaded once, applied in-place to the parsed spec, returns the list of unmatched keys (so typos surface).

### WHERE
- D:/tmp/cli-printing-press/internal/mcpoverrides/overrides.go (119 LOC)

### SNIPPET — overrides.go:34-56 + 73-107
```go
type Overrides struct {
    Descriptions map[string]string `json:"descriptions"`
}

func Load(cliDir string) (Overrides, error) {
    path := filepath.Join(cliDir, Filename)
    data, err := os.ReadFile(path)
    if err != nil {
        if errors.Is(err, fs.ErrNotExist) { return Overrides{}, nil }
        return Overrides{}, fmt.Errorf("reading %s: %w", path, err)
    }
    var o Overrides
    if err := json.Unmarshal(data, &o); err != nil { ... }
    return o, nil
}

// Apply mutates parsed.Resources in place ... Returns the override keys
// that did not match any endpoint. A typo in the override file would
// otherwise silently no-op; the caller should surface unmatched keys.
func (o Overrides) Apply(parsed *spec.APISpec) []string { ... }
```

### WHY
Aura ships MCP servers via `mcp.json` config. When Aura registers an MCP tool, the tool description comes from the server (often verbose, jargon-heavy, sometimes English-only on a server documenting Italian commands). Today Aura has no override mechanism — operators are stuck with whatever the upstream MCP server emits. Adding an `mcp-descriptions.json` (or extending `mcp.json` with a `description_overrides: map[string]string`) lets the user retune tool descriptions WITHOUT forking the upstream MCP server. Surface unmatched keys so typos don't silently no-op.

### EFFORT
~0.5 session. New file ~120 LOC + wire-up in `internal/mcp/registry-builder` or wherever Aura registers MCP tools.

### DELTA
+120 LOC (additive feature). Not a deletion play — this enables description discipline for upstream MCPs WITHOUT changing their code.

---

## 6. Channel adapter shape — Hub + Subscribe + StartRouter

### WHAT
Picobot's `chat.Hub` is a buffered in/out channel pair plus a `subs map[string]chan Outbound` keyed by channel name. Each channel adapter (`channels/telegram.go`, `channels/discord.go`, `channels/slack.go`, `channels/whatsapp.go`) calls `hub.Subscribe("telegram")` before launching its outbound goroutine. `hub.StartRouter(ctx)` reads from `hub.Out` and dispatches by `out.Channel`.

### WHERE
- D:/tmp/picobot/internal/chat/chat.go (99 LOC)
- D:/tmp/picobot/internal/channels/telegram.go (147 LOC including polling + outbound)

### SNIPPET — chat.go:54-93
```go
func (h *Hub) Subscribe(name string) <-chan Outbound {
    ch := make(chan Outbound, cap(h.Out))
    h.subMu.Lock()
    h.subs[name] = ch
    h.subMu.Unlock()
    return ch
}

func (h *Hub) StartRouter(ctx context.Context) {
    go func() {
        for {
            select {
            case <-ctx.Done(): return
            case out, ok := <-h.Out:
                if !ok { return }
                h.subMu.RLock()
                ch, exists := h.subs[out.Channel]
                h.subMu.RUnlock()
                if exists {
                    select {
                    case ch <- out:
                    case <-ctx.Done(): return
                    }
                } else {
                    log.Printf("hub: no subscriber for channel %q, dropping outbound message", out.Channel)
                }
            }
        }
    }()
}
```

### WHY
Per Aura's target architecture diagram memory (`project_target_architecture_diagram_2026-05-15`), the goal is `Chat Apps (Telegram + WhatsApp NEW) → Agent Loop → Context`. Today Aura's `internal/channels/telegram` is single-channel; WhatsApp would bolt on as a parallel substrate. Picobot's Hub + Subscribe + StartRouter pattern is the smallest abstraction that supports N channels with at-least-one-router-goroutine — exactly what WhatsApp Wave 1 will need.

Bonus: the 147-LOC telegram.go file shows polling + outbound in a single goroutine pair with no SDK dependency. That's a possible refactor target for Aura's much larger `internal/telegram/` package.

### EFFORT
~1 session for the Hub abstraction. WhatsApp adapter is a separate phase per memory.

### DELTA
Hub itself is small (+99 LOC additive). The deletion comes when WhatsApp Wave 1 ships and uses Hub instead of bolting onto telegram-shaped wiring — at that point ~200-300 LOC of channel-specific glue collapses.

---

## 7. `text_response` is already lifted (CLAUDE.md confirms) — but the `lastToolResult` fallback is NOT

### WHAT
Picobot's loop has a clever fallback: if the LLM ends a turn with NO text AND no tool call (which models sometimes do after a tool returns a clean string), the loop returns the last tool result verbatim instead of "I've completed processing but have no response to give."

### WHERE
- D:/tmp/picobot/internal/agent/loop.go:283-287 (Run)
- D:/tmp/picobot/internal/agent/loop.go:347-353 (ProcessDirect)

### SNIPPET — loop.go:283-287
```go
if finalContent == "" && lastToolResult != "" {
    finalContent = lastToolResult
} else if finalContent == "" {
    finalContent = "I've completed processing but have no response to give."
}
```

### WHY
The 2026-05-21 lessons say "single-shot bias" is the goal. Combined with `text_response` (already lifted), this fallback catches the remaining failure mode where the LLM thinks the tool result IS the answer and stops. Today Aura would return empty text in that path. ~3 LOC change but removes a class of empty-reply bugs that probe runs probably already log.

### EFFORT
~5 minutes. Single conditional in `internal/chat/agentloop.go` or wherever Aura's outer loop closes the turn.

### DELTA
+3 LOC. No deletion. Pure UX win.

---

## 8. Stub LLM provider for tests

### WHAT
Picobot ships a `StubProvider` that echoes the last user message — the entire test suite can run without ANY LLM dependency.

### WHERE
- D:/tmp/picobot/internal/providers/stub.go (28 LOC)
- D:/tmp/picobot/internal/agent/loop_test.go:11-24 — full agent loop tested with `providers.NewStubProvider()`

### SNIPPET — stub.go entire file
```go
type StubProvider struct{}

func NewStubProvider() *StubProvider { return &StubProvider{} }

func (p *StubProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string) (LLMResponse, error) {
    last := ""
    for i := len(messages) - 1; i >= 0; i-- {
        if messages[i].Role == "user" { last = messages[i].Content; break }
    }
    if last == "" { return LLMResponse{Content: "(stub) Hello from StubProvider"}, nil }
    return LLMResponse{Content: fmt.Sprintf("(stub) Echo: %s", last)}, nil
}

func (p *StubProvider) GetDefaultModel() string { return "stub-model" }
```

### WHY
Aura's tests currently mock the LLM via the `llm.Client` interface in various ad-hoc shapes. A single canonical `StubLLM` that implements `llm.Client` with the same echo-back behavior (and an optional `ScriptedStubLLM` that replays a JSON tape of responses + tool calls for golden-tape testing) would unify ~10+ scattered mock implementations.

Caveat: probe_chat-driven tests are the canonical Aura "real" tests per CLAUDE.md — this stub is for unit tests where the LLM is not the system-under-test.

### EFFORT
~1 session. Audit existing mocks in `internal/llm/*_test.go`, replace with `StubLLM` + optional `ScriptedStubLLM`.

### DELTA
~-200 LOC of duplicated mock code across tests.

---

## 9. `remember` regex pre-handler (anti-pattern — DO NOT lift)

### WHAT
Picobot's loop has a regex `^remember(?:\s+to)?\s+(.+)$` that catches "remember X" messages before the LLM, writes to today's memory, and replies "OK, I've remembered that." — bypassing the LLM entirely.

### WHERE
- D:/tmp/picobot/internal/agent/loop.go:23 + 181-204

### WHY (DO NOT LIFT)
This is exactly the fast-path classifier anti-pattern the user explicitly rejected on 2026-05-21 (memory `feedback_check_tmp_sources_then_brainstorm_best`). Calling it out so a future maintainer sees picobot has it and resists copying. Picobot ships this in production but Aura's 4-source analysis concluded it's wrong shape for an agent system.

### EFFORT
N/A (avoid).

### DELTA
0. Preserve current Aura behavior (LLM always sees the message).

---

## 10. Stable phase ordering with numeric gaps — cli-printing-press

### WHAT
Pipeline phases get filenames like `00-preflight-plan.md`, `10-research-plan.md`, `20-scaffold-plan.md` with gaps of 10. Future phases insert as `15-` etc. without renaming existing files.

### WHERE
- D:/tmp/cli-printing-press/internal/pipeline/state.go:41-58

### SNIPPET — state.go:41-58
```go
// phaseNumber assigns a stable prefix for plan filenames. Numbers use
// gaps (0, 10, 20 …) so future phases can be inserted without renaming
// existing files.
var phaseNumber = map[string]int{
    PhasePreflight:      0,
    PhaseResearch:       10,
    PhaseScaffold:       20,
    PhaseEnrich:         30,
    PhaseRegenerate:     40,
    PhaseReview:         50,
    PhaseAgentReadiness: 55,  // inserted between 50 and 60 without renames
    PhaseComparative:    60,
    PhaseShip:           70,
}

func PlanFilename(phase string) string {
    return fmt.Sprintf("%02d-%s-plan.md", phaseNumber[phase], phase)
}
```

### WHY
Aura's `scripts/ralph/` and `.planning/` could use this. Today phase docs are named like `phase-fix-plan-2026-05-19.md` — flat dating works but doesn't sort by execution order. Numeric-gap prefixes give natural `ls` sort + insertion room. Minor tooling polish.

### EFFORT
~0.25 session if adopting. Or leave as a future-reference pattern.

### DELTA
0 LOC. Convention change.

---

## 11. Skill content size in cli-printing-press — confirms references/ payoff

### WHAT
`skills/printing-press/SKILL.md` = 217 KB. Without `references/` the model would inhale all of that on every turn. Instead the SKILL.md body says "for X, read references/X.md" and the agent opens the reference on demand.

### WHERE
- D:/tmp/cli-printing-press/skills/printing-press/references/ — 14 files, biggest is `browser-sniff-capture.md` at 80 KB.

### SAMPLE LISTING
```
absorb-scoring.md            6166 bytes
browser-sniff-capture.md    80197 bytes  <-- never inlined
codex-delegation.md          8165 bytes
crowd-sniff.md               2869 bytes
deepwiki-research.md         5682 bytes
dogfood-testing.md           1826 bytes
known-specs.md               2646 bytes
noi-examples.md              3177 bytes
novel-features-subagent.md  11434 bytes
per-source-rate-limiting.md  2228 bytes
scorecard-patterns.md        2529 bytes
secret-protection.md        13637 bytes  <-- inlined only when relevant
setup-checks.md             13281 bytes
spec-format.md              12534 bytes
```

### WHY
Pattern was lifted but the SIZE ratio is the lesson — `references/` is what makes a 217 KB skill feasible. Aura skills today don't routinely exceed ~5 KB; once they grow, the `references/` discipline is the only way to keep prompt budget sane. Document in the skill authoring guide if not already there.

### EFFORT
0 — pattern already adopted.

### DELTA
0.

---

## 12. Test pattern — provider-stub-driven loop tests

### WHAT
Loop tests use `providers.NewStubProvider()` (echo) to drive a real Agent loop end-to-end without a network. The hub is a real `chat.NewHub(10)`. Tool execution is real. Only the LLM is stubbed.

### WHERE
- D:/tmp/picobot/internal/agent/loop_test.go (full file, 25 LOC)
- D:/tmp/picobot/internal/agent/tools/registry_test.go (35 LOC)

### SNIPPET — loop_test.go entire file
```go
func TestProcessDirectWithStub(t *testing.T) {
    b := chat.NewHub(10)
    p := providers.NewStubProvider()
    ag := NewAgentLoop(b, p, p.GetDefaultModel(), 5, "", nil, nil)
    resp, err := ag.ProcessDirect("hello", 1*time.Second)
    if err != nil { t.Fatalf("expected no error, got %v", err) }
    if resp == "" { t.Fatalf("expected response, got empty string") }
}
```

### WHY
Aura already has probe_chat for real-LLM E2E. The stub-driven unit test is the complementary cheap layer for testing dispatcher / loop control-flow / tool-arg-validation WITHOUT a network. Pair with the `StubLLM` recommended in #8.

### EFFORT
Folds into #8.

### DELTA
Covered by #8.

---

## 13. Tool name + description ON ONE LINE — picobot discipline

### WHAT
Picobot tool descriptions are 1-2 sentences. No exceptions. `web` is 4 words. `web_search` is 11 words. `cron` is 23 words (the longest, and it explains the 3 actions).

### WHERE
- D:/tmp/picobot/internal/agent/tools/web.go:18
- D:/tmp/picobot/internal/agent/tools/web_search.go:30
- D:/tmp/picobot/internal/agent/tools/cron.go:26-28

### SAMPLE DESCRIPTIONS
```
web:           "Fetch web content from a URL"
web_search:    "Search the web using DuckDuckGo and return relevant results"
filesystem:    "Read, write, and list files in the workspace"
exec:          "Execute shell commands (array form only, restricted for safety)"
cron:          "Schedule one-time or recurring reminders/tasks. Actions: add (schedule), list (show pending), cancel (remove by name)."
message:       "Send a message to the current channel/chat"
read_skill:    "Read the full content of a skill by name"
```

### WHY
Aura already has `description_audit_test.go` (lines 21-60) enforcing first-line markers — a similar concept. But picobot's descriptions are tighter on the FULL string, not just the first line. Worth extending Aura's audit test to enforce `len(description) <= 200` for catalogued tools — bench the LLM with both; if quality is equal, the smaller description wins on prompt budget.

### EFFORT
~5 minutes — add `maxLen := 200; if len(tool.Description()) > maxLen { t.Errorf(...) }` to `description_audit_test.go`.

### DELTA
0 LOC test. Forces some Aura descriptions to shrink in subsequent commits. Estimated -500 to -1000 prompt tokens per turn (varies by registered tool count).

---

## 14. Surprising — picobot has THREE memory-rank implementations

### WHAT
Picobot ships:
- `memory/ranker.go` — string-similarity scorer (cheap, in-process).
- `memory/llm_ranker.go` — LLM-as-ranker (expensive, accurate).
- An interface `Ranker.Rank(query, memories, topK)` letting the agent choose.

### WHERE
- D:/tmp/picobot/internal/agent/memory/ranker.go
- D:/tmp/picobot/internal/agent/memory/llm_ranker.go

### WHY (cautionary — DO NOT lift)
This is exactly the "more layers, more failure modes" picobot accepted that Aura rejected via the 2026-05-21 lessons. Aura's `feedback_minillm_cpu_not_viable_for_tool_retrieval` memory says: skip LLM-rerank for tool retrieval, use embed-cosine. Picobot has both abstractions; Aura should keep its narrower scope. Calling out so future readers see the option and the reason it was rejected.

### EFFORT
N/A.

### DELTA
0.

---

## 15. cli-printing-press `Compose` returns shape clause by HTTP method

### WHAT
mcpdesc.Compose adds a "Returns the new X." (POST) / "Returns the updated X." (PATCH/PUT) / "Returns array of X." (GET) clause automatically. Adds "Destructive." suffix on DELETE without duplicating. Adds "Partial update." on PATCH when no Returns clause already mentions update.

### WHERE
- D:/tmp/cli-printing-press/internal/mcpdesc/compose.go:197-244

### SNIPPET — compose.go:228-244
```go
func appendMethodMarker(desc, method string) string {
    if desc == "" { return desc }
    lower := strings.ToLower(desc)
    switch strings.ToUpper(method) {
    case methodDELETE:
        if !strings.Contains(lower, "destructive") {
            return desc + " Destructive."
        }
    case methodPATCH:
        if !strings.Contains(lower, "partial update") && !strings.Contains(lower, "returns") {
            return desc + " Partial update."
        }
    }
    return desc
}
```

### WHY
Aura's existing `description_audit_test.go` already enforces `Destructive.` / `Read-only.` / `Returns ` markers on the FIRST LINE — but currently authors hand-write them. If Aura ever auto-generates MCP wrapper descriptions (the surface today comes from upstream MCP server descriptions verbatim), this `appendMethodMarker`-style auto-suffix is the missing piece that would close the loop with the audit test.

### EFFORT
~0.25 session. Helper function next to the audit test.

### DELTA
+30 LOC. Pure additive when Aura starts re-describing MCP tools.

---

## Cross-cutting observations

### Picobot's lean-Go playbook (~6273 LOC for a full agent)
1. **One package = one concern**: `agent/`, `chat/`, `mcp/`, `cron/`, `session/`, `providers/`, `channels/`. No shared utils package.
2. **Zero generics**. Interface-based polymorphism. Picobot uses Go 1.24 but doesn't lean on generics for the tool registry — `map[string]Tool` is fine.
3. **No external SDK for major surfaces**: telegram HTTP+polling is hand-rolled. MCP is hand-rolled. SSE parsing is hand-rolled.
4. **stdlib-only with one exception** (websocket dep for WhatsApp, optional).
5. **Tests live next to code** with `_test.go`. No `/test/` directory. Cross-package mocks live in `providers/stub.go`.
6. **Channel adapters are at most 350 LOC each** — when one balloons (whatsapp.go at 449), it's because WhatsApp's protocol is fundamentally more complex, not because of code smell.

### What Aura already does better than picobot
- Description audit test (Aura has it, picobot doesn't).
- Categorized tools + capability gating (Aura `CategorizedTool`, picobot doesn't).
- Streaming Telegram edits (Aura does, picobot fires-and-forgets).
- Wiki-as-graph search layer (picobot has flat memory files only).
- MCP transport hot-reload (Aura's reconciler, picobot loads once at boot).

### What picobot does better than Aura
- Surface size (~22 tools spread across more files vs picobot's ~12 tools in tight `tools/` package).
- `os.Root` sandboxing (Aura validates by string; picobot can't escape by construction).
- Hub/Subscribe abstraction for N channels (Aura is telegram-shaped today).
- One-system-message context build (Aura emits multiple system messages on some paths).

---

## Translatability summary

| # | Pattern                                  | Translatability (1-5) | Aura LOC delta | Effort |
|---|------------------------------------------|-----------------------|----------------|--------|
| 1 | Kitchen-sink action-enum tool            | 5                     | -1100          | 2 sessions |
| 2 | os.Root sandboxing                       | 5                     | -150           | 1 session |
| 3 | Single consolidated system message       | 5                     | -50            | 0.5 session |
| 4 | Lean MCP client (transport interface)    | 3                     | -300 to -500   | 1-2 sessions |
| 5 | mcp-descriptions.json sidecar            | 5                     | +120           | 0.5 session |
| 6 | Hub + Subscribe channel adapter          | 4                     | 0 now, -300 with WhatsApp | 1 session |
| 7 | `lastToolResult` empty-reply fallback    | 5                     | +3             | 5 min |
| 8 | StubLLM + ScriptedStubLLM for tests      | 5                     | -200           | 1 session |
| 9 | `remember` regex pre-handler             | 1 (anti-pattern)      | 0              | avoid |
| 10| Numeric-gap phase filenames              | 4                     | 0              | 0.25 session |
| 11| `references/` skill payoff size signal   | n/a — already lifted  | 0              | 0 |
| 12| Stub-driven loop tests                   | 5                     | covered by #8  | covered by #8 |
| 13| `len(description) <= 200` audit          | 5                     | -500 to -1000 prompt tokens/turn | 5 min |
| 14| Two-tier memory ranker                   | 1 (anti-pattern)      | 0              | avoid |
| 15| HTTP-method auto-suffix                  | 4                     | +30            | 0.25 session |

---

## Top 3 ROI picks

1. **Pattern #1 — Kitchen-sink action-enum tool** (`source` / `scheduler` / `wiki` collapse). Single biggest LOC delta (~-1100), directly satisfies the ~22→~8 tool surface mandate, and the picobot evidence base is 3 production tools (filesystem, cron, exec-as-array). Highest signal-to-effort ratio.
2. **Pattern #13 — `len(description) <= 200` audit test** + Pattern #3 single-system-message. Combined effort ~30 minutes, combined token-budget impact ~-500 to -1500 tokens per turn (varies with active tool count and registered overlays). The cheapest velocity-of-prompt-budget win available.
3. **Pattern #2 — `os.Root` sandboxing**. Deletes a whole class of path-traversal / TOCTOU bugs, removes ~150 LOC of validation helpers, and is one-session well-bounded work. Aura is already on Go 1.24+, so the API is there.

Honourable mentions: Pattern #7 (`lastToolResult` fallback — 5-minute UX win) and Pattern #5 (mcp-descriptions.json sidecar — half-session feature, enables description discipline for upstream MCPs).

## Top 3 anti-patterns to AVOID

- Pattern #9 — `remember` regex pre-handler. Fast-path classifier was explicitly rejected by the 2026-05-21 lessons (4-source analysis converged on REJECT). Picobot ships it; Aura must not.
- Pattern #14 — Two-tier memory ranker. The Aura memory `feedback_minillm_cpu_not_viable_for_tool_retrieval` already locked the answer to embed-cosine; LLM-rerank for tool/memory retrieval is rejected.
- Implicit pattern — picobot has NO streaming Telegram edits. Aura already does this better — keep it.
