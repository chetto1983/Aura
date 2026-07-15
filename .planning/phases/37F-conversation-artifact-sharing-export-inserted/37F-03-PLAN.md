---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 03
type: tdd
wave: 2
depends_on: ["37F-01"]
files_modified:
  - internal/share/snapshot.go
  - internal/share/redact.go
  - internal/share/snapshot_test.go
  - internal/share/redact_test.go
autonomous: true
requirements: [WEBSHARE-03]

must_haves:
  truths:
    - "BuildSnapshot is the ONLY function in the repo that accepts an llm.Message and returns share-bound data"
    - "A hostile history carrying send_file args {\"path\":\"/abs/secret/results.xlsx\"} produces a Snapshot containing no substring of that path"
    - "A shell_exec tool result containing /etc/passwd never reaches the Snapshot — role=tool turns are dropped entirely"
    - "Tool NAMES survive per turn; tool ARGUMENTS never appear in any field"
    - "The Snapshot type has no field capable of holding args, results, or a filesystem path — the leak is a compile error, not a review miss"
    - "The owner's identity id appears nowhere in the Snapshot"
    - "A turn with ContentSidecarPath set leaks no sidecar path"
  artifacts:
    - path: "internal/share/snapshot.go"
      provides: "Snapshot / SnapshotTurn / SnapshotArtifact types + BuildSnapshot — THE redaction point"
      exports: ["Snapshot", "SnapshotTurn", "SnapshotArtifact", "BuildSnapshot"]
      min_lines: 80
    - path: "internal/share/redact.go"
      provides: "allowlist projections: turn projection, tool-name extraction, artifact descriptor allowlist"
      min_lines: 60
    - path: "internal/share/snapshot_test.go"
      provides: "hostile-fixture SC3 tests"
      min_lines: 120
  key_links:
    - from: "internal/share/snapshot.go"
      to: "internal/llm.Message"
      via: "BuildSnapshot consumes []llm.Message and projects field-by-field"
      pattern: "llm\\.Message"
  prohibitions:
    - "MUST NOT model redact.go on internal/agui/server_redact.go SanitizeString — that is a regex DENYLIST and an explicit ANTI-ANALOG; SC3 mandates an allowlist projection"
    - "MUST NOT build any Snapshot field by copying a struct/map and deleting keys — construct field-by-field only"
    - "MUST NOT json.Unmarshal an artifact descriptor into map[string]any and delete(m, \"path\")"
    - "MUST NOT add an Arguments, Args, Result, Path, SidecarPath, IdentityID, ToolCallID or ID field to Snapshot/SnapshotTurn/SnapshotArtifact"
    - "MUST NOT let role='tool' or role='system' turns reach SnapshotTurn — only 'user' and 'assistant'"
    - "MUST NOT read aura.tool_invocations (args_raw / result_preview / result_sidecar_path / error / meta) from this package"
    - "MUST NOT consume []conversations.Turn — consume []llm.Message, which structurally has no ContentSidecarPath field"
    - "MUST NOT carry reasoning/thinking traces (D-08 amended: DROPPED — they are never persisted)"
    - "MUST NOT add a build tag to any test file in this plan"
---

<objective>
Build the SC3 redaction core: the `Snapshot` model and the single `BuildSnapshot` constructor that is
the phase's one security boundary.

WEBSHARE-03 says *"no host/container path and no other identity's data reach a recipient."* This plan
is where that becomes structurally true. The design rule is **allowlist, never denylist**: `Snapshot`
has no field able to hold arguments, results, or a path, so a leak is a **compile error rather than a
review miss**. Everything downstream (Markdown, JSON, the public page) is a pure function of this type,
which is why redaction cannot diverge across surfaces (D-07).

The RED test is a hostile fixture. This plan is the mutation-testing non-negotiable of the phase —
a surviving mutant in `redact.go` is a live leak.

Purpose: make the 9 verified leak sources (L-01..L-09) unrepresentable.
Output: `internal/share/snapshot.go` + `internal/share/redact.go`, both fully unit-tested with no DB,
no Garage, and no build tag.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-PATTERNS.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md
@CLAUDE.md
</context>

