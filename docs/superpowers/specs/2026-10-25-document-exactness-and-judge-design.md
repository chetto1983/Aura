# Exactness first, a judge after: what document and memory search admit

Proposed on 2026-10-25 at `20846f9a9`.
- **The direction is approved.** The operator confirmed the playback in conversation ("è corretto").
  This written spec is not yet approved.
- **The origin.** It answers the agent's first request after the floors plan, "esattezza prima, AI
  dopo" (`docs/superpowers/verification/2026-10-23-document-search-lists-lab-vm.md`, turn 3), and
  the floors record's own gap (`2026-10-23-document-floors-pplx.md`, "Exact phrases").
- **The evidence.**
  - `docs/superpowers/verification/2026-10-25-document-judge-decision-models.md` is the spike that
    measured the burial, the named-term rule and three judges. Verdict: PARTIAL.
  - Paperless-ngx's source was audited the same day, at `dev` 5f17b1a (v3.3.4).

Risk tier: **medium.**
- Admission and order change for every `document_search` and memory recall.
- A new outbound call sends passage and fact text to the judge's provider.
- New settings rows and no migration.
- With the judge route empty, the cosine path is today's, untouched.

## The problem, measured

1. **The rerank throws the lexical evidence away.**
   - The fused statement (`internal/arcadedb/document_retrieval.go`, `fusedStatement`) wraps
     `vector.fuse` in `vector.rerank`, then applies `WHERE score >= :min_relevance`.
   - `vector.rerank` emits one `score`, the cosine (`SQLFunctionVectorRerank.java`, read
     2026-10-25). `vector.fuse` emits one fused `score` and no per-source provenance
     (`SQLFunctionVectorFuse.java`).
   - So once the rerank has run, nothing knows that Lucene found the passage.
   - Memory has the same shape: `memory_vector.go` and `memory_recall.go`, fuse → rerank →
     `relevanceFloor`.
2. **An exact term is buried, then dropped.** On the lab VM on v1.2.0, with both floors off:

   | Query | `calibre_demo.docx` | Top-1 |
   |---|---|---|
   | s04, the Italian question naming "Ubuntu Mono" | 4th of 8, passage 0.199 | `SampleShow.pptx` 0.256 |
   | the bare `Ubuntu Mono` | 6th of 8, passage 0.146 | `esempio.md` 0.252 |

   - The document is the only one holding the term.
   - Its passage is one chunk of 5,037 characters, so the term is one line of it.
   - The 0.39 floor removes it from both answers inside the statement, before Go sees it.
3. **Two places lose it.**
   - The floor, measured above.
   - The rerank's candidate cut, `min(max(limit*4, 20), MaxRetrievalCandidates)`, which keeps the
     best cosines only. Not measured here: the library had 8 documents.
4. **The existing exact rule sees too little, too late.** `rankIdentifiersFirst`
   (`internal/documents/retrieval_rank.go`) promotes only tokens holding a letter and a digit
   (`arcadedb.QueryIdentifiers`), and it runs after the SQL floor.
5. **The floors cannot fix ranking, and they barely separate.** The document floor's band on
   pplx-embed is 0.0214 wide (2026-10-23).
6. **A rule on rare words breaks abstention.** Measured on the same candidates, "della" and
   "degli" occur only in the Constitution PDF and would admit it for two out-of-corpus questions.
7. **The fused query searches the stopwords too.** Its lexical sub-pipeline searches the raw
   query (`params["query"] = escapeLucene(query)`). The lexical fallback strips stopwords first
   (`document_lexical.go`, `lexicalQuery`).

## How Paperless-ngx does it (read from source, 2026-10-25)

- **Lexical search only.** Search is Tantivy. The vector index feeds only AI suggestions and chat;
  results are never fused.
- **Exact before approximate.**
  - In the UI's default mode every token must match, and the title is boosted 2.0.
  - Fuzzy matches are added with a flat score below every exact match.
  - The only cut-off is relative to the best hit, and it applies only to the fuzzy widening.
