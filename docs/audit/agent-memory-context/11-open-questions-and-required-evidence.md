# Required Q&A, Open Questions, and Evidence

## 1. Where is the canonical source of truth for agent memory?

**Answer:** Neo4j through the vendored Agent Memory service is canonical for
long-term entities, facts, preferences, embeddings, and ownership edges
(`memory/long_term.py`; `graph/client.py`). PostgreSQL is separately canonical
for Aura conversation turns (`internal/conversations/store.go`). Agent Memory
also has a second Neo4j short-term conversation model; it is not Aura's
conversation truth.

## 2. Which components may create, mutate, retrieve, summarize, or delete memory?

**Answer:** Model-visible bridge tools, `aura memory`, onboarding, automatic
recall, direct MCP tools/resources, and the dormant consolidation/observer
paths. Per-node update/forget exists; production bulk owner purge does not.
Evidence: `bridge.go:236-277`, `cmd/aura/memory*.go`,
`serve_recall.go`, sidecar `mcp/_tools.py`, `_resources.py`,
`memory/consolidation.py`.

## 3. Can the same event create duplicate memories?

**Answer:** Yes. Fact/preference dedup is check-then-create without a semantic
database uniqueness constraint, and partial owner-link failure makes retry
unable to see the orphan. Evidence: `long_term.py:797-885,1098-1194`;
`graph/schema.py:81-118`; CON-001 and MEM-001.

## 4. Can persistence and indexing diverge?

**Answer:** Partially. Embeddings are native Neo4j node properties, so there is
no separate asynchronous vector store. Logical state can still diverge: a node
and embedding can commit without owner, temporal, or relationship edges because
they are separate transactions (`graph/client.py:140-168`; MEM-001).

## 5. How are contradictory or obsolete memories handled?

**Answer:** Manual/model-chosen update/supersession primitives exist, but no
scheduled conflict gate is active. Normal preference search omits supersession
and validity filters, so obsolete preferences can be recalled. Evidence:
`long_term.py:969-1049`; `graph/queries.py:464-493`; MEM-002/003.

## 6. How is memory isolated among users, agents, sessions, and tenants?

**Answer:** Aura's canonical bridge stamps the authenticated user, and runner
checks conversation ownership. The service itself is fail-open: direct
resources/tools accept absent/forged scope; memory aliases bypass stamping; and
short-term sessions can be claimed. Agent-specific isolation is not a distinct
storage principal. Evidence: SEC-001/002, MCP-001, `bridge.go:123-164`.

## 7. Can retrieved memory contain executable instructions?

**Answer:** Yes, arbitrary stored text can look imperative. Aura marks MCP
output and dynamic recall untrusted and instructs the model to ignore embedded
commands (`bridge.go:167-176`; `dynamic_recall.go:118-125`). Direct MCP
resources do not pass through that fence; no write-time instruction
classification was found.

## 8. Is provenance preserved from storage to final LLM context?

**Answer:** No. IDs/order/revisions survive in an out-of-band adaptive ledger,
but model-visible formatting strips ID, confidence, source, age, validity, and
score; reranker revision is falsely fixed to `none-v1`. Evidence:
`integration_context.py:18-19,207-245,281-298`; MEM-004.

## 9. What determines which memories enter the context?

**Answer:** Static/shadow dynamic-recall control chooses no recall or top 4/8;
the provider independently queries preferences and entities, reranks, and
formats them. Model-driven `memory_search` is separately chosen through tool
discovery. Facts are not in automatic recall. Evidence:
`internal/runner/dynamic_recall.go`; `serve_recall.go`;
`integration_context.py:182-245`.

## 10. Is ranking deterministic, explainable, and testable?

**Partial answer:** Result order/IDs/revisions are validated, and a 5x pool is
reranked. Ties, actual reranker revision, cross-kind allocation, and
model-visible scores/provenance are incomplete; current production-scale
quality is not assessable. Needed: fixed-version seeded reranker tests,
tie-break contract, and current corpus benchmark. Evidence: MEM-004/005,
PERF-001.

## 11. How is the token budget calculated for each model?

**Answer:** Persisted history uses `ContextWindow - max(MaxOutputTokens,20000)
- 13000 - provider reserve` with cl100k estimates. It is not an exact per-model
final request calculation and excludes late system/tool/hook/tool-loop content.
Evidence: `context.go:20-28,91-108,465-474`; CTX-004/005.