## Artifacts this plan produces

`internal/share` (new package), `share.Snapshot`, `share.SnapshotTurn`, `share.SnapshotArtifact`,
`share.BuildSnapshot`.

<feature>
  <name>The canonical redacted conversation snapshot</name>
  <files>internal/share/snapshot.go, internal/share/redact.go, internal/share/snapshot_test.go, internal/share/redact_test.go</files>

  <behavior>
    Given `[]llm.Message` (from `conversations.Store.LoadHistory`) plus conversation metadata and an
    artifact list, `BuildSnapshot` returns a `Snapshot` such that:

    **Turn projection**
    - A `role="user"` message ⇒ one `SnapshotTurn{Role:"user", Text:<Content>}`
    - A `role="assistant"` message ⇒ one `SnapshotTurn{Role:"assistant", Text:<Content>}`
    - A `role="tool"` message ⇒ **NO turn** (L-02: the raw tool result is dropped entirely)
    - A `role="system"` message ⇒ **NO turn**
    - Any other/unknown role ⇒ **NO turn** (fail-closed: an unrecognized role never falls through)
    - `Seq` is the 0-based index **within the emitted turns**, dense and gap-free — it must not leak how
      many turns were dropped
    - An assistant message whose `Content` is empty but which carries `ToolCalls` ⇒ a turn IS emitted
      with `Text:""` and `ToolNames` populated (the provenance is the point)

    **Tool provenance (D-08)**
    - `ToolCalls[].Function.Name` ⇒ `SnapshotTurn.ToolNames` in call order
    - `ToolCalls[].Function.Arguments` ⇒ **never** appears anywhere in the Snapshot (L-01)
    - `ToolCalls[].ID` and `Message.ToolCallID` ⇒ **never** appear (L-09)
    - Duplicate tool names in one turn are preserved in order (a turn that called `send_file` twice
      reports it twice — collapsing would misreport provenance)
    - An empty/whitespace tool name is dropped
    - No tool calls ⇒ `ToolNames` is nil/empty and omitted from JSON

    **Artifact allowlist (L-03)**
    - A `SnapshotArtifact` is constructed from exactly four allowlisted fields: `AssetID`, `FileName`,
      `MIMEType`, `SizeBytes`
    - A source artifact carrying `path` (or any other key) contributes **nothing** beyond those four

    **Identity + path suppression**
    - The owner's identity id appears in no field (L-08)
    - No `ContentSidecarPath` is reachable: the input is `[]llm.Message`, which has no such field (L-07)

    **Idempotence**
    - Projecting an already-projected turn set is a fixpoint: a second pass finds nothing to strip

    **Hostile fixture (the SC3 RED test)**
    A history containing: a `send_file` tool call whose `Arguments` are
    `{"path":"/abs/secret/results.xlsx"}`; a `shell_exec` tool call whose `Arguments` carry
    `/etc/passwd`; a `role="tool"` message whose `Content` is raw stdout containing `/etc/passwd`,
    a container id, and `$AURA_RUN_DIR/conversations/x/3.content`; an assistant turn with normal prose.
    Assert the marshalled Snapshot contains **none** of: `/abs/`, `/etc/`, `AURA_RUN_DIR`, the container
    id, the argument JSON, the owner identity id, or any `ToolCallID`. Assert it **does** contain the
    assistant prose and the tool names `send_file` and `shell_exec`.
  </behavior>

  <implementation>
    RED → GREEN → REFACTOR, one atomic commit per phase.

    **RED** — write `snapshot_test.go` + `redact_test.go` first, with the hostile fixture above as the
    centrepiece. Run them; they MUST fail (the package does not exist yet). Commit:
    `test(37F-03): add failing hostile-fixture tests for the share snapshot redaction core`

    **GREEN** — create the package.

    `snapshot.go` defines exactly the RESEARCH OQ4 shape — treat these json tags as a **wire contract**
    (plan 37F-05 mirrors them in TypeScript; plan 37F-06's adapters and the public page all consume
    them, so a tag rename here is a cross-surface break):
    `Snapshot{SchemaVersion int json:"schema_version"; Title string json:"title"; Model string
    json:"model"; CreatedAt time.Time json:"created_at"; SnapshotAt time.Time json:"snapshot_at";
    Turns []SnapshotTurn json:"turns"; Artifacts []SnapshotArtifact json:"artifacts"}`;
    `SnapshotTurn{Seq int json:"seq"; Role string json:"role"; Text string json:"text";
    ToolNames []string json:"tool_names,omitempty"}`;
    `SnapshotArtifact{AssetID string json:"asset_id"; FileName string json:"filename";
    MIMEType string json:"mime_type"; SizeBytes int64 json:"size_bytes"}`.
    `SchemaVersion` is 1.

    `BuildSnapshot(conv ConvMeta, msgs []llm.Message, artifacts []ArtifactMeta) (Snapshot, error)` —
    define `ConvMeta` and `ArtifactMeta` as small local input structs in this package carrying ONLY what
    the Snapshot needs (title, model, created-at; asset id, filename, mime, size). Do **not** accept
    `conversations.Conversation` — it carries `IdentityID` (L-08), and accepting it would put the leak
    back within reach. This keeps `internal/share` free of a `conversations` import.

    `redact.go` holds the projections: `projectTurns([]llm.Message) []SnapshotTurn`,
    `toolNames([]llm.ToolCall) []string`, `projectArtifact(ArtifactMeta) SnapshotArtifact`. Each
    CONSTRUCTS its output field-by-field. Port the **technique** from `web/src/chat/sseAdapter.ts:353-361`
    — the repo's only allowlist projection — which builds the 4-key artifact object literal rather than
    copying and deleting.

    Doc discipline, both files:
    - `Snapshot`'s doc states the inverse of `audit_store.go:27-38`'s projection-struct doc: **this type
      has no field able to hold args/results/paths, so the leak is a compile error, not a review miss.**
    - `redact.go`'s header states the trust boundary, inverting `sseAdapter.ts:341-345`: the 37A strip
      runs in the browser because the browser is the owner's own session; here **the recipient's browser
      is NOT a trust boundary — this projection IS the boundary** (R-09).
    - Cite `internal/agui/server_redact.go:52` `SanitizeString` **by name as the anti-analog**: it is a
      regex denylist, correct for wire-error credential scrubbing and wrong for SC3 (a denylist misses
      `/etc/passwd` in a shell result and every path shape nobody thought of). Naming it in the header is
      what stops a later reviewer from "helpfully" refactoring `redact.go` into a regex pass.

    Run the tests; they MUST pass. Commit:
    `feat(37F-03): implement the allowlist share snapshot projection`

    **REFACTOR** (only if needed) — fold duplication, keep both files ≤600 LOC. Tests stay green.
    Commit: `refactor(37F-03): …`

    Every test in this plan is a plain unit test: **no build tag, no DB, no Garage, no network**.
    That is what keeps this package inside the two-tag coverage gate.
  </implementation>
