---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 06
type: tdd
wave: 3
depends_on: ["37F-03"]
files_modified:
  - internal/share/markdown.go
  - internal/share/jsonfmt.go
  - internal/share/format_test.go
  - internal/share/share_property_test.go
autonomous: true
requirements: [WEBSHARE-01, WEBSHARE-03]

must_haves:
  truths:
    - "Snapshot.Markdown() and Snapshot.JSON() are pure functions of Snapshot — neither reads an llm.Message"
    - "JSON round-trips losslessly: unmarshalling a marshalled Snapshot yields an equal Snapshot"
    - "For any history, no string from the args/results/sidecar corpus appears in Markdown() or JSON() — SC3 stated as a machine-checkable universal"
    - "MD and JSON agree: the same turns, in the same order, with the same tool names"
    - "Markdown renders per-turn headings and fences code, and a turn whose text contains a fence cannot break out of it"
  artifacts:
    - path: "internal/share/markdown.go"
      provides: "func (Snapshot) Markdown() []byte"
      min_lines: 50
    - path: "internal/share/jsonfmt.go"
      provides: "func (Snapshot) JSON() ([]byte, error)"
    - path: "internal/share/share_property_test.go"
      provides: "the 4 snapshot/format properties incl. redaction totality"
      min_lines: 80
  key_links:
    - from: "internal/share/markdown.go"
      to: "internal/share.Snapshot"
      via: "method on the Snapshot value; no other input"
      pattern: "func \\(s Snapshot\\) Markdown"
  prohibitions:
    - "MUST NOT accept []llm.Message in either adapter — BuildSnapshot is the ONLY function that takes one; an adapter that re-derives from messages is exactly the divergence D-07 exists to prevent"
    - "MUST NOT write two independent serializers that each re-derive from the history"
    - "MUST NOT add a build tag to any test file in this plan"
    - "MUST NOT emit the owner identity, tool arguments, tool results, or any filesystem path in either format"
    - "MUST NOT reach for a markdown library — strings.Builder is the answer"
---

<objective>
Build the two format adapters, both pure functions of `Snapshot`.

D-07 requires the rendered public page AND both file formats to derive from ONE canonical redacted
snapshot, so redaction cannot diverge between surfaces. That only holds if the adapters take `Snapshot`
and nothing else: because `Snapshot` has no field able to hold args/results/paths, an adapter
*physically cannot* emit one. Two writers that each re-derive from `[]llm.Message` would let a redaction
fix in one silently miss the other — the exact failure D-07 exists to prevent.

This plan also lands the phase's property tests, including **redaction totality**: for all histories and
all secrets in the args/results/sidecar corpus, the secret appears in neither output. That is SC3 stated
as a universal rather than as a handful of examples.

Purpose: the export formats (WEBSHARE-01) and the proof that redaction survives serialization.
Output: `internal/share/markdown.go`, `internal/share/jsonfmt.go`, plus the property suite.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-PATTERNS.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md
@internal/share/snapshot.go
@CLAUDE.md
</context>

## Artifacts this plan produces

`share.Snapshot.Markdown()`, `share.Snapshot.JSON()`, and the property suite
(`share_property_test.go`).

<feature>
  <name>Markdown + JSON format adapters over the canonical snapshot</name>
  <files>internal/share/markdown.go, internal/share/jsonfmt.go, internal/share/format_test.go, internal/share/share_property_test.go</files>

  <behavior>
    **JSON (D-07 "lossless structured round-trip")**
    - `JSON()` marshals the Snapshot with its wire tags
    - `JSON⁻¹(JSON(s))` equals `s` for every Snapshot — the round-trip is lossless
    - `tool_names` is omitted (not `null`) when empty — the `omitempty` tag holds through the round-trip
    - Output is deterministic: two calls on the same Snapshot yield identical bytes

    **Markdown (D-07 "human-readable")**
    - A title heading, then one section per turn
    - Each turn gets a heading identifying the role
    - A turn's tool names, when present, render as a provenance line; when absent, no line
    - Artifacts render as a trailing section listing filename + size; a turn with no artifacts omits it
    - Code inside a turn's text is fenced
    - **Fence-injection safety:** a turn whose text itself contains a triple-backtick run cannot terminate
      the fence early — the emitted fence is longer than the longest backtick run in the content
    - Empty snapshot (no turns) produces a valid document with the title and no turn sections

    **Agreement (D-07 one-core)**
    - `Markdown()` and `JSON()` contain the same turn count, the same roles in the same order, and the
      same tool names — asserted by parsing both

    **Redaction totality (SC3, the property)**
    - For all generated histories h and all secrets s drawn from the args/results/sidecar corpus:
      s is absent from `Markdown(BuildSnapshot(h))` AND absent from `JSON(BuildSnapshot(h))`
  </behavior>

  <implementation>
    RED → GREEN → REFACTOR, one atomic commit per phase. Plain unit tests: **no build tag, no DB, no
    Garage**.

    Both adapters are methods on the `Snapshot` value: `func (s Snapshot) Markdown() []byte` and
    `func (s Snapshot) JSON() ([]byte, error)`. Neither takes any other parameter — that signature IS
    the D-07 guarantee, and it is what the acceptance criteria grep for.

    `jsonfmt.go` is ~30 LOC of `json.Marshal`. `conversations_api.go:149` `writeJSON` is an HTTP writer,
    not a `[]byte` marshaller, so it is a partial analog only — do not route through it.

    `markdown.go` has **no analog in the repo** — no Markdown serializer exists anywhere. Use
    `strings.Builder` with `b.Grow()` (the only in-repo precedent for building a large string efficiently
    is `content_disposition.go:47-48`). Do not add a markdown dependency.

    The fence-injection rule is the one non-obvious piece: scan the turn text for the longest run of
    backticks and open/close with a run at least one longer. Document WHY — a turn whose text contains a
    fence would otherwise terminate the block early and let the remainder render as document structure
    rather than as content. This is a formatting-integrity bug, not an XSS one (the public page renders
    the `Snapshot`, not the Markdown), but the exported `.md` is a file a human opens.
  </implementation>
