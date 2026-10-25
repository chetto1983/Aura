# Exact terms rank first (slice 1) Implementation Plan

**Status (2026-10-25):** BUILT, NOT LANDED.
- Tasks 1–5 are written and verified in the scratch worktree `D:/Aura-scratch-named-terms`, detached at
  `e3e562cac`, tip `b65012631`. Unit, race, lint and the live ArcadeDB 26.10.2 tier are green, with the
  exceptions under "Local baseline".
- Task 6 is open: land on master, run the gates, measure, amend the PRD, and run acceptance 1 on the lab
  VM.

Operator's decisions on 2026-10-25:
- the order: exact terms first, then the embeddings on `openai-go`, then the judge ("VA BENE");
- exact terms from structured signals only ("PROCEDI", spec decision 1 as amended in `acbcc8325`);
- the local ArcadeDB moves to the pinned 26.10.2 ("26.10.1 PASSA ALLA 26.10.2").

> **For agentic workers:** the code below is already the implementation. It was built and verified in
> the scratch worktree, so it is not transcribed again (operator, 2026-10-12: "ma se hai già fatto il
> codice perchè rifarlo?"). Task 6 lands the verified commits as they are, through lefthook, then runs
> the real gates and one review. Steps use checkbox (`- [ ]`) syntax. Commits on master.

**Goal:** a term the caller writes verbatim reaches the agent first, from `document_search`, from
memory recall and from the fact search, whatever its cosine. The term is a quoted phrase, an
identifier or a file name. The judge stays off: this is slice 1 of the spec.

**Architecture:**
- **What an exact term is** (`internal/arcadedb/exact_terms.go`). `QueryExactTerms` reads three
  structured forms, and nothing else:
  - the caller's quoted phrases;
  - its file names;
  - its identifiers (`QueryIdentifiers`).
  - `ExactTerm.CarriedBy` checks the term's tokens in order, as whole tokens. `CarriesEvery` is the one
    carrying rule both packages use.
- **Where carriers come from.** For each term, the full-text indexes are asked for the term as one
  Lucene phrase, with no score floor, inside the caller's scope:
  - documents: `DocumentIndex.ExactTermDocuments`, over passages, cards and split file names;
  - memory: `exactMemoryRIDs`, over facts and turns.
  - ArcadeDB 26.10.2 does not keep a phrase's word order, so every text it returns is checked in Go.
  - A card is checked without the two sentences `filecard` writes into every card
    (`filecard.WithoutAuraNotes`).
- **Documents** (`internal/documents/retrieval_exact.go`):
  - `Retrieve` runs the exact lane beside the fused or the lexical leg, and keeps what it found for the
    documents found for every term.
  - `rankDocuments` merges that evidence under the leg `exact`, without touching score or order.
    `rankExactTermsFirst` then moves the carriers ahead, replacing `rankIdentifiersFirst`.
  - The reserved card place sits behind the carriers.
  - The response abstains only when no leg and no carrier answered.
- **Memory** (`internal/arcadedb/memory_exact.go`):
  - Recall and the fact search hydrate the lane's records with the legs' records, and carriers rank
    first. A record only the lane found that misses a term is dropped.
  - The identifier filter stays. Phrases and file names never filter.
- **Tools.** The descriptions tell the agent to quote a name that must appear verbatim.

**Tech Stack:** Go, ArcadeDB 26.10.2 (`SEARCH_INDEX`, Lucene phrase queries, StandardAnalyzer),
golangci-lint v2.14.0, lefthook, WSL for the race and live tiers.

**Spec:** `docs/superpowers/specs/2026-10-25-document-exactness-and-judge-design.md`, decision 1 and
acceptance 1. The spike it rests on: `docs/superpowers/verification/2026-10-25-document-judge-decision-models.md`.

## Global Constraints

- **Exact terms, and only these** (spec decision 1):
  - text inside `"…"`, `“…”` or `«…»`;
  - a token holding a letter and a digit, or an underscore;
  - a word ending in `.` and an extension of 2 to 5 letters or digits, at least one a letter.
  - No capitalisation, no sentence position, no stopword list.
- **What carries a term:** the title or the document's own text holds it as a case-insensitive
  whole-token sequence. Its own text is the passages and the card, minus `filecard`'s trailer and cut
  notice.
- **No selectivity guard.**
- **Who is admitted:** a document carrying every exact term is admitted whatever its score, and ranks
  ahead of every document that does not. Its evidence carries the leg `exact`. Carrying some terms
  but not all gives no bypass.
- **Memory:** the identifier filter stays. A quoted phrase or a file name never filters.
- **No schema change and no re-ingest** (spec non-goal). **No judge, and no `AURA_JUDGE_MODEL`**, in
  this slice.
- **Every file at 600 lines or fewer.**
- **`document_search` is not deferred**: its new description line changes the cached prompt prefix
  once.

## Review Focus

These are the inputs most likely to bite a user, each pinned by a test in its task:
- **The phrase's words in another order** ("Mono … Ubuntu"). The engine returns that text, but it is not
  a carrier.
  - Task 2: `TestExactTermDocumentsKeepOnlyTheCarriers`, `TestDocumentExactLivePhraseFindsTheTermInOrderOnly`.
- **More exact terms than the response carries passages.** The cut keeps the passages with exact
  evidence, so the carrier keeps its place. Task 3: `TestThePassageCutKeepsTheExactEvidence`.
- **A spreadsheet's reserved place.** It never lands between two carriers. Task 3:
  `TestTheReservedPlaceSitsBehindTheCarriers`.
- **A failing exact lane.** It costs the bypass, never the answer.
  - Task 3: `TestAnExactLaneFailureKeepsTheAnswer`.
  - Task 4: `TestRecallNamesAFailedExactLane`.
- **A quoted name in another language.** It never filters memory: `"Torino"` keeps the fact about
  Turin. Task 4: `TestAQuotedPhraseNeverFiltersRecall`.

---

### Task 1: exact terms from structured signals

Scratch commit `308ba82d3`, `feat(arcadedb): exact terms from quotes, identifiers and file names`.

**Files:**
- Create: `internal/arcadedb/exact_terms.go`, `internal/arcadedb/exact_terms_test.go`.
- Modify: `internal/documents/filecard/card.go` (`WithoutAuraNotes`) and
  `internal/documents/filecard/description_test.go`.

**Interfaces (produces):**
- `type ExactTerm struct{ Raw, Key string }`:
  - `Raw` is the caller's wording;
  - `Key` is its lowered tokens joined by a space.
- `func QueryExactTerms(query string) []ExactTerm`: quoted phrases, then file names, then identifiers,
  each once by `Key`.
- `func (t ExactTerm) CarriedBy(texts ...string) bool`: a contiguous run of the term's tokens in one
  text.
- `func CarriesEvery(terms []ExactTerm, texts ...string) bool`: false when there are no terms. It is
  added in Task 4's commit.
- `func (t ExactTerm) lucenePhrase() string`: the term in quotes, `\` and `"` escaped.
- `func filecard.WithoutAuraNotes(rendered string) string`: the rendered card without its trailer and
  cut notice.

- [x] **Tests.**
  - `TestQueryExactTerms`, 12 cases. Among them, `cerca “Gross Sales” e «Units Sold» in
    Financial_Sample.xlsx` yields `gross sales`, `units sold`, `financial_sample xlsx` and
    `financial_sample`.
  - `TestExactTermKeepsTheCallersWording`.
  - `TestExactTermCarriedBy`: the reversed words, split texts and a whole identifier.
  - `TestExactTermLucenePhrase`.
  - `FuzzQueryExactTerms`.
  - `TestWithoutAuraNotesDropsTheTrailerAndTheCutNotice`: the stripped lines equal the rendered
    lines minus the two notes.
- [x] **Verified:** unit and race green; lint clean.

### Task 2: the documents that carry an exact term

Scratch commit `ae6324634`, `feat(arcadedb): find the documents that carry an exact term`.

**Files:**
- Create:
  - `internal/arcadedb/document_exact.go`;
  - `document_exact_test.go`;
  - `document_exact_live_integration_test.go` (tag `arcadedb_integration`).
- Modify:
  - `internal/arcadedb/document_retrieval.go`: `RetrievalLegExact`, and `Score()` reads the lexical
    score for the exact leg;
  - `document_lexical.go`: the helpers `lexicalPassages` and `lexicalCards`, shared with the lexical
    legs;
  - `document_space_live_integration_test.go`: `seedLibrary` takes the library to seed.

**Interfaces (produces):**
- `const RetrievalLegExact RetrievalLeg = "exact"`.
- `type ExactTermHits struct{ Passages []PassageCandidate; Cards []DocumentCard }`.
- `func (d *DocumentIndex) ExactTermDocuments(ctx context.Context, filter CandidateFilter, term ExactTerm) (ExactTermHits, error)`:
  - the phrase, at `min_lexical_score` 0, inside `candidateWhere(filter)`;
  - passages that do not carry the term are dropped before each file's best is chosen;
  - cards are checked on their file name and `WithoutAuraNotes(card)`;
  - a missing type is an empty library.

- [x] **Tests:**
  - `TestExactTermDocumentsKeepOnlyTheCarriers`:
    - a reversed passage is dropped;
    - the document's carrying passage is kept, though another of its passages scored higher;
    - a card without the phrase is dropped;
    - 3 requests, params `"Ubuntu Mono"` and 0, no `vector.` in the statements.
  - `TestExactTermDocumentsIgnoreTheCardTrailer`.
  - `TestExactTermDocumentsTreatAMissingTypeAsAnEmptyLibrary`.
- [x] **Live** (`TestDocumentExactLivePhraseFindsTheTermInOrderOnly`, on 26.10.2):
  - the phrase finds `doc_calibre` alone;
  - the file name finds its card;
  - `"Aura"` finds no card and only the Markdown passage that names Aura.

### Task 3: documents rank the carriers first

Scratch commit `2df8c7692`, `feat(documents): a document carrying every exact term ranks first`.

**Files:**
- Create: `internal/documents/retrieval_exact.go` and `retrieval_exact_test.go`.
- Modify:
  - `internal/documents/retrieval.go`:
    - `PassageIndex.ExactTermDocuments`;
    - the lane in `Retrieve`;
    - one abstention rule;
    - `ProductionRetrievalProfile = "arcadedb-fused-card-v4"`.
  - `retrieval_rank.go`:
    - `rankDocuments`' new signature;
    - the lane's evidence;
    - `sortedPassages` puts exact evidence first;
    - `reserveCardLane(ranked[carriers:])`;
    - `rankIdentifiersFirst` and `carriesIdentifiers` deleted.
  - The callers' tests: `retrieval_test.go` (the fake), `retrieval_rank_test.go`,
    `retrieval_duplicate_test.go`.
  - The `retrieval_eval` benches: `retrieval_fusion_bench_test.go`, `retrieval_recall_bench_test.go`
    and `retrieval_coding_questions_bench_test.go`. The last re-derives production's order: exact
    evidence sets no score, carriers come first, then the reserved places.
  - `internal/arcadedb/document_exact_live_integration_test.go`: the end-to-end case.

**Interfaces:**
- Consumes: `QueryExactTerms`, `ExactTermDocuments`, `RetrievalLegExact`, `CarriesEvery`.
- Produces:
  - `type exactHits struct{ passages []arcadedb.PassageCandidate; cards []RetrievalCard }`;
  - `type exactLane struct{ terms []arcadedb.ExactTerm; hits []exactHits }`, one `hits` entry per term;
  - `func (r *HostRetriever) exactTermLane(ctx, filter arcadedb.CandidateFilter, terms []arcadedb.ExactTerm) exactLane`;
  - `func rankDocuments(cards []RetrievalCard, passages []arcadedb.PassageCandidate, exact exactLane, names map[string]string, limit, topPassages int) []RetrievalDocument`;
  - `func rankCardsOnly(cards []RetrievalCard, terms []arcadedb.ExactTerm, limit, topPassages int) []RetrievalDocument`;
  - `func rankExactTermsFirst(ranked []*rankedDocument, terms []arcadedb.ExactTerm) int`;
  - `func carriesExactTerms(document RetrievalDocument, terms []arcadedb.ExactTerm) bool`.

- [x] **Tests** (`retrieval_exact_test.go`):
  - **Ranking:**
    - `TestAnExactCarrierTheFloorDroppedRanksFirst`: calibre first, its score 0, one exact evidence at
      6.1;
    - `TestOnlyACarrierOfEveryExactTermRanksFirst`;
    - `TestTheCardTrailerCarriesNoExactTerm`, through a card rendered by `filecard`;
    - `TestThePassageCutKeepsTheExactEvidence`: ordinals `[4 6 9]`;
    - `TestTheReservedPlaceSitsBehindTheCarriers`: `[x.md y.md a.md sheet.xlsx b.md]`.
  - **`Retrieve`:**
    - `TestRetrieveRanksTheExactCarrierFirstInBothModes`, dense and lexical:
      - the lane is asked inside the resolved scope;
      - names are looked up for the carrier.
    - `TestRetrieveAnswersWithAnExactCarrierAlone`: no abstention.
    - `TestRetrieveAdmitsOnlyTheDocumentsCarryingEveryExactTerm`: three terms; `other.docx` carries one.
    - `TestAnExactLaneFailureKeepsTheAnswer`: complete, no degradation, `exact_terms_unavailable`
      logged with its cause.
    - `TestAQuestionWithoutExactTermsAsksNoExactLane`.
  - **`retrieval_rank_test.go`:** the identifier cases move to `exactLaneFor`, and
    `TestACardOnlyAnswerRanksTheCarrierFirst` is added.
- [x] **Live:** `TestDocumentExactLiveRetrieveRanksTheQuotedFontFirst`.
  - `quale font mostra "Ubuntu Mono"?` and `"Ubuntu Mono"` put `doc_calibre` first, with exact
    evidence, and `doc_reversed` is absent.
  - Unquoted, `doc_calibre` is absent. That is the burial, reproduced.
- [x] **Verified:**
  - unit and race green on `documents`, `arcadedb`, `agent/tools` and `cmd/aura`;
  - `go vet -tags retrieval_eval` clean;
  - two hand mutations killed: no exact-first cut, and the reserved place over the whole list.

### Task 4: memory admits a carrier past the floor

Scratch commits `993c9bb01`, `feat(arcadedb): memory admits a carrier of every exact term past the
floor`, and its `fixup!` `b65012631` (a turn carrier).

**Files:**
- Create:
  - `internal/arcadedb/memory_exact.go`;
  - `memory_exact_test.go`;
  - `memory_exact_live_test.go` (tag `arcadedb_integration`).
- Modify:
  - `internal/arcadedb/memory_recall.go`:
    - `recallRankedRID.exactOnly`;
    - the lane in `recallSemantic`;
    - `exactRecallOrder` in `hydrateRecallRanking`;
    - the reasons, through `joinReasons`.
  - `memory_vector.go`: `SearchFactsHybrid` decides abstention once, after `exactFactsFirst`, and
    `rankFactsHybrid` holds the fused and fallback read.
  - `exact_terms.go`: `CarriesEvery`, and `equalTokens` becomes `slices.Equal`.
  - `internal/documents/retrieval_exact.go`: `carriesExactTerms` calls `CarriesEvery`.

**Interfaces (produces):**
- `const reasonExactTermsFailed = "exact_terms_failed"`.
- `func (c *Client) exactMemoryRIDs(ctx, statements []string, params map[string]any, excluded []string, terms []ExactTerm) ([]recallRankedRID, error)`:
  - it reuses `recallFactLexicalStatement` and `recallTurnLexicalStatement`, at `min_lexical_score` 0;
  - every record it finds is marked `exactOnly`.
- `func withExactOnly(ranked, exact []recallRankedRID) []recallRankedRID`.
- `func exactRecallOrder(ranked []recallRankedRID, facts map[string]FactHit, turns map[string]ConversationTurnHit, terms []ExactTerm) []recallRankedRID`.
- `func (c *Client) exactFactsFirst(ctx, query string, hits []FactHit, limit int, asOf time.Time) ([]FactHit, error)`.
- `func joinReasons(reasons ...string) string`.

- [x] **Tests** (`memory_exact_test.go`):
  - `TestRecallAdmitsAnExactCarrierTheFloorDropped`: score 0; the phrase at floor 0 with no vector; the
    identity on the turn lane; one lane read per record type.
  - `TestRecallAdmitsAConversationCarryingTheQuotedPhrase`.
  - `TestRecallRanksTheExactCarrierFirst`: the carrier keeps its own score, 0.4.
  - `TestRecallDropsALaneRecordMissingATerm`: two phrases, so the identifier filter cannot be what drops
    it.
  - `TestAQuotedPhraseNeverFiltersRecall`.
  - `TestRecallNamesAFailedExactLane`.
  - `TestFactSearchAdmitsAnExactCarrierTheFloorDropped`.
  - `TestFactSearchRanksTheExactCarrierFirstOnTheLexicalFallback`: no second hydration.
  - `TestCarriesEvery` (`exact_terms_test.go`).
- [x] **Live:** `TestMemoryExactLiveQuotedNameAdmitsTheFactTheFloorDropped`, with a fixed-axis
  embedder.
  - Unquoted, the floor drops the font's fact.
  - Quoted, it comes first in recall and in the fact search, on the hybrid path.
- [x] **Verified:**
  - unit and race green;
  - the existing identifier tests unchanged and green;
  - one hand mutation killed: lane-only non-carriers admitted.

### Task 5: the tools say how to ask

Scratch commit `f2c3f50d3`, `feat(tools): tell the agent to quote a name that must appear verbatim`.

**Files:**
- Modify:
  - `internal/agent/tools/document_search.go`, its description and the `query` parameter;
  - `cmd/arcadedb-mcp/tool_memory_recall.go` and `tool_memory.go`, the `query` parameter;
  - `docs/arcadedb-mcp-live-tools.json`, regenerated with `UPDATE_GOLDEN=1`, 2 lines.
- Test:
  - `internal/agent/tools/document_search_test.go`;
  - new `cmd/arcadedb-mcp/tool_memory_exact_test.go`.

**Copy fixed here:**
- **`document_search`:** "Quote a name or a phrase that must appear verbatim, as in "Ubuntu Mono": a
  document carrying every quoted phrase, identifier and filename of the query ranks first whatever its
  score, with an exact evidence leg."
- **Its `query` parameter:** "…; quote a name that must appear verbatim."
- **The memory `query` parameters:** "Quote a name or phrase that must appear verbatim: a fact (or
  turn) carrying every quoted phrase, identifier and filename ranks first / comes first."