</feature>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: RED — hostile-fixture tests for BuildSnapshot and the projections</name>
  <read_first>
    - `internal/llm/client.go:24-41` — `Message{Role, Content, ToolCalls, ToolCallID, Name}` and `ToolCall{ID, Type, Function{Name, Arguments}}`. `Function.Arguments` is L-01, the hard leak. This is the exact input shape to build fixtures against.
    - `internal/agent/tools/send_file.go:57,138,173,186,200-201` — the real leak shapes: the `{"path":"/abs/…"}` argument, the `deliverFromBox` container staging path, the `cannot read %q: %v` error, and `descriptor["path"] = path`. Build the fixture from these, not from invented strings.
    - `internal/conversations/store.go:118-122` — `Turn.ContentSidecarPath` (`$AURA_RUN_DIR/conversations/<id>/<seq>.content`), L-07. Confirm for yourself that `llm.Message` has no such field — that absence is the mitigation.
    - `web/src/chat/sseAdapter.ts:341-362` — the 4-key allowlist and its trust-boundary comment; the technique to port
    - `internal/objectstore/objectstore_test.go:12-22` — `TestAssetKeyContainsNoFilename`: the house negative-substring invariant style (exact-equality assert THEN a forbidden-substring loop). Mirror this shape.
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §"Redaction Inventory (SC3)" — all 9 rows, L-01..L-09
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md` — the exact test names required
  </read_first>
  <action>
    Create `internal/share/snapshot_test.go` and `internal/share/redact_test.go`, package `share`, **no
    build tag**.

    Write these tests, using the exact names from VALIDATION.md's Requirements → Test Map:
    - `TestSnapshotRedactsHostPaths` — the full hostile fixture. Build a `[]llm.Message` containing a
      `send_file` tool call with `Arguments` = `{"path":"/abs/secret/results.xlsx"}`, a `shell_exec` tool
      call with `/etc/passwd` in its args, a `role="tool"` message whose `Content` is raw stdout with
      `/etc/passwd` + a container id + an `$AURA_RUN_DIR/conversations/…/3.content` path, and a normal
      assistant turn. `json.Marshal` the resulting Snapshot and assert with a forbidden-substring loop
      over `{"/abs/", "/etc/", "AURA_RUN_DIR", "results.xlsx", <container id>, <owner identity id>}` that
      none survive. Then assert the assistant prose DOES survive (a projection that returns an empty
      Snapshot would otherwise pass a pure negative test — this is the mutant that must not live).
    - `TestSnapshotStripsSendFilePath` — the `send_file` `{path}` specifically, on every surface field.
    - `TestSnapshotKeepsToolNamesDropsArgs` — `send_file`/`shell_exec` present in `ToolNames`; the
      argument JSON absent.
    - `TestSnapshotDropsToolRoleTurns` — a `role="tool"` message contributes zero turns; assert on the
      turn count AND that `Seq` stays dense (0,1,2…) across the drop.
    - `TestSnapshotSpilledTurnNoSidecarPath` — a history whose text came from a spilled turn leaks no
      sidecar path.
    - `TestSnapshotOmitsIdentity` — the owner identity id appears in no marshalled field.
    - Table-driven role tests: `user`/`assistant` emit; `tool`/`system`/`` ``/`garbage` do not.
    - `TestSnapshotToolNamesOrderAndDuplicates` — order preserved, duplicates preserved, blank dropped.
    - `TestSnapshotArtifactAllowlist` — only the four keys survive; a source with extra keys contributes
      nothing extra.
    - `TestSnapshotRedactionIdempotent` — projecting twice equals projecting once.
    - `TestSnapshotSchemaVersion` — `SchemaVersion == 1` and the json tags match the OQ4 wire contract
      exactly (assert on the marshalled key set, so a tag rename fails here rather than silently breaking
      plan 37F-05's TypeScript mirror).

    Add `goleak` only if a test starts a goroutine — none should here; do not add it reflexively.

    Run `go test ./internal/share/` and confirm the tests FAIL (the package does not exist). Commit:
    `test(37F-03): add failing hostile-fixture tests for the share snapshot redaction core`
  </action>
  <verify>
    <automated>bash -c 'go test ./internal/share/ 2>&1 | grep -qiE "no (Go |required module|non-test Go )files|cannot find|undefined|build failed|\[build failed\]" && echo "RED-OK: tests fail as required" || { echo "RED-FAIL: expected a failing build/test"; exit 1; }'</automated>
  </verify>
  <acceptance_criteria>
    - `internal/share/snapshot_test.go` and `internal/share/redact_test.go` exist and declare `package share`.
    - Neither file contains a `//go:build` line: `grep -L "go:build" internal/share/*_test.go` lists both files.
    - `go test ./internal/share/` FAILS (nothing is implemented yet). This is the RED gate.
    - The hostile fixture's forbidden-substring list includes at minimum `/abs/`, `/etc/`, `AURA_RUN_DIR`, and the owner identity id.
    - `TestSnapshotRedactsHostPaths` contains at least one POSITIVE assertion (the assistant prose survives) — a pure negative test would pass against a Snapshot that drops everything.
    - Every test name required by `37F-VALIDATION.md` for WEBSHARE-03's unit rows is present: `grep -c "func TestSnapshot" internal/share/snapshot_test.go` returns ≥ 6.
    - The fixture strings are traceable to real code (`send_file.go` arg/descriptor/error shapes), not invented.
  </acceptance_criteria>
  <done>Both test files exist with the hostile fixture and the full L-01..L-09 assertion set, contain no build tag, and fail because the package is not implemented.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: GREEN — Snapshot types + BuildSnapshot + the allowlist projections</name>
  <read_first>
    - `internal/share/snapshot_test.go`, `internal/share/redact_test.go` — the contract you must satisfy (from Task 1)
    - `internal/agui/audit_store.go:27-38` — `AuditEvent`'s doc: the house style for a projection type (what it is, where each field comes from, the sanitize obligation). `Snapshot`'s doc states the INVERSE invariant.
    - `internal/agui/server_redact.go:41-60` — `SanitizeString`. **Read it to know what NOT to do.** It is a regex denylist. Name it in `redact.go`'s header as the anti-analog.
    - `web/src/chat/sseAdapter.ts:346-362` — the allowlist projection technique to port to Go
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §OQ4 — the exact `Snapshot` shape + json tags (a wire contract; plan 37F-05 mirrors it in TS)
  </read_first>
  <action>
    Create `internal/share/snapshot.go` and `internal/share/redact.go`, package `share`.

    `snapshot.go`: the three types with the OQ4 json tags verbatim, plus the local input structs
    `ConvMeta{Title, Model string; CreatedAt time.Time}` and
    `ArtifactMeta{AssetID, FileName, MIMEType string; SizeBytes int64}`, plus
    `BuildSnapshot(conv ConvMeta, msgs []llm.Message, artifacts []ArtifactMeta, snapshotAt time.Time) (Snapshot, error)`.
    Take `snapshotAt` as a parameter rather than calling `time.Now()` inside — a constructor that reads
    the wall clock is not deterministically testable, and the caller (the service) already owns the
    transaction clock.

    Deliberately do NOT accept `conversations.Conversation`: it carries `IdentityID` (L-08). Accepting a
    narrow local `ConvMeta` keeps the owner identity structurally out of reach and keeps this package
    free of a `conversations` import. State that reason in the `ConvMeta` doc — it reads as needless
    indirection otherwise.

    `redact.go`: `projectTurns`, `toolNames`, `projectArtifact`. Every one constructs field-by-field.
    `projectTurns` switches on role with an explicit `user`/`assistant` allowlist and a `default:` that
    drops — never a denylist of known-bad roles. Assign `Seq` from a dense counter over emitted turns,
    not from the input index.

    Headers:
    - `snapshot.go`: state that `BuildSnapshot` is the ONLY function in the repo that accepts an
      `llm.Message` and returns share-bound data, that MD/JSON/the page model are pure functions of
      `Snapshot`, and that `Snapshot` has no field able to hold args/results/paths — so divergence
      between surfaces is a type error, not a review miss (D-07).
    - `redact.go`: state the trust boundary (R-09 — the recipient's browser is not a trust boundary; this
      projection is), and name `internal/agui/server_redact.go:52` `SanitizeString` as the ANTI-analog
      with the one-line reason (regex denylist; misses every path shape nobody thought of). Note that
      `SanitizeString` stays correct in exactly one 37F role: the audit union already applies it to
      `Target`/`Detail`, so `share_audit` rows inherit it for free.

    Keep comments to the non-obvious WHY only (CLAUDE.md). The field names already say what.

    Run `go test ./internal/share/` — all green. Then `go test -race ./internal/share/` and
    `go vet ./internal/share/`. Commit: `feat(37F-03): implement the allowlist share snapshot projection`
  </action>
  <verify>
    <automated>go test ./internal/share/ -count=1 && go test -race ./internal/share/ -count=1 && go vet ./internal/share/ && go build ./... && bash scripts/check-file-size.sh</automated>
  </verify>
  <acceptance_criteria>
    - `go test ./internal/share/ -count=1` passes, including `TestSnapshotRedactsHostPaths`.
    - `go test -race ./internal/share/ -count=1` passes.
    - `go vet ./internal/share/` and `go build ./...` clean.
    - **Structural leak-impossibility, machine-checked:** `grep -nE "Arguments|ArgsRaw|ResultPreview|SidecarPath|IdentityID|ToolCallID" internal/share/snapshot.go` returns NOTHING inside the `Snapshot`/`SnapshotTurn`/`SnapshotArtifact` type declarations.
    - **No denylist:** `grep -nE "delete\(|regexp\.|ReplaceAll" internal/share/redact.go` returns NOTHING. The projection is construction-only.
    - **No forbidden imports:** `go list -deps ./internal/share/ | grep -E "internal/(conversations|agui)$"` returns NOTHING — `internal/share` imports neither.
    - `grep -c "SanitizeString" internal/share/redact.go` returns ≥ 1 (the anti-analog is named in the header so a reviewer cannot mistake the design).
    - Both files ≤ 600 LOC; `bash scripts/check-file-size.sh` exits 0.
    - `golangci-lint run ./internal/share/` reports 0 issues.
    - Coverage of the new package is ≥ 85%: `go test ./internal/share/ -cover -count=1` reports ≥ 85.0%.
  </acceptance_criteria>
  <done>`internal/share/snapshot.go` + `redact.go` implement the allowlist projection; all Task-1 tests pass under `-race`; the type carries no field able to hold a leak; `redact.go` contains no regex/delete; the package imports neither `conversations` nor `agui`.</done>
