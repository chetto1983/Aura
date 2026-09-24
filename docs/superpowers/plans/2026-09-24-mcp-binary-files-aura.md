# MCP binary files: Aura bridge implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A tool call on any mounted MCP server that returns a file puts the file in the agent's `/workspace`, where it can open, convert or compute on it. The file is deleted when the turn ends. The model sees paths, never bytes.

**Architecture:**

- **Decode.** `mcp.DecodeToolPayload` keeps image, audio and embedded-resource blocks as `FilePart`s and `resource_link` blocks as links.
- **Links.** `MountedServer.CallTool` reads each link back with `resources/read` on the session that made the call.
- **Write.** `bridgedTool.newResult` hands the files to a `FileSink`. Its implementation, `tools.MCPFileSink`, writes them to `/workspace/mcp-files/<request-id>/<server>/<name>` through the sandbox router, and registers the request directory with a `TurnCleanup` that `LlmAgent.Run` installs and drains.
- **What the model reads.** The server's text plus a reserved JSON footer of paths.
- **No per-server code.** Nothing in Aura knows about mail or WhatsApp.

**Tech Stack:** Go 1.27.1, go-sdk v1.7.0 (`sdkmcp`), the `usersandbox` router, goleak, `-race`.

**Spec:** `D:\Aura\docs\superpowers\specs\2026-09-24-mcp-binary-files-design.md`, section "Aura: the generic bridge". Its companions ship first: `2026-09-24-mcp-binary-files-pim-fork.md` and `2026-09-24-mcp-binary-files-whatsapp-fork.md`. This plan's code works without them; only Task 8 needs them.

## Global Constraints

**Repository and commits**
- Repository: `D:\Aura`, branch `master`. Commit on `master` directly; no feature branch.
- Another session may be working in the same tree. Commit with explicit paths only (`git add <new files>` then `git commit -- <paths>`), and unstage anything you did not write. Re-read `internal/agent/llm_agent.go` immediately before editing it.
- End every commit message with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`. Never use `--no-verify`.
- No push without the operator's go. Pushing `master` publishes the edge image, which the appliances install.

**Build and test**
- Go runs in WSL. Create `<scratchpad>/aura_go.sh`:

  ```bash
  #!/usr/bin/env bash
  # Run one command in the Aura tree from WSL, with the Go toolchain on PATH.
  set -uo pipefail
  export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
  cd /mnt/d/Aura || exit 1
  "$@"
  ```

  Run it as `MSYS_NO_PATHCONV=1 wsl bash /mnt/c/Users/Davide/AppData/Local/Temp/claude/d--Aura/<session>/scratchpad/aura_go.sh <command…>`. Below, "Run: `X`" means `X` through this script.
- Never run a Windows `.exe`. Edit with the Edit tool.
- Per task, run the touched packages' tests with `-race` plus `go vet` on them. The full `make quality` and the coverage gate run once, in Task 7. Mutation testing runs in CI only.
- **File-size limit: 600 lines per file.** Measured before this work:
  - `llm_agent.go` is 591 lines and gains 2;
  - `bridge_supervisor.go` is 509 and gains about 6;
  - `cmd/aura/main.go` is 597, so Task 5 first moves `runtimeToolHandles` into its own file.

**Values from the spec** (verbatim)
- Caps: `mcp.MaxFileBytes = 25 << 20` per file, and `mcp.MaxCallFileBytes = 50 << 20` per call.
- Path: `/workspace/mcp-files/<request-id>/<server>/<name>`. Cleanup removes `/workspace/mcp-files/<request-id>`.
- Footer: `{"files":[{path, name, mime_type, size_bytes, sha256 | not_materialized}], "note": …}`. The note, verbatim: `Delete each file (rm) once you are done with it. Whatever is left is deleted when this turn ends; copy a file elsewhere in /workspace to keep it.`
- Reasons, verbatim:
  - `no agent turn owns the file`
  - `sandbox unavailable: <cause>`
  - `<n> bytes exceeds the 26214400-byte file cap`
  - `the call's files exceed the 52428800-byte cap`
  - `read failed: <cause>`
  - `the server returned no contents`
  - `write failed: <cause>`
  - `this host has no workspace for MCP files`
- MIME precedence for a link: the contents' type, unless it is empty or `application/octet-stream`; then the link's type; then `application/octet-stream`.
- Aura never fetches a link URI itself. `resources/read` on the server's own session is the only read path.

## Review Focus

1. **A long email body with an attachment.** A text result longer than the preview cap must not truncate away the path. Pinned in Task 4 (`TestFilesFooterSurvivesASmallPreviewCap`).
2. **Two files with the same name.** Outlook sends several `image001.png`; the same attachment may also be fetched twice in one turn. Neither may overwrite the other. Pinned in Task 3 (`TestMCPFileSinkNeverOverwritesAFileOfTheSameName`), across calls through the box listing and within one call.
3. **A hostile or odd server file name.** `../../.bashrc`, a Windows path, control characters, 300-byte names. Each must become one safe path component. Pinned in Task 3 (`TestMCPFileNameIsOneSafeComponent`).
4. **A turn that is cancelled or crashes.** Its files must still be removed, on a context the cancellation cannot abort. Pinned in Task 2 (the panic test and `TestRunTurnCleanupOutlivesACancelledTurn`).
5. **A caller with no turn.** toolpipe, the docs MCP and a readiness probe call a server that returns images. They must get the text and write nothing. Pinned in Task 3 (`TestMCPFileSinkWithoutATurnWritesNothing`) and Task 4 (`TestAMountWithoutAFileSinkSaysSoAndKeepsTheText`).

---

### Task 1: Decode binary content

**Files:**
- Create: `internal/mcp/file.go`
- Modify: `internal/mcp/result.go` (`ToolPayload`, `DecodeToolPayload`, the `DecodeToolResult` comment)
- Test: `internal/mcp/file_test.go`, `internal/mcp/result_test.go`

**Interfaces:**
- Produces:
  - constants `mcp.MaxFileBytes`, `mcp.MaxCallFileBytes`, `mcp.OctetStream`;
  - `mcp.FilePart{Name, MIMEType string; Data []byte; Unavailable string}`, with method `NotMaterialized(reason string) FileOutcome`;
  - `mcp.FileOutcome{Path, Name, MIMEType string; SizeBytes int64; SHA256, NotMaterialized string}`, with JSON tags `path,omitempty`, `name`, `mime_type,omitempty`, `size_bytes`, `sha256,omitempty`, `not_materialized,omitempty`;
  - functions `mcp.FileFromContents(*sdkmcp.ResourceContents) (FilePart, bool)`, `mcp.NameFromURI(string) string` and `mcp.FileCapExceeded(size int64) string`;
  - `ToolPayload.Files []FilePart` and `ToolPayload.Links []*sdkmcp.ResourceLink`.

- [ ] **Step 1: Write the failing tests.** Create `internal/mcp/file_test.go`:

```go
package mcp

import (
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNameFromURI(t *testing.T) {
	for uri, want := range map[string]string{
		"attachment://stash/abc123": "abc123",
		"whatsapp-media://393331234567@s.whatsapp.net/3EB0C767": "3EB0C767",
		"file:///tmp/report.pdf":                  "report.pdf",
		"https://example.com/a/b.pdf?sig=x#frag": "b.pdf",
		"notes://dir/":                            "dir",
		"":                                        "",
	} {
		if got := NameFromURI(uri); got != want {
			t.Errorf("NameFromURI(%q) = %q, want %q", uri, got, want)
		}
	}
}

func TestFileFromContents(t *testing.T) {
	blob, ok := FileFromContents(&sdkmcp.ResourceContents{URI: "attachment://a/invoice.pdf", MIMEType: "application/pdf", Blob: []byte("%PDF")})
	if !ok || blob.Name != "invoice.pdf" || blob.MIMEType != "application/pdf" || string(blob.Data) != "%PDF" {
		t.Fatalf("blob contents = %+v, %v", blob, ok)
	}
	text, ok := FileFromContents(&sdkmcp.ResourceContents{URI: "notes://today.md", MIMEType: "text/markdown", Text: "# hi"})
	if !ok || string(text.Data) != "# hi" {
		t.Fatalf("text contents = %+v, %v", text, ok)
	}
	for _, empty := range []*sdkmcp.ResourceContents{nil, {URI: "empty://x"}} {
		if _, ok := FileFromContents(empty); ok {
			t.Fatalf("contents %+v carry no bytes, yet made a file", empty)
		}
	}
}

func TestFilePartNotMaterializedKeepsTheFirstReason(t *testing.T) {
	part := FilePart{Name: "a.pdf", MIMEType: "application/pdf", Data: []byte("abc")}
	want := FileOutcome{Name: "a.pdf", MIMEType: "application/pdf", SizeBytes: 3, NotMaterialized: "sandbox unavailable: down"}
	if got := part.NotMaterialized("sandbox unavailable: down"); got != want {
		t.Fatalf("outcome = %+v, want %+v", got, want)
	}
	unread := FilePart{Name: "old.pdf", Unavailable: "read failed: expired"}
	if got := unread.NotMaterialized("sandbox unavailable: down"); got.NotMaterialized != "read failed: expired" {
		t.Fatalf("the reason the bytes were missing must win: %+v", got)
	}
}

func TestFileCapExceeded(t *testing.T) {
	if got := FileCapExceeded(MaxFileBytes + 1); got != "26214401 bytes exceeds the 26214400-byte file cap" {
		t.Fatalf("FileCapExceeded = %q", got)
	}
}
```

Append to `internal/mcp/result_test.go` (add `"reflect"` to its imports):

```go
func TestDecodeToolPayload_KeepsEveryFileBlock(t *testing.T) {
	size := int64(8)
	link := &sdkmcp.ResourceLink{URI: "attachment://stash/abc", Name: "invoice.pdf", MIMEType: "application/pdf", Size: &size}
	result := &sdkmcp.CallToolResult{Content: []sdkmcp.Content{
		&sdkmcp.TextContent{Text: `{"attachmentId":"abc"}`},
		&sdkmcp.ImageContent{Data: []byte("png-bytes"), MIMEType: "image/png"},
		&sdkmcp.AudioContent{Data: []byte("ogg-bytes"), MIMEType: "audio/ogg"},
		&sdkmcp.EmbeddedResource{Resource: &sdkmcp.ResourceContents{URI: "file:///tmp/report.pdf", MIMEType: "application/pdf", Blob: []byte("%PDF")}},
		&sdkmcp.EmbeddedResource{Resource: &sdkmcp.ResourceContents{URI: "notes://today/notes.md", MIMEType: "text/markdown", Text: "# hi"}},
		&sdkmcp.EmbeddedResource{Resource: &sdkmcp.ResourceContents{URI: "empty://x"}},
		link,
	}}

	payload, isError := DecodeToolPayload(result)

	if isError {
		t.Fatal("a result carrying files is not an error")
	}
	if payload.Text != `{"attachmentId":"abc"}` {
		t.Fatalf("Text = %q, want the text block alone", payload.Text)
	}
	want := []FilePart{
		{MIMEType: "image/png", Data: []byte("png-bytes")},
		{MIMEType: "audio/ogg", Data: []byte("ogg-bytes")},
		{Name: "report.pdf", MIMEType: "application/pdf", Data: []byte("%PDF")},
		{Name: "notes.md", MIMEType: "text/markdown", Data: []byte("# hi")},
	}
	if !reflect.DeepEqual(payload.Files, want) {
		t.Fatalf("Files = %#v, want %#v", payload.Files, want)
	}
	if len(payload.Links) != 1 || payload.Links[0] != link {
		t.Fatalf("Links = %#v, want the one link", payload.Links)
	}
}
```

