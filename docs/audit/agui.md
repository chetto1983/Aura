# Audit: internal/agui

**Verdict:** needs-work — three confirmed issues (one silent-error-drop type contract, one concurrent-write race on Fanout.subs, two not-wired exported symbols) plus one dead constant alias; no critical severity.

**Counts:** critical 0 / high 1 / medium 2 / low 2

---

## Findings

### [HIGH][RACE] Fanout.Subscribe concurrent calls race on f.subs

**Location:** `internal/agui/fanout.go:51-57`  
**Confidence:** high

`Subscribe()` appends to `f.subs` (an unprotected `[]chan events.Event`) without any mutex or synchronization. The only guard is the `started.Load()` check (lines 52-54), which prevents a Subscribe-after-Run call but does nothing to serialize concurrent Subscribe calls before Run. Two goroutines calling `Subscribe` simultaneously before `Run` is a data race on the slice header (len/cap) and the backing array during growth.

The doc comment says "Subscribe MUST be called before Run" but makes no single-goroutine requirement explicit. The exported `Fanout` API presents no concurrency contract.

In current production (Telegram `agui_subscriber.go` lines 73-75), all three calls are sequential on the handler goroutine, so the race is not triggered today. But the public type contract is unsafe.

**Suggested fix:** Add a `mu sync.Mutex` field to `Fanout`, lock it in `Subscribe` around the `f.subs = append(...)` line, and document that Subscribe is safe for concurrent use before Run. Alternatively, document the single-goroutine-only requirement explicitly in the type doc.

---

### [MEDIUM][BUG] Fanout silently drops errors from its iter.Seq2 source

**Location:** `internal/agui/fanout.go:74`  
**Confidence:** high

`f.source` is typed `iter.Seq2[events.Event, error]`, but the producer goroutine iterates with the single-value form:

```go
for ev := range f.source {
```

In Go 1.22+, ranging an `iter.Seq2` with one variable silently drops the second value (here: the error). Any (nil, err) pair from the source yields a nil `ev` that is then passed to `send()` where `ev.Type()` is called — a nil-interface dereference panic.

The comment (line 31-33) explains the intent: "Errors from the wrapped source are already mapped to a RUN_ERROR event by Translate (the translated stream is pure events.Event), so Fanout never inspects the error slot." This is true for all current callers (`Translate` never yields a non-nil error in the second slot), making this safe in practice today.

However: (a) the type signature of `NewFanout` accepts any `iter.Seq2[events.Event, error]`, not just `Translate` output; (b) if someone passes a source that does yield `(nil, err)`, the nil event causes a panic at `ev.Type()` inside the goroutine body (line 79); (c) future callers may inadvertently pass an error-yielding source.

**Suggested fix — option A (correct the type):** Change `Fanout.source` and `NewFanout` to accept `iter.Seq[events.Event]` (single-value iterator). `Translate`'s `iter.Seq2` output is adapted at the call site by the existing `streamSSE` pattern or by adding a small adapter. This makes the contract explicit.

**Suggested fix — option B (guard the nil):** Keep the type, add the two-value range `for ev, _ := range f.source`, and add a nil guard before `ev.Type()`:

```go
for ev, _ := range f.source {
    if ev == nil {
        continue
    }
    ...
```

---

### [MEDIUM][NOT-WIRED] agui.Subscribe exported but has zero production callers

**Location:** `internal/agui/client.go:38-42`  
**Confidence:** high (verified with repo-wide grep)

`Subscribe` is exported and documented as "the single-consumer convenience" over `NewFanout` + `Subscribe()` + `Run()`. The intended consumer (Phase-13 Telegram channel) instead calls the three primitives directly (`agui.Translate` → `agui.NewFanout` → `fo.Subscribe()` × 3 → `fo.Run()`) in `internal/channels/telegram/agui_subscriber.go:71-86`.

Repo-wide search finds zero non-test, non-definition references to `agui.Subscribe` outside the package (only `fanout_test.go:282` uses it in a test that proves the alias `<-chan Event` type-pins correctly — the test has an independent purpose). The production path never goes through this function.

**Suggested fix:** Either wire `agui.Subscribe` into at least one production callsite (e.g., re-route the single-subscriber case in `agui_subscriber.go`) or unexport it (`subscribe`) and keep it as an internal helper for tests. If the three-subscriber Telegram pattern is the only current consumer, remove the single-subscriber convenience from the exported API surface to reduce cognitive load.

---

### [LOW][DEAD-CODE] agui.EventType type alias is unreferenced outside the package

**Location:** `internal/agui/client.go:21`  
**Confidence:** high (verified with repo-wide grep)

```go
type EventType = events.EventType
```

This re-export is documented as letting "a consumer switch on event kind without importing the SDK." No external package uses `agui.EventType`. The Telegram channel (`bot_test.go:21`, `artifact.go`) references `events.EventType` directly via the `github.com/ag-ui-protocol/ag-ui/...` import.

The companion `Event = events.Event` alias IS referenced externally (the `Subscribe` return type uses it, though the external callers use `events.Event` directly too — they just happen to be type-compatible aliases). `EventType` is strictly dead in the public API.

**Suggested fix:** Remove the `EventType` alias. If a future consumer needs it, add it then (YAGNI). This is a one-line deletion.

---

### [LOW][DEAD-CODE] artifactEventName internal alias is a redundant indirection

**Location:** `internal/agui/translator.go:25`  
**Confidence:** high

```go
const artifactEventName = ArtifactEventName
```

This unexported alias was added (per `13-06-SUMMARY.md`) to keep the translator body byte-identical after `ArtifactEventName` was exported. Both the translator body (line 110) and `translator_artifact_test.go:96` reference `artifactEventName`, which in turn points at `ArtifactEventName`. The indirection adds no behavior and a future reader must chase the alias to understand the actual value.

**Suggested fix:** Remove `artifactEventName` and replace its two uses with `ArtifactEventName` directly. The translator body and tests remain correct; golden comparison tests are unaffected since the value is identical.

---

## What was checked (completeness notes)

- All five non-test source files read line-by-line: `types.go`, `client.go`, `fanout.go`, `server.go`, `translator.go`.
- All test files read to understand intended contracts: `fanout_test.go`, `server_test.go`, `server_integration_test.go`, `translator_test.go`, `translator_reasoning_test.go`, `translator_artifact_test.go`, `helpers_test.go`, `main_test.go`.
- Repo-wide grep confirmed usage of every exported symbol: `agui.Subscribe`, `agui.EventType`, `agui.ErrEmptyThreadID`, `agui.ErrNoMessages`, `agui.ValidateRunInput`, `agui.NewServer`, `agui.NewFanout`, `agui.Translate`, `agui.ArtifactEventName`, `agui.NewIDGenerator`, `agui.ServerConfig`.
- `go vet ./internal/agui/` and `go build ./internal/agui/` confirmed clean.
- `go test -race ./internal/agui/` passes (sequential Subscribe pattern in tests doesn't trigger the concurrent-subscribe race).
- `for ev := range Seq2` single-variable behavior confirmed with a standalone Go program: errors are silently dropped, nil events pass through.
- `sanitizeString` regex behavior verified: DSN, userinfo, bearer/api_key/token redaction works correctly; minor gap for user-only URLs (no password) is expected.
- Goroutine lifecycle: `streamSSE` producer is correctly drained by context cancellation (HTTP server cancels ctx on client disconnect per documented Go behavior). No goroutine leak.
- No nil-pointer risks in the production call paths (Translate never yields nil events; Actions is a value type, not a pointer).
- Go module version: go 1.26.4 — loopvar capture fix is in place; no loop-capture findings.
