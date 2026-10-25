# Exact terms rank first (slice 1) Implementation Plan

**Status (2026-10-25):** LANDED AND REVIEWED, NOT PUSHED, NOT ACCEPTED.
- Tasks 1–5 are on master: `d92a5b921`, `815519a0d`, `6beb4eef8`, `f1562629c`, `4614dc874`.
- Task 6 Steps 1–3 are done. One review gave "with fixes", and its fixes are on master in five commits,
  from "fix(documents): judge an exact carrier on all a document holds" to this plan's update. The
  gates are green in the isolated worktree `D:/Aura-exact-fixes`, with the exceptions under "Local
  baseline".
- Steps 4–6 are open: they need the push, which waits for the operator, and the lab VM.

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
- **Where carriers come from.** Each full-text index is asked once for every term, the Lucene phrases
  OR-ed, with no score floor, inside the caller's scope (review I6):
  - documents: `DocumentIndex.ExactTermDocuments`, over passages, cards and split file names;
  - memory: `exactMemoryRIDs`, over facts and turns.
  - ArcadeDB 26.10.2 does not keep a phrase's word order, so every text it returns is checked in Go.
  - A card is checked without the lines `filecard` writes into it about Aura
    (`filecard.WithoutAuraNotes`).
- **Documents** (`internal/documents/retrieval_exact.go`):
  - `Retrieve` runs the exact lane beside the fused or the lexical leg. The lane returns what carries
    any term; `rankDocuments` keeps a document only the lane found when what it holds for it carries
    every term (review C1).
  - `rankDocuments` merges that evidence under the leg `exact`, without touching score or order.
    `rankExactTermsFirst` then moves the carriers ahead, replacing `rankIdentifiersFirst`.
  - The reserved card place sits behind the carriers.
  - The response abstains only when no leg and no carrier answered, and never when the lane failed.
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
- Amended after the review (C1, I6): it takes `terms []ExactTerm` and asks each index once, the phrases
  OR-ed (`lucenePhrases`). It keeps each file's first passage per term it carries
  (`bestPassagePerTerm`) and the cards carrying any term (`carriesAny`); no terms, no query.

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
- Amended after the review (C1, I1, I2): `exactLane{terms, passages, cards, failed}` holds the lane's
  one answer, and `exactHits` and `carriersOfEvery` are gone. `rankedDocument` gains `laneOrder`,
  `fromLegs` and `carrier`; `rankExactTermsFirst(ranked []*rankedDocument) int` reads `carrier`. A
  failed lane sets `DegradationExactTerms`.

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
- Amended after the review (I3, I6, M3–M5): `exactMemoryRIDs` sends one OR query per statement over
  `HybridCandidates`, and joins the errors of the statements that failed, keeping what the others found.
  The fact prose that hides duplicate turns is built after `exactRecallOrder`. `JoinReasons` is exported,
  for the documents.

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

- [x] **Step 1: Land the verified commits on master** through lefthook, in order (landed as
  `d92a5b921`, `815519a0d`, `6beb4eef8`, `f1562629c`, `4614dc874`):
  ```bash
  git cherry-pick 308ba82d3 ae6324634 2df8c7692
  git cherry-pick -n 993c9bb01 b65012631
  git commit   # Task 4's message, from `git log -1 --format=%B 993c9bb01`
  git cherry-pick f2c3f50d3
  git diff b65012631 HEAD -- $(git diff --name-only e3e562cac b65012631)   # expected: empty
  ```
  Master has moved since `e3e562cac`, but no commit after it touches these 31 files (checked
  2026-10-25), so the picks apply cleanly.
