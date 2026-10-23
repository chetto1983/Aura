# Document search residual frictions Implementation Plan

**Status (2026-10-23):** written. Spec §5 was amended a third time the same day, on the operator's
decision ("Frizioni + soglie ora").

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for
> tracking. Commits on master.

**Goal:** close the four interface frictions Aura's agent reported on `489815bf7`:
- a listing cannot page;
- the three counts do not say their scope;
- known ids cannot be listed;
- closest names are not similar.

**Architecture:**
- `docfields.resolve` keeps a closest name only when it is similar.
- `docfields.ListQuery` gains `DocumentIDs`, matched against `search_document_id`.
- `document_search` gains `offset` and returns `next_offset` in list mode. It lists `document_ids` without
  a query.
- The description states the three count scopes once, as static text.

**Tech Stack:** Go; the existing `internal/docfields` and `internal/agent/tools` packages.

**Spec:** `docs/superpowers/specs/2026-10-18-document-fields-design.md` §5, "Amended a third time 2026-10-23".

## Global Constraints

- **Similar name:**
  - edit distance at most half of the longer `NameKey`, in runes;
  - or one `NameKey` contains the other;
  - at most 3, closest first.
- **`offset`:** an integer ≥ 0, list mode only. `next_offset` = offset + returned rows, present only when
  that is less than `matching_documents`.
- **Refusals:**
  - `offset` with a `query` is refused;
  - no `query`, no filter and no `document_ids` is refused.
- **The description:** stays one static text.
- **Code:** no file over 600 lines; no comments unless the why is non-obvious.

## Review Focus

- **An offset past the end.** `offset` ≥ `matching_documents` returns no rows and no `next_offset`, but it
  is not reported as "the filter admits nothing". The filter admits documents; the page is empty.
- **Unknown ids in a listing.** `document_ids` naming no document of this identity's lists nothing. It is
  never an error, and never another identity's document.
- **A one-letter unknown name.** "x" against "Cliente" is not similar. Containment must not make every
  name contain a single letter, so containment needs both keys of at least 3 runes.
- **Accents and case.** The distance is computed on `NameKey` (lowered, trimmed), so "FATTURE" against
  "Fattura" is distance 1.

---

### Task 1: a closest name is a similar name

**Files:** Modify `internal/docfields/filters.go` and `internal/docfields/filters_test.go`.

- [ ] **Step 1: Tests.**
  - Rewrite `TestUnknownNameReturnsClosest`: "Rosi SRL" now gets `["Rossi SRL", "Rossi Snc"]`, without
    `ACME SpA`. The commit message justifies the rewrite: the operator's decision.
  - Add `TestClosestNamesAreSimilar`:
    - against labels `Cliente`, `Gialli Import` and `Fattura`, the names `fatture`, `contratti`,
      `__tag_test_inesistente__` and `x` get, in order: `["Fattura"]`, `[]`, `[]`, `[]`;
    - `Gialli` gets `["Gialli Import"]`.
- [ ] **Step 2: Run** `go test ./internal/docfields/ -run 'TestUnknownName|TestClosestNames'`. Expected: FAIL.
- [ ] **Step 3: Implement** `similar(a, b string) bool` in `filters.go`. `closest` keeps only similar
  labels, ranked as before.
- [ ] **Step 4: Run.** Expected: PASS. Also run `go test -race ./internal/docfields/`.
- [ ] **Step 5: Commit** `fix(docfields): answer an unknown name only with names that resemble it`.

### Task 2: a listing pages and lists known ids; the counts say their scope

**Files:**
- Modify `internal/docfields/library.go`, `library_search.go`, `library_integration_test.go` and
  `workspace_test.go`.
- Modify `internal/agent/tools/document_search.go`, `document_search_list.go` and
  `document_search_list_test.go`.

**Interfaces:**
- Produces:
  - `ListQuery.DocumentIDs []string`, matched as `search_document_id IN :document_ids`, bounded at 200;
  - `documentSearchArgs.Offset int` (`json:"offset"`);
  - `documentSearchResult.NextOffset int` (`json:"next_offset,omitempty"`).

- [ ] **Step 1: Tests.**
  - `TestListDocumentIDsNarrowTheListing`: unit test on `libraryPredicates`; the predicate and its
    param are present, and 201 ids are refused.
  - Integration: `TestLibraryListHonoursSourcePrefixes` gains a `DocumentIDs` case.
  - `TestDocumentSearchListPages`:
    - the fake's total is 3 and its rows are 2, with `{"tags":["urgente"],"limit":2}`;
    - the result has `next_offset` 2, and the fake got Offset 0;
    - with `"offset":2` and 1 row, there is no `next_offset`, and the fake got Offset 2.
  - `TestDocumentSearchListsKnownIDs`: `{"document_ids":["doc_a"]}` with no query lists, and the fake
    got `DocumentIDs ["doc_a"]`.
  - `TestDocumentSearchRefusesNeitherQueryNorFilter`:
    - `{"query":"x","offset":1}` is now refused;
    - `{"tags":["urgente"],"document_ids":["d"]}` is now accepted;
    - `{}` is still refused.
  - `TestDocumentSearchListPastTheEndIsNotAnAbstention`: total 3, offset 5, 0 rows; not abstained, no
    `next_offset`.
  - `TestDocumentSearchSpecNamesCitationAndOpenContract`: the description says "library_lists counts the
    whole library" and "next_offset".
- [ ] **Step 2: Run** `go test ./internal/docfields/ ./internal/agent/tools/`. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - The schema:
    - `"offset": {"type":"integer","minimum":0,…}`;
    - the `document_ids` description reads "narrows a search, or with no query lists those documents".
  - The description gains one count-scope sentence; the closest-names wording becomes "names on the list
    that resemble one that matched nothing".
  - A listing abstains only when `matching_documents` is 0.
- [ ] **Step 4: Run** `go test` and `-race` on both packages, the `docfields` `arcadedb_integration` tier,
  and `go vet ./cmd/aura/`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(document_search): a listing pages, lists known ids, and its counts say their scope`.

### Task 3: gates, push, ask the agent again

- [ ] **Step 1: Push** with the CLAUDE.md recipe, once no other session's CI run is in flight on master.
  Check that CI is green on the SHA.
- [ ] **Step 2: Continue the operator's conversation** `01a168cf-…` on the VM once the updater has
  applied the SHA:
  - ask the agent to retry the four frictions it named;
  - record the turn in the same verification file, as a third turn.
- [ ] **Step 3: Commit** the verification and Status lines.

## Decisions taken while writing this plan

- **Half the longer name, or containment from 3 runes.** The operator's measured misses are distance 6
  of 7 (`fatture` to `cliente`). The kept typos are distance 1 to 3 of 8 to 9. Half is the widest bound
  that drops the first and keeps the second.
  - Cost if wrong: a typo farther than half a name gets no suggestion, and the agent reads
    `library_lists` instead.
- **A page past the end is not an abstention.** The filter still admits documents, so
  `filter_admits_nothing` would be false.
- **Offset only in list mode.** Retrieval ranks a candidate set with floors, and an offset into it has no
  stable meaning.

## File structure

| File | Responsibility |
|---|---|
| `internal/docfields/filters.go` | resolving names; closest names that resemble |
| `internal/docfields/library.go`, `library_search.go` | `ListQuery.DocumentIDs` and its predicate |
| `internal/agent/tools/document_search.go`, `document_search_list.go` | `offset`, `next_offset`, listing ids, the description |