- **No absolute relevance floor.** Every hit is returned, and filters never change the order.
- **The rarest term picks the snippet.** The snippet is the fragment whose query terms are rarest,
  so a verbatim name lands on its own passage.

**Borrowed:** the lexical match decides membership. A second scorer may reorder results but never
drop an exact hit.

**Not borrowed:**
- the exhaustive list, which Paperless builds for a person scrolling while Aura's agent consumes a
  top-k;
- requiring every token, which fits a typed keyword query, not an agent's Italian sentence;
- the absence of abstention, which Aura's agent needs.

## The judge, measured (spike, PARTIAL)

The setup:
- the 29 questions of the floors set, over the operator's 8 documents;
- 232 (question, document) pairs from the shipped fused query with the floors off;
- two yes/no questions per pair, `answers` and `topical`, through OpenRouter's Decisions API.

| | Cosine (floor 0.39) | jev-1.13 | **jev-1.14** | d1-ultra |
|---|---:|---:|---:|---:|
| answerable, right top-1 | 11/12 | 12/12 | **12/12** | 12/12 |
| worst true / best negative | 0.4047 / 0.3833 | 0.78 / 0.29 | **0.94 / 0.28** | 0.70 / 0.13 |
| median per pair | — | 308 ms | 311 ms | 378 ms |

The whole set cost $0.011–0.014 per judge, about $0.0005 per search of 8 documents.

The named-term rule (decision 1) fired on 10 of the 12 answerable questions and admitted the gold
document alone every time. It admitted nothing for any of the 5 out-of-corpus questions.

## Goal

- **Named terms surface first.** A term the question names, held by a document, always reaches
  the agent, ranked first. No score removes it.
- **The judge decides admission.** When a judge route is configured, admission is decided by a
  judge that reads the question and the document, instead of a cosine floor.
- **The cosine floors stay.** They are the path when no judge is configured or it does not answer,
  and the response says which path ran.
- **"Nothing relevant" is a normal answer.** A document that is on the subject without holding the
  answer reaches the agent marked as related, not as evidence.
- **Memory recall gets the same two layers.**

## Non-goals

- **A local judge model.** It is a later route of the same shape, as the embedding sidecar is to
  OpenRouter. The operator: "ci sono anche modelli locali open ma per adesso scegliamo openrouter".
- **Chunking.** The 5,037-character chunk dilutes the cosine. That is recorded, not addressed here.
- **An operator query language** like Paperless's `tag:` and `created:[..]`.
- **Metadata editing from the tools and self-recalibration.** They are the agent's second and
  third requests, and the operator's order puts them after this spec.
- **Re-embedding, ingestion and ArcadeDB schema changes.**

## Anti-goals

- **An exact hit dropped** by the cosine, the floor, the candidate cut or the judge.
- **An exact rule that admits an out-of-corpus question.** The rare-word rule did, which is why
  the rule here is about names.
- **A judge failure** that fails a turn or silently changes what an answer means.
- **A judge threshold** used for a model it was never measured on without the response saying so.
- **Passage text sent to a provider** the operator did not choose.

## Decisions

### 1. Named terms admit and rank first

- **What counts as a named term.**
  - the text inside double quotes or « »;
  - a file name, `name.ext`;
  - a token holding a letter and a digit, or an underscore, which is today's
    `arcadedb.QueryIdentifiers`;
  - a run of capitalised words that does not start the sentence ("Ubuntu Mono", "Carretera",
    "Gross Sales");
  - a run of two or more capitalised words at the start of the sentence: the bare `Ubuntu Mono`
    names a term, and "Quanti" alone does not;
  - in a query with no lowercase letter at all (a keyword query, such as `Carretera`), every word
    that is not a stopword.
  - A lowercase word is never a named term, however rare.
  - The two sentence-initial cases are the designer's call. Acceptance 1 measures them, because the
    spike ran the rule only on full Italian sentences.
- **What carries one.** A document carries a named term when its title or its own text contains it
  as a case-insensitive, whole-word sequence.
  - Its own text is the passages and the card, minus the sentences Aura writes into every card.
  - Measured: every card says "Written by Aura at ingest from the file itself", so "Aura" matched
    all 8 documents.