- [ ] **Step 2: Run them to verify they fail.** Run: `go test ./internal/mcp/ -run 'NameFromURI|FileFromContents|NotMaterialized|FileCapExceeded|KeepsEveryFileBlock'`.

Expected: build FAIL with `undefined: NameFromURI` (and `FilePart`, `payload.Files`).

- [ ] **Step 3: Implement.** Create `internal/mcp/file.go`:

```go
package mcp

import (
	"fmt"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// file.go names the binary content a tool result can carry. A server hands a file
// back inline (an image, audio or embedded-resource block) or as a resource_link the
// client reads back on the same session. Either way it becomes a FilePart, and the
// bridge turns each FilePart into a FileOutcome: a path in the agent's workspace, or
// the reason there is none. The bytes never reach the model.

const (
	// MaxFileBytes caps one file: Gmail's attachment limit, and far below the aura
	// container's 768 MiB even after base64 inflation on the wire.
	MaxFileBytes = 25 << 20
	// MaxCallFileBytes caps the files of one tool call together.
	MaxCallFileBytes = 50 << 20
	// OctetStream is the MIME type that says nothing about a file.
	OctetStream = "application/octet-stream"
)

// FilePart is one file a tool result carried. Unavailable says why its bytes could
// not be obtained; Data is then empty.
type FilePart struct {
	Name        string
	MIMEType    string
	Data        []byte
	Unavailable string
}

// FileOutcome is what became of one FilePart, as the model reads it: the workspace
// path it was written to, or why it was not.
type FileOutcome struct {
	Path            string `json:"path,omitempty"`
	Name            string `json:"name"`
	MIMEType        string `json:"mime_type,omitempty"`
	SizeBytes       int64  `json:"size_bytes"`
	SHA256          string `json:"sha256,omitempty"`
	NotMaterialized string `json:"not_materialized,omitempty"`
}

// NotMaterialized is p's outcome when it was not written: for reason, or for the
// reason its bytes were unavailable in the first place.
func (p FilePart) NotMaterialized(reason string) FileOutcome {
	if p.Unavailable != "" {
		reason = p.Unavailable
	}
	return FileOutcome{Name: p.Name, MIMEType: p.MIMEType, SizeBytes: int64(len(p.Data)), NotMaterialized: reason}
}

// FileCapExceeded is the reason a file over MaxFileBytes is refused.
func FileCapExceeded(size int64) string {
	return fmt.Sprintf("%d bytes exceeds the %d-byte file cap", size, MaxFileBytes)
}

// FileFromContents is one resource's contents as a FilePart: a blob as its bytes,
// text as UTF-8. False when the contents carry neither.
func FileFromContents(rc *sdkmcp.ResourceContents) (FilePart, bool) {
	switch {
	case rc == nil:
		return FilePart{}, false
	case rc.Blob != nil:
		return FilePart{Name: NameFromURI(rc.URI), MIMEType: rc.MIMEType, Data: rc.Blob}, true
	case rc.Text != "":
		return FilePart{Name: NameFromURI(rc.URI), MIMEType: rc.MIMEType, Data: []byte(rc.Text)}, true
	default:
		return FilePart{}, false
	}
}

// NameFromURI is the last path segment of uri, the name a file gets when its server
// gave none: attachment://stash/abc123 names abc123.
func NameFromURI(uri string) string {
	rest := uri
	if _, after, ok := strings.Cut(rest, "://"); ok {
		rest = after
	}
	if i := strings.IndexAny(rest, "?#"); i >= 0 {
		rest = rest[:i]
	}
	rest = strings.TrimRight(rest, "/")
	return rest[strings.LastIndex(rest, "/")+1:]
}
```

In `internal/mcp/result.go`, add two fields to `ToolPayload`, after `Structured`:

```go
	// Files are the binary blocks the result carried inline: image, audio and
	// embedded resources. Links are its resource_links; the bridge reads each one
	// back on the calling session and appends it to Files (mcptools.resolveLinks)
	// before anything downstream sees the payload.
	Files []FilePart
	Links []*sdkmcp.ResourceLink
```

In the `DecodeToolResult` doc comment, replace `Non-text content parts (images, resource links, ...) are
// skipped rather than stringified` with `Non-text content parts (images, resource links, ...) are
// skipped rather than stringified; DecodeToolPayload keeps them as Files and Links`.

Replace the body of `DecodeToolPayload` from `var b strings.Builder` to the end of the `payload = ToolPayload{...}` literal with:

```go
	var b strings.Builder
	for _, part := range result.Content {
		switch c := part.(type) {
		case *sdkmcp.TextContent:
			b.WriteString(c.Text)
		case *sdkmcp.ImageContent:
			payload.Files = append(payload.Files, FilePart{MIMEType: c.MIMEType, Data: c.Data})
		case *sdkmcp.AudioContent:
			payload.Files = append(payload.Files, FilePart{MIMEType: c.MIMEType, Data: c.Data})
		case *sdkmcp.EmbeddedResource:
			if file, ok := FileFromContents(c.Resource); ok {
				payload.Files = append(payload.Files, file)
			}
		case *sdkmcp.ResourceLink:
			payload.Links = append(payload.Links, c)
		}
	}
	payload.Text = strings.TrimRight(b.String(), "\n")
	payload.Structured = structuredJSON(result)
```

The `isError` lines after it are unchanged.

- [ ] **Step 4: Run the package.** Run: `go vet ./internal/mcp/`, then `go test -race -count=1 ./internal/mcp/`. Expected: `ok`. `TestDecodeToolResult_SkipsNonTextContent` still passes: the text projection is unchanged.

- [ ] **Step 5: Commit.**

```bash
cd /d/Aura
git add internal/mcp/file.go internal/mcp/file_test.go
git commit -F - -- internal/mcp/file.go internal/mcp/file_test.go internal/mcp/result.go internal/mcp/result_test.go <<'EOF'
feat(mcp): decode the files a tool result carries

DecodeToolPayload kept text only and dropped every other block, so an
attachment or a media file a server returned never reached Aura at all.
It now keeps image, audio and embedded-resource blocks as FileParts and
resource_link blocks as Links, beside the text it already decoded; the
text projection is unchanged. file.go names the per-file and per-call caps
(25 and 50 MiB) and the outcome shape the bridge reports to the model.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 2: Turn cleanup: a collector on the context, drained at turn end

**Files:**
- Create: `internal/agent/tools/turn_cleanup.go`, `internal/agent/tools/turn_cleanup_test.go`
- Create: `internal/agent/llm_agent_turn_cleanup.go`
- Create: `internal/agent/llm_agent_turn_cleanup_test.go` (package `agent_test`) and `internal/agent/llm_agent_turn_cleanup_internal_test.go` (package `agent`)
- Modify: `internal/agent/llm_agent.go` (2 lines in `Run`)

**Interfaces:**
- Produces:
  - `tools.TurnCleanup`, with `Add(key string, step func(context.Context) error)` and `Run(ctx) error`; both are nil-safe;
  - `tools.WithTurnCleanup(ctx) (context.Context, *TurnCleanup)`;
  - `tools.TurnCleanupFromContext(ctx) *TurnCleanup`, which returns nil outside a run;
  - in package `agent`: `runTurnCleanup(ctx context.Context, requestID string, cleanup *tools.TurnCleanup)`.

- [ ] **Step 1: Write the failing collector tests.** Create `internal/agent/tools/turn_cleanup_test.go`:

```go
package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
)

func TestTurnCleanupRunsEachKeyOnceNewestFirst(t *testing.T) {
	ctx, cleanup := WithTurnCleanup(context.Background())
	var order []string
	step := func(name string) func(context.Context) error {
		return func(context.Context) error { order = append(order, name); return nil }
	}
	TurnCleanupFromContext(ctx).Add("a", step("a"))
	TurnCleanupFromContext(ctx).Add("b", step("b"))
	TurnCleanupFromContext(ctx).Add("a", step("a-again"))

	if err := cleanup.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !slices.Equal(order, []string{"b", "a"}) {
		t.Fatalf("order = %v, want [b a]", order)
	}
	if err := cleanup.Run(context.Background()); err != nil || len(order) != 2 {
		t.Fatalf("a second Run repeated a step: %v, %v", order, err)
	}
}

func TestTurnCleanupRunsEveryStepAndJoinsTheFailures(t *testing.T) {
	_, cleanup := WithTurnCleanup(context.Background())
	first, second := errors.New("first"), errors.New("second")
	ran := 0
	cleanup.Add("1", func(context.Context) error { ran++; return first })
	cleanup.Add("2", func(context.Context) error { ran++; return second })

	err := cleanup.Run(context.Background())

	if ran != 2 || !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("ran %d steps, err = %v; want both run and both reported", ran, err)
	}
}

func TestTurnCleanupOutsideARunIsNil(t *testing.T) {
	cleanup := TurnCleanupFromContext(context.Background())
	if cleanup != nil {
		t.Fatalf("a bare context carries a cleanup: %v", cleanup)
	}
	cleanup.Add("k", func(context.Context) error {
		t.Fatal("a nil cleanup must not keep a step")
		return nil
	})
	if err := cleanup.Run(context.Background()); err != nil {
		t.Fatalf("nil Run: %v", err)
	}
}

func TestTurnCleanupAcceptsConcurrentCalls(t *testing.T) {
	_, cleanup := WithTurnCleanup(context.Background())
	var ran atomic.Int32
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			cleanup.Add(fmt.Sprint(i%4), func(context.Context) error { ran.Add(1); return nil })
		})
	}
	wg.Wait()

	if err := cleanup.Run(context.Background()); err != nil || ran.Load() != 4 {
		t.Fatalf("ran %d steps (err %v), want one per distinct key", ran.Load(), err)
	}
}
```

- [ ] **Step 2: Run them to verify they fail.** Run: `go test ./internal/agent/tools/ -run TurnCleanup`. Expected: build FAIL with `undefined: WithTurnCleanup`.

- [ ] **Step 3: Implement the collector.** Create `internal/agent/tools/turn_cleanup.go`:

```go
package tools

import (
	"context"
	"errors"
	"sync"
)

// TurnCleanup collects what a turn leaves in the box and must remove when the turn
// ends: today, the directory MCP files are materialized into (MCPFileSink).
// LlmAgent.Run installs one per run and drains it from its outermost defer, so a
// normal end, an ask_user pause, an error and a panic all reach it. Every swarm
// worker is its own Run, so each gets its own.
type TurnCleanup struct {
	mu    sync.Mutex
	keys  map[string]struct{}
	steps []func(context.Context) error
}

// Add registers step under key. A key already registered is not registered twice,
// so every call of a turn can ask for the same directory to be removed. A nil
// TurnCleanup ignores it.
func (c *TurnCleanup) Add(key string, step func(context.Context) error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.keys[key]; ok {
		return
	}
	if c.keys == nil {
		c.keys = map[string]struct{}{}
	}
	c.keys[key] = struct{}{}
	c.steps = append(c.steps, step)
}