## 12. What is discarded first when the context window is full?

**Answer:** Eligible old sidecar-backed tool output is replaced with a pointer,
then oldest rounds are dropped. Leading protected prefix/always block is
preserved. A sole active resume body can incorrectly be emptied. Evidence:
`context.go:195-307,412-462`; CTX-002.

## 13. Can compaction remove safety or business-critical information?

**Answer:** Yes. The ladder can delete the active unresolved round on resume,
and a `BeforeModel` hook can remove system/current user/tool state. Ordinary
hard-drop can also remove older business state. No durable summarization is
currently used. Evidence: CTX-002/003.

## 14. Can context grow without a hard bound?

**Answer:** Yes. Nonpositive context-window config disables the history guard;
all persisted rows are loaded; hooks/tool results/final synthesis grow after
the ladder; observer and direct MCP inputs also have unbounded surfaces.
Evidence: CTX-004–006, REL-001, SEC-003.

## 15. Is agent-to-agent context transfer explicit and safe?

**Answer:** Yes on the inspected swarm path. The worker receives only a
self-contained goal, not parent conversation/user/other workers, and cannot
nested-spawn (`internal/agent/tools/swarm_spawn.go:20-35`;
`internal/swarm/swarm.go`). Correctness depends on the parent including all
needed constraints; no implicit memory transfer occurs.

## 16. Can retries create duplicated actions or memory writes?

**Answer:** Yes. Go correctly avoids replaying ambiguous mutating transport
calls, but semantic check-then-create, partial writes, CLI update omission, and
false-success completion defeat end-to-end idempotency. Evidence: MEM-001,
MCP-004/007, CON-001.

## 17. What happens when MCP, storage, retrieval, embeddings, or the LLM fail independently?

**Answer:** Mount failures are fail-soft; read-only calls can reconnect;
mutations are not replayed after transport ambiguity; recall often falls back
to static. Semantic sidecar dependency errors become successful text and may
complete idempotency. Provider overflow/timeouts can surface errors or optional
lossy middle-out. Readiness does not represent core memory degradation.
Evidence: MCP-004, OBS-001, ARC-002, CTX-004.

## 18. Are timeouts and cancellations propagated end to end?

**Answer:** Not fully. Calls use contexts and bounded defaults, but both common
clients block on a non-cancellable session mutex and HTTP close starts its
timeout after that lock. Sidecar observer tasks are detached. Evidence:
MCP-002, REL-001.

## 19. Are all declared MCP tools actually registered, reachable, and consumed?

**Partial answer:** Initial listed tools are collision-validated and mounted;
six canonical long-term tools are model-visible, other host-only tools are
hidden but direct-callable. Reconnect additions remain absent and removed tools
remain registered. Prompts/resources are registered by the sidecar but not
consumed through Aura's tool bridge. Evidence: MCP-005 and
`bridge.go:236-277,467-545`.

## 20. Are there parallel, obsolete, duplicated, or bypassed implementations?

**Answer:** Yes. Active `internal/knowledge/client.go` bypasses common MCP
hardening; automatic recall opens fresh raw sessions beside the mounted
reconnecting server; Agent Memory short-term conversation state parallels
Aura's PostgreSQL conversation truth. Evidence: ARC-001/003 and architecture
report.

## 21. Can sensitive data or secrets be persisted or logged?

**Answer:** Yes. Memory fields accept arbitrary PII/secrets, preference content
can be debug-logged, explicit full reasoning trace can retain verbatim context,
and the knowledge client puts Neo4j password in argv. Default trace
hash/redaction and file mode are positive controls. Evidence: ARC-001,
PRIV-001, `integration_context.py:96-101`,
`internal/reasoningtrace/reasoningtrace.go`.

## 22. Can one user retrieve another user's memories?

**Answer:** Yes, through unauthenticated global resources/forged scope, a
supported aliased recipe, or short-term session claiming. These are confirmed
code paths, not merely missing tests. Evidence: SEC-001/002 and MCP-001.

## 23. Can stored prompt injection influence privileged agent behavior?