</task>

<task type="auto">
  <name>Task 3: Mutation spot-check on the SC3 core (≥70% killed) + record the score</name>
  <read_first>
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md` §"Manual-Only Verifications" — the mutation row and where the score is recorded
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §"Mutation testing ≥70% killed" — `redact.go` and `snapshot.go` are the named non-negotiable targets: a surviving mutant here is a live leak
    - `CLAUDE.md` §"Quality tooling & gates" — `go-mutesting` runs in **WSL** (the only fork supporting go1.26); `PASS`=killed, `FAIL`=survived; PATH needs `~/.local/bin:~/go/bin` prepended
  </read_first>
  <action>
    Run the mutation spot-check on the two SC3 files, in **WSL** (not Windows, not a native `.exe`):
    `export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"` then
    `go-mutesting ./internal/share/redact.go ./internal/share/snapshot.go`.
    No build tag is needed — this package has no DB/Garage dependency, which is precisely why it is
    cheap to mutate.

    Score = killed / total. The floor is **70%**.

    Apply the mutation-autopsy rule before chasing the number: classify each survivor. `%w`-dense and
    equivalent mutants are near-equivalent and may be advisory-accepted **with a written reason**. A
    survivor that changes what reaches the Snapshot is **not** acceptable at any score — kill it by
    adding the missing assertion or a test seam, because that survivor IS a leak.

    Record the score and the survivor classification in `37F-VALIDATION.md`'s Manual-Only table, matching
    the precedent entries (db.go 82.8%, budget.go 89.4%).
  </action>
  <verify>
    <automated>go test ./internal/share/ -cover -count=1 | grep -E "coverage: [0-9.]+%" && grep -qiE "mutat" .planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md && echo MUTATION-RECORDED</automated>
  </verify>
  <acceptance_criteria>
    - Mutation score on `internal/share/redact.go` + `internal/share/snapshot.go` is ≥ 70% killed, recorded as a number in `37F-VALIDATION.md`'s Manual-Only table.
    - Every survivor is classified in that table as either "equivalent/near-equivalent (advisory-accepted, reason: …)" or "killed by <new assertion>".
    - **Zero survivors of the class "changes what reaches the Snapshot"** — any such mutant is killed, not accepted.
    - The command actually ran in WSL (the score is a real measurement, not an estimate); the SUMMARY records the command used.
    - `go test ./internal/share/ -cover -count=1` still reports ≥ 85%.
  </acceptance_criteria>
  <done>Mutation score ≥70% on the SC3 core is measured in WSL and recorded in `37F-VALIDATION.md`, with every survivor classified and no leak-class survivor accepted.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| conversation history → share-bound data | `BuildSnapshot` is the boundary. Everything upstream (`llm.Message`, `Turn`, `tool_invocations`) is owner-trusted and leak-bearing; everything downstream (`Snapshot`) is recipient-safe. This is the only place the transition happens. |