- [x] **Tests:**
  - `TestDocumentSearchSpecTellsTheAgentToQuoteVerbatimNames`;
  - `TestMemoryQueriesTellTheAgentToQuoteVerbatimNames`, through the listed MCP schemas;
  - `TestMemoryRecallDescriptionTeachesModeSelection`, which holds the 768-byte cap, still green.
- [x] **Verified:** race green on `agent/...`, `cmd/arcadedb-mcp` and `cmd/aura`; the golden test is
  green after regeneration.

### Task 6: land, measure, record, accept

- [ ] **Step 1: Land the verified commits on master** through lefthook, in order:
  ```bash
  git cherry-pick 308ba82d3 ae6324634 2df8c7692
  git cherry-pick -n 993c9bb01 b65012631
  git commit   # Task 4's message, from `git log -1 --format=%B 993c9bb01`
  git cherry-pick f2c3f50d3
  git diff b65012631 HEAD -- $(git diff --name-only e3e562cac b65012631)   # expected: empty
  ```
  Master has moved since `e3e562cac`, but no commit after it touches these 31 files (checked
  2026-10-25), so the picks apply cleanly.
- [ ] **Step 2: Gates, in WSL, on master:**
  - `go vet`, `go build`, and `go test -race` on these packages: `internal/arcadedb`,
    `internal/documents/...`, `internal/agent/...`, `cmd/arcadedb-mcp` and `cmd/aura`;
  - `go vet -tags retrieval_eval ./internal/documents/` and `-tags arcadedb_integration` on the two
    `cmd`s;
  - the live tier, `-tags arcadedb_integration -run 'Document|MemoryExact' ./internal/arcadedb/`;
  - `golangci-lint`, with and without the tags;
  - `bash scripts/coverage_docker.sh`: the 85% aggregate and the package policy. `internal/arcadedb`
    is delegated to its live profile (amendment #203).
  - Mutation runs in CI.
- [ ] **Step 3: One independent review** of the five commits, by a fresh read-only reviewer on the most
  capable model. Fix what it finds in follow-up commits.
- [ ] **Step 4: Measure the cost of a term.** On the lab VM, time one `aura docs search` with 1, 4 and
  16 identifiers. Each term costs 3 full-text queries per document search and 2 per memory read.
  - There is no cap today.
  - If 16 identifiers cost more than one search's budget, record the number and propose a cap from it.
    Never cap before the measurement.
- [ ] **Step 5: Acceptance 1 on the lab VM**, once the updater has applied the landed SHA:
  - `docker exec aura aura docs search '<s04 with "Ubuntu Mono" quoted>'` and
    `'"Ubuntu Mono"'` return `calibre_demo.docx` first.
  - The five out-of-corpus questions of the floors set still abstain.
  - `TestCodingAgentQuestions` (tag `retrieval_eval`) on `complex_qrels.json`, before and after the
    landing, on the same identity:
    - recall@1 and MRR must be no lower;
    - EXACT and LOOKALIKE are reported apart, from `AURA_BENCH_OUT`.
    - Re-seed the complex identity as in `2026-10-17-workbook-row-passages.md` if it is gone.
  - The operator's validation form: in the operator's conversation, ask Aura's agent whether its
    tools work or show friction. Without being told to, does it quote a name it knows is in a
    document? It also re-checks "Ubuntu Mono", an identifier and "fattura" (acceptance 6).
- [ ] **Step 6: Record.**
  - `docs/superpowers/verification/2026-10-2x-document-exact-terms-lab-vm.md`, with what was measured
    and what it does not show.
  - The PRD amendment that records it, amending "Exact identifiers rank first". It cites the
    measurement and the phrase-order finding.
  - This plan's Status line, in the same commit.
  - Push only when the operator asks, with the WSL recipe; then check CI on that SHA.

## File structure

| File | Responsibility |
|---|---|
| `internal/arcadedb/exact_terms.go` | What an exact term is, and what carries it |
| `internal/arcadedb/document_exact.go` | The documents carrying one term, from the three full-text indexes |
| `internal/arcadedb/document_lexical.go` | Lexical passage and card reads, shared by the lexical leg and the exact lane |
| `internal/arcadedb/memory_exact.go` | Memory's exact lane, the carriers-first order, the fact search's carriers |
| `internal/documents/filecard/card.go` | `WithoutAuraNotes`: a card without the sentences Aura writes into it |
| `internal/documents/retrieval_exact.go` | The documents' exact lane, carriers, carriers-first ranking |
| `internal/documents/retrieval_rank.go` | `rankDocuments` with the lane's evidence; the exact-first passage cut |
| `internal/documents/retrieval.go` | The lane in `Retrieve`, one abstention rule, profile v4 |
| `internal/agent/tools/document_search.go`, `cmd/arcadedb-mcp/tool_memory*.go` | How the agent is told to quote |

## Decisions taken while writing this plan

- **Exact evidence sets no score and no order.** One phrase's BM25 is not the scale of the legs.
  - A carrier only the lane found reports score 0. Memory's JSON omits it.
  - The carriers rank first by `rankExactTermsFirst`, not by a number.
- **A carrier is judged on what the response returns:** the title, the card without its notes, and the
  passages returned.
  - The passage cut keeps exact evidence first. So a document loses the bypass only when more than
    `TopPassages` (3) of its terms each sit in a different passage, and the agent could not have seen
    the fourth anyway.
  - The `retrieval_eval` bench re-derives the same order from the same data.
- **The documents' lane keeps a document only if it was found for every term.** The match is by
  content key, as `rankDocuments` merges, since the passage index returns one copy of a file stored
  twice.
  - Memory takes the union and checks `CarriesEvery` on the hydrated record, because the `FACT[statement]`
    index does not hold a fact's subject and object names.
- **The reserved card place sits behind the carriers.** A file the question names verbatim outranks
  one that only might answer it. With no carriers, the order is unchanged.
- **The lane runs in lexical mode too.** The card-only degraded paths have no passage index, so the lane
  does not run there, but the cards that carry the terms still rank first.
- **A lane failure: two conventions, each the subsystem's own.**
  - Documents log `exact_terms_unavailable` through the degradation log and leave the response
    untouched, as `passage_names_unavailable` does.
  - Memory names `exact_terms_failed` in the result's reason, joined with `+`, because memory names
    every reason a read is less than whole.
- **The engine's phrase order.** ArcadeDB 26.10.2's phrase query does not check word order. Read in
  `FullTextQueryExecutor.collectPhraseMatches`, which says "We can't verify word order without position
  indexing, so we just require all terms"; the docs promise the order. So `CarriedBy` checks every
  returned text in Go, before a file's best passage is chosen.
  - Reporting this upstream is the operator's call, not done.
- **`forceOpen` is removed.** It was dead: a document with no passage already requires opening.
  - When the fused leg returns nothing, cards and carriers take the one `rankDocuments` path. Names and
    neighbours cost nothing there.
- **Memory's rule lives in the `query` parameter.** `memory_recall`'s description is held under
  768 bytes by a test.
- **No cap on the number of exact terms.** It is not measured; Task 6 Step 4 measures it.

## What this plan does not cover

- **A card's caveats.** "Aura did not read this file at ingest" and the PDF caveat name Aura and
  "ingest" inside unread cards, and `WithoutAuraNotes` strips only the trailer and the cut notice.
  Keeping caveats apart needs a schema change, a non-goal of the spec.
- **The judge (slice 3), the embeddings on `openai-go` (slice 2), chunking.**

## Local baseline (2026-10-25, the same on master and in scratch)

- **12 live `internal/arcadedb` tests fail locally, identically on both trees.**
  - The databases `aura_memory_it` and `aura_memory` do not exist on this stack.
  - Two concurrent-writer tests report a goroutine leak.
  - CI provisions its own databases.
- **`TestCommandHook_OversizedRewriteRejected`** timed out once under the full race run, and passed
  three times alone.
- **`TestEnvExampleNamesHaveReaders`** needs `GIT_DIR` inside the worktree, whose `.git` file holds a
  Windows path.
- **Seven lint findings under the `arcadedb_integration` tag**, in `memory_*_live_test.go` and
  `testclient_test.go`. None is in a file this plan touches.