// Run runs every registered step once, newest first, and forgets them. A failing
// step does not stop the rest; the failures come back joined.
func (c *TurnCleanup) Run(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	steps := c.steps
	c.steps, c.keys = nil, nil
	c.mu.Unlock()
	var errs []error
	for i := len(steps) - 1; i >= 0; i-- {
		if err := steps[i](ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

type turnCleanupCtxKey struct{}

// WithTurnCleanup returns ctx carrying a fresh TurnCleanup. A nested run gets its
// own, which shadows the outer one for everything that run calls.
func WithTurnCleanup(ctx context.Context) (context.Context, *TurnCleanup) {
	cleanup := &TurnCleanup{}
	return context.WithValue(ctx, turnCleanupCtxKey{}, cleanup), cleanup
}

// TurnCleanupFromContext returns the TurnCleanup of the run ctx belongs to, or nil
// outside one: toolpipe, the docs MCP and a readiness probe have no turn.
func TurnCleanupFromContext(ctx context.Context) *TurnCleanup {
	cleanup, _ := ctx.Value(turnCleanupCtxKey{}).(*TurnCleanup)
	return cleanup
}
```

- [ ] **Step 4: Run the collector tests.** Run: `go test -race -count=1 ./internal/agent/tools/ -run TurnCleanup`. Expected: `ok`.

- [ ] **Step 5: Write the failing agent tests.** Create `internal/agent/llm_agent_turn_cleanup_test.go`. It reuses `newIC`, `collect` and `textResponseCall` from `llm_agent_test.go`, and `askUserCall` and `newPauseIC` from `llm_agent_pause_test.go`.

```go
package agent_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// leaveFileTool stands in for an MCP tool that materialized a file: it registers a
// cleanup step with its turn and counts how often that step runs.
type leaveFileTool struct{ cleaned *atomic.Int32 }

func (leaveFileTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "leave_file",
		Summary:     "Leave a file for the turn to clean up.",
		Description: "Leave a file for the turn to clean up.",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Deferred:    false,
	}
}

func (l leaveFileTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	tools.TurnCleanupFromContext(ctx).Add("file", func(context.Context) error {
		l.cleaned.Add(1)
		return nil
	})
	return tools.NewResult(ctx, "left a file")
}

func newCleanupAgent(t *testing.T, client llm.Client, cleaned *atomic.Int32) *agent.LlmAgent {
	t.Helper()
	r := tools.NewRegistry()
	r.Register(tools.TextResponse{})
	r.Register(tools.AskUser{})
	r.Register(leaveFileTool{cleaned: cleaned})
	return agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     client,
		LLM:        llm.Config{Model: "test-model", Provider: "test", TotalTimeoutSec: 30},
		Registry:   r,
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "go"}},
	})
}

func TestLlmAgent_TurnCleanupRunsOnceWhenTheTurnEnds(t *testing.T) {
	var cleaned atomic.Int32
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "leave_file", `{}`)),
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c2", "leave_file", `{}`)),
		agenttest.ToolCallTurn(textResponseCall("c3", "done")),
	)

	if _, err := collect(newCleanupAgent(t, fc, &cleaned).Run(newIC(t, agent.BudgetOptions{MaxSteps: new(5)}))); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := cleaned.Load(); got != 1 {
		t.Fatalf("cleanup ran %d times, want once for the one key two calls registered", got)
	}
}

func TestLlmAgent_TurnCleanupRunsWhenTheTurnPausesForTheUser(t *testing.T) {
	var cleaned atomic.Int32
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "leave_file", `{}`)),
		agenttest.ToolCallTurn(askUserCall("c2", `{"question":"deploy now?","kind":"approval","priority":40}`)),
	)

	if _, err := collect(newCleanupAgent(t, fc, &cleaned).Run(newPauseIC(t))); err != nil {
		t.Fatalf("a pause is Event-only; Run: %v", err)
	}
	if got := cleaned.Load(); got != 1 {
		t.Fatalf("cleanup ran %d times across an ask_user pause, want 1", got)
	}
}

// panicOnSecondStream answers the first model call from its script and panics on the
// second: a crash after a tool has already left something in the box.
type panicOnSecondStream struct {
	script *agenttest.FakeClient
	calls  int
}

func (p *panicOnSecondStream) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	p.calls++
	if p.calls == 2 {
		panic("model client crashed")
	}
	return p.script.Stream(ctx, req)
}

func TestLlmAgent_TurnCleanupRunsWhenTheTurnPanics(t *testing.T) {
	var cleaned atomic.Int32
	client := &panicOnSecondStream{script: agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "leave_file", `{}`)),
	)}

	if _, err := collect(newCleanupAgent(t, client, &cleaned).Run(newIC(t, agent.BudgetOptions{MaxSteps: new(5)}))); err == nil {
		t.Fatal("a panicking model client must surface as the Run error")
	}
	if got := cleaned.Load(); got != 1 {
		t.Fatalf("cleanup ran %d times after a panic, want 1", got)
	}
}
```

Create `internal/agent/llm_agent_turn_cleanup_internal_test.go`:

```go
package agent

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
)

func TestRunTurnCleanupOutlivesACancelledTurn(t *testing.T) {
	turnCtx, cancel := context.WithCancel(context.Background())
	ctx, cleanup := tools.WithTurnCleanup(turnCtx)
	stepErr := errors.New("step never ran")
	cleanup.Add("dir", func(ctx context.Context) error { stepErr = ctx.Err(); return nil })
	cancel()

	runTurnCleanup(ctx, "req-1", cleanup)

	if stepErr != nil {
		t.Fatalf("the cleanup step saw %v; a stopped turn still owes the box its cleanup", stepErr)
	}
}

func TestRunTurnCleanupLogsAFailureWithTheRequest(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	ctx, cleanup := tools.WithTurnCleanup(context.Background())
	cleanup.Add("dir", func(context.Context) error {
		return errors.New("remove /workspace/mcp-files/req-9: exit 1")
	})

	runTurnCleanup(ctx, "req-9", cleanup)

	for _, want := range []string{"turn cleanup failed", "request_id=req-9", "/workspace/mcp-files/req-9"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log %q lacks %q", logs.String(), want)
		}
	}
}
```

- [ ] **Step 6: Run them to verify they fail.** Run: `go test ./internal/agent/ -run 'TurnCleanup'`.

Expected: build FAIL with `undefined: runTurnCleanup`. Once that file exists, the three `agent_test` tests fail with `cleanup ran 0 times`, because `Run` installs no collector yet.

- [ ] **Step 7: Implement the drain.** Create `internal/agent/llm_agent_turn_cleanup.go`:

```go
package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// turnCleanupTimeout bounds what the box may take to remove a turn's leftovers,
// normally one rm of one directory. A box that does not answer must not hold the end
// of the turn.
const turnCleanupTimeout = 30 * time.Second

// runTurnCleanup drains the turn's TurnCleanup. Run calls it from its outermost
// defer, on a context the turn's own cancellation cannot abort: a turn the user
// stopped still owes the box its cleanup. A failure is logged and nothing more,
// because the next turn writes into a directory of its own.
func runTurnCleanup(ctx context.Context, requestID string, cleanup *tools.TurnCleanup) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), turnCleanupTimeout)
	defer cancel()
	if err := cleanup.Run(ctx); err != nil {
		slog.Warn("turn cleanup failed", "request_id", requestID, "error", err)
	}
}
```

Re-read `internal/agent/llm_agent.go`, then make two edits in `Run`.

1. Directly after `turnCtx = tools.WithRequestID(turnCtx, requestID)`, add:

```go
	turnCtx, turnCleanup := tools.WithTurnCleanup(turnCtx)