</feature>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: RED+GREEN — JSON adapter with lossless round-trip</name>
  <read_first>
    - `internal/share/snapshot.go` — the `Snapshot`/`SnapshotTurn`/`SnapshotArtifact` types and their json tags (plan 37F-03). Read the tags from the file; they are the wire contract plan 37F-05 mirrors in TypeScript.
    - `internal/agui/conversations_api.go:149-155` — `writeJSON`/`writeJSONStatus`. A **partial** analog only: it writes to an `http.ResponseWriter`, not to `[]byte`. Do not route the adapter through it.
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §OQ4 — the one-core rule and why two writers are rejected
  </read_first>
  <action>
    RED: add the JSON cases to `internal/share/format_test.go`, package `share`, **no build tag**:
    - `TestSnapshotJSONRoundTrip` — marshal, unmarshal, `reflect.DeepEqual` back to the original, for a
      snapshot with turns, tool names, and artifacts
    - `TestSnapshotJSONOmitsEmptyToolNames` — a turn with no tool calls emits no `tool_names` key at all
      (assert on the raw JSON string, not on the unmarshalled struct — `omitempty` is the thing under test)
    - `TestSnapshotJSONDeterministic` — two calls yield identical bytes
    Run: fail. Commit: `test(37F-06): add failing JSON round-trip tests for the share snapshot`

    GREEN: create `internal/share/jsonfmt.go` with `func (s Snapshot) JSON() ([]byte, error)` wrapping
    `json.Marshal` with a `%w`-wrapped error. Header: state that this is a pure function of `Snapshot`
    and that the type's shape — not this function — is what makes it safe to serialize.
    Commit: `feat(37F-06): implement the share snapshot JSON adapter`
  </action>
  <verify>
    <automated>go test ./internal/share/ -run 'TestSnapshotJSON' -count=1 && go vet ./internal/share/</automated>
  </verify>
  <acceptance_criteria>
    - `go test ./internal/share/ -run 'TestSnapshotJSON' -count=1` passes.
    - The signature is exactly a method on the value with no extra input: `grep -qE "func \(s Snapshot\) JSON\(\) \(\[\]byte, error\)" internal/share/jsonfmt.go`.
    - `grep -n "llm\." internal/share/jsonfmt.go` returns NOTHING — the adapter never touches a message.
    - `TestSnapshotJSONOmitsEmptyToolNames` asserts on the raw marshalled string (so it fails if the `omitempty` tag is dropped).
    - `internal/share/format_test.go` carries no `//go:build` line.
    - `golangci-lint run ./internal/share/` reports 0 issues.
  </acceptance_criteria>
  <done>`Snapshot.JSON()` is a pure method on the value, round-trips losslessly, omits empty `tool_names`, and is deterministic.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: RED+GREEN — Markdown adapter with fence-injection safety</name>
  <read_first>
    - `internal/share/snapshot.go` — the type being rendered
    - `internal/agui/content_disposition.go:40-55` — the only in-repo precedent for building a string efficiently (`strings.Builder` + `Grow`). There is **no Markdown serializer anywhere in this repo** — this file has no analog, so follow RESEARCH OQ4's signature and this builder idiom.
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-PATTERNS.md` §"No Analog Found" — the `markdown.go` row states exactly this and prescribes per-turn `##` headings + fenced code per D-07
  </read_first>
  <action>
    RED: add the Markdown cases to `internal/share/format_test.go`:
    - `TestSnapshotMarkdownStructure` — title heading present; one heading per turn; roles in input order
    - `TestSnapshotMarkdownToolProvenance` — tool names render when present; no provenance line when absent
    - `TestSnapshotMarkdownArtifactSection` — artifacts listed with filename + size; section omitted when
      there are none
    - `TestSnapshotMarkdownFenceInjection` — **the one that matters**: a turn whose text contains a
      triple-backtick run (and a case with a longer run) cannot terminate the fence early; assert the
      emitted fence is strictly longer than the longest run in the content and that the full text survives
    - `TestSnapshotMarkdownEmpty` — zero turns yields a valid document with the title
    - `TestSnapshotFormatsAgree` — **the D-07 test named in VALIDATION.md**: parse both `Markdown()` and
      `JSON()` from the same Snapshot and assert the same turn count, the same roles in the same order,
      and the same tool names
    Run: fail. Commit: `test(37F-06): add failing Markdown adapter tests incl. fence injection`

    GREEN: create `internal/share/markdown.go` with `func (s Snapshot) Markdown() []byte` using
    `strings.Builder` + `Grow`. Emit the title, then per-turn role headings, the provenance line when
    `ToolNames` is non-empty, the turn text in a fence sized longer than the longest backtick run it
    contains, and a trailing artifacts section when non-empty.

    Document the fence-sizing WHY (it is the non-obvious line): a turn whose text contains a fence would
    otherwise terminate the block early, and the remainder would render as document structure instead of
    as content.

    Commit: `feat(37F-06): implement the share snapshot Markdown adapter`
  </action>
  <verify>
    <automated>go test ./internal/share/ -count=1 && go test -race ./internal/share/ -count=1 && go vet ./internal/share/ && bash scripts/check-file-size.sh</automated>
  </verify>
  <acceptance_criteria>
    - `go test ./internal/share/ -count=1` passes — the whole package, including plans 37F-03/04's tests (this plan must not regress them).
    - The signature is exactly a method on the value with no extra input: `grep -qE "func \(s Snapshot\) Markdown\(\) \[\]byte" internal/share/markdown.go`.
    - `grep -n "llm\." internal/share/markdown.go` returns NOTHING.
    - `TestSnapshotMarkdownFenceInjection` exists and covers BOTH a 3-backtick and a longer run.
    - `TestSnapshotFormatsAgree` exists (the exact name from `37F-VALIDATION.md`).
    - No markdown library was added: `git diff go.mod go.sum` is empty.
    - `strings.Builder` is used: `grep -q "strings.Builder" internal/share/markdown.go`.
    - `internal/share/markdown.go` ≤600 LOC; `bash scripts/check-file-size.sh` exits 0.
    - `golangci-lint run ./internal/share/` reports 0 issues.
  </acceptance_criteria>
  <done>`Snapshot.Markdown()` renders title + per-turn headings + fenced text + tool provenance + an artifact section, is injection-safe against embedded fences, and agrees with `JSON()` on turns/roles/tool names.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: Property suite — redaction totality, idempotence, round-trip</name>
  <read_first>
    - `internal/share/snapshot_test.go` — the hostile fixture built in plan 37F-03; the corpus of secret strings to generate from
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md` §"Property-based testing" — the exact property statements. This plan owns 4 of the 6: redaction idempotence, redaction totality, serializer round-trip, and (already covered in 37F-04) token opacity + key disjointness. Expiry monotonicity belongs to plan 37F-11.
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §"Redaction enforcement mechanism" item 4 — the property test as the machine-checkable form of SC3
    - Check whether `gopter` or `rapid` is already a module dependency: `grep -E "gopter|pgregory.net/rapid" go.mod`. If NEITHER is present, do **not** add one — express the properties as seeded generative loops over the corpus with stdlib `math/rand` (deterministic seed, reported on failure). Adding a dependency for this is not worth a new supply-chain surface; the properties are simple universals over a finite corpus.
  </read_first>
  <action>
    Create `internal/share/share_property_test.go`, package `share`, **no build tag**.

    Build a generator that produces random `[]llm.Message` histories mixing: user turns, assistant turns
    with and without tool calls, `role="tool"` turns, and `system` turns — where every tool call's
    `Arguments` and every tool result's `Content` are drawn from a **secret corpus** of realistic leak
    strings taken from the real code shapes (a `send_file` `{"path":"/abs/…"}` argument, a `shell_exec`
    result containing `/etc/passwd`, an `$AURA_RUN_DIR/conversations/…/N.content` sidecar path, a
    container id, a `cannot read %q` error string). Seed deterministically and print the seed on failure
    so a red run is reproducible.

    Properties:
    - **`TestPropertyRedactionTotality`** — the SC3 universal. For every generated history and every
      secret placed into it, assert the secret is absent from BOTH `Markdown()` and `JSON()` of
      `BuildSnapshot(h)`. This is the single most valuable test in the phase.
    - **`TestPropertyRedactionIdempotent`** — `BuildSnapshot` applied to an already-projected turn set is
      a fixpoint: a second pass finds nothing to strip.
    - **`TestPropertyJSONRoundTrip`** — for every generated Snapshot, `JSON⁻¹(JSON(s))` equals `s`.

    Guard against the vacuous-pass failure mode: assert that each generated history actually **contains**
    at least one secret and that the resulting Snapshot is **non-empty** (has ≥1 turn). A totality property
    over an empty snapshot passes trivially and proves nothing — that is the mutant this guard kills.
  </action>
  <verify>
    <automated>go test ./internal/share/ -run 'TestProperty' -count=1 -v 2>&1 | tail -20 && go test -race ./internal/share/ -count=1 && go test ./internal/share/ -cover -count=1</automated>
  </verify>
  <acceptance_criteria>
    - `go test ./internal/share/ -run 'TestProperty' -count=1` passes; each property runs ≥100 generated cases.
    - `TestPropertyRedactionTotality` asserts against BOTH `Markdown()` and `JSON()`.
    - **The anti-vacuity guard exists**: the test asserts each generated history contains ≥1 secret AND each resulting Snapshot has ≥1 turn. Verified by reading the test — a totality property that passes on an empty snapshot is worthless.
    - The seed is deterministic and printed on failure: `grep -qiE "seed" internal/share/share_property_test.go`.
    - No new module dependency unless `gopter`/`rapid` was ALREADY in `go.mod`: `git diff go.mod go.sum` is empty.
    - `go test ./internal/share/ -cover -count=1` reports ≥ 85%.
    - `go test -race ./internal/share/ -count=1` passes.
    - `golangci-lint run ./internal/share/` reports 0 issues.
  </acceptance_criteria>
  <done>The property suite proves redaction totality over generated hostile histories against both output surfaces, with an anti-vacuity guard, a reproducible seed, and no new dependency.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| Snapshot → exported bytes | The adapters are downstream of the only redaction point. Their safety is inherited from the type, not re-established — which is exactly why they must not accept any other input. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37F-33 | Information Disclosure | an adapter re-deriving from `[]llm.Message` and re-introducing a leak | mitigate | Both adapters are methods on `Snapshot` with no other parameter; grep-gated that neither file references `llm.`. A redaction fix therefore cannot miss one surface. |
| T-37F-01 | Information Disclosure | secrets surviving serialization | mitigate | `TestPropertyRedactionTotality` asserts the universal over generated histories against both surfaces, with an anti-vacuity guard so an empty-snapshot mutant cannot pass it. |
| T-37F-34 | Tampering | fence injection in an exported `.md` | mitigate | The emitted fence is strictly longer than the longest backtick run in the turn text, so content cannot escape into document structure. |
| T-37F-35 | Tampering | lossy JSON breaking D-07's round-trip claim | mitigate | `TestSnapshotJSONRoundTrip` + `TestPropertyJSONRoundTrip` assert equality after a marshal/unmarshal cycle. |
| T-37F-SC | Tampering | npm/pip/cargo installs | accept | Stdlib only (`encoding/json`, `strings`). Explicitly no markdown library and no property-testing library unless already vendored — asserted by an empty `go.mod`/`go.sum` diff. |
</threat_model>

<verification>
- `go test ./internal/share/ -count=1` — the whole package green
- `go test -race ./internal/share/ -count=1`
- `go vet ./internal/share/ && go build ./...`
- `golangci-lint run ./internal/share/` → 0 issues
- `go test ./internal/share/ -cover -count=1` → ≥ 85%
- `git diff go.mod go.sum` → empty (no new dependency)
- `bash scripts/check-file-size.sh` → 0
</verification>

<success_criteria>
Markdown and JSON are both pure functions of `Snapshot` and neither can reach an `llm.Message`. JSON
round-trips losslessly; Markdown is fence-injection-safe; the two agree on turns, roles, and tool names.
Redaction totality holds as a property over generated hostile histories against both surfaces, with an
anti-vacuity guard. No dependency was added.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-06-SUMMARY.md` when done.
Record whether a property library was already vendored or the seeded-loop form was used.
</output>
