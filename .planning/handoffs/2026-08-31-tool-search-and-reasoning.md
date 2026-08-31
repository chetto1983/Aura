# Handoff — 2026-08-31

Session covering the tool manifest, the install path, first-operator provisioning,
adaptive reasoning, and a tool-search ranker investigation that ended in a decision
NOT to ship. Everything below is measured; where it is not, it says so.

## Shipped and pushed

| commit | what |
|---|---|
| `ebcb94f9a` | `shell_poll`/`shell_kill` promoted to always-active (+242 tok) |
| `39c677d87` | `memory_merge_entities` hidden so memory earns an always-loaded MCP slot |
| `b578ff3ec` | box images published to GHCR, pinned in `install.sh`, `make sandbox-images` |
| `56f341a9e` | an un-ingested document library returns empty, not HTTP 500 |
| `5216704de` | WARN when an MCP server is skipped for want of a grant owner |
| `12a332029` | first operator gets object store, filesystem roots, box, MCP remount |
| `40821c37e` | box destroyed when the identity is deprovisioned |
| `b14f03382` | public-mutation idempotency owned by an identity that survives |
| `b40d333c4` | `aura doctor` probes MCP with operator credentials |
| `b2b954d90` | adaptive reasoning is provider-agnostic |
| `50b3006ea` | the completion critic gate says when it vetoes |
| `dd8a3186a` | amendment #202 plus three false doc statements corrected |
| `dd3fe8652` | held-out measurement harness for the tool-search ranker |

CI was green through `b40d333c4`. Later runs were left unchecked at the operator's
instruction: a large parallel effort on memory batches was landing at the same time
and was expected to fail them.

## The two most serious defects fixed

**Password reset and first-operator bootstrap were dead on every deployment that had
ever enrolled an operator.** The idempotency middleware owned public mutations by the
seeded `local` identity, which `retireLegacyLocalIdentityForAuthulaUser` deletes the
first time someone logs in. With an FK to `identities`, every later public mutation
wrote a dangling key and answered 503 forever. `CLIServiceIdentity` already existed
for exactly this and survives the retirement. Verified live: 503 becomes 202.

**A fresh appliance could not run a single tool.** `aura-sandbox` and `aura-egress`
are built from this repo but are not compose services, so nothing produced them: the
only build of `aura-sandbox` lived in a chaos-test script. `ensureImage` then tried
Docker Hub, got a pull-access-denied, `Route` denied, and every box-capable tool
denied with it. Now published to GHCR and pinned by the installer.

## Open, in priority order

1. **ON DELETE RESTRICT on `aura.telegram_accounts` and `aura.telegram_setup_pending`.**
   The deprovision saga deletes the identity row AFTER memory, bucket and dirs, so for
   any identity that ever linked Telegram the delete fails with the data planes already
   destroyed. Hit twice live this session; it is what forced manual SQL deletes and so
   created two orphan memory tenants (since cleaned). Same class as the dormant
   `audit_logs` landmine the code already documents, but this one is armed.

2. **Two answers in the cockpit, not reproduced.** The completion critic was ruled out
   by measurement: zero vetoes across question, file-creation and web-search turns on
   both OpenRouter and Ollama. The repudiation chain exists end to end
   (`Actions.DiscardStreamed`, the `aura.discard` CUSTOM event, `sseAdapter.ts:300`).
   The gate now logs source, attempt and verdict, so a recurrence is diagnosable.
   Reproducing it needs an authenticated cockpit session.

3. **First-operator MCP remount not re-verified live.** The context bug was fixed and
   unit-tested, but no fresh boot-with-no-identity was run again afterwards. The other
   three legs (bucket, dirs, box) were verified live.

4. **Leftovers on disk.** The pinned llama.cpp GGUF (6.72 GB, SHA verified) sits in
   `/d/aura-models/` but was never installed; it was for a live llama.cpp reasoning
   check that turned out unnecessary. The MTP drafter it pairs with was never fetched.

## Tool search: parked, with the numbers