```

2. In the FIRST `defer func() {` of the returned closure (the one that calls `a.hooks.OnTurnEnd`), make this the first statement:

```go
			runTurnCleanup(ic.Ctx, requestID, turnCleanup)
```

It is the outermost defer, so it runs after the panic-recovering one.

- [ ] **Step 8: Run the packages.** Run: `go vet ./internal/agent/ ./internal/agent/tools/`, then `go test -race -count=1 ./internal/agent/ ./internal/agent/tools/`. Expected: `ok` for both. Also check `wc -l internal/agent/llm_agent.go`: it must be 600 or fewer.

- [ ] **Step 9: Commit.**

```bash
cd /d/Aura
git add internal/agent/tools/turn_cleanup.go internal/agent/tools/turn_cleanup_test.go internal/agent/llm_agent_turn_cleanup.go internal/agent/llm_agent_turn_cleanup_test.go internal/agent/llm_agent_turn_cleanup_internal_test.go
git commit -F - -- internal/agent/tools/turn_cleanup.go internal/agent/tools/turn_cleanup_test.go internal/agent/llm_agent_turn_cleanup.go internal/agent/llm_agent_turn_cleanup_test.go internal/agent/llm_agent_turn_cleanup_internal_test.go internal/agent/llm_agent.go <<'EOF'
feat(agent): a turn cleans up what its tools left in the box

A file an MCP result is materialized into belongs to the turn that asked
for it, and nothing removed such a thing. Run now puts a TurnCleanup on the
turn context and drains it from its outermost defer, on a context the
turn's cancellation cannot abort: a normal end, an ask_user pause, an error
and a panic all reach it. It rides the context rather than
hooks.OnTurnEnd because the runner wires that hook for the root agent
only, and a swarm worker's files would leak; each worker is its own Run
and gets its own collector. A failed step is logged with the request id.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 3: `MCPFileSink`: files into the box, named safely, removed with the turn

**Files:**
- Create: `internal/agent/tools/mcp_files.go`, `internal/agent/tools/mcp_files_test.go`
- Modify: `internal/agent/tools/sandbox_route.go` (add `writeBoxFile` and the `io` import)
- Modify: `internal/agent/tools/document_open.go` (delete `(*DocumentOpen).write` and call `writeBoxFile`)

**Interfaces:**
- Consumes:
  - `mcp.FilePart`, `mcp.FileOutcome`, `mcp.FileCapExceeded`, `mcp.MaxFileBytes`, `mcp.MaxCallFileBytes` (Task 1);
  - `TurnCleanupFromContext` (Task 2);
  - `RequestIDFromContext`, `ShellQuoteArg`, `boxWorkspaceRoot`, `truncatePreview` (existing).
- Produces:
  - `tools.MCPFileSink{Router *usersandbox.SandboxRouter}`, with `Materialize(ctx context.Context, server string, parts []mcp.FilePart) []mcp.FileOutcome`;
  - `writeBoxFile(ctx, router, h, boxPath, size, body) error`, shared with `document_open`.

- [ ] **Step 1: Write the failing tests.** Create `internal/agent/tools/mcp_files_test.go`. It reuses `fakeBox` and `routerWith` from `fs_box_fake_router_test.go`.

```go
package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// mcpTurnCtx is a context the way LlmAgent.Run leaves it: a request id and a cleanup.
func mcpTurnCtx(t *testing.T) (context.Context, *TurnCleanup) {
	t.Helper()
	return WithTurnCleanup(WithRequestID(t.Context(), "req-1"))
}

func hexSum(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestMCPFileSinkWritesEachFileWhereTheTurnRemovesIt(t *testing.T) {
	be := &fakeBox{}
	ctx, cleanup := mcpTurnCtx(t)
	pdf, png := []byte("%PDF-1.7"), []byte("\x89PNG")

	out := (&MCPFileSink{Router: routerWith(be)}).Materialize(ctx, "aura-pim", []mcp.FilePart{
		{Name: "Fattura settembre è.pdf", MIMEType: "application/pdf", Data: pdf},
		{MIMEType: "image/png", Data: png},
	})

	want := []mcp.FileOutcome{
		{Path: "/workspace/mcp-files/req-1/aura-pim/Fattura settembre è.pdf", Name: "Fattura settembre è.pdf", MIMEType: "application/pdf", SizeBytes: 8, SHA256: hexSum(pdf)},
		{Path: "/workspace/mcp-files/req-1/aura-pim/file.png", Name: "file.png", MIMEType: "image/png", SizeBytes: 4, SHA256: hexSum(png)},
	}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("outcomes = %+v\nwant %+v", out, want)
	}
	if be.written[want[1].Path] != string(png) {
		t.Fatalf("written = %v", be.written)
	}
	if err := cleanup.Run(context.Background()); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if last := be.execs[len(be.execs)-1].Command; last != "rm -rf -- '/workspace/mcp-files/req-1'" {
		t.Fatalf("turn cleanup ran %q", last)
	}
}

func TestMCPFileSinkNeverOverwritesAFileOfTheSameName(t *testing.T) {
	be := &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		if strings.HasPrefix(cmd, "ls ") {
			return usersandbox.ExecResult{Stdout: []byte("report.pdf\nreport-2.pdf\n")}
		}
		return usersandbox.ExecResult{}
	}}
	ctx, _ := mcpTurnCtx(t)

	out := (&MCPFileSink{Router: routerWith(be)}).Materialize(ctx, "aura-pim", []mcp.FilePart{
		{Name: "report.pdf", MIMEType: "application/pdf", Data: []byte("a")},
		{Name: "image001.png", MIMEType: "image/png", Data: []byte("b")},
		{Name: "image001.png", MIMEType: "image/png", Data: []byte("c")},
	})

	got := []string{out[0].Name, out[1].Name, out[2].Name}
	if want := []string{"report-3.pdf", "image001.png", "image001-2.png"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
}

func TestMCPFileNameIsOneSafeComponent(t *testing.T) {
	cases := []struct{ name, mime, want string }{
		{"Relazione finale è.docx", "", "Relazione finale è.docx"},
		{"../../.bashrc", "", "bashrc"},
		{`C:\fakepath\Preventivo.xlsx`, "", "Preventivo.xlsx"},
		{"a\x00b\nc.txt", "", "abc.txt"},
		{"..", "image/jpeg", "file.jpg"},
		{"", "image/png", "file.png"},
		{"scan", "application/pdf", "scan.pdf"},
		{"note", "text/plain; charset=utf-8", "note.txt"},
		{"blob", "application/x-unknown", "blob"},
	}
	for _, c := range cases {
		got := mcpFileName(c.name, c.mime)
		if got != c.want {
			t.Errorf("mcpFileName(%q, %q) = %q, want %q", c.name, c.mime, got, c.want)
		}
		if err := documents.ValidateStagedFileName(got); err != nil {
			t.Errorf("mcpFileName(%q) = %q, which the document_open rule refuses: %v", c.name, got, err)
		}
	}
	long := mcpFileName(strings.Repeat("à", 150)+".pdf", "application/pdf")
	if len(long) > maxMCPFileNameBytes || !strings.HasSuffix(long, ".pdf") || !utf8.ValidString(long) {
		t.Fatalf("a 304-byte name became %d bytes %q", len(long), long)
	}
}

func TestMCPPathSegment(t *testing.T) {
	for in, want := range map[string]string{"aura-pim": "aura-pim", "../evil": "_evil", "": "_", "è": "_"} {
		if got := mcpPathSegment(in); got != want {
			t.Errorf("mcpPathSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMCPFileSinkRefusesAFileOverTheCapAndKeepsTheRest(t *testing.T) {
	be := &fakeBox{}
	ctx, _ := mcpTurnCtx(t)

	out := (&MCPFileSink{Router: routerWith(be)}).Materialize(ctx, "s", []mcp.FilePart{
		{Name: "huge.bin", Data: make([]byte, mcp.MaxFileBytes+1)},
		{Name: "small.txt", MIMEType: "text/plain", Data: []byte("ok")},
	})

	if out[0].NotMaterialized != "26214401 bytes exceeds the 26214400-byte file cap" || out[0].Path != "" {
		t.Fatalf("oversized outcome = %+v", out[0])
	}
	if out[1].Path != "/workspace/mcp-files/req-1/s/small.txt" {
		t.Fatalf("the small file must still be written: %+v", out[1])
	}
}

func TestMCPFileSinkRefusesACallOverTheCapWithoutTouchingTheBox(t *testing.T) {
	be := &fakeBox{}
	ctx, cleanup := mcpTurnCtx(t)
	twenty := make([]byte, 20<<20)

	out := (&MCPFileSink{Router: routerWith(be)}).Materialize(ctx, "s", []mcp.FilePart{
		{Name: "a", Data: twenty}, {Name: "b", Data: twenty}, {Name: "c", Data: twenty},
	})

	for _, o := range out {
		if o.NotMaterialized != "the call's files exceed the 52428800-byte cap" {
			t.Fatalf("outcome = %+v", o)
		}
	}
	if err := cleanup.Run(context.Background()); err != nil || len(be.execs) != 0 || len(be.written) != 0 {
		t.Fatalf("a refused call touched the box: execs=%v written=%v err=%v", be.execs, be.written, err)
	}
}

func TestMCPFileSinkWithoutATurnWritesNothing(t *testing.T) {
	be := &fakeBox{}
	sink := &MCPFileSink{Router: routerWith(be)}
	for _, ctx := range []context.Context{t.Context(), WithRequestID(t.Context(), "req-1")} {
		out := sink.Materialize(ctx, "s", []mcp.FilePart{{Name: "a.txt", Data: []byte("x")}})
		if out[0].NotMaterialized != "no agent turn owns the file" {
			t.Fatalf("outcome = %+v", out[0])
		}
	}
	if len(be.execs) != 0 || len(be.written) != 0 {
		t.Fatalf("no turn, yet the box was touched: %v %v", be.execs, be.written)
	}
}

func TestMCPFileSinkDeniesWhenTheBoxIsUnreachable(t *testing.T) {
	be := &fakeBox{resolveE: errors.New("daemon down")}
	ctx, cleanup := mcpTurnCtx(t)

	out := (&MCPFileSink{Router: routerWith(be)}).Materialize(ctx, "s", []mcp.FilePart{{Name: "a.txt", Data: []byte("x")}})

	if !strings.HasPrefix(out[0].NotMaterialized, "sandbox unavailable: ") || !strings.Contains(out[0].NotMaterialized, "daemon down") {
		t.Fatalf("outcome = %+v", out[0])
	}
	if err := cleanup.Run(context.Background()); err != nil || len(be.execs) != 0 {
		t.Fatalf("nothing was written, so nothing is removed: %v %v", be.execs, err)
	}
}

func TestMCPFileSinkRemovesAPartialFileAndSaysTheWriteFailed(t *testing.T) {
	be := &fakeBox{writeE: errors.New("disk full")}
	ctx, _ := mcpTurnCtx(t)

	out := (&MCPFileSink{Router: routerWith(be)}).Materialize(ctx, "s", []mcp.FilePart{{Name: "a.pdf", Data: []byte("x")}})

	if !strings.HasPrefix(out[0].NotMaterialized, "write failed: ") || !strings.Contains(out[0].NotMaterialized, "disk full") {
		t.Fatalf("outcome = %+v", out[0])
	}
	if last := be.execs[len(be.execs)-1].Command; last != "rm -f -- '/workspace/mcp-files/req-1/s/a.pdf'" {
		t.Fatalf("partial file not removed; last exec %q", last)
	}
}

func TestMCPFileSinkPassesAnUnreadableLinkThroughWithoutTouchingTheBox(t *testing.T) {
	be := &fakeBox{}
	ctx, _ := mcpTurnCtx(t)

	out := (&MCPFileSink{Router: routerWith(be)}).Materialize(ctx, "s", []mcp.FilePart{{Name: "old.pdf", Unavailable: "read failed: attachment expired"}})

	if out[0].NotMaterialized != "read failed: attachment expired" || len(be.execs) != 0 {
		t.Fatalf("outcome = %+v, execs = %v", out[0], be.execs)
	}
}

func TestMCPFileSinkCleanupReportsAFailedRemoval(t *testing.T) {
	be := &fakeBox{respond: func(cmd string) usersandbox.ExecResult {
		if strings.HasPrefix(cmd, "rm -rf") {
			return usersandbox.ExecResult{ExitCode: 1, Stderr: []byte("busy")}
		}
		return usersandbox.ExecResult{}
	}}
	ctx, cleanup := mcpTurnCtx(t)
	(&MCPFileSink{Router: routerWith(be)}).Materialize(ctx, "s", []mcp.FilePart{{Name: "a.txt", Data: []byte("x")}})

	err := cleanup.Run(context.Background())

	if err == nil || !strings.Contains(err.Error(), "/workspace/mcp-files/req-1") || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("cleanup error = %v, want the directory and the box's reason", err)
	}
}
```

- [ ] **Step 2: Run them to verify they fail.** Run: `go test ./internal/agent/tools/ -run 'MCPFile|MCPPath'`. Expected: build FAIL with `undefined: MCPFileSink`.

- [ ] **Step 3: Share the box write.** In `internal/agent/tools/sandbox_route.go`, add `"io"` to the imports after `"fmt"`. Then append:

```go

// writeBoxFile streams size bytes of body into boxPath inside the box h names,
// buffering nothing on the way. A failed copy takes its partial file with it: the
// daemon extracts the tar as it reads it, so a source that dies mid-stream leaves a
// SHORT file behind, and a truncated file that looks like a whole one is worse than
// no file at all -- the agent would compute a confident wrong answer from it.
func writeBoxFile(ctx context.Context, router *usersandbox.SandboxRouter, h usersandbox.BoxHandle, boxPath string, size int64, body io.Reader) error {
	if err := router.WriteFileStream(ctx, h, boxPath, size, body); err != nil {
		_, _ = router.Exec(ctx, h, usersandbox.ExecRequest{Command: "rm -f -- " + ShellQuoteArg(boxPath)})
		return fmt.Errorf("write %s: %w", boxPath, err)
	}
	return nil
}
```

In `internal/agent/tools/document_open.go`:
- delete the whole `(*DocumentOpen).write` method and its doc comment;
- replace `if err := t.write(ctx, handle, boxPath, meta.SizeBytes, body); err != nil {` with `if err := writeBoxFile(ctx, t.Router, handle, boxPath, meta.SizeBytes, body); err != nil {`.

Then, in the comment above that call, add after its first sentence: `Nothing is buffered: an indexed document may be up to the 100 MiB ingest ceiling, and the aura container has 768 MiB in total.` This keeps the one sentence of the deleted comment that was specific to documents.

- [ ] **Step 4: Implement the sink.** Create `internal/agent/tools/mcp_files.go`:

```go
package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	pathpkg "path"
	"strings"
	"unicode"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// MCPFileSink puts the files an MCP tool result carried into the caller's box, at
// /workspace/mcp-files/<request-id>/<server>/<name>, and asks the turn to remove
// /workspace/mcp-files/<request-id> when it ends. It writes through the router seam
// document_open uses, for the reason document_open gives: a path the agent is handed
// must be one shell_exec and fs_read can open.
//
// It lives here, not in mcptools, so the MCP bridge never imports the sandbox; the
// bridge sees it as mcptools.FileSink.
type MCPFileSink struct {
	Router *usersandbox.SandboxRouter
}

const (
	mcpFilesBoxDir = boxWorkspaceRoot + "/mcp-files"
	// maxMCPFileNameBytes keeps a server's file name well under the 255-byte limit of
	// every filesystem the box mounts, with room left for a -N suffix.
	maxMCPFileNameBytes = 200
	// maxMCPExtensionBytes is the longest suffix still kept as an extension when a
	// long name is cut; anything longer is part of the name.
	maxMCPExtensionBytes = 16
)

// mcpFileExtensions gives a nameless or extensionless file an extension from its
// MIME type. It lists the types mail and chat actually carry; any other type gets no
// extension rather than a guess.
var mcpFileExtensions = map[string]string{
	"application/pdf": ".pdf",
	"audio/mp4":       ".m4a",
	"audio/mpeg":      ".mp3",
	"audio/ogg":       ".ogg",
	"image/gif":       ".gif",
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/webp":      ".webp",
	"text/plain":      ".txt",
	"video/mp4":       ".mp4",
}

// Materialize writes every part it can and reports each part's outcome, in order.
// A part it cannot write is reported with the reason, and the others still go in.
func (s *MCPFileSink) Materialize(ctx context.Context, server string, parts []mcp.FilePart) []mcp.FileOutcome {
	out := make([]mcp.FileOutcome, len(parts))
	pending := make([]int, 0, len(parts))
	total := 0
	for i, part := range parts {
		switch {
		case part.Unavailable != "":
			out[i] = part.NotMaterialized("")
		case len(part.Data) > mcp.MaxFileBytes:
			out[i] = part.NotMaterialized(mcp.FileCapExceeded(int64(len(part.Data))))
		default:
			pending = append(pending, i)
			total += len(part.Data)
		}
	}
	if len(pending) == 0 {
		return out
	}
	w, reason := s.open(ctx, server, total)
	for _, i := range pending {
		if reason != "" {
			out[i] = parts[i].NotMaterialized(reason)
			continue
		}
		out[i] = w.write(ctx, parts[i])
	}
	return out
}

// open routes to the caller's box and prepares the call's directory. The turn owns
// it, so it is registered for removal before anything is written into it.
func (s *MCPFileSink) open(ctx context.Context, server string, total int) (*mcpFileWriter, string) {
	cleanup := TurnCleanupFromContext(ctx)
	requestID := RequestIDFromContext(ctx)
	if cleanup == nil || requestID == "" {
		// Nothing would ever remove the file: toolpipe, the docs MCP and a readiness
		// probe get the text alone.
		return nil, "no agent turn owns the file"
	}
	if total > mcp.MaxCallFileBytes {
		return nil, fmt.Sprintf("the call's files exceed the %d-byte cap", mcp.MaxCallFileBytes)
	}
	handle, err := s.Router.Route(ctx)
	if err != nil {
		return nil, "sandbox unavailable: " + err.Error()
	}
	turnDir := pathpkg.Join(mcpFilesBoxDir, mcpPathSegment(requestID))
	cleanup.Add(handle.ContainerID+":"+turnDir, func(ctx context.Context) error {
		return removeBoxDir(ctx, s.Router, handle, turnDir)
	})
	dir := pathpkg.Join(turnDir, mcpPathSegment(server))
	taken, err := s.names(ctx, handle, dir)
	if err != nil {
		return nil, "sandbox unavailable: " + err.Error()
	}
	return &mcpFileWriter{router: s.Router, handle: handle, dir: dir, taken: taken}, ""
}

// names lists what dir already holds, so a second file of the same name in the same
// turn gets a -2 instead of overwriting the first. A dir that does not exist yet is
// empty.
func (s *MCPFileSink) names(ctx context.Context, h usersandbox.BoxHandle, dir string) (map[string]bool, error) {
	res, err := s.Router.Exec(ctx, h, usersandbox.ExecRequest{Command: "ls -1A -- " + ShellQuoteArg(dir) + " 2>/dev/null"})
	if err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	for name := range strings.SplitSeq(string(res.Stdout), "\n") {
		if name != "" {
			taken[name] = true
		}
	}
	return taken, nil
}

func removeBoxDir(ctx context.Context, router *usersandbox.SandboxRouter, h usersandbox.BoxHandle, dir string) error {
	res, err := router.Exec(ctx, h, usersandbox.ExecRequest{Command: "rm -rf -- " + ShellQuoteArg(dir)})
	if err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("remove %s: exit %d: %s", dir, res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

// mcpFileWriter writes one call's files into one directory of one box.
type mcpFileWriter struct {
	router *usersandbox.SandboxRouter
	handle usersandbox.BoxHandle
	dir    string
	taken  map[string]bool
}

func (w *mcpFileWriter) write(ctx context.Context, part mcp.FilePart) mcp.FileOutcome {
	name := uniqueMCPFileName(mcpFileName(part.Name, part.MIMEType), w.taken)
	boxPath := pathpkg.Join(w.dir, name)
	if err := writeBoxFile(ctx, w.router, w.handle, boxPath, int64(len(part.Data)), bytes.NewReader(part.Data)); err != nil {
		return part.NotMaterialized("write failed: " + err.Error())
	}
	w.taken[name] = true
	sum := sha256.Sum256(part.Data)
	return mcp.FileOutcome{
		Path:      boxPath,
		Name:      name,
		MIMEType:  part.MIMEType,
		SizeBytes: int64(len(part.Data)),
		SHA256:    hex.EncodeToString(sum[:]),
	}
}

// mcpFileName makes a server's file name one path component the box can hold, the
// rule document_open applies to its file names (documents.ValidateStagedFileName):
// the last segment of whatever path it names, without leading dots or control
// characters, at most maxMCPFileNameBytes, and with an extension from its MIME type
// when it has none. Spaces and accents stay: they are part of the name the human
// knows the file by.
func mcpFileName(name, mimeType string) string {
	name = name[strings.LastIndexAny(name, `/\`)+1:]
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimLeft(strings.TrimSpace(name), ".")
	if name == "" {
		name = "file"
	}
	if pathpkg.Ext(name) == "" {
		base, _, _ := strings.Cut(mimeType, ";")
		name += mcpFileExtensions[strings.ToLower(strings.TrimSpace(base))]
	}
	if len(name) > maxMCPFileNameBytes {
		ext := pathpkg.Ext(name)
		if len(ext) > maxMCPExtensionBytes {
			ext = ""
		}
		name = truncatePreview(strings.TrimSuffix(name, ext), maxMCPFileNameBytes-len(ext)) + ext
	}
	return name
}

// uniqueMCPFileName is name, or name with the first free -N before its extension.
func uniqueMCPFileName(name string, taken map[string]bool) string {
	if !taken[name] {
		return name
	}
	ext := pathpkg.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 2; ; n++ {
		if candidate := fmt.Sprintf("%s-%d%s", stem, n, ext); !taken[candidate] {
			return candidate
		}
	}
}

// mcpPathSegment reduces a host-side name -- a mounted server's, a request id -- to
// one directory name: ASCII letters, digits, '-', '_' and '.', no leading dot.
func mcpPathSegment(s string) string {
	seg := strings.Map(func(r rune) rune {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("-_.", r)) {
			return r
		}
		return '_'
	}, s)
	if seg = strings.TrimLeft(seg, "."); seg == "" {
		return "_"
	}
	return seg
}
```

- [ ] **Step 5: Run the package.** Run: `go vet ./internal/agent/tools/`, then `go test -race -count=1 ./internal/agent/tools/`. Expected: `ok`, with the `document_open` tests still green on the shared `writeBoxFile`.

- [ ] **Step 6: Commit.**

```bash
cd /d/Aura
git add internal/agent/tools/mcp_files.go internal/agent/tools/mcp_files_test.go
git commit -F - -- internal/agent/tools/mcp_files.go internal/agent/tools/mcp_files_test.go internal/agent/tools/sandbox_route.go internal/agent/tools/document_open.go <<'EOF'
feat(tools): MCPFileSink writes an MCP result's files into the box

A file an MCP server returns is only useful where the agent's commands
run. MCPFileSink writes each one to
/workspace/mcp-files/<request-id>/<server>/<name> through the same router
seam document_open uses, and registers the request directory with the
turn's cleanup. Server names become one safe component (last segment, no
leading dot or control character, 200 bytes, an extension from the MIME
type when missing) and never overwrite: the box listing and the call's own
names both count. A file over 25 MiB, a call over 50 MiB, a caller with no
turn and an unreachable box each leave the text alone with the reason.

document_open's stream-then-remove-the-partial-file write moves into
writeBoxFile so both callers share it.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 4: The bridge reads links, materializes files, and reports paths

**Files:**
- Create: `internal/agent/mcptools/bridge_links.go`, `internal/agent/mcptools/bridge_files.go`, `internal/agent/mcptools/bridge_files_test.go`
- Modify: `internal/agent/mcptools/bridge_supervisor.go`:
  - add the `files` field;
  - change `decodeResult`'s signature;
  - update its two call sites.
- Modify: `internal/agent/mcptools/bridge_call.go` (`newResult` and a new `resultText`)
- Modify: `internal/agent/mcptools/mount.go`:
  - add `MountOptions.Files`;
  - change `MountServer`'s signature;
  - delete `mountStdio` and `mountStdioWithPolicy`;
  - change `openAttachAndMount` and `openIdentityScopedHTTPMount` to take `opts`.
- Modify: `internal/agent/mcptools/mount_test.go:51`
- Test: `internal/agent/mcptools/managed_mount_test.go` (append one test)

**Interfaces:**
- Consumes:
  - `mcp.FilePart`, `mcp.FileOutcome`, `mcp.FileFromContents`, `mcp.NameFromURI`, `mcp.FileCapExceeded`, `mcp.OctetStream`, `mcp.MaxFileBytes`, `ToolPayload.Files`, `ToolPayload.Links` (Task 1);
  - `tools.NewResultReservingTail` (existing).
- Produces:
  - `mcptools.FileSink`, an interface with `Materialize(ctx, server string, parts []mcp.FilePart) []mcp.FileOutcome`;
  - `MountOptions.Files FileSink`;
  - `MountServer(processCtx, handshakeCtx, reg, name, cfg, opts MountOptions)`.

- [ ] **Step 1: Write the failing tests.** Create `internal/agent/mcptools/bridge_files_test.go`:

```go
package mcptools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

// recordingSink stands in for tools.MCPFileSink: it records what it was handed and
// reports each available part as written under one fixed directory.
type recordingSink struct {
	server string
	parts  []mcp.FilePart
}

func (s *recordingSink) Materialize(_ context.Context, server string, parts []mcp.FilePart) []mcp.FileOutcome {
	s.server, s.parts = server, parts
	out := make([]mcp.FileOutcome, len(parts))
	for i, part := range parts {
		if part.Unavailable != "" {
			out[i] = part.NotMaterialized("")
			continue
		}
		out[i] = mcp.FileOutcome{
			Path: "/workspace/mcp-files/req-1/fixture/" + part.Name, Name: part.Name,
			MIMEType: part.MIMEType, SizeBytes: int64(len(part.Data)),
		}
	}
	return out
}

const fixtureFileTemplate = "fixture://files/{name}"

// mountReturning mounts a fixture whose one tool, fetch, answers with content. The
// tool is declared at mount and its handler replaced after, so the advertised tool
// set never drifts.
func mountReturning(t *testing.T, content ...sdkmcp.Content) (*MountedServer, *sdkmcp.Server) {
	t.Helper()
	tool := mustTool("fetch", "", nil, nil)
	srv, server := newInMemoryMounted(t, tool)
	server.AddTool(tool, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return &sdkmcp.CallToolResult{Content: content}, nil
	})
	return srv, server
}