**Partial answer:** It can reach model context as untrusted text. Aura's fences
are strong LLM-layer controls, but direct resources lack the fence, hooks can
rewrite protected policy, and model compliance is probabilistic. Required
runtime evidence: adversarial stored-memory evaluation across supported models
and hook configurations. Evidence: SEC-001, CTX-003, MEM-004.

## 24. Can the system be debugged from production telemetry?

**Answer:** Not reliably for semantic memory failures. Generic MCP metrics and
runbooks exist, but error-shaped success, partial writes, retrieval
quality/staleness, purge backlog, observer pressure, and exact context-source
tokens are absent. TCP/readiness can stay green. Evidence: OBS-001 and
`07-testing-observability-operability.md`.

## 25. Which critical behavior is not protected by automated tests?

**Answer:** Direct resource auth/identity spoof, same-session two-user
isolation, aliases, concurrent dedup, step fault recovery, semantic
error/idempotency, production erasure, malicious hooks, exact final request,
reconnect generations, waiter cancellation, and semantic readiness. The current
model-context focused test exists but fails. Evidence: TST-001.

## 26. What prevents silent data loss, stale retrieval, or context corruption?

**Answer:** Partial controls include atomic per-query epoch increments,
non-replay of ambiguous mutations, dynamic-tail coherence/placement validation,
protected leading context blocks, explicit overflow in some cases, and
untrusted framing. They do not prevent logical partial writes, stale
preferences, active-round deletion, hook mutation, or final overflow. Evidence:
positive controls in the register plus MEM-001/002 and CTX-002–004.

## 27. Does the implementation match its documentation and intended architecture?

**Answer:** Partially. Vendored long-term memory, narrowed model surface,
static/shadow recall, and context ladder match substantial intent. Runtime
profiles do not bind SSRF enforcement; history hard cap is unwired; readiness
omits default memory; recent memory-surface proposals are only partially
implemented; the extended sidecar retains short-term surfaces contrary to the
long-term-only operating assumption. Evidence: MCP-003, CTX-006, ARC-002/003.

## 28. What are the highest-risk cross-component failure chains?

**Answer:**

1. unauthenticated direct caller -> global resource/forged scope -> cross-user
   disclosure;
2. common session ID -> ownership overwrite/link -> victim conversation read;
3. memory alias -> typed source discarded -> identity/hiding bypass ->
   `tool_search` global read;
4. multi-step graph write -> partial commit -> JSON “success” -> 30-day
   idempotency completion -> green telemetry -> unrepaired corruption;
5. deprovision nil graph/conversation purge -> identity delete -> retained PII
   -> stale UUID direct retrieval;
6. persisted-history budget passes -> tool schemas/hook/tool results grow ->
   provider truncation/rejection -> missing active/safety state;
7. model-only context copy bug -> grounding/authority omitted -> normal-looking
   UI with hallucinated/unsafe model action.

## Open questions and exact required evidence

1. **Deployment network policy:** no repository artifact establishes isolation
   between sibling containers. Required: deployed Compose/Kubernetes/network
   policy and reachability test.
2. **FastMCP framework limits:** application code has no caps, but underlying
   ASGI/body/concurrency defaults were not established. Required: deployed
   package versions/config and boundary test.
3. **Actual runtime profile:** local/dev can bypass strict operation registry.
   Required: supported topology/profile/environment inventory.
4. **Deletion promise:** no authoritative user/legal deletion SLA was found.
   Required: product/privacy policy with data classes and deadlines.
5. **Existing graph condition:** static code proves possible corruption but not
   current volume. Required: approved read-only queries for cross-owner session
   edges, unowned nodes, duplicate canonical facts/preferences, stale active
   recall, and ownership inconsistencies.
6. **Performance and quality:** current-model 100K evidence is invalidated.
   Required: reproducible target-scale benchmark described in PERF-001.
7. **Provider truncation behavior:** exact behavior varies by supported
   provider/model. Required: capability/version matrix and exact-wire boundary
   tests, including OpenRouter middle-out state.
8. **Prompt-injection residual:** required adversarial stored-memory evaluation
   across supported models, provenance formats, and hook configurations.
9. **Retention/consolidation operator:** required deployment scheduler/job
   inventory and backlog/receipt telemetry, if any exists outside the repo.
10. **Concurrent working-tree changes:** revalidate line anchors and run the
    complete approved test matrix against a frozen revision before release.
