# Exactness first, a judge after: what document and memory search admit

Proposed on 2026-10-25 at `20846f9a9`.
- **Approved on 2026-10-25.**
  - The operator confirmed the playback ("è corretto").
  - Then the written spec, with the designer's calls listed below, and the order ("va bene").
- **The order, in three slices, each with its own plan:**
  1. exact terms (decision 1);
  2. the embedding client on `openai-go` (decision 7);
  3. the judge (decisions 2–6), built on the same client shape.
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

- **Exact terms surface first.** A term the caller quotes, an identifier or a file name, held by a
  document, always reaches the agent, ranked first. No score removes it.
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

### 1. Exact terms admit and rank first

**Amended 2026-10-25, while planning, on the operator's decision ("procedi").**
- **What the first wording did.** It recognised names from capital letters, from the position in
  the sentence and from a stopword list.
- **Why that went.** That is the heuristic on natural language the operator's standing rule
  forbids: regexes and static word lists over prose are fragile, and a structured signal must be
  found instead.
- **How it surfaced.** The planner's first test showed the failure: "Ho letto il file. Mostra Ubuntu
  Mono?" produced the term "mostra ubuntu mono". No document carries it, so the bypass would have
  switched off without a word.
- **The new direction.** Only structured signals remain, and the agent, which understands the
  language, chooses the words. This is also where 2026's agentic-search work points:
  - Pi-Serini (arXiv 2605.10848): BM25 driven by the model, 83.1% on BrowseComp-Plus;
  - READ (arXiv 2608.06305): embedding-free lexical operations, 58.8% against 15.7% for dense top-k;
  - "Is Grep All You Need?" (arXiv 2605.15184): literal search above vector search in every
    harness tried.
- **The spike.** Its named-term measurement used the dropped heuristic. It records what that
  heuristic did and is not evidence for this rule.

- **What counts as an exact term.** Only these three structured forms:
  - the text inside double quotes, `"…"`, `“…”` or `«…»`, written by the caller.
    - `document_search`'s description tells the agent to quote a name or a phrase that must
      appear verbatim, as Paperless's query syntax does.
    - Memory recall's description says the same.
  - a token holding a letter and a digit, or an underscore: today's `arcadedb.QueryIdentifiers`.
  - a file name, a word ending in `.` and an extension of 2 to 5 letters or digits, at least one of
    them a letter, such as `Financial_Sample.xlsx`. A file name is a structured format, the
    exception the rule allows.
  - Nothing else is an exact term: no capitalisation, no sentence position, no stopword list.
- **What carries one.** A document carries an exact term when its title or its own text contains it
  as a case-insensitive, whole-word sequence.
  - Its own text is the passages and the card, minus the lines Aura writes into cards about
    itself: `filecard`'s trailer, its cut notice, and the caveats saying it did not read the file
    (amended after the review, below).
  - Measured: every card says "Written by Aura at ingest from the file itself", so "Aura" matched
    all 8 documents.
- **No selectivity guard.** The guard was the designer's call from one case, h12, and the trailer
  caused that case. Without the trailer, "Aura" is carried by the 2 documents that contain it. An
  exact term is now one the caller chose, so a document that carries it is what was asked for.
  - **Open, from the review (2026-10-25).** That holds for a quoted phrase only. Identifiers and
    file names are extracted, not chosen, so `A4`, `Q3`, `24h` or `COVID-19` in a question now admit
    every document carrying them past the floor. Acceptance 1 adds out-of-corpus questions holding
    such tokens. If they break abstention, identifiers go back to ranking first only and only quoted
    phrases admit: the operator's call, on that measurement.
  - Measured on the lab VM on 2026-10-25 (`2026-10-25-document-exact-terms-lab-vm.md`): none of
    `A4`, `Q3`, `24h`, `COVID-19` admitted a document through the exact lane, because the library
    holds none of them. One question cleared the floor by its cosine alone. The question stays open
    for a library that holds such a token.
- **The effect.**
  - A document that carries every exact term of the question is admitted, whatever its cosine or,
    later, judge score.
  - It ranks ahead of every document that does not, keeping score order within each group.
  - Its evidence carries a new leg, `exact`.
  - This replaces `rankIdentifiersFirst`: identifiers become one kind of exact term.
  - Carrying some exact terms but not all gives no bypass.