Measured on the 85-tool deferred corpus. GATE is the shipped regression suite;
HELD-OUT is 24 queries written against the inventory and never used for tuning.

| ranker | GATE top-1 | GATE r@3 | HELD top-1 | HELD r@3 |
|---|---:|---:|---:|---:|
| Okapi BM25 (shipped) | 100% | 100% | 25% | 54% |
| BM25L / BM25+ | 100% | 100% | 21% | 50% |
| embedding in Go (desc + task prefixes) | 85% | 92% | 58% | 83% |
| fusion in Go, RRF 1:1 | 85% | 100% | 50% | 71% |
| fusion in Go, weighted 1:3 | 88% | 100% | 62% | 71% |
| ArcadeDB vector.fuse RRF | 88% | 100% | 58% | 71% |
| ArcadeDB vector.fuse DBSF | 92% | 100% | 62% | 79% |
| **ArcadeDB vector.fuse LINEAR** | **96%** | **100%** | **67%** | **83%** |

Cost of the native path: embed 53 ms plus fuse 70 ms, about **123 ms median**, against
sub-millisecond in-process BM25.

Three things worth carrying forward:

- **The engine beats Go-side fusion again.** Best Go hybrid 62/71, vector.fuse LINEAR
  67/83. This reproduces the 0.300-vs-0.850 result already recorded in
  `document_retrieval.go`, on a different corpus.
- **LINEAR beat RRF here**, the opposite of the choice made for document retrieval.
  That choice is not wrong there: the corpora differ. Fusion strategy is a per-corpus
  measurement, not an inherited default.
- **BM25L was the wrong hypothesis** and is now closed with a number.

### Why it was parked anyway

Real traffic was then measured: 12 varied prompts driven through the live agent
(gemma4:31b-cloud via Ollama) produced **9 tool_search calls, every one of them the
select-by-name form, none in natural language**. The ranker's recall was exercised
zero times. Eight of nine names were right first try; the ninth invented a
`scheduling__` namespace that does not exist, and the token-overlap suggestion in
`unknownNameReport` recovered it in one round.

So the 25-to-67 improvement is real but sits on a path with no measured traffic here.
Shipping it would cost 123 ms per lookup and couple tool DISCOVERY to ArcadeDB and the
embedding sidecar, and this session spent hours with the memory MCP unmounted while the
agent ran, which is exactly when tool discovery must not fail.

**Revisit when the corpus grows.** The operator's framing: at roughly 50 MCP servers the
name space stops being memorable and select-by-name should start missing. The signal to
watch is the ratio in `aura.tool_invocations` between select-form and natural-language
queries, plus how often `unknownNameReport` fires. When natural language becomes a real
share of traffic the table above is already the answer, and vector.fuse LINEAR is the
row to implement, with an in-process BM25 fallback so a memory outage cannot cost the
agent its own tools.

Caveats: n=9 calls, one deployment, one model. The gate corpus claims to record
production natural-language queries, so some model at some point did write them. This
says select-by-name dominates HERE, not that the ranker is dead.

## Roster summaries: also parked

Giving the deferred roster each tool's Summary instead of bare names costs a measured
32 to 261 tokens per turn (+229, about 4.6% of a 4,945-token manifest); swarm_spawn
alone is ~90 of it because its Summary is a Description in disguise. Surveyed against
hermes-agent, LibreChat, pi and Claude Code: only pi and Claude Code defer at all, pi
by position (full schema moved out of the cached prefix) and Claude Code by bare names
plus a search, which is the shape Aura already has. Aura is not an outlier, and both it
and Claude Code back the bare roster with a search that indexes text the model cannot
see. Recorded in amendment #202.

## Process notes

A second agent was committing to this repo throughout. Its commits absorbed
uncommitted work of this session and vice versa, and two of its lint violations
(SA4000, modernize) had to be fixed before anything here could commit. If both run
again, expect it.

`.planning/handoffs/` did not exist and was created for this file. CLAUDE.md lists it,
which is one more instance of the warning CLAUDE.md itself gives about its own age.
