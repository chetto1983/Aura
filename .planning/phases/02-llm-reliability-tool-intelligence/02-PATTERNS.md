# Phase 2: LLM Reliability & Tool Intelligence - Pattern Map

**Mapped:** 2026-05-10
**Files analyzed:** 9 created/modified files + 5 deletion-sweep sites
**Analogs found:** 9 / 9 (every new/modified file has at least one in-tree analog)

> Source for each cited excerpt was opened with the Read tool during this mapping session. Line ranges are stable as of HEAD (commit 185dd08a). Where the planner needs a literal verbatim copy, use these excerpts. Where shape but not content is what matters, the excerpt is marked "shape-template".

---

## File Classification

| New / Modified File | Role | Data Flow | Closest In-Tree Analog | Match Quality |
|--------------------|------|-----------|------------------------|---------------|
| `internal/llm/classify.go` (NEW) | utility / classifier | transform (error → bucket+cleaned-message) | NONE — closest is `internal/health/sanitize.go` (key-only redaction, NOT value redaction) | partial — classifier itself is greenfield; only the redactor has a weak analog |
| `internal/llm/retry.go` (REWRITE) | wrapper / middleware | request-response (wraps `llm.Client`) | EXISTING `internal/llm/retry.go` (same file pre-rewrite) | exact — replace-in-place; constructor signature preserved per D-10 |
| `internal/reindex/types.go` (NEW) | types / interface declarations | — | `internal/search/search.go:54-64` (`WikiPageReindexer` / `WikiPageIndexer` interface declarations) | role-match — interface-only file precedent |
| `internal/reindex/worker.go` (NEW) | service / async worker | event-driven, drop-newest | `internal/conversation/archive.go:332-385` `BufferedAppender` | role-match — same shape, lifecycle differs (dedicated ctx + done chan, never close producer-side) |
| `internal/wiki/store_writes.go` (NEW factor-out) | service / storage | CRUD with optimistic concurrency | `internal/wiki/store.go:158-217` `WritePage` | exact — file split, function moves verbatim then is extended |
| `internal/wiki/schema.go` (EXTEND) | model / schema | — | `internal/wiki/schema.go:18-32` `Page` struct itself | exact — additive `omitempty` field on the same struct |
| `internal/tools/wiki.go` (NEW) | tool / handler | request-response (LLM-called) | `internal/tools/scheduler.go:42-90` `RunTaskNowTool` (single-action, JSON-result, error-wrapping) + `internal/tools/auth.go:27-83` `RequestDashboardTokenTool` (constructor that returns nil on missing dep, structured tool result) | exact — Tool interface implementation pattern is identical |
| `internal/tools/registry_search_vector.go` (RENAME-EXPORT) | infra / vector index | streaming (HTTP → embedding → Qdrant) | EXISTING file at same path | exact — rename `toolVectorIndex` → `ToolVectorIndex` only; preserve WR-04 mu-released-around-HTTP semantics |
| `internal/agentloop/loop.go` (MODIFY) | orchestration | request-response (per-turn closure) | EXISTING `loop.go:154-157` already invokes `opts.ToolsProvider()` per-turn | exact — no shape change required; only the closure setup at registration changes |
| `internal/telegram/setup.go` (MODIFY) | wiring / config | — | `internal/telegram/setup.go:519-528` (`BuildVectorIndex`) and `:744-748` (`NewToolSearchTool` registration site to delete) | exact — surgical wiring change |
| `internal/telegram/conversation.go` (MODIFY) | wiring | — | `internal/telegram/conversation.go:282-333` (existing `currentToolDefs` closure pattern) | exact — replace `currentToolDefs` with the per-turn ToolsProvider |

| Deleted File | Reason |
|--------------|--------|
| `internal/tools/tool_search.go` | D-27, D-31 — replaced by automatic top-K=5 injection |
| `internal/tools/tool_search_test.go` | D-31 — test of deleted tool |
| `cmd/debug_telegram_sandbox/main.go` (lines 61, 195, 319, 816 — clean in place, NOT delete) | D-27 — references `--expect-tool-search-calls-max` flag and `ToolSearchCalls` counter |
| `internal/telegram/conversation.go` lines 259, 293, 306, 331 (clean in place) | D-27 — `tool_search` mentions in prompt + maxCallsPerTool + status strings |
| `internal/telegram/debug_smoke.go` line 186 (clean in place) | D-27 — `tool_search` case in smoke flow |

---

## Pattern Assignments

### `internal/llm/classify.go` (NEW — utility / transform)

**Closest analog:** Greenfield. The closest in-tree shape is the `Bucket`-style enum used in `internal/wiki/schema.go:34-41` (typed error wrapper), and the redaction-handler shape in `internal/health/sanitize.go:9-89`. Note the existing redactor sanitizes ATTRIBUTE KEYS only (line 49-55) — Phase 2's classifier needs a VALUE-pattern redactor that does NOT exist in-tree. RESEARCH.md §"Common Operation 1" includes the canonical sketch.

**Bucket enum + sentinel errors — shape-template** (no exact in-tree analog; pattern from RESEARCH.md §"Common Operation 1" lines 571-585):

```go
type Bucket int

const (
    BucketTransient Bucket = iota
    BucketContent
    BucketPermanent
)

func (b Bucket) String() string {
    return [...]string{"transient", "content", "permanent"}[b]
}

var (
    ErrSchemaValidation  = errors.New("schema validation failed")
    ErrEmptyOutput       = errors.New("empty assistant output")
    ErrMalformedToolCall = errors.New("malformed tool call arguments")
)
```

**Typed error wrapper analog** from `internal/wiki/schema.go:34-41`:

```go
// ValidationError contains all validation failures for a wiki page.
type ValidationError struct {
    Errors []string
}

func (e *ValidationError) Error() string {
    return fmt.Sprintf("wiki validation failed: %s", strings.Join(e.Errors, "; "))
}
```

**Use this shape for `ConflictError` (in `store_writes.go`) and `APIError` (in `classify.go`).**

**HTTP status extraction — analog gap.** `internal/llm/openai.go:173-175` and `:240-243` currently surface HTTP errors as **string-only** `fmt.Errorf("LLM API error (status %d): %s", resp.StatusCode, string(respBody))`. Classifier needs `errors.As(err, &apiErr)` to work on a typed value. **Planner action:** introduce a typed `*APIError{ StatusCode int, Body string }` in `classify.go` (or `client.go`), and modify `openai.go:175` and `:243` to return it. This is a 2-line change but is REQUIRED for D-09 priority pipeline (HTTP status before message-pattern).

**Imports pattern** (typical Go stdlib; from `internal/llm/retry.go:1-7`):

```go
package llm

import (
    "context"
    "errors"
    "math"
    "net"
    "strings"
    "time"
)
```