// serveFiles answers every read of fixture://files/{name} with body typed
// contentsMIME, and counts the reads.
func serveFiles(server *sdkmcp.Server, contentsMIME string, body []byte, reads *atomic.Int32) {
	server.AddResourceTemplate(
		&sdkmcp.ResourceTemplate{URITemplate: fixtureFileTemplate, Name: "files", MIMEType: contentsMIME},
		func(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			reads.Add(1)
			return &sdkmcp.ReadResourceResult{Contents: []*sdkmcp.ResourceContents{
				{URI: req.Params.URI, MIMEType: contentsMIME, Blob: body},
			}}, nil
		})
}

func sizeOf(n int64) *int64 { return &n }

// executeFetch runs fetch the way the agent does, with previewCap as the preview cap.
func executeFetch(t *testing.T, srv *MountedServer, previewCap int) tools.ToolResult {
	t.Helper()
	bridged := bridgeToolsWithPolicy("fixture", srv, []*sdkmcp.Tool{mustTool("fetch", "", nil, nil)}, 0, defaultBridgePolicy("fixture"))
	ctx := tools.WithToolCallContext(t.Context(), "sess", "tc1", t.TempDir(), previewCap)
	res, err := bridged[0].Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return res
}

func TestCallToolReadsALinkBackOnTheCallingSession(t *testing.T) {
	srv, server := mountReturning(t,
		&sdkmcp.TextContent{Text: `{"attachmentId":"abc"}`},
		&sdkmcp.ResourceLink{URI: "fixture://files/abc", Name: "invoice.pdf", MIMEType: "application/pdf", Size: sizeOf(8)},
	)
	var reads atomic.Int32
	serveFiles(server, mcp.OctetStream, []byte("%PDF-1.7"), &reads)

	payload, err := srv.CallTool(t.Context(), "fetch", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	want := []mcp.FilePart{{Name: "invoice.pdf", MIMEType: "application/pdf", Data: []byte("%PDF-1.7")}}
	if !reflect.DeepEqual(payload.Files, want) {
		t.Fatalf("Files = %#v, want %#v", payload.Files, want)
	}
	if payload.Links != nil || reads.Load() != 1 {
		t.Fatalf("links left %v, reads %d; want resolved, read once", payload.Links, reads.Load())
	}
}

func TestCallToolNeverReadsALinkOverTheCap(t *testing.T) {
	srv, server := mountReturning(t, &sdkmcp.ResourceLink{URI: "fixture://files/big", Name: "big.zip", Size: sizeOf(mcp.MaxFileBytes + 1)})
	var reads atomic.Int32
	serveFiles(server, "application/zip", []byte("PK"), &reads)

	payload, err := srv.CallTool(t.Context(), "fetch", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if len(payload.Files) != 1 || payload.Files[0].Unavailable != mcp.FileCapExceeded(mcp.MaxFileBytes+1) || reads.Load() != 0 {
		t.Fatalf("Files = %+v, reads %d; want refused unread", payload.Files, reads.Load())
	}
}

func TestCallToolReportsAFailedLinkReadAsTheFileNotTheCall(t *testing.T) {
	srv, server := mountReturning(t,
		&sdkmcp.TextContent{Text: "still useful"},
		&sdkmcp.ResourceLink{URI: "fixture://files/gone", Name: "gone.pdf"},
	)
	server.AddResourceTemplate(
		&sdkmcp.ResourceTemplate{URITemplate: fixtureFileTemplate, Name: "files"},
		func(_ context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return nil, sdkmcp.ResourceNotFoundError(req.Params.URI)
		})

	payload, err := srv.CallTool(t.Context(), "fetch", map[string]any{})
	if err != nil {
		t.Fatalf("a failed link read must not fail the call: %v", err)
	}

	if payload.Text != "still useful" || len(payload.Files) != 1 || !strings.HasPrefix(payload.Files[0].Unavailable, "read failed: ") {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestPreferredMIMETakesTheMoreSpecificType(t *testing.T) {
	cases := []struct{ contents, link, want string }{
		{"application/pdf", "image/png", "application/pdf"},
		{mcp.OctetStream, "image/png", "image/png"},
		{"", "image/png", "image/png"},
		{"", "", mcp.OctetStream},
		{mcp.OctetStream, "", mcp.OctetStream},
	}
	for _, c := range cases {
		if got := preferredMIME(c.contents, c.link); got != c.want {
			t.Errorf("preferredMIME(%q, %q) = %q, want %q", c.contents, c.link, got, c.want)
		}
	}
}

func TestLinkFileRefusesOversizedAndEmptyReads(t *testing.T) {
	link := &sdkmcp.ResourceLink{URI: "fixture://files/abc", MIMEType: "image/jpeg"}
	big := &sdkmcp.ReadResourceResult{Contents: []*sdkmcp.ResourceContents{{URI: link.URI, Blob: make([]byte, mcp.MaxFileBytes+1)}}}
	if got := linkFile(link, big); got.Unavailable != mcp.FileCapExceeded(mcp.MaxFileBytes+1) || got.Data != nil {
		t.Fatalf("oversized read = %+v", got)
	}
	for _, empty := range []*sdkmcp.ReadResourceResult{nil, {}} {
		if got := linkFile(link, empty); got.Unavailable != "the server returned no contents" {
			t.Fatalf("empty read = %+v", got)
		}
	}
	text := &sdkmcp.ReadResourceResult{Contents: []*sdkmcp.ResourceContents{{URI: link.URI, Text: "hi"}}}
	if got := linkFile(link, text); got.Name != "abc" || string(got.Data) != "hi" || got.MIMEType != "image/jpeg" {
		t.Fatalf("a nameless link takes the URI's last segment: %+v", got)
	}
}

func TestBridgedToolReportsFilePathsAndNeverTheBytes(t *testing.T) {
	secret := []byte("attachment-bytes-that-must-not-reach-the-model")
	srv, server := mountReturning(t,
		&sdkmcp.TextContent{Text: "one image, one link"},
		&sdkmcp.ImageContent{Data: secret, MIMEType: "image/png"},
		&sdkmcp.ResourceLink{URI: "fixture://files/abc", Name: "invoice.pdf"},
	)
	var reads atomic.Int32
	serveFiles(server, "application/pdf", secret, &reads)
	sink := &recordingSink{}
	srv.files = sink

	res := executeFetch(t, srv, 2048)

	if sink.server != "fixture" || len(sink.parts) != 2 {
		t.Fatalf("sink got server %q, %d parts; want fixture, 2", sink.server, len(sink.parts))
	}
	for _, leak := range []string{string(secret), base64.StdEncoding.EncodeToString(secret)} {
		if strings.Contains(res.Preview, leak) {
			t.Fatalf("the model saw the file's bytes: %q", res.Preview)
		}
	}
	for _, want := range []string{"one image, one link\n\n{\"files\":[", `"path":"/workspace/mcp-files/req-1/fixture/invoice.pdf"`, filesNote} {
		if !strings.Contains(res.Preview, want) {
			t.Fatalf("preview %q lacks %q", res.Preview, want)
		}
	}
}

func TestFilesFooterSurvivesASmallPreviewCap(t *testing.T) {
	srv, _ := mountReturning(t,
		&sdkmcp.TextContent{Text: strings.Repeat("email body line\n", 400)},
		&sdkmcp.ImageContent{Data: []byte("png"), MIMEType: "image/png"},
	)
	srv.files = &recordingSink{}

	res := executeFetch(t, srv, 512)

	if !res.Truncated || !strings.Contains(res.Preview, `"path":"/workspace/mcp-files/req-1/fixture/"`) {
		t.Fatalf("a long text truncated the path away: %q", res.Preview)
	}
}

func TestAMountWithoutAFileSinkSaysSoAndKeepsTheText(t *testing.T) {
	srv, _ := mountReturning(t,
		&sdkmcp.TextContent{Text: "hi"},
		&sdkmcp.ImageContent{Data: []byte("png"), MIMEType: "image/png"},
	)

	res := executeFetch(t, srv, 2048)

	if !strings.HasPrefix(res.Preview, "hi\n\n") || !strings.Contains(res.Preview, `"not_materialized":"this host has no workspace for MCP files"`) {
		t.Fatalf("preview = %q", res.Preview)
	}
	if strings.Contains(res.Preview, `"note"`) {
		t.Fatalf("no file was written, so there is nothing to delete: %q", res.Preview)
	}
}

func TestALinkOnlyResultStillReachesTheModel(t *testing.T) {
	srv, server := mountReturning(t, &sdkmcp.ResourceLink{URI: "fixture://files/abc", Name: "photo.jpg", MIMEType: "image/jpeg"})
	var reads atomic.Int32
	serveFiles(server, mcp.OctetStream, []byte("\xff\xd8\xff"), &reads)
	srv.files = &recordingSink{}

	res := executeFetch(t, srv, 2048)

	if !strings.HasPrefix(res.Preview, `{"files":[{"path":"/workspace/mcp-files/req-1/fixture/photo.jpg"`) {
		t.Fatalf("preview = %q", res.Preview)
	}
}
```

Append to `internal/agent/mcptools/managed_mount_test.go`:

```go
func TestMountManagedServerCarriesTheFileSink(t *testing.T) {
	httpSrv, _ := startSDKHTTPFixture(t, managedTools()...)
	server := unprotectedHTTPFixtureServer(httpSrv.URL)
	server.Type = mcp.ServerTypeStreamableHTTP
	sink := &recordingSink{}

	closer, _, host, err := MountManagedServerWithOptions(context.Background(), context.Background(), tools.NewRegistry(), "docs", server,
		MountOptions{Egress: mcp.RuntimeEgressPolicy(false, server), Files: sink})
	if err != nil {
		t.Fatalf("MountManagedServerWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = closer() })

	if host.files != sink {
		t.Fatalf("mounted host files = %v, want the sink the mount was given", host.files)
	}
}
```

In `internal/agent/mcptools/mount_test.go`, change the call at line 51 to:

```go
	closer, names, err := MountServer(context.Background(), context.Background(), reg, "bad",
		mcp.ServerConfig{Command: "aura-nonexistent-mcp-binary-xyz"}, MountOptions{})
```

- [ ] **Step 2: Run them to verify they fail.** Run: `go test ./internal/agent/mcptools/`.

Expected: build FAIL with `srv.files undefined`, `undefined: preferredMIME` and `too many arguments in call to MountServer`.

- [ ] **Step 3: Implement link resolution.** Create `internal/agent/mcptools/bridge_links.go`:

```go
package mcptools

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/mcp"
)

// bridge_links.go reads back the files a tool result linked instead of carrying. A
// resource_link names a resource on the server that returned it, and the one way Aura
// reads it is resources/read on the SAME session that made the call -- an identity
// child's session for an identity-pooled server, so a tenant-scoped resource is read
// as the tenant that asked. Aura never fetches the URI itself, so a hostile link
// cannot make it reach anything on the network.

// resolveLinks turns every link in payload into a FilePart and clears Links.
func resolveLinks(ctx context.Context, session *sdkmcp.ClientSession, payload mcp.ToolPayload) mcp.ToolPayload {
	for _, link := range payload.Links {
		payload.Files = append(payload.Files, readLink(ctx, session, link))
	}
	payload.Links = nil
	return payload
}

// readLink reads one link, unless it declares a size over the cap: that one is never
// fetched. A failure becomes the part's reason, not the call's error, because the
// text of the result is still good.
func readLink(ctx context.Context, session *sdkmcp.ClientSession, link *sdkmcp.ResourceLink) mcp.FilePart {
	if link.Size != nil && *link.Size > mcp.MaxFileBytes {
		return linkPart(link, mcp.FileCapExceeded(*link.Size))
	}
	result, err := mcp.BoundedCall(ctx, func(ctx context.Context) (*sdkmcp.ReadResourceResult, error) {
		return session.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: link.URI})
	}, nil)
	if err != nil {
		return linkPart(link, "read failed: "+err.Error())
	}
	return linkFile(link, result)
}

// linkFile is the file a link's read returned: the first contents that carry bytes,
// named and typed by the link wherever the contents say less.
func linkFile(link *sdkmcp.ResourceLink, result *sdkmcp.ReadResourceResult) mcp.FilePart {
	if result != nil {
		for _, contents := range result.Contents {
			file, ok := mcp.FileFromContents(contents)
			if !ok {
				continue
			}
			if len(file.Data) > mcp.MaxFileBytes {
				return linkPart(link, mcp.FileCapExceeded(int64(len(file.Data))))
			}
			part := linkPart(link, "")
			part.MIMEType = preferredMIME(file.MIMEType, link.MIMEType)
			part.Data = file.Data
			return part
		}
	}
	return linkPart(link, "the server returned no contents")
}

// linkPart is a link's FilePart before its bytes arrive: the link's own name, or
// failing that the URI's last segment.
func linkPart(link *sdkmcp.ResourceLink, unavailable string) mcp.FilePart {
	name := link.Name
	if name == "" {
		name = mcp.NameFromURI(link.URI)
	}
	return mcp.FilePart{Name: name, MIMEType: link.MIMEType, Unavailable: unavailable}
}

// preferredMIME picks the more specific of what the contents and the link say. The
// contents win unless they say nothing: the Python SDK fixes one MIME type for every
// read of a resource template, so the link is where such a server states the real one.
func preferredMIME(contents, link string) string {
	for _, candidate := range []string{contents, link} {
		if candidate != "" && candidate != mcp.OctetStream {
			return candidate
		}
	}
	return mcp.OctetStream
}
```

In `internal/agent/mcptools/bridge_supervisor.go`:

1. In `MountedServer`, directly after the `identityPool *identitySessionPool` field, add:

```go
	// files materializes the files a tool result carries (bridge_files.go). Set once
	// at mount, before any call; nil on a host with no workspace.
	files FileSink
```

2. Replace `decodeResult` with:

```go
// decodeResult is the ONE result-decode call site in the tree (RESEARCH Pitfall
// 1): bridgedTool.Execute, the two cmd/aura host-memory callers and the
// readiness check all route through CallTool, so the domain-outcome chain
// cannot be bypassed by adding a caller. A successful result's links are read back
// on session, the one that made the call (bridge_links.go).
func (s *MountedServer) decodeResult(ctx context.Context, session *sdkmcp.ClientSession, name string, res *sdkmcp.CallToolResult) (mcp.ToolPayload, error) {
	payload, isErr := mcp.DecodeToolPayload(res)
	if isErr {
		return mcp.ToolPayload{}, mcp.DecodeToolCallError(s.name, name, payload.Text)
	}
	return resolveLinks(ctx, session, payload), nil
}
```

3. In `CallTool`, change `return s.decodeResult(name, res)` inside `if callErr == nil {` to `return s.decodeResult(ctx, session, name, res)`, and change the final `return s.decodeResult(name, res)` to `return s.decodeResult(ctx, retry, name, res)`.

- [ ] **Step 4: Implement the footer.** Create `internal/agent/mcptools/bridge_files.go`:

```go
package mcptools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chetto1983/aura/internal/mcp"
)

// FileSink writes the files a tool result carried where the agent can open them.
// tools.MCPFileSink is the implementation; it lives beside document_open, so this
// package never imports the sandbox.
type FileSink interface {
	Materialize(ctx context.Context, server string, parts []mcp.FilePart) []mcp.FileOutcome
}

// filesNote tells the model what to do with the files; the turn deletes whatever it
// leaves behind.
const filesNote = "Delete each file (rm) once you are done with it. Whatever is left is deleted when this turn ends; copy a file elsewhere in /workspace to keep it."

// noFileSink is every file's reason on a mount with no sink: a host with no
// workspace, such as the pool-free CLI paths.
const noFileSink = "this host has no workspace for MCP files"

// filesFooter materializes parts and renders what became of them as the JSON block the
// model reads after the result's text. Only paths, sizes and hashes appear: the bytes
// never reach the model, the idempotency ledger or the transcript.
func (s *MountedServer) filesFooter(ctx context.Context, parts []mcp.FilePart) string {
	footer := struct {
		Files []mcp.FileOutcome `json:"files"`
		Note  string            `json:"note,omitempty"`
	}{Files: s.materialize(ctx, parts)}
	for _, outcome := range footer.Files {
		if outcome.Path != "" {
			footer.Note = filesNote
			break
		}
	}
	raw, err := json.Marshal(footer)
	if err != nil {
		return fmt.Sprintf("the files this result carried could not be reported: %v", err)
	}
	return string(raw)
}

func (s *MountedServer) materialize(ctx context.Context, parts []mcp.FilePart) []mcp.FileOutcome {
	if s.files != nil {
		return s.files.Materialize(ctx, s.name, parts)
	}
	outcomes := make([]mcp.FileOutcome, len(parts))
	for i, part := range parts {
		outcomes[i] = part.NotMaterialized(noFileSink)
	}
	return outcomes
}
```

In `internal/agent/mcptools/bridge_call.go`, replace the first three lines of `newResult`'s body:

```go
	res, err := tools.NewResult(ctx, payload.Text)
	if err != nil {
		return tools.ToolResult{}, err
	}
```

with:

```go
	res, err := b.resultText(ctx, payload)
	if err != nil {
		return tools.ToolResult{}, err
	}
```

Then append:

```go

// resultText is what the model reads: the server's text, then what became of any
// files the result carried. The footer is reserved from the preview cap, because a
// long email body must not truncate away the path to its attachment.
func (b *bridgedTool) resultText(ctx context.Context, payload mcp.ToolPayload) (tools.ToolResult, error) {
	if len(payload.Files) == 0 {
		return tools.NewResult(ctx, payload.Text)
	}
	footer := b.srv.filesFooter(ctx, payload.Files)
	if payload.Text != "" {
		footer = "\n\n" + footer
	}
	return tools.NewResultReservingTail(ctx, payload.Text, footer)
}
```

- [ ] **Step 5: Wire the sink through every mount path.** In `internal/agent/mcptools/mount.go`:

1. **`MountOptions`.** Add after the `OAuth` field:

```go
	// Files materializes the binary content a tool result carries into the calling
	// turn's workspace (bridge_files.go). Nil means this host has none, and every
	// file is reported as not materialized.
	Files FileSink
```

2. **`MountServer`.** Replace it and its body with:

```go
// MountServer spawns one stdio MCP server, mounts all advertised tools into reg,
// and returns a closer that shuts the subprocess down. On any failure (spawn,
// tools/list, name collision) it returns an error and leaves reg untouched / the
// subprocess reaped, so a misconfigured server can never half-register or leak a
// process. The closer MUST be called at agent shutdown (goleak-clean).
//
// processCtx bounds the spawned subprocess's ENTIRE lifetime (the daemon/boot
// context — must NOT be a short-lived, deferred-cancel context, see Pitfall #2);
// handshakeCtx bounds ONLY this mount attempt (the connect handshake AND the
// mount-time tools/list): a hung handshake is dropped within handshakeCtx's
// deadline without affecting processCtx or any other server sharing it. opts
// carries what the boot path gives every mount; today that is the file sink.
func MountServer(processCtx, handshakeCtx context.Context, reg *tools.Registry, name string, cfg mcp.ServerConfig, opts MountOptions) (closer func() error, names []string, err error) {
	closer, names, _, err = mountStdioWithPolicyHost(processCtx, handshakeCtx, reg, name, cfg, defaultBridgePolicy(name), opts)
	return closer, names, err
}
```

3. **Dead code.** Delete `mountStdio` and `mountStdioWithPolicy`, together with the doc comment above `mountStdio`. `MountServer` was their only caller.

4. **`openAttachAndMount`.** Replace its last parameter, `views *mcp.ViewCatalog`, with `opts MountOptions`. Add `srv.files = opts.Files` as its first statement, and change its `hydrateViews(handshakeCtx, session, name, views, advertised, policy)` to pass `opts.Views`. Change its two callers:
   - in `mountManagedHTTPHost`: `return openAttachAndMount(srv, processCtx, handshakeCtx, open, reg, name, policy, opts)`;
   - in `mountStdioWithPolicyHost`: the same, passing `opts`.

5. **`openIdentityScopedHTTPMount`.** Replace its `views *mcp.ViewCatalog` parameter with `opts MountOptions`. Add `parent.files = opts.Files` directly after `parent.identityPool = pool`, and pass `opts.Views` to its `hydrateViews`. Change its caller in `mountManagedHTTPHost` to `return openIdentityScopedHTTPMount(processCtx, handshakeCtx, reg, name, policy, opts, connect)`.

- [ ] **Step 6: Run the package.** Run: `go vet ./internal/agent/mcptools/`, then `go test -race -count=1 ./internal/agent/mcptools/`. Expected: `ok`, under the package's goleak `TestMain`. Also check `wc -l internal/agent/mcptools/bridge_supervisor.go`: it must be 600 or fewer.

- [ ] **Step 7: Commit.**

```bash
cd /d/Aura
git add internal/agent/mcptools/bridge_links.go internal/agent/mcptools/bridge_files.go internal/agent/mcptools/bridge_files_test.go
git commit -F - -- internal/agent/mcptools/bridge_links.go internal/agent/mcptools/bridge_files.go internal/agent/mcptools/bridge_files_test.go internal/agent/mcptools/bridge_supervisor.go internal/agent/mcptools/bridge_call.go internal/agent/mcptools/mount.go internal/agent/mcptools/mount_test.go internal/agent/mcptools/managed_mount_test.go <<'EOF'
feat(mcptools): MCP results' files reach the workspace, paths reach the model

CallTool now reads every resource_link a result carries back with
resources/read on the session that made the call, so an identity-pooled
server's tenant-scoped resource is read as the tenant that asked; Aura
never fetches a link URI itself. A link declaring more than 25 MiB is never
read, and a failed read becomes that file's reason rather than the call's
error. The link's MIME type wins where the contents say only
application/octet-stream, which is what a Python template always says.

bridgedTool hands the files to MountOptions.Files and appends a JSON
footer of paths, sizes and hashes, reserved from the preview cap. The
bytes never reach the model, the ledger or the transcript. MountServer
takes MountOptions so the stdio boot path can carry the sink too; the two
wrappers that only forwarded to it are deleted.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 5: Boot and live mounts carry the sink

**Files:**
- Create: `cmd/aura/runtime_tool_handles.go` (the `runtimeToolHandles` struct, moved out of `main.go` unchanged, plus one field)
- Modify: `cmd/aura/main.go`:
  - delete the struct;
  - set `handles.MCPFiles`;
  - update the two mount calls.
- Modify: `cmd/aura/mcp_tools.go` (add `mcpMountOptions`)
- Modify: `cmd/aura/mcp_live_mount.go` (the `MountOptions` literal)
- Test: `cmd/aura/mcp_mount_options_test.go`

**Interfaces:**
- Consumes: `tools.MCPFileSink` (Task 3), `mcptools.FileSink`, `MountOptions.Files` and `MountServer(…, opts)` (Task 4).
- Produces:
  - `runtimeToolHandles.MCPFiles mcptools.FileSink`;
  - `mcpMountOptions(ctx context.Context, strict bool, server mcp.ManagedServer, handles *runtimeToolHandles) mcptools.MountOptions`.

- [ ] **Step 1: Write the failing tests.** Create `cmd/aura/mcp_mount_options_test.go`:

```go
package main

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

func TestMCPMountOptionsCarryTheViewsAndTheFileSink(t *testing.T) {
	handles := &runtimeToolHandles{MCPViews: mcp.NewViewCatalog(), MCPFiles: &tools.MCPFileSink{}}

	opts := mcpMountOptions(context.Background(), true, mcp.ManagedServer{URL: "https://mcp.example/mcp"}, handles)

	if opts.Views != handles.MCPViews || opts.Files != handles.MCPFiles {
		t.Fatalf("mount options = %+v, want the handles' views and file sink", opts)
	}
}

func TestBuildRegistryWithMCP_HandsMountsTheBoxFileSink(t *testing.T) {
	withMemoryMCPRegistry(t)
	router := usersandbox.NewSandboxRouter(nil, config.ProfileSingleUserHardened, config.SandboxConfig{})

	_, handles, closers, err := buildRegistryWithMCP(context.Background(), config.LoadDB(), nil, nil, router, nil)
	if err != nil {
		t.Fatalf("buildRegistryWithMCP: %v", err)
	}
	defer func() { _ = closeMCPServers(closers) }()

	sink, ok := handles.MCPFiles.(*tools.MCPFileSink)
	if !ok || sink.Router != router {
		t.Fatalf("MCPFiles = %#v, want an MCPFileSink over the boot router", handles.MCPFiles)
	}
}
```

- [ ] **Step 2: Run them to verify they fail.** Run: `go test ./cmd/aura/ -run 'MCPMountOptions|HandsMountsTheBoxFileSink'`.

Expected: build FAIL with `unknown field MCPFiles` and `undefined: mcpMountOptions`.

- [ ] **Step 3: Move the handles struct and add the field.** Cut `type runtimeToolHandles struct { … }` from `cmd/aura/main.go`: the whole declaration, from its `type` line to the closing `}` after the embedded `mediaToolHandles`. Paste it unchanged into the new `cmd/aura/runtime_tool_handles.go`:

```go
package main

import (
	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

// <the runtimeToolHandles declaration, moved verbatim>
```

Then, inside it, directly after the `ViewCallers mcptools.ViewCallers` field, add:

```go
	// MCPFiles materializes the files an MCP tool result carries into the calling
	// turn's box. Nil on the pool-free manifest paths, which mount no MCP server.
	MCPFiles mcptools.FileSink
```

Keep only the imports the moved declaration actually uses; `goimports` or the build tells you which.

- [ ] **Step 4: One option builder for every managed mount.** Append to `cmd/aura/mcp_tools.go`:

```go

// mcpMountOptions is the option set every runtime mount of a managed server uses --
// at boot and live after an authorization -- so neither can forget one the other
// carries. That already happened once with the grant store (runtimeMCPOAuth).
func mcpMountOptions(ctx context.Context, strict bool, server mcp.ManagedServer, handles *runtimeToolHandles) mcptools.MountOptions {
	return mcptools.MountOptions{
		Egress: mcp.RuntimeEgressPolicy(strict, server),
		Views:  handles.MCPViews,
		OAuth:  runtimeMCPOAuth(ctx),
		Files:  handles.MCPFiles,
	}
}
```

Add the `mcptools` import to `mcp_tools.go` if it is missing.

In `cmd/aura/main.go` `buildRegistryWithMCP`:
- directly after `handles.ViewCallers = mcptools.ViewCallers{}`, add `handles.MCPFiles = &tools.MCPFileSink{Router: sandboxRouter}`;
- replace `return mcptools.MountServer(mountCtx, c, reg, name, mcpServers[name])` with `return mcptools.MountServer(mountCtx, c, reg, name, mcpServers[name], mcptools.MountOptions{Files: handles.MCPFiles})`;
- replace the whole `mcptools.MountOptions{ Egress: …, Views: handles.MCPViews, OAuth: runtimeMCPOAuth(mountCtx), }` literal passed to `MountManagedServerWithOptions` with `mcpMountOptions(mountCtx, cfg.Profile.Strict(), server, &handles)`.

In `cmd/aura/mcp_live_mount.go`, replace the `mcptools.MountOptions{ Egress: …, Views: m.handles.MCPViews, OAuth: runtimeMCPOAuth(ctx), }` literal with `mcpMountOptions(ctx, m.strict, server, m.handles)`. If that literal was the file's last use of the `mcp` import, drop the import; the build says so.

- [ ] **Step 5: Run the package.** Run: `go vet ./cmd/aura/`, then `go test -race -count=1 ./cmd/aura/`. Expected: `ok`. Check `wc -l cmd/aura/main.go`: it must be 600 or fewer, and is about 570 after the move.

- [ ] **Step 6: Commit.**

```bash
cd /d/Aura
git add cmd/aura/runtime_tool_handles.go cmd/aura/mcp_mount_options_test.go
git commit -F - -- cmd/aura/runtime_tool_handles.go cmd/aura/mcp_mount_options_test.go cmd/aura/main.go cmd/aura/mcp_tools.go cmd/aura/mcp_live_mount.go <<'EOF'
feat(aura): every MCP mount writes its files into the caller's box

The boot mount, the live mount after an authorization and the stdio boot
mount now all carry an MCPFileSink over the same sandbox router the box
tools use. The boot and live mounts built their option literals
separately, the way the grant store was once forgotten on one of them;
mcpMountOptions is now the one place both get their options from.
runtimeToolHandles moves to its own file, which keeps main.go under the
600-line limit it was about to cross.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 6: A real box: the file exists during the turn and is gone after it

**Files:**
- Create: `internal/agent/tools/mcp_files_docker_test.go` (`//go:build docker_integration`)

**Interfaces:**
- Consumes: `MCPFileSink`, `WithTurnCleanup` and `WithRequestID`, plus the docker tier's `skipUnlessDockerdTools`, `newDockerRouter` and `ctxWith`.

- [ ] **Step 1: Write the test.** Create `internal/agent/tools/mcp_files_docker_test.go`:

```go
//go:build docker_integration

// mcp_files_docker_test.go is the docker_integration proof for MCPFileSink: against a
// live box, a file an MCP result carried lands at the path the model is told, with the
// bytes it came with, and is gone once the turn's cleanup has run.

package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

func TestMCPFileSink_AFileLivesForItsTurnInARealBox(t *testing.T) {
	skipUnlessDockerdTools(t)
	router := newDockerRouter(t, nil)
	ctx, cleanup := WithTurnCleanup(WithRequestID(ctxWith(t, "sess-dk-mcp", "call-dk-mcp"), "req-dk-mcp"))

	out := (&MCPFileSink{Router: router}).Materialize(ctx, "aura-pim", []mcp.FilePart{
		{Name: "Fattura è.pdf", MIMEType: "application/pdf", Data: []byte("%PDF-1.7 docker")},
	})
	if out[0].Path != "/workspace/mcp-files/req-dk-mcp/aura-pim/Fattura è.pdf" {
		t.Fatalf("outcome = %+v", out[0])
	}

	handle, err := router.Route(ctx)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	read, err := router.Exec(ctx, handle, usersandbox.ExecRequest{Command: "cat -- " + ShellQuoteArg(out[0].Path)})
	if err != nil || string(read.Stdout) != "%PDF-1.7 docker" {
		t.Fatalf("the box holds %q (err %v), want the file's bytes", read.Stdout, err)
	}

	if err := cleanup.Run(context.Background()); err != nil {
		t.Fatalf("turn cleanup: %v", err)
	}
	gone, err := router.Exec(ctx, handle, usersandbox.ExecRequest{Command: "test -e /workspace/mcp-files/req-dk-mcp && echo LEFT || echo GONE"})
	if err != nil || strings.TrimSpace(string(gone.Stdout)) != "GONE" {
		t.Fatalf("after cleanup the turn directory is %q (err %v), want GONE", gone.Stdout, err)
	}
}
```

- [ ] **Step 2: Compile the tier.** Run: `go vet -tags docker_integration ./internal/agent/tools/`. Expected: no output.

- [ ] **Step 3: Run it where a daemon is reachable.** Run: `go test -tags docker_integration -count=1 -run TestMCPFileSink_AFileLivesForItsTurnInARealBox ./internal/agent/tools/`.

Expected: `ok` when WSL reaches a Docker daemon. Otherwise the test SKIPs locally, and it runs in CI's `Sandbox docker_integration tier` job, where `skipUnlessDockerdTools` fails instead of skipping. Report which one happened; a local skip is not a pass.

- [ ] **Step 4: Commit.**

```bash
cd /d/Aura
git add internal/agent/tools/mcp_files_docker_test.go
git commit -F - -- internal/agent/tools/mcp_files_docker_test.go <<'EOF'
test(tools): prove an MCP file lives for its turn in a real box

The unit tests pin the commands MCPFileSink composes against a fake
backend; this one runs them against a live box: the file is at the path
the model is told, with its bytes, and the turn directory is gone after
cleanup, accented file name included.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 7: Gates, then push

- [ ] **Step 1: The pre-push gate.** First check with `git status` that no other session is mid-change in the tree; run gates alone ([[feedback_never_run_gates_against_a_busy_tree]]). Run: `make quality`.

Expected: `ok: quality gate passed (deadcode vet build file-size …)`.
- A `deadcode` finding on `mountStdio`, `mountStdioWithPolicy` or `(*DocumentOpen).write` means a deletion was missed.
- A `dupl` finding in non-test code means a helper was duplicated rather than shared.

- [ ] **Step 2: The coverage gate.** Bring the stack up if it is down (`make db-migrate memory-up`). Then run `unset AURA_WEB_AUTH_SECRET; bash scripts/coverage_docker.sh`; the `unset` is needed because of the `.env` leak into config tests.

Expected:
- `ok: owned coverage NN.N% >= 85%`;
- the package policy passes for `internal/mcp`, `internal/agent/mcptools`, `internal/agent/tools` and `internal/agent`. Each stays at or above its pinned floor.

If a package falls under its floor, add daemon-free tests for the uncovered lines. Never lower a floor.

- [ ] **Step 3: Push. ASK THE OPERATOR FIRST.** A push of `master` publishes the edge image, and the appliances install it. The two fork plans must have shipped before Task 8, but the Aura push itself does not depend on them: without links or files a result behaves exactly as today. With the go, push from WSL so the lefthook gates run on the pushed commit:

```bash
cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks push origin master
```

Expected: the lefthook banner, with every pre-push command green. No banner means no gate ran.

Then run `gh run list --branch master --limit 10`. Every job must end green, including `Sandbox docker_integration tier`, the coverage job, and `Publish Aura edge image`. A red job is fixed, whether or not it looks related to this change.

---

### Task 8: Rollout and E2E on the lab VM

Prerequisites:
- the PIM plan's Task 4 is pushed, and `aura-pim-mcp:sidecar` is published;
- the WhatsApp plan's Task 4 is pushed, and `whatsapp-mcp:latest` is published;
- this plan's Task 7 is pushed, and the edge image is published.

Every step below changes the VM only through its updater. Mailbox and chat access is read-only. The operator drives the cockpit; you watch the backend ([[feedback_user_drives_frontend_e2e_no_test_identities]]).

- [ ] **Step 1: Let the updater converge.** On the VM (`192.168.101.158`, via the WSL sshpass script pattern in [[reference_lab_vm_192_168_101_158]]), check `sudo systemctl list-timers` for the updater's next run and wait for it. Do not run the updater by hand. Then confirm that each running container uses the new image: compare the container's `Image` id with the pulled tag's `Id` for `aura`, `aura-pim-mcp` and `whatsapp` ([[feedback_verify_container_images_current_after_changes]]).

- [ ] **Step 2: Mail.** Ask the operator to request, in the cockpit, a real email's PDF attachment together with a fact that needs the file itself, for example "how many pages does the PDF attached to <email> have?". Watch:
  - the tool trace: `calendar` / `get_email_attachment`, then a footer path under `/workspace/mcp-files/<request-id>/aura-pim/`;
  - a box command that opens that path, for example `pdfinfo` or python;
  - the answer, which must match the real page count;
  - after the turn, in the operator's box container (find it with `sudo docker ps`), `ls /workspace/mcp-files` must show no directory for that request id;
  - the aura logs: `agent turn end` for the request, and no `turn cleanup failed`.

- [ ] **Step 3: WhatsApp.** Repeat Step 2 with a received image ("what is in the photo <contact> sent me?") and a received document. Check that a PNG the bridge saved as `.jpg` is reported as `image/png`, and that a document keeps its sender's name.

- [ ] **Step 4: Score and clean.** Score the run against the spec's E2E list. A score of 9.8 or more closes it; anything less goes back to the task that owns the gap. Then delete every personal copy the test produced: files the model copied elsewhere in `/workspace` during these turns, listed and removed with the operator watching. Leave nothing from the test in the box.

- [ ] **Step 5: Record the measurement in the PRD.** In `prd.md` §13 "MCP integrations", add a dated paragraph (2026-MM-DD, the day of the run) that records what was measured:
  - per-file sizes and the turn's added latency for mail and WhatsApp;
  - that cleanup ran;
  - the request ids used as evidence.

  It also records what the run does NOT prove:
  - servers other than the two forks;
  - files near the 25 MiB cap on the appliance's memory;
  - the identity-pooled link read, if no identity-pooled server returned a link during the run.

  Commit it with explicit paths, and push after the operator's go.