- [x] **Step 2: Gates, in WSL, on master** (for the review fixes, in the isolated worktree at their
  tip, because master's tree held a parallel session's half-edited `cmd/aura`):
  - green: vet under the four tags, build, race on the five packages, the live tier with the new
    underscore-file test, lint (0 issues, and under the tags only the seven below);
  - coverage 49722/56858 = 87.4%, package policy passed. The first run failed in `internal/packs`
    because the runner exported `GIT_WORK_TREE`, recorded under "Local baseline".
  - `go vet`, `go build`, and `go test -race` on these packages: `internal/arcadedb`,
    `internal/documents/...`, `internal/agent/...`, `cmd/arcadedb-mcp` and `cmd/aura`;
  - `go vet -tags retrieval_eval ./internal/documents/` and `-tags arcadedb_integration` on the two
    `cmd`s;
  - the live tier, `-tags arcadedb_integration -run 'Document|MemoryExact' ./internal/arcadedb/`;
  - `golangci-lint`, with and without the tags;
  - `bash scripts/coverage_docker.sh`: the 85% aggregate and the package policy. `internal/arcadedb`
    is delegated to its live profile (amendment #203).
  - Mutation runs in CI.
- [x] **Step 3: One independent review** of the five commits, by a fresh read-only reviewer on the most
  capable model. Fix what it finds in follow-up commits.
  - Verdict "with fixes": 1 critical, 6 important, 8 minor. Nine of the fixes were each removed in
    turn, and every removal failed the test written for it.
  - Fixed: C1, I1, I2 and I6 in "fix(documents): judge an exact carrier on all a document holds"; I3,
    I6, M3, M4 and M5 in "fix(arcadedb): memory asks every exact term in one query per kind"; the
    caveats in "fix(filecard): a card's unread caveats carry no exact term"; I3's bound in
    `document_search`'s description; M6 by C1; M7 in the first commit; M8 here.
  - Recorded, not fixed: I3's bound, I4, I5, M1 and M2, under "What this plan does not cover".
- [ ] **Step 4: Measure the cost of a search with exact terms.** On the lab VM, time one
  `aura docs search` with 1, 4 and 16 identifiers. Since review I6 a search costs 3 full-text queries
  in the documents and 2 per memory read whatever the number of terms; what grows is the OR query and
  the Go check.
  - There is no cap on the number of terms.
  - If 16 identifiers cost more than one search's budget, record the number and propose a cap from it.
    Never cap before the measurement.
- [ ] **Step 5: Acceptance 1 on the lab VM**, once the updater has applied the landed SHA:
  - `docker exec aura aura docs search '<s04 with "Ubuntu Mono" quoted>'` and
    `'"Ubuntu Mono"'` return `calibre_demo.docx` first.
  - The five out-of-corpus questions of the floors set still abstain.
  - Review I4: out-of-corpus questions holding everyday alphanumerics also abstain, such as
    "quanto costa la carta A4 su Amazon?", and questions with `Q3`, `24h` and `COVID-19`. If one does
    not, the operator decides whether identifiers keep admitting past the floor or only rank first.
  - Review I5: one quoted Italian name that the library holds only after an elided article, such as
    `"Agenzia delle Entrate"` against "dell'Agenzia delle Entrate". The expected result is a miss,
    recorded as the limit it is.
  - `"Aura"` returns only the documents whose own text names Aura; no unread file comes back for its
    caveat.
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
| `internal/arcadedb/document_exact.go` | The texts carrying any of the terms, one query per full-text index |
| `internal/arcadedb/document_lexical.go` | Lexical passage and card reads, shared by the lexical leg and the exact lane |
| `internal/arcadedb/memory_exact.go` | Memory's exact lane, the carriers-first order, the fact search's carriers |
| `internal/documents/filecard/card.go` | `WithoutAuraNotes`: a card without the lines Aura writes into it about itself |
| `internal/documents/retrieval_exact.go` | The documents' exact lane, carriers-first ranking |
| `internal/documents/retrieval_rank.go` | `rankDocuments` with the lane's evidence and order, the carrier check; the exact-first passage cut |
| `internal/documents/retrieval.go` | The lane in `Retrieve`, one abstention rule, profile v4 |
| `internal/agent/tools/document_search.go`, `cmd/arcadedb-mcp/tool_memory*.go` | How the agent is told to quote |

## Decisions taken while writing this plan

- **Exact evidence sets no score.** One phrase's BM25 is not the scale of the legs.
  - A carrier only the lane found reports score 0. Memory's JSON omits it.
  - The carriers rank first by `rankExactTermsFirst`, not by a number.
  - Amended after the review (I1): "no order" sorted the carriers only the lane found by document id.
    The lane's best rank now breaks the tie the legs leave them in.
- **A carrier is judged on what the response returns:** the title, the card without its notes, and the
  passages returned.
  - The passage cut keeps exact evidence first. So a document loses the bypass only when more than
    `TopPassages` (3) of its terms each sit in a different passage, and the agent could not have seen
    the fourth anyway.
  - The `retrieval_eval` bench re-derives the same order from the same data.
- **Union, then the check in Go, in both packages.** The lane returns what carries any term; a record
  only the lane found stays when what the ranking holds for it carries every term.
  - Amended after the review (C1). The first plan kept, in the documents, only what the indexes found
    for every term. The engine and Go tokenise apart: StandardAnalyzer keeps `calibre_demo.docx` one
    token, and the identifier `calibre_demo` Go cuts from it is in no index, so every file name with
    an underscore stayed buried. Memory already took the union, because the `FACT[statement]` index
    does not hold a fact's subject and object names.
  - The documents' check runs in `rankDocuments`, after the merge by content key and text alias, so two
    copies of a text count as one document (M6).
- **The reserved card place sits behind the carriers.** A file the question names verbatim outranks
  one that only might answer it. With no carriers, the order is unchanged.
- **The lane runs in lexical mode too.** The card-only degraded paths have no passage index, so the lane
  does not run there, but the cards that carry the terms still rank first.
- **A lane failure is named in the response.**
  - Amended after the review (I2). The documents logged `exact_terms_unavailable` and left the
    response untouched, as `passage_names_unavailable` does. Names change only titles; the lane
    changes admission, and a failed lane with empty legs produced a healthy abstention, which the tool
    tells the agent means the library does not hold the answer.
  - Documents now set `degradation_reason` to `exact_terms_unavailable`, joined to the legs' own
    reason with `+` (`arcadedb.JoinReasons`), and never abstain on a failed lane.
  - Memory names `exact_terms_failed` in the result's reason, joined with `+`. A memory statement that
    fails costs its own kind only (M3), and an abstention joins `no_qualified_candidates` to the
    reasons instead of replacing them (M5); the fact search's lexical fallback keeps its own reason
    for its abstention, as its test always required.
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
- **One query per index, not per term; no cap on the number of terms.** Amended after the review (I6):
  a query per term per index made a pasted list of 60 codes cost 180 sequential queries. The phrases
  are OR-ed, which the 26.10.2 executor returns as a union, so a search costs 3 queries in the
  documents, 2 per recall and 1 per fact search. Task 6 Step 4 measures the time.
- **The lane's pool** (review I3): each index is read up to its candidate cap, best BM25 first. The
  documents read 200 passages and 200 cards per index by default; memory reads `HybridCandidates`,
  400 per kind, where it read the caller's 20.
- **A card's caveats are Aura's notes too.** The plan first left them, as needing a schema change. The
  review showed three are constants, or open with one: `unreadCaveat`, `pdfUnreadCaveat` and the
  read-error caveat. `WithoutAuraNotes` now drops the lines they open, so quoting "Aura" no longer
  ranks every unread file first.

## What this plan does not cover

- **A term held in more records than the lane's pool** (I3). It is checked only in the best-matching
  records, and `document_search`'s description says so. Not measured on a library of more than 200
  documents.
- **Identifiers admit past the floor** (I4). That is the spec's decision, but identifiers and file names
  are extracted, not chosen by the caller, so `A4` or `COVID-19` admit what carries them. Acceptance 1
  measures it; the operator decides on the result.
- **Italian elided articles** (I5). StandardAnalyzer keeps "dell'Agenzia" one token, so the phrase
  `"Agenzia delle Entrate"` cannot find it. Fixing it is an analyzer change, a spec non-goal.
- **Abbreviations read as file names** (M1). `p.es`, `sig.ra`, `p.zza` pass `isFileName`: the first
  switches the bypass off with a term no document carries, the last ranks every `p.zza` first. The fix
  the review suggests, only an extension `filecard` routes, would still accept `p.es`: `.es` is routed
  as Erlang and JavaScript (`filecard/source.go`).
- **Turns under limit 4** (M2). Recall gives turns no slot below limit 4, so a phrase only a
  conversation carries is not returned there.
- **The phrase order upstream.** Reporting it to ArcadeDB is the operator's call.
- **The judge (slice 3), the embeddings on `openai-go` (slice 2), chunking.**

## Local baseline (2026-10-25, the same on master and in scratch)

- **12 live `internal/arcadedb` tests fail locally, identically on both trees.**
  - The databases `aura_memory_it` and `aura_memory` do not exist on this stack.
  - Two concurrent-writer tests report a goroutine leak.
  - CI provisions its own databases.
- **`TestCommandHook_OversizedRewriteRejected`** timed out once under the full race run, and passed
  three times alone.
- **A worktree's `.git` file holds a Windows path**, which WSL git cannot read, so
  `TestEnvExampleNamesHaveReaders` fails there without `GIT_DIR`. Exporting `GIT_DIR` and
  `GIT_WORK_TREE` is worse: they reach the tests that run git themselves, and `internal/packs`'
  `git init` failed under the coverage gate (a non-bare `git init` would reuse `GIT_DIR`). A relative
  gitfile, `gitdir: ../Aura/.git/worktrees/Aura-exact-fixes`, reads on both sides with no variable.
- **Seven lint findings under the `arcadedb_integration` tag**, in `memory_*_live_test.go` and
  `testclient_test.go`. None is in a file this plan touches.
