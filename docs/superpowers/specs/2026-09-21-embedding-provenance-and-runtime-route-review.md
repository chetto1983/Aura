# Adversarial review — `docs/superpowers/specs/2026-09-21-embedding-provenance-and-runtime-route-design.md`

Reviewer: Codex, 2026-09-21. Method: every measured claim was traced through the repository; the
pinned llama.cpp request path was checked at tag `b10951`; the Compose dependency graph, all five
vector schemas, every production `embedding IS NULL` sweep, and every dense-read fallback were
enumerated. Tests and spikes are not counted as production vector writers.

Legend: **[V]** verified by reading the cited file or pinned upstream source. **[S]** predicted from
those mechanics but not exercised against the live stack. Paths are repo-relative unless they name
an upstream repository.

**Verdict: reject as implementation-ready.** The design correctly identifies the silent
same-width-model problem, but its marker can certify an unknown legacy corpus, its identity does
not identify the effective embedding transform, its repair reaches only `FACT`, and its runtime
route is still a set of boot snapshots with a stale-env escape hatch. Those are failures in the
safety protocol, not polish gaps.

Ordered by damage.

---

## 1. An absent marker certifies an unknown legacy corpus instead of detecting it. [V]

- The proposed first-boot rule is unconditional: “absent — write it”
  (`docs/superpowers/specs/2026-09-21-embedding-provenance-and-runtime-route-design.md:180-186,190-193`).
  It neither proves that the database is empty nor inspects existing non-null vectors.
- Existing records carry no model identity. The five current properties are declared at
  `internal/arcadedb/memory_vector.go:56-59`, `memory_conversation.go:86-92`,
  `memory_reasoning.go:35-44`, and `services/ingest/arcade.py:268-282,311-328`; none has a
  provenance field. The only runtime semantic use of the existing revision/fingerprint pair is
  the strict-profile shape gate (`internal/config/config_document_retrieval.go:62-67`).
- The spec itself admits that current live writers may disagree
  (`docs/superpowers/specs/2026-09-21-embedding-provenance-and-runtime-route-design.md:270-271`), then incorrectly says the design adds the means to answer
  that question. A marker written after the fact records the first post-upgrade writer, not the
  writers that produced the stored floats.

**Predicted failure:** upgrade a tenant whose corpus was embedded by model A while the first process
to start now uses model B. That process writes `B` into the absent marker and immediately treats all
of A's vectors as B. Health becomes greener while the exact silent corruption this design targets
continues.

**Smallest safe redesign:** an absent marker on a database containing any non-null vector means
`unverified`, not `ready`. Only an empty database may initialize automatically. A legacy non-empty
database must either be explicitly adopted after external proof or enter a fenced clear/re-embed
operation. The marker needs at least `unverified / repairing / ready` state, not just one identity
string.

## 2. `embed-v1:<model>:<dimensions>:<artifact>` identifies requested configuration, not the vector-producing transform. [V]

The dangerous false negatives — vector space moves while the identity does not — already have
concrete routes in this tree:

- The identity deliberately excludes task prefixes (`docs/superpowers/specs/2026-09-21-embedding-provenance-and-runtime-route-design.md:173-176`), but
  those prefixes are executable client code, not immutable model metadata. Query/document prefixes
  live in Go at `internal/embeddings/tasks.go:5-11`; the document prefix is duplicated in Python at
  `services/ingest/chunk.py:48-54`. Changing either side changes the vectors compared by retrieval
  without changing model, width, artifact, or proposed identity.
- Normalization is deployment configuration: Compose passes `--embd-normalize 2`
  (`compose.yaml:865-866`). Go may also truncate and renormalize Matryoshka output
  (`internal/embeddings/client.go:110-140,230-254`). Neither transform is named by the identity.
- A cloud artifact is only the **host** (`docs/superpowers/specs/2026-09-21-embedding-provenance-and-runtime-route-design.md:166-170`). Changing
  `/embedding-v1` to `/embedding-v2` on the same host, changing the service behind a custom host,
  or an OpenRouter model alias moving to another provider/artifact keeps the proposed string while
  changing the floats.
- The local fingerprint is trusted from env. The installer returns without re-checking the file
  whenever both values already exist (`scripts/install_env.sh:141-160`, especially :147-148), and
  the identity omits `AURA_EMBED_REVISION` entirely. Replacing the mounted GGUF while retaining a
  stale `.env` therefore preserves the string across a real model change.