- **Selectivity.** A named term carried by more than half of the library's ready documents selects
  nothing, and it is ignored. This is the designer's call, from the one measured case, h12, where
  the term was carried 8 of 8.
- **The effect.**
  - A document that carries every selective named term of the question is admitted, whatever its
    cosine or judge score.
  - It ranks ahead of every document that does not, keeping score order within each group.
  - Its evidence carries a new leg, `exact`.
  - This replaces `rankIdentifiersFirst`: identifiers become one kind of named term.
  - Carrying some named terms but not all gives no bypass (the designer's call).
- **Where the candidates come from.**
  - The full-text index must be asked for the named terms themselves, so that a carrier reaches
    Go even when the rerank cut or the floor would have removed it.
  - Read `arcadedb-docs` `how-to/data-modeling/full-text-index.adoc` before writing a matcher:
    Lucene phrase queries may express a multi-word name directly.
  - The lexical leg already exists (`LexicalCandidates`).
  - Whether exactness is a separate statement or a source inside the fused one is left to the
    builder. Measure both answers on the acceptance sets.
- **Memory.** Memory recall already drops a candidate that lacks a query identifier (letter and
  digit, `QueryIdentifiers`, applied in `memory_recall.go` and `memory_vector.go`). That filter
  stays as it is: an identifier is the same in every language. The other named terms (quotes,
  file names, capitalised runs) only admit and rank first, and never filter. A filter on
  "Torino" would drop a fact about "Turin", and cross-language recall is why memory's dense leg
  exists. The selectivity guard counts each kind on its own: valid facts, and turns that have not
  been deleted. These are the designer's calls.

### 2. A judge decides admission, routed like the embeddings

- **The route.**
  - `AURA_JUDGE_MODEL`, a model id on OpenRouter's Decisions API
    (`POST https://openrouter.ai/api/alpha/decisions`). Empty means no judge, and empty is the
    default.
  - The key is the services key (`settings.ServicesKeySetting`, read per call through
    `settings.Credentials`, like the embeddings').
  - On the lab VM the operator sets `typesafe/jev-1.14`, the widest measured margin.
  - An operator-named endpoint and a local judge are later routes, and this spec adds neither.
- **The settings.**
  - The model is an `aura.settings` row, written through the generic settings path and shown in
    the cockpit next to the embedding route.
  - The cockpit names the provider beside it, because passage and fact text go there.
  - A change needs no re-embedding and takes effect without a restart, in the daemon and in
    `aura-arcadedb-mcp`. The MCP's route watcher (`cmd/arcadedb-mcp/space_watch.go`) is the
    existing way a route change reaches that process.
- **The questions.** Each candidate gets two `noul` questions, worded as in the spike:
  - `answers`: does the document contain what the query asks;
  - `topical`: is it about the subject.
  - The state is the query, the title, the card, and at most the three passages the response would
    carry. For memory it is the fact's statement, or the turn's content.
- **What the judge sees.**
  - With a judge configured, the SQL relevance floor is not applied to the documents' fused
    statement or to the card statement (`documentCardStatement`, `document_cards.go`).
    - A document without passages reaches Go only through its card.
    - `calibre_demo.docx`'s card scored 0.228 against the 0.39 floor.
    - The spike lifted both, because `AURA_DOCUMENT_MIN_RELEVANCE` binds both.
  - The distance floor stays.
  - The judge scores the head of the merged card-and-passage ranking (`rankDocuments`'s order,
    before the limit) plus every exact carrier.
  - How long that head is, is left to the builder. It is measured on the acceptance sets for
    recall at a bounded cost. The spike judged 8 documents per search.
  - **Lexical-only mode** (the dense legs cannot run) is unchanged in this spec. It keeps its
    measured BM25 floors, and the judge does not run there. This is the designer's call, to bound
    the change; a later measurement can extend the judge to it.
- **Admission and order, with a judge.** The judge may reorder an exact carrier but never drop it,
  which is Paperless's rule.
  1. Exact carriers with `answers` ≥ T_answers, `relation: "answers"`.
  2. Other documents with `answers` ≥ T_answers, `relation: "answers"`, by `answers`.
  3. `relation: "related"`:
     - exact carriers below T_answers, whatever their `topical`;
     - other documents with `answers` < T_answers and `topical` ≥ T_topical.

     They are a place to look, not evidence: carriers first, then by `topical`.
  4. Anything else is dropped.
- **Abstention.**
  - When no document is `answers`, the response sets `abstained: true`.
  - With related documents it uses a new reason, `no_answering_document`, and lists them.
    `Abstained` keeps its meaning, "the corpus could not answer" (`retrieval.go`). The related
    documents are where the agent may look.
  - With nothing at all left, the reason is `no_qualified_passage`, as today.
  - This is the approved playback ("niente di rilevante resta una risposta normale e l'agente sa
    dove guardare"). It is also the abstention acceptance 2 counts.
- **Without a judge.** Exact carriers rank first. Every admitted document is `relation: "answers"`,
  which is today's meaning of an admitted document.
- **A file with no passages at all** (`PassageCount == 0`: an image, a video, a file known only by
  its card) cannot show the judge its answer.
  - It is admitted as `answers`, to be opened, when `topical` ≥ T_topical.
  - `reserveCardLane` keeps its one slot for it.
  - A workbook that has row passages is judged on its rows like any document. When they do not
    answer, it comes back `related`, and `requires_open` sends the agent into the file. The spike
    showed the case: h05's workbook, `topical` 0.50, `answers` 0.11.
  - These are the designer's calls.
- **The thresholds.**
  - T_answers and T_topical live in a table keyed by the judge's model id, one row per family
    (documents, memory), next to `relevance_floors.go`'s tables.
  - A model without a row is served with a default and reports `judge_reason:
    "uncalibrated_judge"`, as `uncalibrated_floors` does today.
  - The 0.5 that separated everything in the spike is in-sample. The rows are filled by the
    acceptance measurement, not copied from it.
- **Failure.**
  - No route means today's cosine floors, with no field set.
  - An error or a timeout falls back to the cosine floors for that call, and the response carries
    `judge_reason: "judge_unavailable"`.
  - The timeout is left to the builder, from measured latency. The spike's p90 per pair was
    367–477 ms, with 4 requests in parallel.
  - A judge failure never fails the turn.

### 3. The response says what happened

- **Per document.**
  - `relation` is `"answers"` or `"related"`.
  - The evidence gains an `exact` leg and a `judge` leg. The judge leg's score is `answers`.
- **Per response.** `judge_reason` is empty, `uncalibrated_judge` or `judge_unavailable`.
- **The profile.** `ProductionRetrievalProfile` is bumped, because admission changed.
- **`document_search`'s description** gains one line: a related document is a place to look, not
  a source to cite. The tool is not deferred (`Deferred: false`,
  `internal/agent/tools/document_search.go`), so the line enters every turn's manifest and changes
  the cached prompt prefix once.
- **Memory recall's result** carries the same `relation` and `judge_reason`.

### 4. Memory follows the documents

- `memory_recall` and the fact search in `cmd/arcadedb-mcp` judge their candidates the same way,
  with the memory row of the threshold table.
- Facts are short. One request per query with one `noul` per candidate is allowed if it measures
  equal to one request per pair on the memory set (TypeSafe's speculative fan-out,
  `docs.typesafe.ai/patterns/fan-out.md`).
- The memory floors on pplx-embed were never measured, which is one more reason to measure the
  judge here. The cosine floors remain memory's fallback.

### 5. Cost and observability

- Every judge call logs its model, latency, input tokens and `usage.cost`.
- One search of 8 documents measured about 11.6k input tokens, $0.0005.
- The cockpit's spend view already attributes OpenRouter cost by key. The judge spends the
  services key.

## Shape

Indicative; the builder places it.
- **A judge client, its route and its threshold table.** These are new code: a small package, or
  files beside `internal/embeddings`.
- **`internal/documents`**: `retrieval.go`, a new `retrieval_judge.go`, and `retrieval_rank.go`,
  where the exact ordering replaces `rankIdentifiersFirst`. Each file stays at 600 lines or fewer.
- **`internal/arcadedb`**:
  - `document_retrieval.go`: the floor applied only without a judge, and the stopword-stripped
    lexical sub-query, fixed on touch;
  - `document_lexical.go` and `identifiers.go`: named terms;
  - `memory_vector.go` and `memory_recall.go`.
- **`internal/settings` and `cmd/arcadedb-mcp`**: the route and its watcher.
- **`internal/agui` and `web/`**: the cockpit row.
- **`prd.md`**: the env catalog and the amendment recording the spike.

### New env vars

- `AURA_JUDGE_MODEL`: the judge's model id on OpenRouter's Decisions API. Empty, the default, means
  no judge.

## Acceptance

Measured first, then recorded (the PRD-first rule), each run with a dated file in
`docs/superpowers/verification/`:

1. **Named terms, judge off.**
   - On the lab VM library, s04 and the bare `Ubuntu Mono` return `calibre_demo.docx` first.
   - The five out-of-corpus questions of the floors set still abstain.
   - On `scripts/fixtures/document_retrieval_eval/complex_qrels.json` (34 questions), recall@1 and
     MRR are no lower than today's, and its EXACT and LOOKALIKE categories are reported apart.
2. **The judge against its baseline, out of sample** (BASELINE OR DROP).
   - The sets: `qrels.json` (20) and `complex_qrels.json` (34), plus out-of-corpus questions for
     each corpus, written and committed before the first judged run.
   - jev-1.14 must not lose recall@1 or MRR against the cosine, and must abstain on at least as many
     out-of-corpus questions.
   - The thresholds are chosen on one set and confirmed on the other.
   - If the judge does not beat the cosine, it is dropped and only decision 1 ships.
3. **Memory.**
   - A question set over a live memory: answerable and out-of-memory questions, written before the
     run.
   - The judge against the memory floors, with the same rule as acceptance 2.
4. **The fallback is today's path.**
   - With `AURA_JUDGE_MODEL` empty, every acceptance set returns the same documents in the same
     order as before, apart from decision 1's re-ordering.
   - A judge that times out yields `judge_reason: "judge_unavailable"` and the cosine answer.
5. **Gates.**
   - vet, build and race;
   - coverage: aggregate 85% and the package policy;
   - mutation ≥ 70% on the admission code;
   - goleak on the judge client.
6. **On the lab VM, by the agent itself.** The operator's validation form
   (2026-10-23, "basta che gli chiedi se i tool che ha vanno bene o se trova frizioni"):
   - Aura's agent re-checks, with its own tools, exactness ("Ubuntu Mono", an identifier) and
     abstention ("fattura", an out-of-corpus question).
   - It reports the remaining frictions, in the operator's conversation.

### 6. The judge calls the Decisions API through `openai-go`, the SDK Aura already pins

The operator decided this on 2026-10-25, in two steps. First: "scrivere un piccolo client HTTP come
quello degli embedding". Then: "guarda SDK di openai se funziona" and "anche per gli embedding
bespoke non mi piace".

**The inventory.**
- OpenRouter publishes `github.com/OpenRouterTeam/go-sdk` with `Alpha.Decisions.Create`. Aura does
  not depend on it, and this spec does not add it.
- `openai-go` v3.85.0, already in `go.mod`, has no typed Decisions service. Its README, section
  "Making custom/undocumented requests", documents `client.Post(ctx, path, params, &res)` for
  endpoints it does not type, with the client's options (base URL, key, retries) applied.
- Aura already calls an untyped OpenRouter endpoint this way: `internal/mediagen/client_video.go`,
  with `client.Post(ctx, "videos", …)`.

**Measured 2026-10-25 from WSL** (a throwaway program in the session scratchpad).
- The client: `openai.NewClient(option.WithBaseURL("https://openrouter.ai/api/v1/"),
  option.WithAPIKey(key))`.
- `client.Post(ctx, "../alpha/decisions", body, &res)` reached the API: `typesafe/jev-1.14-20261014`,
  `answers` 0.97 for the s04 pair, in 567 ms, with `usage.cost` in the answer.
- The path `/api/alpha/decisions` returned 404. The SDK trims the leading slash and resolves the
  path under `/api/v1/`, so the path must be relative to the base, or the base must be
  `https://openrouter.ai/api/`.
- The SDK checks a request's origin (scheme, host and port), not its path
  (`internal/requestconfig/origin.go`).

**So the judge is a thin seam over `openai-go`.**
- One function sends the documented request (`docs.typesafe.ai/api/system-one.md`), `noul`
  questions only, and decodes `answers`, `model` and `usage`.
- No `net/http` client of its own.
- A request with an empty key sends nothing and fails as the embeddings' `ErrNoCredential` does.

### 7. The embedding client moves to `openai-go` too, as its own slice

The operator does not want the bespoke embedding client either ("anche per gli embedding bespoke
non mi piace").
- **Measured the same day.** `client.Embeddings.New` with `perplexity/pplx-embed-v1-4b` and
  `Dimensions: 768` returned 2 vectors of 768 values in 357 ms. The cost sits in
  `usage.JSON.ExtraFields["cost"]`.
- **The local sidecar, measured the same day.** The target was the workstation's llama.cpp
  sidecar at `http://127.0.0.1:8081/v1/`, EmbeddingGemma-300M Q8_0 at 768 dimensions.
  - The SDK client was built with `option.WithAPIKey("")`, and `Embeddings.New` was sent with
    model `aura-local-embedding`, no dimensions and two prefixed inputs.
  - Logged by a middleware, the body was byte for byte the one `internal/embeddings/client.go`
    builds: `{"input":[…],"model":"aura-local-embedding"}`. No `Authorization` header was sent.
  - The vectors equal, value for value (0 of 768 differing, for both inputs), the answer to the
    same request sent the way Aura's client sends it.
- **Not measured:**
  - the hosted route's vectors against Aura's client;
  - error and status mapping (`StatusError`, timeouts, the bounded body);
  - batching beyond two inputs.
- **A separate change.** Replacing `internal/embeddings/client.go` is not part of this spec's
  admission change. It is a refactor of the path every vector is written by, and its own
  acceptance is that the same input gives the same vectors and the same errors on both routes,
  before and after.
- The client is kept with what it does beyond transport, as the refactor rule requires:
  - batching;
  - the prefix recipe;
  - the space attestation (`AttestLocal`);
  - the `ErrNoCredential` contract.
- The order between that slice and this spec is the operator's call.

## Open questions

1. **The head the judge reads** (decision 2): the number of documents per search, against
   recall@1 and cost on the acceptance sets.

## Calls made while writing, for the operator to check

- The judge is off by default. An appliance stays on the cosine.
- Named terms are capitalised runs, quotes, file names and identifiers, never lowercase words.
  - A sentence-initial run counts from two words up.
  - A query with no lowercase letter counts every word that is not a stopword.
- The selectivity guard: a term in more than half the library is ignored. In memory it is counted
  per kind.
- A partial match of the named terms gives no bypass.
- In memory, identifiers keep filtering, and the other named terms only admit and rank first.
- The judge may reorder an exact carrier, but never drop it. One that does not answer is
  `related`.
- With no answering document the response abstains, with the reason `no_answering_document`,
  and lists the related documents.
- A file with no passages at all is admitted on `topical`. A workbook with rows that do not answer
  is `related`.
- With a judge, the card statement's floor is lifted too. Lexical-only mode keeps its BM25 floors.
- `ProductionRetrievalProfile` is bumped.

## What this spec does not prove

- **The judge.** It rests on 8 documents, 29 questions written by an agent that had read them,
  one run per model, and an in-sample threshold. Acceptance 2 is the real test, and it can drop
  the judge.
- **The named-term rule.** It was checked on Italian question syntax and one library.
  Capitalisation is a weak signal in a question typed all lowercase; there the judge, not the
  rule, carries exactness.
- **The candidate cut.** Its effect in a library larger than 8 documents was not measured.
- **Memory.** Nothing here was measured on memory.
- **The judge's latency on the agent's turn**, against the 0.6 s a search of 8 documents measured
  in the spike.