**Redactor analog gap.** `internal/health/sanitize.go:49-89` redacts log attribute KEYS by name (`token`, `auth`, etc.) — NOT message values. The classifier's `redact(s string)` needs its own regex panel. RESEARCH.md §"Common Operation 1" lines 631-641 shows the panel (URL token, Bearer, basic-auth URL, base64 ≥ threshold, Authorization header, OpenRouter `sk-or-v1-`, JWT). **Planner action:** add `redact_test.go` with seeded-bad inputs and assert NO match in the panel after redaction (Pitfall #5).

---

### `internal/llm/retry.go` (REWRITE — wrapper / middleware)

**Closest analog:** ITSELF — the existing file at `internal/llm/retry.go:1-92`. D-10 requires the constructor signature `NewRetryClient(inner Client, cfg RetryConfig)` to be preserved so callers in `internal/telegram/setup.go:800-804` don't change.

**Existing structure to extend** (`internal/llm/retry.go:1-91` — verbatim, full file):

```go
package llm

import (
    "context"
    "math"
    "time"
)

// RetryClient wraps a Client with exponential backoff retry logic.
type RetryClient struct {
    inner      Client
    maxRetries int
    baseDelay  time.Duration
    maxDelay   time.Duration
}

// RetryConfig holds retry configuration.
type RetryConfig struct {
    MaxRetries int
    BaseDelay  time.Duration
    MaxDelay   time.Duration
}

// DefaultRetryConfig returns sensible defaults (5 retries, 1s base, 30s max).
func DefaultRetryConfig() RetryConfig {
    return RetryConfig{
        MaxRetries: 5,
        BaseDelay:  time.Second,
        MaxDelay:   30 * time.Second,
    }
}

// NewRetryClient wraps a Client with retry logic.
func NewRetryClient(inner Client, cfg RetryConfig) *RetryClient {
    return &RetryClient{
        inner:      inner,
        maxRetries: cfg.MaxRetries,
        baseDelay:  cfg.BaseDelay,
        maxDelay:   cfg.MaxDelay,
    }
}

// Send retries the request with exponential backoff on failure.
func (r *RetryClient) Send(ctx context.Context, req Request) (Response, error) {
    var lastErr error
    for attempt := 0; attempt <= r.maxRetries; attempt++ {
        resp, err := r.inner.Send(ctx, req)
        if err == nil {
            return resp, nil
        }
        lastErr = err
        if attempt < r.maxRetries {
            delay := r.backoffDelay(attempt)
            select {
            case <-ctx.Done():
                return Response{}, ctx.Err()
            case <-time.After(delay):
            }
        }
    }
    return Response{}, lastErr
}
```

**Caller integration (must not break)** — `internal/telegram/setup.go:800-804`:

```go
return llm.NewRetryClient(openaiClient, llm.RetryConfig{
    MaxRetries: cfg.LLMMaxRetries,
    BaseDelay:  time.Second,
    MaxDelay:   30 * time.Second,
})
```

**Rewrite shape** — RESEARCH.md §"Pattern 1" lines 290-333 has the canonical Send-rewrite sketch. Add fields to `RetryConfig`:

```go
type RetryConfig struct {
    MaxRetries           int
    BaseDelay            time.Duration
    MaxDelay             time.Duration
    MaxContentRetries    int        // NEW (D-10) — default 3
    ContentTemperatures  []float64  // NEW (D-10) — default [0.0, 0.3, 0.7]
    JitterRatio          float64    // NEW (D-10) — default 0.5
}
```

**Critical preservation** — caller temp from D-08:

```go
callerTemp := req.Temperature  // capture BEFORE any retry — preserved on TRANSIENT, only overridden on CONTENT
```

The `Stream()` method (existing `retry.go:65-83`) must be preserved with the same classify-then-retry semantics (RESEARCH.md does not show a Stream rewrite — planner action: mirror the Send shape).

---

### `internal/reindex/types.go` (NEW — types / interfaces)

**Closest analog:** `internal/search/search.go:42-64` (interface declarations file pattern).

**Interface declarations analog** (`internal/search/search.go:42-64`):

```go
// Queryer is the minimal semantic retrieval boundary.
type Queryer interface {
    Search(ctx context.Context, query string, topK int) ([]Result, error)
}

// Searcher is the read-only wiki retrieval boundary used by tools and Telegram
// context injection.
type Searcher interface {
    Queryer
    IsIndexed() bool
}

// WikiPageReindexer is the wiki index maintenance boundary used after one wiki
// page changes.
type WikiPageReindexer interface {
    ReindexWikiPage(ctx context.Context, slug string) error
}

// WikiPageIndexer is the startup/full-rebuild wiki index boundary.
type WikiPageIndexer interface {
    IndexWikiPages(ctx context.Context) error
    WikiPageReindexer
}
```

**Use this shape for `Submitter`** per D-14:

```go
// Submitter is the producer-side boundary for the reindex worker. Implementations
// MUST NOT block; Submit returns false when the job was dropped due to a full
// queue or because the worker has been Stopped.
type Submitter interface {
    Submit(Job) bool
}

type Op int

const (
    OpUpsert Op = iota
    OpDelete
)

type Job struct {
    Slug string
    Op   Op
}

type Health struct {
    QueueDepth   int
    Dropped      int64
    LastSuccess  time.Time
    LastError    string
}
```

---

### `internal/reindex/worker.go` (NEW — service / async worker)