- **Where the candidates come from.** The full-text indexes are asked for each exact term as a
  Lucene phrase, so a carrier reaches Go even when the rerank cut or the floor would have removed
  it.
  - Lucene phrases are documented in `arcadedb-docs`, `how-to/data-modeling/full-text-index.adoc`,
    "Phrase Queries".
  - The indexes are the passages', the cards' and the split file names'.
  - The phrase is the term as the caller wrote it. Lucene's own analyzer then tokenises it the way
    it tokenised the documents, and Go checks the carrying on the texts returned.
- **Amended 2026-10-25, after the slice 1 review: one query per index, then the check in Go.**
  - **One query per index asks for every term**, the phrases OR-ed. ArcadeDB 26.10.2's executor
    reads OR-ed phrases as SHOULD clauses and returns their union
    (`FullTextQueryExecutor.collectMatches`). A search costs 3 full-text queries in the documents,
    2 in a recall and 1 in a fact search, whatever the number of terms; it cost 3, 2 and 1 per term.
  - **The lane returns what carries any term, and Go decides who carries every one**, on all the
    ranking holds for a document: its title, its card without Aura's notes, its returned passages.
    The first plan kept only documents the indexes found for every term, and the engine and Go
    tokenise apart: StandardAnalyzer keeps `calibre_demo.docx` one token, while Go cuts the
    identifier `calibre_demo` from it, which no index holds. Every file name with an underscore
    stayed buried. Measured live on 26.10.2: "apri calibre_demo.docx", unquoted, with every card
    pointing away from the query's vector, now returns the file first by its exact evidence.
  - **ArcadeDB 26.10.2's phrase ignores word order** (`collectPhraseMatches`: "We can't verify word
    order without position indexing, so we just require all terms"), so every returned text is
    checked in Go.
  - **The lane's pool is bounded.** It reads each index up to its candidate cap, best BM25 first:
    200 passages and 200 cards per index in the documents by default
    (`AURA_DOCUMENT_RETRIEVAL_CANDIDATES` and the retriever's candidate limit), 400 records per kind
    in memory (`HybridCandidates`). A term held in more records than that is checked only
    in the best-matching ones, and `document_search`'s description says so. This is the one
    candidate cut the anti-goal above still meets; it is not measured on a library that large.
  - **A failed lane is named, never hidden.** The documents report `exact_terms_unavailable` in
    `degradation_reason`, joined to the legs' own reason with `+`, and never abstain on it: without
    the lane, an empty answer no longer says the library holds no carrier. Memory reports
    `exact_terms_failed` the same way, and an abstention keeps it.
  - **Not covered.** An Italian elided article hides a quoted name from the phrase query:
    StandardAnalyzer has no ElisionFilter, so "dell'Agenzia" is one token and `"Agenzia delle
    Entrate"` finds nothing there, while Go would accept the text. Reasoned from UAX#29, not
    measured; fixing it is an analyzer change, a non-goal. Acceptance 1 adds one such case.
- **Memory.**
  - Recall already drops a candidate that lacks a query identifier (`IdentifiersMatch`, in
    `memory_recall.go` and `memory_vector.go`), and that filter stays.
  - Every exact term (identifier, quoted phrase, file name) also admits and ranks first, past the
    relevance floor.
  - Quoted phrases and file names never filter. A filter on "Torino" would drop a fact about
    "Turin", and cross-language recall is why memory's dense leg exists.

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
  - `document_lexical.go` and `identifiers.go`: exact terms;
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

1. **Exact terms, judge off.**
   - On the lab VM library, s04 with the name quoted (`… mostra "Ubuntu Mono" …`) and the bare
     `"Ubuntu Mono"` return `calibre_demo.docx` first.
   - On the lab VM, Aura's agent, asked to look for a name it knows is written in a document,
     quotes it without being told how (the operator's validation form, acceptance 6).
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
- Exact terms are quoted phrases, identifiers and file names only. This was amended while planning,
  on the operator's rule against heuristics over prose.
- No selectivity guard. Its one measured case was Aura's own card trailer, which is now stripped.
- A partial match of the exact terms gives no bypass.
- In memory, identifiers keep filtering. Every exact term admits and ranks first.
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