| server → recipient's browser | The 37A path strip runs in the browser because that browser is the OWNER's own session. A share recipient's browser is not a trust boundary — redaction must complete before bytes leave the server (R-09). |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37F-01 | Information Disclosure | `BuildSnapshot` / `Snapshot` (L-01, L-04) | mitigate | No `Arguments`/args field exists on any Snapshot type; `toolNames` copies `Function.Name` only. Asserted by `TestSnapshotKeepsToolNamesDropsArgs` and by a grep gate on the type declarations. |
| T-37F-18 | Information Disclosure | `role="tool"` turn content (L-02) | mitigate | `projectTurns` allowlists `user`/`assistant`; `default:` drops. A raw `shell_exec` stdout (paths, hostnames, container ids, env) can never reach a `SnapshotTurn`. |
| T-37F-19 | Information Disclosure | `send_file` artifact descriptor `{path}` (L-03) | mitigate | `projectArtifact` constructs the 4-key allowlist server-side; the 37A strip is client-side and is NOT reused. |
| T-37F-20 | Information Disclosure | `Turn.ContentSidecarPath` (L-07) | mitigate | The constructor consumes `[]llm.Message`, which structurally has no such field. A naive `json.Marshal(turn)` is not reachable from this package. |
| T-37F-13 | Information Disclosure | owner identity on the snapshot (L-08) | mitigate | `BuildSnapshot` accepts a narrow local `ConvMeta`, never `conversations.Conversation`; the owner UUID is not in scope. open-webui leaks exactly this via `getUserInfoById`. |
| T-37F-21 | Information Disclosure | internal correlation ids (L-09) | mitigate | No `ID`/`ToolCallID` field on any Snapshot type — D-03's "no enumerable IDs" extended to the snapshot body. |
| T-37F-22 | Tampering | a future reviewer refactors `redact.go` into a regex denylist | mitigate | `redact.go`'s header names `SanitizeString` as the anti-analog with the reason; a grep gate fails the build if `regexp.`/`ReplaceAll`/`delete(` appear in the file. |
| T-37F-23 | Information Disclosure | reasoning/CoT at rest then exported | mitigate | D-08 amended: `SnapshotTurn` has no reasoning field. Not persisted anywhere, so not producible (verified 3 ways). |
| T-37F-SC | Tampering | npm/pip/cargo installs | accept | Stdlib + `internal/llm` only. No new dependency. |
</threat_model>

<verification>
- `go test ./internal/share/ -count=1` — all green, hostile fixture included
- `go test -race ./internal/share/ -count=1`
- `go vet ./internal/share/ && go build ./...`
- `golangci-lint run ./internal/share/` → 0 issues
- `go test ./internal/share/ -cover -count=1` → ≥ 85%
- Mutation (WSL): `go-mutesting ./internal/share/redact.go ./internal/share/snapshot.go` → ≥70% killed
- `bash scripts/check-file-size.sh` → 0
</verification>

<success_criteria>
The 9 verified leak sources are unrepresentable in the `Snapshot` type. A hostile history carrying real
`send_file` paths, `shell_exec` stdout, and a spilled-turn sidecar path produces a Snapshot with none of
them, while still carrying the assistant prose and the tool names. The projection is construction-only
(no regex, no delete), the package imports neither `conversations` nor `agui`, and the mutation score on
the SC3 core is ≥70% with zero leak-class survivors.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-03-SUMMARY.md` when done.
Record the mutation score, the survivor classification, and the package coverage number.
</output>