**Closest analog:** `internal/conversation/archive.go:332-385` `BufferedAppender` — same `chan + drain + select/default` shape, but the reindex worker MUST diverge on lifecycle (RESEARCH.md Pitfall #2 + #4):
- Reindex: dedicated `ctx` + `done` chan, NEVER close the channel from outside.
- BufferedAppender: closes channel from `Close()` — UNSAFE for multi-producer scenarios.

**Worker struct + Submit + drain analog** (`internal/conversation/archive.go:332-385` — verbatim):

```go
// BufferedAppender wraps a TurnAppender with a buffered channel and a single
// drain goroutine so hot conversation paths are non-blocking. Turns that
// arrive when the buffer is full are dropped and logged.
type BufferedAppender struct {
    store  TurnAppender
    ch     chan Turn
    logger *slog.Logger
    wg     sync.WaitGroup
}

// NewBufferedAppender starts the drain goroutine. bufSize should be 100 for
// production; tests may use smaller values.
func NewBufferedAppender(store TurnAppender, bufSize int) *BufferedAppender {
    a := &BufferedAppender{
        store:  store,
        ch:     make(chan Turn, bufSize),
        logger: slog.Default(),
    }
    a.wg.Add(1)
    go a.drain()
    return a
}

// Append enqueues a Turn non-blocking. If the buffer is full the turn is
// dropped and an error is logged (archive_dropped_total).
func (a *BufferedAppender) Append(_ context.Context, t Turn) error {
    select {
    case a.ch <- t:
    default:
        a.logger.Error("archive_dropped_total: buffer full, turn dropped",
            "chat_id", t.ChatID, "turn_index", t.TurnIndex, "role", t.Role)
    }
    return nil
}

// Close signals the drain goroutine to flush remaining turns and waits for it
// to finish. ctx is reserved for future timeout support.
func (a *BufferedAppender) Close(_ context.Context) error {
    close(a.ch)         // <-- DO NOT MIRROR THIS in reindex.Worker (Pitfall #4)
    a.wg.Wait()
    return nil
}

func (a *BufferedAppender) drain() {
    defer a.wg.Done()
    for t := range a.ch {  // <-- DO NOT MIRROR; use select { ctx.Done(), <-jobs } instead
        if err := a.store.Append(context.Background(), t); err != nil {
            ...
        }
    }
}
```

**Required corrections vs analog (RESEARCH.md §"Pattern 3" lines 386-428):**

```go
type Worker struct {
    jobs         chan Job
    reindexer    search.WikiPageReindexer
    ctx          context.Context           // DEDICATED — owned by worker
    cancel       context.CancelFunc
    done         chan struct{}             // signals drain goroutine exited
    droppedTotal atomic.Int64
    stopped      atomic.Bool
    logger       *slog.Logger
}

func (w *Worker) Submit(j Job) bool {
    if w.stopped.Load() {                                      // Pitfall #8
        w.logger.Warn("reindex_dropped_after_stop", slog.String("slug", j.Slug))
        return false
    }
    select {
    case w.jobs <- j:
        return true
    default:
        w.droppedTotal.Add(1)
        w.logger.Warn("reindex_dropped_total", slog.String("slug", j.Slug))
        return false
    }
}

func (w *Worker) Stop() {
    w.stopped.Store(true)
    w.cancel()      // cancels in-flight ReindexWikiPage HTTP calls (Pitfall #2)
    <-w.done        // wait for drain goroutine to actually exit
    // NEVER close(w.jobs) — let GC reclaim it (Pitfall #4)
}

func (w *Worker) drain() {
    defer close(w.done)
    for {
        select {
        case <-w.ctx.Done():
            return
        case j := <-w.jobs:
            if err := w.reindexer.ReindexWikiPage(w.ctx, j.Slug); err != nil {
                w.logger.Warn("reindex_failed",
                    slog.String("slug", j.Slug),
                    slog.Any("error", err))
            }
        }
    }
}
```

**Bot lifecycle integration analog** (`internal/telegram/bot.go:208-241`):

```go
// Stop gracefully stops update intake and waits for background workers.
func (b *Bot) Stop() {
    if gate := b.userGate(); gate != nil {
        gate.Close()
    }
    if b.bot != nil && b.started.Load() {
        b.bot.Stop()
    }
    if b.sched != nil {
        b.sched.Stop()
    }
    if b.docs != nil {
        b.docs.Stop()
    }
    ...
    if b.archiver != nil {
        if err := b.archiver.Close(context.Background()); err != nil {
            logger.Error("telegram shutdown: archiver close failed", "error", err)
        }
    }
    ...
}
```

**Phase 2 add:** call `b.reindex.Stop()` AFTER `b.archiver.Close()` (so any final reindex enqueues from archiver-driven flushes still drain).

---

### `internal/wiki/store_writes.go` (NEW factor-out — service / CRUD)

**Closest analog:** `internal/wiki/store.go:158-217` `WritePage` (verbatim source for the factor-out) and `internal/wiki/store.go:271-302` `DeletePage` (which also moves).

**Imports pattern** (`internal/wiki/store.go:1-17`):

```go
package wiki

import (
    "context"
    "errors"
    "fmt"
    "log/slog"
    "os"
    "path/filepath"
    "sort"
    "strings"
    "sync"
    "time"

    "github.com/go-git/go-git/v5"
    "github.com/go-git/go-git/v5/plumbing/object"
)
```

**Per-slug fileMutex pattern** (`internal/wiki/store.go:137-140`):

```go
func (s *Store) fileMutex(slug string) *sync.Mutex {
    mu, _ := s.mu.LoadOrStore(slug, &sync.Mutex{})
    return mu.(*sync.Mutex)
}
```

**Existing `WritePage` body to factor out** (`internal/wiki/store.go:158-217` — verbatim, this code MOVES to `store_writes.go`):

```go
// WritePage atomically writes a wiki page to disk as .md and commits it to git.
// It validates the page against the schema before writing.
func (s *Store) WritePage(ctx context.Context, page *Page) error {
    if err := Validate(page); err != nil {
        return fmt.Errorf("validation failed: %w", err)
    }

    slug := Slug(page.Title)
    filename := slug + ".md"
    path := filepath.Join(s.dir, filename)

    mu := s.fileMutex(slug)
    mu.Lock()
    defer mu.Unlock()

    // Remove legacy .yaml if it exists
    yamlPath := filepath.Join(s.dir, slug+".yaml")
    if _, err := os.Stat(yamlPath); err == nil {
        os.Remove(yamlPath)
        s.gitCommit(ctx, slug+".yaml", "delete")
    }

    // Serialize as markdown with YAML frontmatter
    data, err := MarshalMD(page)
    if err != nil {
        return fmt.Errorf("marshaling markdown: %w", err)
    }

    // Atomic write: temp file + rename
    tmp, err := os.CreateTemp(s.dir, slug+".*.tmp")
    if err != nil {
        return fmt.Errorf("creating temp file: %w", err)
    }
    tmpName := tmp.Name()

    if _, err := tmp.Write(data); err != nil {
        tmp.Close()
        os.Remove(tmpName)
        return fmt.Errorf("writing temp file: %w", err)
    }
    tmp.Close()

    if err := os.Rename(tmpName, path); err != nil {
        os.Remove(tmpName)
        return fmt.Errorf("renaming temp file: %w", err)
    }

    s.logger.Info("wiki page written", "slug", slug, "path", path)

    // Update graph files
    s.updateIndex(ctx)
    s.appendLog(ctx, "update", slug)

    // Commit to git
    if err := s.gitCommit(ctx, filename, "update"); err != nil {
        s.logger.Error("git commit failed for wiki page", "slug", slug, "error", err)
    }

    return nil
}
```

**ETag check insertion point** — RESEARCH.md §"Pattern 2" lines 343-376 places the check INSIDE the existing `mu.Lock(); defer mu.Unlock()` critical section (right after line 171 of the existing code). The check needs a `readPageLocked(slug)` helper that does NOT call `s.fileMutex(slug)` again (caller is already holding the mutex).

**ConflictError shape — shape-template** (modeled after `internal/wiki/schema.go:34-41` `ValidationError`):

```go
// ConflictError is returned by WritePage when the on-disk updated_at does not
// match the caller-supplied expected_updated_at, OR when expected_updated_at
// is "" (create-only sentinel) but a page with that slug already exists.
type ConflictError struct {
    Slug     string
    Expected string  // "" means create-only (D-02)
    Actual   string
}

func (e *ConflictError) Error() string {
    return fmt.Sprintf("page %s was modified since last read (expected %s, got %s)",
        e.Slug, e.Expected, e.Actual)
}
```

**`gitCommit` pattern + `unversioned` set/clear hook** (`internal/wiki/store.go:351-388` — verbatim):

```go
// gitCommit ignores ctx because go-git's Worktree API is synchronous; we
// keep the parameter so callers don't need a special case in the wiki
// write path.
func (s *Store) gitCommit(_ context.Context, filename, action string) error {
    s.gitMu.Lock()
    defer s.gitMu.Unlock()

    repo, err := git.PlainOpen(s.dir)
    if err != nil {
        return fmt.Errorf("opening repo for commit: %w", err)
    }

    wt, err := repo.Worktree()
    if err != nil {
        return fmt.Errorf("getting worktree: %w", err)
    }

    if _, err := wt.Add(filename); err != nil {
        return fmt.Errorf("staging %s: %w", filename, err)
    }

    slug := filename
    if idx := strings.LastIndex(slug, "."); idx != -1 {
        slug = slug[:idx]
    }
    msg := fmt.Sprintf("wiki: %s %s", action, slug)
    if _, err := wt.Commit(msg, &git.CommitOptions{
        Author: &object.Signature{
            Name:  "Aura",
            Email: "aura@local",
        },
    }); err != nil {
        if isCleanWorktreeCommitError(err) {
            return nil
        }
        return fmt.Errorf("committing %s: %w", filename, err)
    }

    s.logger.Info("wiki page committed to git", "file", filename, "action", action)
    return nil
}
```

**Where the existing code IGNORES the gitCommit error** (`internal/wiki/store.go:212-214`) — Phase 2 wires this to set `Unversioned=true` instead of just logging:

```go
// Commit to git
if err := s.gitCommit(ctx, filename, "update"); err != nil {
    s.logger.Error("git commit failed for wiki page", "slug", slug, "error", err)
}
```

**D-17 / D-18 hook (per RESEARCH.md):** after the gitCommit branch, re-read the page (using a locked helper), set/clear `Unversioned`, and atomic-rewrite. Do NOT commit again on the metadata-only rewrite (would loop or fail).

---

### `internal/wiki/schema.go` (EXTEND — model)

**Closest analog:** itself — `internal/wiki/schema.go:17-32` `Page` struct.

**Existing struct** (`internal/wiki/schema.go:17-32` — verbatim):

```go
// Page represents a wiki page with YAML frontmatter and markdown body.
type Page struct {
    Title         string   `yaml:"title"`
    Tags          []string `yaml:"tags,omitempty"`
    Category      string   `yaml:"category,omitempty"`
    Related       []string `yaml:"related,omitempty"`
    Sources       []string `yaml:"sources,omitempty"`
    SchemaVersion int      `yaml:"schema_version"`
    PromptVersion string   `yaml:"prompt_version"`
    CreatedAt     string   `yaml:"created_at"`
    UpdatedAt     string   `yaml:"updated_at"`

    // Body holds the markdown content below the frontmatter.
    // Not serialized in YAML — written after the --- delimiter.
    Body string `yaml:"-"`
}
```

**Phase 2 addition (D-19):** add ONE field with `omitempty`. Keep `CurrentSchemaVersion = 2` (line 12) UNCHANGED.

```go
    Unversioned bool `yaml:"unversioned,omitempty" json:"unversioned,omitempty"`
```

**Critical companion change** — `internal/wiki/parser.go:236-258` `MarshalMD` builds an inline frontmatter struct that EXCLUDES `Unversioned` because it duplicates each field by hand:

```go
// MarshalMD serializes a Page into markdown-with-frontmatter format.
func MarshalMD(page *Page) ([]byte, error) {
    // Marshal frontmatter fields (excluding Body which has yaml:"-")
    fm := struct {
        Title         string   `yaml:"title"`
        Tags          []string `yaml:"tags,omitempty"`
        Category      string   `yaml:"category,omitempty"`
        Related       []string `yaml:"related,omitempty"`
        Sources       []string `yaml:"sources,omitempty"`
        SchemaVersion int      `yaml:"schema_version"`
        PromptVersion string   `yaml:"prompt_version"`
        CreatedAt     string   `yaml:"created_at"`
        UpdatedAt     string   `yaml:"updated_at"`
    }{
        Title:         page.Title,
        ...
    }
```

**Planner action:** add `Unversioned bool yaml:"unversioned,omitempty"` to the inline struct AND to the value initializer. Without this, the field will be set on the in-memory `Page` but NOT round-trip to disk.

**`Validate` no-op for new field:** the existing `Validate` function (`internal/wiki/schema.go:60-128`) does not need to learn about `Unversioned` — it's a boolean with safe zero value.

---

### `internal/tools/wiki.go` (NEW — tool / handler)

**Closest analog:** `internal/tools/auth.go:27-83` `RequestDashboardTokenTool` (single-action LLM tool with constructor that returns nil on missing dep, a structured tool result, and `%w`-wrapped errors). Secondary analog: `internal/tools/scheduler.go:42-90` `RunTaskNowTool` (returns marshaled JSON struct).

**Constructor + Tool interface analog** (`internal/tools/auth.go:27-83` — verbatim):

```go
// RequestDashboardTokenTool mints a new bearer token for the calling
// Telegram user and delivers it via Telegram. The LLM never sees the
// token text; the tool's return value is a bookkeeping confirmation.
type RequestDashboardTokenTool struct {
    store     auth.TokenWriter
    sender    TokenSender
    allowlist AllowlistFunc
}

// NewRequestDashboardTokenTool builds the tool. All three deps are
// required — the constructor returns nil if any is missing so the bot
// can skip registration when auth isn't configured.
func NewRequestDashboardTokenTool(store auth.TokenWriter, sender TokenSender, allowlist AllowlistFunc) *RequestDashboardTokenTool {
    if store == nil || sender == nil || allowlist == nil {
        return nil
    }
    return &RequestDashboardTokenTool{store: store, sender: sender, allowlist: allowlist}
}

func (t *RequestDashboardTokenTool) Name() string { return "request_dashboard_token" }

func (t *RequestDashboardTokenTool) Description() string {
    return "Mint a fresh bearer token for this user's dashboard session and send it to them via Telegram. Use when the user asks for dashboard access, login link, or token. The token is delivered out-of-band — never echo it in your reply."
}

func (t *RequestDashboardTokenTool) Parameters() map[string]any {
    return map[string]any{
        "type":       "object",
        "properties": map[string]any{},
    }
}

func (t *RequestDashboardTokenTool) Execute(ctx context.Context, args map[string]any) (string, error) {
    userID := UserIDFromContext(ctx)
    if userID == "" {
        return "", errors.New("request_dashboard_token: no user context")
    }
    ...
    if err := t.store.Issue(ctx, userID); err != nil {
        return "", fmt.Errorf("request_dashboard_token: issue: %w", err)
    }
    ...
}
```

**JSON Schema with REQUIRED fields analog** (`internal/tools/scheduler.go:60-71`):

```go
func (t *RunTaskNowTool) Parameters() map[string]any {
    return map[string]any{
        "type": "object",
        "properties": map[string]any{
            "name": map[string]any{
                "type":        "string",
                "description": "Name of the saved scheduled task to run now.",
            },
        },
        "required": []string{"name"},
    }
}
```

**For `write_wiki_page` (D-01..D-05) the schema MUST include `additionalProperties: false`** (RESEARCH.md anti-pattern: "Letting the LLM control privileged frontmatter") — there is NO existing in-tree analog using `additionalProperties: false`. **Planner action:** add it explicitly:

```go
return map[string]any{
    "type":                 "object",
    "additionalProperties": false,
    "properties":           map[string]any{...},
    "required":             []string{"title", "body", "expected_updated_at"},
}
```

**Argument extraction analog** — existing helpers in `internal/tools/args.go:1-44` and `internal/tools/web_common.go:77-87`:

```go
// internal/tools/args.go:5-12
func stringArg(args map[string]any, key string) string {
    v, ok := args[key]
    if !ok {
        return ""
    }
    s, _ := v.(string)
    return s
}

// internal/tools/args.go:14-33
func stringSliceArg(args map[string]any, key string) []string {
    ...
    case []any:
        values := make([]string, 0, len(x))
        for _, item := range x {
            if s, ok := item.(string); ok {
                values = append(values, s)
            }
        }
        return cleanStrings(values)
    ...
}

// internal/tools/web_common.go:77-87
func requiredString(args map[string]any, key string) (string, error) {
    v, ok := args[key]
    if !ok {
        return "", fmt.Errorf("%s is required", key)
    }
    s, ok := v.(string)
    if !ok || strings.TrimSpace(s) == "" {
        return "", fmt.Errorf("%s must be a non-empty string", key)
    }
    return strings.TrimSpace(s), nil
}
```

**JSON-marshaled result analog** (`internal/tools/scheduler.go:81-89`):

```go
result, err := t.runner.RunTaskNow(ctx, name)
if err != nil {
    return "", fmt.Errorf("run_task_now: %w", err)
}
data, err := json.MarshalIndent(result, "", "  ")
if err != nil {
    return "", fmt.Errorf("run_task_now: marshal result: %w", err)
}
return string(data), nil
```

**Conflict-as-tool-result vs error** — RESEARCH.md anti-pattern: "Surfacing the conflict as a tool ERROR (text string)". Conflict MUST be returned as a successful tool RESULT containing a JSON object (D-03), so the LLM parses it deterministically. RESEARCH.md §"Common Operation 2" lines 654-704 has the canonical sketch:

```go
if err := t.store.WritePage(ctx, page, a.ExpectedUpdatedAt); err != nil {
    var conflict *wiki.ConflictError
    if errors.As(err, &conflict) {
        // Tool RESULT (not tool ERROR) — LLM parses deterministically (D-03).
        payload, _ := json.Marshal(map[string]string{
            "error":               "conflict",
            "slug":                conflict.Slug,
            "expected_updated_at": conflict.Expected,
            "actual_updated_at":   conflict.Actual,
        })
        return string(payload), nil  // <-- nil error, structured result
    }
    return "", err
}
```

**`Definition()` with examples analog** (`internal/tools/tool_search.go:64-80` — to be deleted but worth referencing for the Definition shape):

```go
func (t *ToolSearchTool) Definition() ToolDefinition {
    return ToolDefinition{
        Name:        t.Name(),
        Description: t.Description(),
        Parameters:  t.Parameters(),
        Examples: []ToolCallExample{
            {
                Description: "Find tools for running shell diagnostics in the Aura container.",
                Arguments:   map[string]any{"query": "run shell command in container", "limit": 3},
            },
            ...
        },
    }
}
```

---

### `internal/tools/registry_search_vector.go` (RENAME-EXPORT)

**Closest analog:** itself. The change is mechanical — rename the type and all method receivers. Verify no external consumer. Per RESEARCH.md A4 (line 734): `Registry.ToolVectorHealth()` and `ToolVectorHealth` struct are ALREADY exported, so no external consumer reads `toolVectorIndex` directly.

**Existing struct + WR-04 mu-released-around-HTTP critical pattern** (`internal/tools/registry_search_vector.go:46-69` — verbatim, mu placement is Phase 1 carry-forward and MUST be preserved):

```go
type toolVectorIndex struct {
    qclient    qdrant.Client
    collection string
    cfg        ToolVectorConfig
    http       *http.Client

    // mu guards the in-memory Build/Search state below. It is held only
    // for fast in-memory mutations / reads, never around HTTP I/O (WR-04).
    mu          sync.RWMutex
    docCount    int
    lastRebuild time.Time
    lastError   error

    // buildMu serializes concurrent Build calls so they do not race each
    // other on the Qdrant collection (delete/create/upsert). Search does
    // NOT take buildMu — it only takes mu.RLock for the in-memory state,
    // which means a Search initiated mid-Build returns the previous
    // build's docCount/lastError snapshot and proceeds without blocking
    // on the Build's HTTP calls (WR-04).
    buildMu sync.Mutex

    logger *slog.Logger
}
```

**The publish/release pattern** (`internal/tools/registry_search_vector.go:118-141`) — this is the canonical "release mutex around HTTP" idiom for Phase 2 to copy:

```go
// Build (re)builds the Qdrant-backed tool index. WR-04: idx.mu is NOT held
// across the Qdrant/embedding HTTP calls. buildMu serializes concurrent
// Build calls; idx.mu is taken only briefly to publish the resulting
// in-memory state (docCount, lastRebuild, lastError). This means Search
// callers continue to see the previous snapshot during a long rebuild
// instead of blocking on the rebuild's HTTP round-trips.
func (idx *toolVectorIndex) Build(ctx context.Context, docs []toolVectorDoc) error {
    if idx == nil || idx.cfg.Backend == "fts" {
        return nil
    }
    // Serialize Builds so two callers do not race the delete/create/upsert
    // sequence on the same Qdrant collection.
    idx.buildMu.Lock()
    defer idx.buildMu.Unlock()

    // publish writes the build outcome to the shared in-memory state.
    // Used in defer-style for early returns and at the end on success.
    publish := func(docCount int, buildErr error) {
        idx.mu.Lock()
        idx.docCount = docCount
        idx.lastError = buildErr
        idx.lastRebuild = time.Now()
        idx.mu.Unlock()
    }
    ...
}
```

**Search WR-04 release point** (`internal/tools/registry_search_vector.go:223-276`) — Search takes `mu.RLock`, releases via `defer`, and the embedding HTTP call at line 234 happens UNDER the RLock. **NOTE:** this is a slight tension — `Search` reads docCount/lastError under RLock but then makes an HTTP call before releasing. The Build path is the strict WR-04 pattern; Search is a lighter read-locked path. Phase 2's per-turn `ToolsProvider` closure calls `Registry.Search(...)` (`internal/tools/registry_search.go:19-103`), which already does its own `r.mu.RLock(); ... r.mu.RUnlock()` cleanly at lines 39, 62 BEFORE the vector `Search()` HTTP call at line 76. **No change needed.**

**Embedding-text narrowing (D-24)** — `internal/tools/registry_search.go:122-141` `searchableToolText` currently embeds `name + description + tags + examples JSON + parameters JSON`:

```go
func searchableToolText(def ToolDefinition, tags []string) string {
    var b strings.Builder
    b.WriteString(def.Name)
    b.WriteByte(' ')
    b.WriteString(def.Description)
    for _, tag := range tags {
        b.WriteByte(' ')
        b.WriteString(tag)
    }
    if len(def.Examples) > 0 {
        data, _ := json.Marshal(def.Examples)
        b.WriteByte(' ')
        b.Write(data)
    }
    if len(def.Parameters) > 0 {
        data, _ := json.Marshal(def.Parameters)
        b.WriteByte(' ')
        b.Write(data)
    }
    return strings.ToLower(b.String())
}
```

**Phase 2 change (D-24):** narrow the EMBEDDING text only. Lex search keeps the full text. Two options the planner chooses between:
1. Add a sibling `searchableToolEmbeddingText(def, tags)` that returns only `name + " " + description`.
2. Keep `searchableToolText` for lex and write the embedding doc inline at `BuildVectorIndex` site (`registry.go:229-238`).

Option 2 is less code change but Option 1 is more discoverable. **Planner's call.**

**Collection invalidation (RESEARCH.md Open Question 2):** because the embedding TEXT changes between Phase 1 and Phase 2, the existing `aura_tool_search` Qdrant collection (line 76, line 524 of setup.go) will serve stale embeddings under the warm-cache short-circuit. Two fixes — Option (a): rename collection to `aura_tool_search_v2`. Option (b): force one-time delete on Phase 2 first boot. **Recommend (a)** — idempotent across restarts.

---

### `internal/agentloop/loop.go` (MODIFY — orchestration)

**Closest analog:** itself. The `ToolsProvider` hook ALREADY exists and ALREADY runs per-turn. No structural change required.

**Existing per-turn ToolsProvider invocation** (`internal/agentloop/loop.go:79-97` for the field + `:154-157` for the per-turn call — verbatim):

```go
// Field declaration at lines 79-97 inside Options:
type Options struct {
    MaxIterations           int
    MaxElapsed              time.Duration
    Tools                   []llm.ToolDefinition
    ToolsProvider           func() []llm.ToolDefinition
    ...
}
```

```go
// Per-turn call at lines 154-157:
tools := opts.Tools
if opts.ToolsProvider != nil {
    tools = opts.ToolsProvider()
}
```

**The `tier == tierSearch` branch referenced in CONTEXT.md D-27 (line 462) DOES NOT EXIST in the current `loop.go`** (verified — file is 408 lines; only one `tier`-prefixed reference at line 6 in a historical comment about "tiered iteration budgets" that is past-tense). RESEARCH.md is correct that the loop is small and stable. **Planner action:** mark D-27's "tier == tierSearch case in `loop.go:462`" as already removed in the loop history. The cleanup is purely in `internal/telegram/conversation.go` and `cmd/debug_telegram_sandbox/main.go`.

---

### `internal/telegram/setup.go` (MODIFY — wiring)

**Closest analog:** itself. Three surgical changes:

**1. Existing tool-vector index build at line 519-528** (`internal/telegram/setup.go:519-528` — verbatim):

```go
// Tool vector search index. Builds embeddings for all registered
// tools when TOOL_SEARCH_BACKEND is vector or hybrid.
toolRegistry.BuildVectorIndex(tools.ToolVectorConfig{
    Backend:      cfg.ToolSearchBackend,
    TopK:         cfg.ToolSearchTopK,
    QdrantURL:    cfg.QdrantURL,
    QdrantAPIKey: cfg.QdrantAPIKey,
    Collection:   "aura_tool_search",     // <-- consider "aura_tool_search_v2" for clean cutover
    EmbedBaseURL: cfg.EmbeddingBaseURL,
    EmbedAPIKey:  cfg.EmbeddingAPIKey,
    EmbedModel:   cfg.EmbeddingModel,
})
```

**2. Existing reindex synchronous callsite — analog ONLY (NOT a Phase 2 callsite per D-32 deferred caller exemption)**: `internal/ingest/pipeline.go:155, :180`:

```go
if err := p.wiki.WritePage(ctx, page); err != nil {
    return Result{}, fmt.Errorf("ingest: write page: %w", err)
}
...
if p.search != nil {
    if err := p.search.ReindexWikiPage(ctx, slug); err != nil {
        p.logger.Warn("ingest: reindex failed; page is still readable", "slug", slug, "err", err)
    }
}
```

D-15 says the new `internal/reindex/` worker calls `search.WikiPageReindexer.ReindexWikiPage(ctx, slug)` — same surface. **Phase 2 wires the worker into `wiki.Store` (via Submitter constructor injection) but leaves `internal/ingest/pipeline.go` untouched per D-05/D-32 deferred-caller exemption.**

**3. tool_search registration to DELETE** (`internal/telegram/setup.go:744-748`):

```go
if pdfTool := tools.NewCreatePDFTool(sourceStore, b); pdfTool != nil {
    toolRegistry.Register(pdfTool)
}
if tool := tools.NewToolSearchTool(toolRegistry); tool != nil {  // <-- DELETE these 3 lines
    toolRegistry.Register(tool)
}
```

**LLM-client-with-retry construction analog** (`internal/telegram/setup.go:792-805`) — to verify `NewRetryClient` constructor is preserved:

```go
func createLLMClient(cfg *config.Config, logger *slog.Logger) llm.Client {
    _ = logger
    openaiClient := llm.NewOpenAIClient(llm.OpenAIConfig{
        APIKey:  cfg.LLMAPIKey,
        BaseURL: cfg.LLMBaseURL,
        Model:   cfg.LLMModel,
    })
    return llm.NewRetryClient(openaiClient, llm.RetryConfig{
        MaxRetries: cfg.LLMMaxRetries,
        BaseDelay:  time.Second,
        MaxDelay:   30 * time.Second,
    })
}
```

**Phase 2 expansion** — populate the new `RetryConfig` fields:

```go
return llm.NewRetryClient(openaiClient, llm.RetryConfig{
    MaxRetries:          cfg.LLMMaxRetries,
    BaseDelay:           time.Second,
    MaxDelay:            30 * time.Second,
    MaxContentRetries:   3,                                // D-07 CONTENT
    ContentTemperatures: []float64{0.0, 0.3, 0.7},        // D-07 staircase
    JitterRatio:         0.5,                              // D-07 jitter
})
```

---

### `internal/telegram/conversation.go` (MODIFY — wiring)

**Closest analog:** itself — `internal/telegram/conversation.go:282-333` already contains the closure pattern Phase 2 needs to replace.

**Existing closure pattern that gets REPLACED** (`internal/telegram/conversation.go:282-333`):

```go
var toolMu sync.Mutex
activeToolNames := append([]string(nil), toolAllowlist...)
currentToolNames := func() []string {
    toolMu.Lock()
    defer toolMu.Unlock()
    return append([]string(nil), activeToolNames...)
}
currentToolDefs := func() []llm.ToolDefinition {
    names := currentToolNames()
    return orderToolDefinitionsForAllowlist(b.tools.DefinitionsFor(names), names)
}
maxCallsPerTool := map[string]int{
    "search_memory": 2,
    "tool_search":   2,                                              // <-- DELETE this entry (D-27)
}
duplicatePolicy := agentloop.DuplicateOrMaxCallsPolicy(maxCallsPerTool, nil)
addActiveTools := func(names []string) {
    toolMu.Lock()
    defer toolMu.Unlock()
    activeToolNames = appendUniqueStrings(activeToolNames, names...)
}
baseStats := turnStats{
    promptVersion:           promptPlan.Version,
    promptModules:           append([]string(nil), promptPlan.Modules...),
    promptHash:              promptPlan.Hash,
    toolset:                 "registered",
    toolsetSelectReason:     "core tools plus tool_search discoveries",   // <-- update string (D-27)
    retrievalCapsulePresent: retrievalCapsulePresent,
}
```

**Phase 2 replacement (per RESEARCH.md §"Pattern 4" lines 440-462):**

```go
alwaysOnCore := []string{
    "write_wiki_page", "search_memory", "list_sources", "read_source",
    "schedule_task", "request_dashboard_token",
}
toolsProvider := func() []llm.ToolDefinition {
    coreDefs := b.tools.DefinitionsFor(alwaysOnCore)
    latestUserMsg := convCtx.LatestUserMessageText() // returns "" on cold start (Open Question 1)
    if strings.TrimSpace(latestUserMsg) == "" {
        return coreDefs // cold-start (D-23): core only (Pitfall #6)
    }
    retrieved := b.tools.Search(latestUserMsg, 5, alwaysOnCore...)
    if retrieved == nil {  // Qdrant down or index empty (D-25)
        b.logger.Warn("tools_provider_fallback", "reason", "qdrant_down")
        return b.tools.Definitions() // FULL toolset
    }
    out := append([]llm.ToolDefinition(nil), coreDefs...)
    for _, r := range retrieved {
        out = append(out, b.tools.DefinitionsFor([]string{r.Name})...)
    }
    return out
}
```

**Open Question A2 (RESEARCH.md line 732):** `convCtx.LatestUserMessageText()` does NOT exist today. The `Messages()` accessor does (`internal/conversation/context.go:250-264`). **Planner action:** add a small accessor on `*conversation.Context`:

```go
// LatestUserMessageText walks backwards over Messages() and returns the last
// user-role content. Returns "" on cold-start (no user turn yet).
func (c *Context) LatestUserMessageText() string {
    msgs := c.Messages()
    for i := len(msgs) - 1; i >= 0; i-- {
        if msgs[i].Role == "user" {
            return msgs[i].Content
        }
    }
    return ""
}
```

**Existing system-prompt content with "tool_search" mention to clean** (`internal/telegram/conversation.go:259`):

```go
content += fmt.Sprintf("\n\n## Aura Runtime\n- Prompt Version: %s\n- Tool Surface: core tools plus tool_search discoveries\n\nChoose tools autonomously when they help. When a needed capability is not visible, call tool_search with a natural-language capability description, then use only tools returned in that search. ...", version)
```

**Phase 2 cleanup:** remove the "tool_search" mentions; replace with a sentence like "Tools are auto-injected each turn based on your latest message — call any tool by name; you never need to discover them."

---

## Shared Patterns

### Go-Stdlib-Only Concurrency (RESEARCH.md "Don't Hand-Roll" + REQUIREMENTS.md "Out of Scope")
**Source:** `internal/conversation/archive.go:332-385` (BufferedAppender), `internal/tools/registry_search_vector.go:46-69` (mu/buildMu split)
**Apply to:** `internal/reindex/worker.go`, any new concurrency code in Phase 2
**Rule:** `context.Context` + `sync.Mutex` + `sync.RWMutex` + `sync/atomic` + buffered chan. NO worker pools, NO third-party retry libraries.

### `fmt.Errorf("...: %w", err)` Wrapping
**Source:** `internal/wiki/store.go:104, :161, :183, :189, :196, :202` and `internal/tools/auth.go:67-70, :74-80`
**Apply to:** ALL new error returns in Phase 2 files
**Pattern:** Always wrap with `%w` and prefix with the function/operation name.

```go
// internal/wiki/store.go:104
return nil, fmt.Errorf("creating wiki dir: %w", err)

// internal/tools/auth.go:69
return "", fmt.Errorf("request_dashboard_token: issue: %w", err)
```

### Sentinel Error + `errors.Is/As`
**Source:** `internal/wiki/schema.go:34-41` (`ValidationError`), `internal/conversation/archive.go:387-395` (`ErrDuplicateTurn` use)
**Apply to:** `internal/llm/classify.go` (sentinels for content failures), `internal/wiki/store_writes.go` (`ConflictError`)
**Rule:** Define sentinel as `var ErrX = errors.New(...)` for sentinel-only matches; define a struct error with fields for caller-actionable details (slug, expected, actual).

### Per-Slug Mutex (TOCTOU Prevention)
**Source:** `internal/wiki/store.go:137-140`
**Apply to:** ETag check in `internal/wiki/store_writes.go` MUST be inside this critical section (Pitfall #1 + RESEARCH.md §"Pattern 2")

### WR-04 Mu-Released-Around-HTTP
**Source:** `internal/tools/registry_search_vector.go:118-141` (Build with `publish` helper) and `:51-66` (split mu vs buildMu doc-comment)
**Apply to:** Any future code that combines in-memory state mutation with HTTP I/O. NOT directly applicable in Phase 2 — `internal/reindex/worker.go` does not use a mutex around HTTP because it has a single drain goroutine; Pitfall #2 is the relevant one there (dedicated ctx).

### `slog`-Based Structured Logging with `arg_keys` Privacy
**Source:** `internal/tools/registry.go:194` (`r.logger.Info("tool started", "tool", name, "arg_keys", argKeys(args))`) and `internal/wiki/store.go:205, :213`
**Apply to:** All logging in `internal/llm/classify.go`, `internal/reindex/worker.go`, `internal/tools/wiki.go`
**Rule:** NEVER log argument values. Log keys only. The classifier's `cleaned` message (after value-pattern redaction) is safe.

```go
// internal/tools/registry.go:194
r.logger.Info("tool started", "tool", name, "arg_keys", argKeys(args))
```

### Tool Interface Implementation
**Source:** `internal/tools/registry.go:16-21` (interface), `internal/tools/auth.go:43-83` (clean impl)
**Apply to:** `internal/tools/wiki.go`
**Required methods:**
- `Name() string` — fixed string identifier
- `Description() string` — LLM-facing prose; encodes the contract (D-04)
- `Parameters() map[string]any` — JSON Schema fragment with `"type":"object"`, `"required"`, optionally `"additionalProperties":false`
- `Execute(ctx context.Context, args map[string]any) (string, error)` — return JSON string on success, error on failure (NOT for conflict — see D-03)
- Optional: `Definition() ToolDefinition` (provider) for examples

### Constructor Returns nil When Deps Missing
**Source:** `internal/tools/auth.go:36-41`
**Apply to:** `tools.NewWriteWikiPageTool`, `reindex.NewWorker`
**Rule:** If a critical dependency (store, submitter, registry) is missing, return nil. Caller (`setup.go`) checks `if tool := X(); tool != nil { register }`.

```go
func NewRequestDashboardTokenTool(store auth.TokenWriter, sender TokenSender, allowlist AllowlistFunc) *RequestDashboardTokenTool {
    if store == nil || sender == nil || allowlist == nil {
        return nil
    }
    return &RequestDashboardTokenTool{store: store, sender: sender, allowlist: allowlist}
}
```

### Test Convention (`go test -race`, table-driven, `t.Helper()`)
**Source:** `internal/wiki/store_test.go:15-82` and `internal/conversation/archive_test.go:1-74`
**Apply to:** All `_test.go` files in Phase 2

```go
// internal/wiki/store_test.go:15-24
func newTestStore(t *testing.T) (*Store, string) {
    t.Helper()
    dir := t.TempDir()
    logger := slog.Default()
    store, err := NewStore(dir, logger)
    if err != nil {
        t.Fatalf("NewStore failed: %v", err)
    }
    return store, dir
}
```

```go
// internal/conversation/archive_test.go:15-25 — interface assertion idiom
var (
    _ conversation.TurnAppender        = (*conversation.ArchiveStore)(nil)
    _ conversation.ChatTurnReader      = (*conversation.ArchiveStore)(nil)
    ...
)
```

**Apply to:**
- `reindex.Submitter` and `search.WikiPageReindexer` interface assertions in `worker_test.go`.
- `wiki.PageReader+PageWriter` interface assertion in `wiki_test.go`.

### Always-Required CLAUDE.md Post-Edit Validation
**Source:** CLAUDE.md "Post-Edit Validation" section
**Apply to:** Every file edit in Phase 2
**Commands:**
```
go vet ./...
go build ./...
go test -race ./internal/{llm,reindex,tools,wiki}/
```

---

## Files With No Direct Analog (Pure Greenfield)

| File | Role | Data Flow | Reason | Recommended Public Idiom |
|------|------|-----------|--------|--------------------------|
| `internal/llm/classify.go` (the `Classify(error) (Bucket, bool, string)` function itself) | classifier | transform | No in-tree error-classifier exists. Closest is the `health.SanitizeHandler` which only acts on attribute keys. | Hermes-style priority pipeline (sentinel → `errors.As(*APIError)` → `errors.As(net.Error)` → message-pattern). RESEARCH.md §"Common Operation 1" lines 565-641 has the canonical sketch verified against `D:\tmp\hermes-agent\agent\error_classifier.py`. |
| `internal/llm/classify.go` value-pattern redactor | utility | transform | `internal/health/sanitize.go` only redacts attribute keys. No in-tree value redactor. | RESEARCH.md §"Common Operation 1" lines 631-641 panel: URL token, Bearer, basic-auth URL, base64 ≥ threshold, Authorization header, OpenRouter `sk-or-v1-…`, JWT. Build a regex panel in `redact.go` and seed-test in `redact_test.go`. |
| `internal/wiki/store_writes.go` `readPageLocked` helper | helper | file I/O (no mutex) | Existing `ReadPage` (`store.go:219-240`) uses `normalizeSlugInput` and reads from disk WITHOUT taking `fileMutex`. Phase 2 needs a sibling that the caller has guaranteed already holds `fileMutex(slug)`. | Copy the body of `ReadPage` lines 221-239, drop the slug normalization (caller already has the canonical slug from `Slug(page.Title)`), keep the file-read + parse logic. Document explicitly that the function assumes the mutex is held. |
| `internal/conversation/context.go` `LatestUserMessageText()` accessor | helper | read | `Messages()` exists at `:250-264`; reverse-walk does not. RESEARCH.md Open Question A2. | One-liner: walk `Messages()` backwards, return first `Role == "user"` content. |

---

## go.mod Update (D-21)

**File:** `go.mod`
**Current state** (`go.mod` line 15 — verified by Grep):

```
github.com/go-git/go-git/v5 v5.18.0
```

**Change:**

```
github.com/go-git/go-git/v5 v5.19.0
```

**Validation:** `go get github.com/go-git/go-git/v5@v5.19.0 && go mod tidy && go vet ./... && go build ./... && go test -race ./internal/wiki/`

**Affected callsites** (all in `internal/wiki/store.go`):
- Line 15-16: imports
- Line 120: `git.PlainOpen`
- Line 124: `git.ErrRepositoryNotExists`
- Line 128: `git.PlainInit`
- Line 355: `git.PlainOpen`
- Line 374-378: `wt.Commit(msg, &git.CommitOptions{...})`

Per RESEARCH.md A1 (line 731) v5.19.0 has no breaking API changes affecting these surfaces.

---

## Cross-Reference Index for Planner

| RESEARCH.md anchor | This document's section | Verbatim excerpt? |
|--------------------|-------------------------|-------------------|
| `internal/wiki/store.go:160-217` (WritePage) | `store_writes.go` | YES (`store.go:158-217`) |
| `internal/wiki/store.go:351-388` (gitCommit) | `store_writes.go` | YES (`store.go:351-388`) |
| `internal/wiki/store.go:137-140` (fileMutex) | Shared Patterns: Per-Slug Mutex | YES (`store.go:137-140`) |
| `internal/wiki/schema.go:17-32` (Page struct) | `schema.go` | YES (`schema.go:17-32`) |
| `internal/tools/registry_search_vector.go` (toolVectorIndex + WR-04) | `registry_search_vector.go` | YES (`registry_search_vector.go:46-69, :118-141`) |
| `internal/tools/registry_search.go:19-103` (Search lex+vector merge) | `registry_search_vector.go` shared section | partial (called out, not duplicated) |
| `internal/agentloop/loop.go:83,154-157` (per-turn ToolsProvider) | `agentloop/loop.go` | YES (`loop.go:79-97, :154-157`) |
| `internal/search/search.go:54-57,444-460` (WikiPageReindexer) | `reindex/types.go` | YES (`search.go:42-64`) |
| `internal/conversation/archive.go:332-385` (BufferedAppender) | `reindex/worker.go` | YES (`archive.go:332-385`) |

---

## Metadata

**Analog search scope:** `internal/{llm,wiki,tools,reindex(N/A),agentloop,telegram,conversation,search,health,logging,ingest}/`
**Files Read for analog extraction:** 18 (including 02-CONTEXT.md, 02-RESEARCH.md partial, schema.go, store.go, retry.go, registry.go, registry_search.go, registry_search_vector.go, registry_search_vector_test.go, archive.go, archive_test.go, store_test.go, loop.go, conversation.go (slice), setup.go (two slices), bot.go (slice), parser.go, openai.go (slice), search.go (two slices), pipeline.go (slice), context.go (slice), tool_search.go, auth.go, scheduler.go (slice), args.go, web_common.go (slice), definition.go, sanitize.go)
**Pattern extraction date:** 2026-05-10
**Mapper constraint compliance:** read-only (no source files modified); Write tool used only for this PATTERNS.md.