The false positives — identity moves while the vector space does not — are equally real:

- Changing only the hostname to a DNS alias or proxy for the same cloud model changes the proposed
  cloud artifact even though the embedding function is unchanged.
- More seriously, today's model-only cockpit edit sends the new model name and an Authorization
  header to the unchanged local server. `compose.yaml:119` is non-loopback, so
  `internal/config/config_routes.go:19-27` does not substitute the OpenRouter base. The pinned local
  server loads one fixed GGUF (`compose.yaml:837,850-866`). In
  [`ggml-org/llama.cpp@b10951/tools/server/server-context.cpp:4905-4990`](https://github.com/ggml-org/llama.cpp/blob/b10951/tools/server/server-context.cpp#L4905-L4990), the embedding handler reads
  `input`, `encoding_format`, and normalization but never validates the request's `model`; it runs
  the loaded model and reports `meta->model_name`. In
  [`tools/server/server-http.cpp:187-190`](https://github.com/ggml-org/llama.cpp/blob/b10951/tools/server/server-http.cpp#L187-L190), API-key validation is bypassed when the server was started
  with no API keys, as Aura starts it. **The actual result is HTTP success with a local
  EmbeddingGemma vector; the unknown cloud model name and Authorization header are ignored.**
  The spec does not define how identity derivation classifies this hybrid route. Calling it cloud
  stamps requested model/host metadata over local floats; calling it local means a cloud-looking
  cockpit change neither changes identity nor backend. Neither branch attests the effective model.

**Predicted failure:** the marker can be equal while query and stored vectors use different
prefixes, normalization, endpoint paths, or a moved cloud alias; it can also become different after
a no-op route/hostname change. The former serves plausible nonsense. The latter
needlessly disables a healthy corpus and demands a destructive rebuild.

**Smallest safe redesign:** name the effective embedding contract, not the route request. Bind the
identity to an immutable/effectively attested model revision or local artifact, dimensions,
document/query preprocessing version, pooling/normalization, and response projection/truncation.
Do not make a network hostname part of vector-space identity. Validate the local fingerprint
against the artifact actually mounted, and define how a mutable cloud alias is attested before it
is allowed to stamp a corpus.

## 3. Boot-time publication plus silent env fallback recreates the original disagreement and does not make the route runtime-editable. [V]

- The design has both sidecars fetch once “at boot with bounded retry” and then silently use env
  (`docs/superpowers/specs/2026-09-21-embedding-provenance-and-runtime-route-design.md:214-219`). It checks the marker only before the first vector write in
  a session (`:185-186`). There is no refresh, lease, generation check per batch, or invalidation of
  an already-authorized session.
- `arcadedb-mcp` waits only for `aura: service_started`, not daemon health
  (`compose.yaml:719-733`). `aura-ingest` does not depend on `aura` at all
  (`compose.yaml:932-948`). The Python worker is therefore expected to take its fallback on normal
  starts, not only exceptional ones.
- The fallbacks are already divergent: MCP inherits `AURA_MEMORY_EMBED_BASE_URL` and
  `AURA_EMBED_MODEL` (`compose.yaml:790-795`), while ingest is hard-wired to the local URL and gets
  no model or fingerprint (`compose.yaml:978-979`). Python also defaults to that URL and hard-codes
  `embeddinggemma` (`services/ingest/app.py:33-37,175-182,204-212`).
- Embedding settings are currently boot-bound. `hotLLMProfileKeys` excludes both embedding keys
  (`internal/agui/settings_api.go:36-51`); any other changed key is reported as requiring restart
  (`:170-201`), and only hot LLM keys call the runtime reloader (`:378-397`). Daemon embedding
  clients themselves capture `cfg.EmbedRoute()` at construction
  (`cmd/aura/embedding_client.go:10-24`, `chat_memory_projection.go:119-123`,
  `serve_memory_backfill.go:60-64`).

**Predicted failure:** with marker A and all processes running, the operator saves route B. The
daemon may expose B, but existing MCP and ingest processes retain A and have already passed their
one session check; existing daemon clients also retain their boot route. During a repair that flips
the marker to B, an old authorized session can continue writing A. After a restart, ingest may again
fall back to its local env before the daemon is reachable. This is the original bug with an extra
record beside it.

**Smallest safe redesign:** an unavailable authority may preserve lexical reads, but it must not
authorize vector writes or dense queries from a guessed/stale route. Consumers need a watched or
versioned route and must compare the generation at every vector batch (or send the embedding
through the measured daemon proxy). Saving a route must publish to all live daemon clients and
sidecars, with acknowledgement/drain semantics; otherwise call the feature restart-bound.

## 4. The proposed repair repairs only `FACT`; the marker is not a repair protocol for five types. [V]

- The entire written repair is “clear, update the marker, let the existing sweeps run” and cites
  only `clearFactEmbeddingsStatement` (`docs/superpowers/specs/2026-09-21-embedding-provenance-and-runtime-route-design.md:204-212`). The code confirms the
  scope: `ReEmbedAllFacts` clears `FACT`, then `EmbedMissingFacts` selects only
  `embedding IS NULL AND statement IS NOT NULL`
  (`internal/arcadedb/memory_vector.go:357-404`). The scheduled drain calls that same fact-only
  function (`memory_backfill.go:209-231`). A production-tree search finds no other
  `embedding IS NULL` sweep.
- `ConversationTurn`, `ReasoningTrace`, `Passage`, and `IndexedDocument` have no clear-and-backfill
  path. Their source fields exist, but no current repair machinery consumes them
  (`memory_conversation.go:76-92`, `memory_reasoning.go:25-44`,
  `services/ingest/arcade.py:247-328`). CocoIndex's unchanged-file path is memoized
  (`services/ingest/app.py:305-318`), so “let the existing sweeps run” does not even imply that
  documents will be revisited.
- No ordering/fence is specified around clear and marker update. Clear first permits still-valid A
  sessions to refill A; mark B first lets B readers see uncleared A records. The marker is read once
  per session, so changing it is not a fence (`docs/superpowers/specs/2026-09-21-embedding-provenance-and-runtime-route-design.md:185-186,206-209`). [S on
  the exact interleaving; the missing fence is verified.]

For `FACT` alone, a **fully completed, fenced clear before any re-embed** makes `NULL` a valid work
queue: after interruption, non-null can mean new and null can mean pending. The design never
establishes that invariant across all five properties. Once another type was not cleared, or an old
writer races the clear, a non-null vector is indistinguishably old or new.

**Predicted failure:** the operator runs the advertised repair and gets new fact vectors while
document cards/passages, turns, and traces remain in A. The global marker says B, so the guard now
authorizes B queries over a mixed A/B corpus and has no record-level evidence with which to resume
or roll back.

**Smallest safe redesign:** choose one of two real protocols. Either stamp every vector with an
identity/generation and backfill by generation, or stop/fence every writer, atomically enter a
`repairing` generation and clear **all five** properties, run five explicit drains, verify zero
pending/old records, then publish `ready`. Two schema owners are implementation cost, not evidence
that the information is unnecessary.


## 5. “Degrade to the lexical leg that already exists” is false for document retrieval, and reasoning cannot surface the reason. [V]

- Facts do have the advertised fallback (`internal/arcadedb/memory_vector.go:245-253`). Conversation
  turns have a full-text statement and typed lexical result reason
  (`memory_conversation.go:266-280,286-395`). Unified fact/turn recall switches both statements to
  lexical when its query vector is absent (`memory_recall.go:222-265,321-332`). Reasoning traces
  also have a lexical query (`memory_reasoning_statements.go:86-97`) and fall back in
  `memory_reasoning.go:310-342,416-426`.
- Reasoning returns `[]ReasoningTrace`, not a retrieval result carrying path/reason
  (`memory_reasoning.go:310-315`). A new `reasonEmbeddingIdentityMismatch` constant beside
  `memory_vector.go:213-219` cannot be surfaced “by name” from that API without changing its
  contract.
- Documents do not have a lexical-only entry point. `HostRetriever` embeds before **both** card and
  passage legs and explicitly returns an empty `Documents` list on embedding failure
  (`internal/documents/retrieval.go:292-305`). `DocumentIndex.FusedCandidates` rejects a missing
  dense vector before querying (`internal/arcadedb/document_retrieval.go:95-112`), and both the
  passage statement (`:175-186`) and card statement
  (`internal/arcadedb/document_cards.go:94-107`) fuse/rerank with that vector. The comment saying
  the card leg “survives” (`document_cards.go:113-120`) does not match the call path.

**Predicted failure:** a provenance mismatch produces useful lexical memory results but silently
turns document search into `RetrievalCardOnly` with **zero cards**. The operator sees “degraded” and
the user sees no documents, so the advertised safe fallback is functionally an outage for one
whole schema plane.

**Smallest safe redesign:** specify and implement lexical-only card and passage queries, their
merge/ranking rule, and an explicit mismatch reason. Change reasoning's return contract if the
reason must be observable. Acceptance must query a known fact, turn, trace, and document during a
mismatch and require non-empty lexical evidence from each, not merely observe a status string.

## 6. Python cannot both use the env floor and share “one-place” identity logic; the existing supervisor seam was missed. [V]

- Ingest receives only base URL and dimensions in Compose (`compose.yaml:978-979`), hard-codes the
  request model (`services/ingest/app.py:175-182,204-212`), and receives neither revision nor
  fingerprint. If the daemon fetch fails, Python lacks the inputs needed to derive the proposed
  local identity at all.
- Reimplementing the string rules in Python repeats the exact failure used to justify centralizing
  them. The current cross-language duplication is visible in
  `internal/arcadedb/document_schema.go:131-133` and `services/ingest/arcade.py:74-83`; the spec
  criticizes that duplication at `docs/superpowers/specs/2026-09-21-embedding-provenance-and-runtime-route-design.md:58-63` and then recreates it for a
  more safety-critical contract.
- Python can be the first database creator (`services/ingest/arcade.py:149-176`), while memory
  schema is created by Go on first tenant access (`internal/arcadedb/tenant_clients.go:89-101`). A
  new marker type with “whichever writer first” ownership therefore also needs duplicated DDL or a
  sole provisioning owner the design does not name.
- The ingest container already has the missing control seam in Go. Its supervisor opens Postgres
  (`cmd/aura-ingest-supervisor/main.go:28-36`), owns the exact child environment
  (`internal/ingestsupervisor/process.go:23-48`), fingerprints each `ProcessSpec`
  (`internal/ingestsupervisor/supervisor.go:56-96`), and restarts a child when that fingerprint
  changes (`:195-223`). The design instead puts route-fetch/identity work into each Python child.

**Predicted failure:** fallback Python computes a different spelling/classification from Go, or
trusts a daemon identity while embedding through its stale local endpoint. In either case it can
write floats that do not match the marker while every comparison says equal. Independently, two
first writers can race to create/stamp an unspecified marker record.

**Smallest safe redesign:** publish one opaque canonical identity/generation and pass it with the
matching endpoint through the existing Go supervisor's `ProcessSpec`; include it in the child
fingerprint so a live route change drains/restarts workers. Python may compare the opaque value but
must not derive it. If no authority can supply the pair, start ingestion without vector writes (or
do not start the worker). Give marker schema/provisioning one owner and make absent-row creation an
atomic compare-and-set.

## 7. The unmeasured daemon proxy is a load-bearing prerequisite disguised as an open measurement. [V]

- The approved decision says credentialed embedding consumers use the daemon
  (`docs/superpowers/specs/2026-09-21-embedding-provenance-and-runtime-route-design.md:153-154,221-225`). That is not optional for cloud operation: the
  credential is deliberately not published, and neither ingest's Compose environment nor MCP's
  environment supplies the daemon's `OPENROUTER_API_KEY` (`compose.yaml:734-795,949-979`).
- The same section says proxy capacity is “not promised”, and the disclaimer leaves its
  768 MiB / 1 CPU budget unmeasured (`docs/superpowers/specs/2026-09-21-embedding-provenance-and-runtime-route-design.md:225-226,267-269`). The design thus
  approves an architecture whose only cloud data path is not part of the promise.
- `CLAUDE.md:7-30` requires measurement before an architectural amendment and says a green unit
  suite cannot close it. Proxy capacity, cancellation, batching, credential failure, and
  backpressure are behavior of the required path, not later optimization.

**Predicted failure:** the picker and marker ship, a cloud model is selected, and sidecar writers
either cannot embed at all or overload the daemon. Falling back to local would then violate the
marker; copying the key into sidecars would violate the approved credential decision.

**Smallest safe redesign:** measure the proxy on the live stack before the architecture is called
approved, including the three-writer concurrency and daemon budget. If it fails, return to the
operator with the measured alternatives. Until one credentialed path is proven, cloud embedding is
not an implementable acceptance criterion.

## 8. The picker component generalizes; the cited catalogue seam does not. [V]

- `ModelPicker` is genuinely generic over `{id: string}` and preserves typed/unpublished values
  (`web/src/settings/ModelPicker.tsx:29-39,82-123`). Reusing that component is sound.
- `useMediaModelCatalog` is closed over `MediaKind`
  (`web/src/settings/useMediaModelCatalog.ts:28-32`), and that union is exactly image, video,
  transcription, and speech (`mediaModelCatalog.ts:3,44-62`). There is no embedding kind.
- Backend registration is equally closed: only those four routes exist
  (`internal/agui/settings_api.go:94-102`); voice accepts only two constants
  (`settings_voice_models.go:17-27`), and media accepts only `mediagen.KindImage/KindVideo`
  (`settings_media_models.go:61-95`).
- On fetch failure, the current hook records `models: []`
  (`useMediaModelCatalog.ts:45-56`). It has no mandatory curated list to keep the picker useful, so
  the cited seam does not meet the spec's own “curated list is mandatory, fetch optional” rule
  (`docs/superpowers/specs/2026-09-21-embedding-provenance-and-runtime-route-design.md:151-152,230-233`).

**Predicted failure:** an implementation that merely wires `AURA_EMBED_MODEL` to the cited hook
either fails the TypeScript/backend kind constraints or shows an empty picker precisely when the
provider catalogue is down, violating the stated acceptance test.

**Smallest safe redesign:** reuse `ModelPicker`, but name a dedicated embedding catalogue DTO,
hook, route, backend lister/filter, and curated-list merge. The curated rows must be present before
and after a failed fetch; remote rows are enrichment only.


## 9. The test plan is a useful sketch, but it is not a repository-closing plan. [V]

- The spec names unit, `arcadedb_integration`, vitest, and one live-stack story
  (`docs/superpowers/specs/2026-09-21-embedding-provenance-and-runtime-route-design.md:239-254`). The integration tier is real:
  `scripts/agent_memory_eval.py:40-47` runs live race-tagged ArcadeDB suites, and its helpers fatal
  rather than skip under CI (`internal/arcadedb/testclient_test.go:16-30`,
  `cmd/arcadedb-mcp/memory_live_integration_helpers_test.go:380-393`). That part is not decorative.
- The proposed integration covers “two writers” and one generic repair. It does not name the
  Python process, all three simultaneous writers, all five vector types, legacy non-empty/no-marker
  bootstrap, first-writer CAS, a daemon unavailable at boot then recovering, an already-authorized
  session across marker change, crash after each repair phase, or document lexical behavior.
- No file targets, test files, exact commands, race/goleak scope, mutation target, Python test
  invocation, or full-matrix coverage evidence are specified. `CLAUDE.md:220-223` requires
  realistic tagged tests, no skip-as-green, package-local/tiered **85%** coverage, and daemon-free
  tests for pure logic. `CLAUDE.md:224` additionally requires a real E2E score above 9.8.

**Predicted failure:** the listed happy path can pass with fact-only repair and a status-only
degradation assertion while Python still writes stale vectors and documents return empty. A bare
new Go integration test could be green without exercising the process that owns two of the five
types.

**Smallest safe redesign:** turn acceptance into a matrix over `{daemon, MCP, Python} × five types`
and the failure transitions above. Name the exact unit/integration/Python/vitest/E2E files and
commands, require the CI-fatal live helpers, identify critical mutation targets, and close with
`scripts/coverage_docker.sh` plus the delegated `arcadedb_integration` 85% report.

## 10. Gate 1 cannot check the 600-LOC rule because the spec names no files; obvious landing files are already close. [V]

- Gate 1 requires named file targets at or below 600 LOC (`CLAUDE.md:56-64`), and the hard rule
  forbids creating or touching a file past 600 (`CLAUDE.md:192,214-215`). This design has no
  `Files` section and names behaviors rather than implementation targets.
- Current likely landing files are already substantial: `internal/arcadedb/memory_vector.go` is
  532 lines and `internal/agui/settings_api.go` is 507 lines; `services/ingest/arcade.py` is 421 and
  `web/src/settings/ModelSettingsPanel.tsx` is 330. Putting marker/repair or
  catalogue/publication logic into the first two can cross the cap with only 68 or 93 new lines. [S
  on the eventual edit; verified counts.]
- The engine inventory does **not** justify a new Go vector-math layer. Existing memory queries
  already use ArcadeDB `vector.neighbors`, `vector.fuse`, and rerank rather than Go arithmetic
  (`internal/arcadedb/memory_vector.go:24-28,128-151`). ArcadeDB's [official vector documentation](https://docs.arcadedb.com/arcadedb/concepts/vector-search)
  says embeddings are generated by an external model and documents its native search/math surface;
  `CLAUDE.md:153-168` records the repository's 78-function inventory and the verified absence of a
  text-to-vector function. A provenance state machine is an Aura control-plane gap; ranking,
  normalization, fusion, and index rebuild logic are not.
- Conversely, the proposed Python boot-fetch adapter ignores an existing package seam:
  `internal/ingestsupervisor` already owns env publication and restart-on-fingerprint-change
  (`process.go:23-48`, `supervisor.go:56-96,195-223`). That is an inventory-before-invention miss.

**Predicted failure:** implementation either bloats `memory_vector.go`/`settings_api.go` past the
hard cap or invents a second child-route lifecycle beside `ingestsupervisor`. Without file targets,
review cannot tell which before code exists.

**Smallest safe redesign:** add a file map with separate identity, provenance-state, repair,
publication, embedding-catalogue, and per-language adapter/test targets, current LOC, and projected
LOC. State explicitly that ArcadeDB keeps all vector search/fusion/index work and that the ingest
supervisor owns Python child configuration.

## 11. “What this design does not prove” is candid about measurements but omits the assumptions that decide correctness. [V]

- The four disclosures at `docs/superpowers/specs/2026-09-21-embedding-provenance-and-runtime-route-design.md:256-271` honestly call out catalogue
  modality, latency, proxy capacity, and unknown live agreement.
- They do not disclose that an absent marker cannot establish historical provenance (finding 1),
  the requested route may not be the effective model (finding 2), boot fallback invalidates a
  runtime authority (finding 3), repair exists for only one type (finding 4), or documents have no
  lexical-only path (finding 5). Those are all assumed by the architecture and acceptance text,
  not optional performance questions.
- The last bullet says the design “adds the means to answer” whether live writers agree
  (`:270-271`). It only answers whether future processes report the same configured string after a
  first writer has self-certified the database. It cannot answer which transform produced any
  existing record.

**Predicted failure:** the omissions let implementation planning treat the safety protocol as
settled while measuring only proxy throughput and catalogue shape. The project can satisfy every
declared open question and still ship silent mixed-space retrieval.

**Smallest safe redesign:** move the five correctness unknowns above into the design body as
blocked decisions with machine-checkable acceptance. Keep latency/catalogue as follow-up
measurements; do not group correctness prerequisites under “not proved”.

---


## Measured-claim verdicts

| # | Verdict | Tree evidence |
|---|---|---|
| 1 | **IMPRECISE** | Exactly three production processes persist vectors into tenant ArcadeDBs, and no fourth production writer/type was found: Go owns `FACT`, `ConversationTurn`, `ReasoningTrace` (`memory_vector.go:56-59`, `memory_conversation.go:86-92`, `memory_reasoning.go:35-44`); Python owns `Passage` and `IndexedDocument` (`services/ingest/arcade.py:247-328`). `cmd/arcadedb-mcp/main.go:8-27,65-69` imports no pgx/settings package and reads embed env directly. Among the three writers, only `cmd/aura` reads `aura.settings`; globally, `cmd/aura-media-index` also does (`cmd/aura-media-index/main.go:235-250`) but does not embed. The stated Python expression is wrong: `services/ingest/app.py:36` uses `os.environ.get(...)`, not `os.environ[...]`; the model is hard-coded at :182 and :206. Tests, spikes, and in-memory `semindex` are not a fourth persisted writer. |
| 2 | **VERIFIED** | `compose.yaml:119` supplies the non-loopback service URL; `internal/config/config_routes.go:19-27` substitutes the shared cloud base only for loopback. A model-only save therefore sends the cloud name/key to llama.cpp. The important operational refinement is that pinned llama.cpp b10951 accepts the request: `server-context.cpp:4905-4990` ignores request `model`, and `server-http.cpp:187-190` ignores Authorization when no server API key is configured. It returns a vector from Aura's loaded local GGUF, not an unknown-model error and not a cloud vector. |
| 3 | **VERIFIED** | The five-type list and owners are complete for production persisted vectors. Go's common `vectorDimensions = 768` is at `memory_vector.go:29-34`; the three Go indexes consume it. Python receives dimensions at `services/ingest/app.py:37,56` and owns both document DDL blocks at `arcade.py:247-328`. Go's `DocumentIndex` is explicitly a reader (`document_schema.go:113-117`), and shared memory schema is ensured at `tenant_clients.go:89-101`. |
| 4 | **IMPRECISE** | “Exactly one non-test use” is literally false: the values are loaded (`internal/config/config.go:415-422`), catalogued (`config_knobs.go:132-133`), forwarded (`compose.yaml:251-252`), and derived by install code (`scripts/install_env.sh:141-160`). There is exactly one **semantic runtime consumer after load**, the strict shape gate at `config_document_retrieval.go:62-67`. The substantive conclusion is verified: no vector write stores either value and no stored record is compared with either. |
| 5 | **IMPRECISE** | The named symbols exist exactly where claimed: reasons at `memory_vector.go:213-215`, fact fallback at :245-253, clear statement at :396-399, and the scheduled fact drain at `memory_backfill.go:209-231`. They do what the spec says only for `FACT`. There is no null-vector sweep for the other four types, so the assertion that existing repair machinery can repair the five-type corpus is wrong. |
| 6 | **IMPRECISE** | `ModelPicker` generalizes (`web/src/settings/ModelPicker.tsx:29-39,82-123`). `useMediaModelCatalog`, its `MediaKind`, and the backend handlers do not: they are closed to image/video/transcription/speech (`useMediaModelCatalog.ts:28-32`, `mediaModelCatalog.ts:3`, `settings_api.go:94-102`, `settings_voice_models.go:23-27`, `settings_media_models.go:61-74`). A new embedding catalogue path and curated fallback are required. |

## What survives

### Build as written

- **Detection before configuration** is the right order. A same-width model disagreement is silent
  today, and serving a new query vector against an old corpus is worse than an explicit degraded
  result.
- **Refuse dense writes/reads on a proven mismatch** and make the reason operator-visible. Keep
  lexical service available wherever a real lexical path exists.
- **Repair is explicit and never a side effect of saving settings.** The warning/acknowledgement in
  the cockpit and the destructive-operation boundary are correct.
- **Reuse `ModelPicker` itself.** Its generic/current-value behavior is a good UI primitive.
- **Keep vector math in ArcadeDB.** The repository already delegates neighbors, fusion, rerank, and
  indexes to the engine; provenance and routing are application control plane, not a reason to add
  a Go vector layer.

### Rework before implementation

- Replace the route-derived string with an attested effective-transform identity covering model
  revision/artifact, preprocessing, normalization/pooling, dimensions, and projection behavior.
- Turn the marker into a generation/state protocol with safe legacy bootstrap, atomic
  initialization, writer fencing, and a defined interruption/resume invariant. A database marker
  alone is a useful fast guard, not proof of each existing record.
- Choose explicitly between per-record generation stamps and a stop-the-world atomic clear of all
  five properties. The current hybrid — global marker plus fact-only clear — is not repairable.
- Make route changes genuinely live across daemon clients, MCP, and ingest, or state that they need
  coordinated restart. Reuse `internal/ingestsupervisor` to deliver and rotate Python child config.
- Design lexical-only document retrieval and an observable reasoning degradation contract.
- Prove the daemon proxy before cloud embedding is accepted, then write a complete file/test/coverage
  plan under the repository gates.

### Drop

- Drop **“absent marker → first writer stamps it”** for any non-empty database.
- Drop silent vector-capable **env fallback** after the daemon retry. Authority unavailable may mean
  lexical/read-only; it must not mean “guess a vector space and write”.
- Drop **“clear facts, update marker, existing sweeps repair the corpus.”** It repairs one of five
  types and has no fence.
- Drop the claim that prefixes are safely excluded because the model identity already names them;
  the tree proves the prefixes are separate, duplicated executable contracts.
- Drop the claim that `useMediaModelCatalog` and the existing handlers generalize as-is. Only the
  `ModelPicker` component does.
