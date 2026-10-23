# Document search lists Implementation Plan

**Status (2026-10-23):**
- Tasks 1 and 2 are done.
- Tasks 2b and 2c were added the same day. Aura's agent tested its tools on the lab VM, and the
  operator decided on two of the frictions it reported ("Query facoltativa", "Sì, nomi e stato").
- Spec §5 is amended twice.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for
> tracking. Commits on master (the operator's standing rule).

**Goal:** every `document_search` result names the library's types and tags with their document counts, so
the agent can filter by a tag the operator's words do not spell.

**Architecture:**
- **`docfields.Store.LibraryLists`.** It builds the lists from two things that already exist:
  - the labels (`Store.Labels`);
  - the per-label document counts of the unfiltered library. `Store.List` already computes these as
    facets, and the cockpit's lists page reads them.
- **The tool.** `document_search` asks for the lists on every call, through the
  `DocumentFieldFilters` interface it already holds. It adds them to its JSON result as
  `library_lists`.
- **The description.** The tool description gains one fixed sentence.

**Tech Stack:** Go, ArcadeDB (tenant database), the existing `internal/docfields` and
`internal/agent/tools` packages.

**Spec:** `docs/superpowers/specs/2026-10-18-document-fields-design.md` §5, "Amended 2026-10-23".

## Global Constraints

- The lists hold types and tags only. Counterparties are not listed.
- Ordering: most documents first, then by name, case-insensitive.
- A label that no document holds is not listed.
- At most 30 names per list. A cut list says how many it left out (`omitted_types`, `omitted_tags`).
- The counts cover the whole library, every file type (`FileType: "all"`), with reviewed and suggested
  values alike. That is the same count the cockpit's lists page shows.
- The lists appear on every call where the document fields are configured, an abstention included.
- A failure to read the lists never fails the search: the result omits them and the failure is logged.
- The tool description stays identical text for every identity, for the prompt cache.
- No file over 600 lines; refactor-on-touch; no comments unless the why is non-obvious.

## Review Focus

- **A label with no document.** A type the operator created but never assigned must not be listed:
  filtering by it admits nothing.
- **A label id in the facets with no label row.** A suggestion keyed to a deleted label must not
  surface as an id or an empty name.
- **Ties.** Two tags with the same count are ordered by name, so the result is deterministic and
  cache-stable across calls.
- **A library with no labels at all.** The result carries empty lists, not `null`, so the agent reads
  "no tags" instead of "unknown".
- **Fields not configured** (`DocumentSearch.Fields == nil`). The result has no `library_lists` key,
  as before.

---

### Task 1: the lists in `docfields`

**Files:**
- Create: `internal/docfields/lists.go`
- Create: `internal/docfields/lists_test.go`
- Modify: `internal/docfields/library_integration_test.go`, one test

**Interfaces:**
- Produces:
  - `type ListedName struct { Name string \`json:"name"\`; Documents int \`json:"documents"\` }`
  - `type LibraryLists struct { Types []ListedName \`json:"types"\`; Tags []ListedName \`json:"tags"\`; OmittedTypes int \`json:"omitted_types,omitempty"\`; OmittedTags int \`json:"omitted_tags,omitempty"\` }`
  - `const MaxListedNames = 30`
  - `func (s *Store) LibraryLists(ctx context.Context, identityID string) (LibraryLists, error)`

- [ ] **Step 1: Write the failing unit test** `TestListedNamesOrdersCapsAndDropsEmpty` in `lists_test.go` for the
  pure helper `listed(labels []Label, counts map[string]int) ([]ListedName, int)`:
  - labels `interno`/l-int (3), `bozza`/l-boz (1), `Urgente`/l-urg (1), `Vuoto`/l-vuo (0); counts also hold
    `l-gone` (5), with no label;
  - expected `[{interno 3} {bozza 1} {Urgente 1}]`, omitted 0. The tie is ordered by `NameKey`,
    `Vuoto` and `l-gone` are absent, and an empty input returns a non-nil empty slice;
  - 31 labels with counts 31..1 return 30 names and omitted 1.
- [ ] **Step 2: Run it.** `go test ./internal/docfields/ -run TestListedNames`. Expected: FAIL, `listed`
  undefined.
- [ ] **Step 3: Implement** `listed`, and `Store.LibraryLists` on top of it:
  - `List(ctx, identityID, ListQuery{FileType: "all", Limit: 1}).Facets["type"]` and `["tags"]`;
  - `Labels(ctx, identityID, KindType)` and `KindTag`.
- [ ] **Step 4: Run it.** Same command. Expected: PASS.
- [ ] **Step 5: Integration test** `TestLibraryListsNameTypesAndTagsWithCounts` in `library_integration_test.go`:
  - start from `officeLibrary`;
  - save a tag label `Interno`, and apply it to `chat/a.pdf` with `ApplyToDocuments`;
  - expect Types `[{Fattura 2}]`: `Contratto` holds no document, so it is not listed;
  - expect Tags `[{Interno 1}]`: the `lbl-urgente` suggestion has no label row, so it is not listed.

  Run with `-tags arcadedb_integration` against the local ArcadeDB. Expected: PASS, and FAIL before
  Step 3.
- [ ] **Step 6: Commit** `feat(docfields): the library's types and tags with their document counts`.

### Task 2: `document_search` carries the lists

**Files:**
- Modify: `internal/agent/tools/document_search_fields.go`: the interface gains `LibraryLists`.
- Modify: `internal/agent/tools/document_search.go`: the result field, the call, one description sentence.
- Modify: `internal/agent/tools/document_search_fields_test.go`: the fake and three tests.

**Interfaces:**
- Consumes: `docfields.LibraryLists`, `(*docfields.Store).LibraryLists` from Task 1.
- Produces:
  - `DocumentFieldFilters.LibraryLists(ctx context.Context, identityID string) (docfields.LibraryLists, error)`;
  - `documentSearchResult.Lists *docfields.LibraryLists \`json:"library_lists,omitempty"\``.

- [ ] **Step 1: Write the failing tests:**
  - `TestDocumentSearchNamesTheLibraryLists`: a plain query with the office fake returns
    `library_lists.tags` = `[{interno 3}]`.
  - `TestDocumentSearchListsTheLibraryOnAnAbstention`: an unknown counterparty abstains and still carries the
    lists.
  - `TestDocumentSearchAnswersWithoutListsItCannotRead`: the fake's lists error, and the search still answers
    with no `library_lists` key.
  - With `Fields == nil`, the existing tests' results must have no `library_lists` key; one assertion is
    added to `TestDocumentSearchSevenArgumentsStrict`'s no-store case.
- [ ] **Step 2: Run them.** `go test ./internal/agent/tools/ -run 'TestDocumentSearch'`. Expected: FAIL to
  compile on the fake's missing method, then FAIL on the missing key.
- [ ] **Step 3: Implement.**
  - The call: `t.Fields.LibraryLists(ctx, owner)` after the filter, and before or instead of retrieval.
  - On error: `slog.Warn("document_search: library lists unavailable", "err", err)`, and no lists.
  - The description sentence: "library_lists in every result names the types and tags the operator's
    library holds, with how many documents carry each: when the question asks for a kind of document or
    a quality the words do not spell (internal, draft), filter by the listed name."
- [ ] **Step 4: Run them.** Same command, plus `go test -race ./internal/agent/tools/`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(document_search): every result names the library's types and tags`.

### Task 2b: a search with no query lists what the filters admit

Spec §5, "Amended again 2026-10-23". The operator decided it after the agent's report on the VM.

**Files:**
- Modify: `internal/arcadedb/document_retrieval.go`: `candidateWhere` builds its scope clause with
  the new exported `SourcePredicate`.
- Modify: `internal/docfields/library.go`:
  - `ListQuery.SourcePrefixes`;
  - the facet count extracted, so `LibraryLists` stops reading a page it throws away (review
    minor 2, fixed on touch).
- Modify: `internal/docfields/library_search.go`: the scope clause through `arcadedb.SourcePredicate`.
- Modify: `internal/docfields/lists.go`.
- Modify: `internal/agent/tools/document_search.go`, `document_search_fields.go`: the list mode, the
  schema with `query` no longer required, and the description.
- Test: `internal/arcadedb/document_retrieval_test.go` (or the file holding `candidateWhere` tests),
  `internal/docfields/library_integration_test.go`, `internal/agent/tools/document_search_fields_test.go`.

**Interfaces:**
- Produces:
  - `func SourcePredicate(keys, prefixes []string, params map[string]any) string` in `arcadedb`. It
    returns `""` when both are empty, otherwise `(source_key IN :source_keys OR source_key LIKE
    :source_prefix_0 …)`.
  - `ListQuery.SourcePrefixes []string`.
  - `DocumentFieldFilters.List(ctx context.Context, identityID string, q docfields.ListQuery) (docfields.ListResult, error)`.
  - `documentSearchResult.Matching int \`json:"matching_documents,omitempty"\``.

- [ ] **Step 1: Failing tests:**
  - `TestSourcePredicateJoinsKeysAndPrefixes`: a key and a prefix with `%` give one OR clause with the
    prefix escaped; nothing gives `""`.
  - `TestLibraryListHonoursSourcePrefixes` (integration): `SourcePrefixes: ["chat/"]` over
    `officeLibrary` gives 3 documents, and `["altro/"]` gives 0.
  - `TestDocumentSearchListsWhatTheFiltersAdmit`:
    - `{"tags":["urgente"]}` with no query never calls `Retrieve`;
    - it lists the fake's rows as documents with `requires_open`;
    - `matching_documents` is the fake's total, and `profile` is `field_list`;
    - the fake received `FileType "all"`, `Sort "-indexed_at"`, the resolved tag ids and the limit.
  - `TestDocumentSearchListHonoursTheRunScope`: a folder scope in the context reaches
    `ListQuery.SourcePrefixes` as `"folder/"`.
  - `TestDocumentSearchRefusesNeitherQueryNorFilter`: `{}` is refused, and so is
    `{"tags":["urgente"],"document_ids":["d"]}`.
  - `TestDocumentSearchListOfNothingAbstains`: total 0 gives `abstained` with
    `filter_admits_nothing`.
- [ ] **Step 2: Run them.** Expected: FAIL (undefined `SourcePredicate`, missing `List` on the fake,
  the query required).
- [ ] **Step 3: Implement.** `candidateWhere` and `workspacePredicates` both call `SourcePredicate`.
  `LibraryLists` counts with the extracted facet helper. The list mode sits in
  `document_search_list.go` if `document_search.go` would pass 600 lines.
- [ ] **Step 4: Run** `go test` and `-race` on `./internal/arcadedb/ ./internal/docfields/ ./internal/agent/tools/`,
  and the `arcadedb_integration` tier of `docfields`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(document_search): with no query, list the documents the filters admit`.

### Task 2c: every result names each document's fields

**Files:**
- Create: `internal/docfields/document_names.go`, `internal/docfields/document_names_test.go`.
- Modify: `internal/docfields/library_integration_test.go`, `internal/agent/tools/document_search*.go`
  and the tests.

**Interfaces:**
- Produces:
  - `type NamedValue struct { Name string \`json:"name"\`; Reviewed bool \`json:"reviewed"\` }`
  - `type FieldNames struct { Type, Counterparty, DocumentDate *NamedValue; Tags []NamedValue }`, with
    JSON `type`, `counterparty`, `document_date` and `tags`, each omitempty.
  - `func (s *Store) DocumentFields(ctx context.Context, identityID string, documentIDs []string) (map[string]FieldNames, error)`
  - `DocumentFieldFilters.DocumentFields`, same signature.
  - `documentSearchResult.DocumentFields map[string]docfields.FieldNames \`json:"document_fields,omitempty"\``,
    declared before the embedded response.

- [ ] **Step 1: Failing tests:**
  - `TestNameFieldsByLabelAndReview` (unit, pure `nameFields(values []Value, names map[string]string) FieldNames`):
    - an operator type is named and reviewed;
    - a rule tag is named and not reviewed;
    - an accepted tag is reviewed;
    - a date keeps its ISO value as name;
    - a `new:` proposal, a label id with no name and a custom field are left out.
  - `TestDocumentFieldsNameTheHits` (integration over `officeLibrary`):
    - `chat/b.xlsx` gives type `Fattura` reviewed, and no tag, because `lbl-urgente` has no label row;
    - `chat/c.txt` maps to an empty `FieldNames`;
    - an unknown id is absent.
  - `TestDocumentSearchNamesEachHitsFields`: in search mode, the fake's names for the hit's id reach
    `document_fields`.
  - `TestDocumentSearchListNamesEachListedDocument`: in list mode, the names reach `document_fields`.
  - `TestDocumentSearchAnswersWithoutFieldsItCannotRead`: a `DocumentFields` error leaves no key.
  - The cut-preview test also asserts `"document_fields"` in the preview.
- [ ] **Step 2: Run them.** Expected: FAIL.
- [ ] **Step 3: Implement.**
  - `DocumentFields` reads the hits' rows by `search_document_id`, then `withFields`, then the three
    kinds of labels, and names them.
  - The tool asks after retrieval or listing, only when there is at least one document. On an error
    it logs a warning and leaves the key out.
  - One description sentence: "document_fields names each returned document's type, counterparty,
    document_date and tags; reviewed true is a value the operator set or accepted."
- [ ] **Step 4: Run** the same commands as Task 2b. Expected: PASS.
- [ ] **Step 5: Commit** `feat(document_search): every result names each document's own fields`.

### Task 3: gates, the lab VM, step 7 again

**Files:**
- Create: `docs/superpowers/verification/2026-10-23-document-search-lists-lab-vm.md`
- Modify: this plan's Status line, the document fields plan's Status line, `prd.md` §11 (the measured
  result).

- [ ] **Step 1: Gates in WSL.** `make quality`, then `bash scripts/coverage_docker.sh`. Update the package
  policy only where a pinned denominator moved.
- [ ] **Step 2: Push** with the CLAUDE.md recipe. CI, Skills, CodeQL and the edge image must be green on the
  SHA; the VM's updater applies it.
- [ ] **Step 3: Ask Aura's agent on the lab VM.** The operator's instruction (2026-10-23): no 25-question
  run. Aura's agent is capable, so ask it.
  - Upload the step-7 files through the cockpit and review them as declared, so the library holds types
    and tags.
  - Through `POST /agent/run`, ask the agent to work with `document_search`, including the questions
    f20 ("i documenti interni") and f21 ("la bozza interna").
  - Ask it to say whether its document tools serve it well, and where it finds friction.

  Expected: the agent filters by the `interno` and `bozza` tags it reads from `library_lists`, and its
  report names no friction about the lists, or names friction to fix. Then delete the files, the labels
  the review created and the conversations.
- [ ] **Step 4: Write the verification file**: what was measured, and what it does not show (one model, one
  run). Update the PRD note and both plans' Status lines.
- [ ] **Step 5: Commit** `docs: document_search lists, measured on the lab VM`, and push.

## Decisions taken while writing this plan

- **Counts come from `Store.List`'s facets.** No new query: they are the cockpit lists page's counts
  (`web/src/documents/ListsPage.tsx`), so the agent and the operator see the same numbers.
  - Cost if wrong: one page read of one row per search.
- **The lists ride on every call, not only on an empty or abstaining one.** The operator chose "in the
  result". A first search that finds something can still be the wrong set; f20 found 7 documents and
  never filtered.
  - Cost if wrong: a few hundred bytes per result.
- **30 per list.** The lab VM library holds 17 labels. A library that grows past 30 tags still gets the
  most used, with a count of the rest.
- **Whole library, not the hits' neighbours.** Paperless-ngx ranks its candidates by how similar their
  documents are to the input, capped at 10 tags (`src/paperless_ai/taxonomy.py`). That works for
  classification, where the document is the input. In search, the missing tag sits on documents the
  query does not reach.
  - Cost if wrong: on a library with many more than 30 tags, a rarely used one that the question needs
    can be cut. The `omitted_tags` count says so; this run does not measure it.

## File structure

| File | Responsibility |
|---|---|
| `internal/docfields/lists.go` | the library's types and tags with document counts |
| `internal/agent/tools/document_search.go` | the result carries `library_lists`; one description sentence |
| `internal/agent/tools/document_search_fields.go` | the interface method the tool reads the lists through |
