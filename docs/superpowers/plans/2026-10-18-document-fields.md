# Document fields implementation plan

**Status (2026-10-23):** PARTIAL. Task 0 measured in ffa66d6a9
(`verification/2026-10-18-document-fields-mirror.md`); T1-T12 in 2c9923ec3..dfc4f5e13; Task 13
steps 1-6 in 28fa1f13d..4a46a87e2 (`verification/2026-10-18-document-fields.md`, final review's
fixes c65598a2a..4d66b7008), on master. Open: step 7, the operator's run on the lab VM, where the
real agent picks the filters from the operator's words; the dev-stack run wrote them into the set.
Step 7 was measured on 2026-10-23 on d46cedab6 (`verification/2026-10-23-document-fields-lab-vm.md`):
- **Below the gate.** The score is 9.44 against the 9.8 gate: 23 of 25 exact sets, and the
  extractor's proposals are 14/14 since 2fa2df738.
- **Why.** The two tag questions fail because the agent never sees the operator's tag list. How it
  learns the lists is the operator's decision, before the next run.
The operator approved the spec, decision 2 included ("Approvata, scrivi il piano").

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task by task, with
> test-driven-development. Steps use checkbox (`- [ ]`) syntax for tracking. This plan runs on
> branch `claude/github-abuse-review-0o3kqw`.

**Goal:** the operator sees, filters, corrects and reviews type, counterparty, date, tags and
custom fields on every document in a Documenti view. The agent's `document_search` filters by the
same fields. Every value records who set it.

**Architecture** (revised 2026-10-18 after Task 0; the operator moved the fields into ArcadeDB):
- **ArcadeDB is the truth, in the identity's own database.** Document types `DocumentLabel`,
  `DocumentCustomField`, `DocumentFields` and `DocumentFieldEvent` hold the lists, one record per
  `source_key`, the values with provenance, the rejections and the history. The database per
  identity is the isolation. An `internal/docfields` Store writes them through the tenant clients.
- **Triggers keep the fields on the rows the search scans.** An `AFTER UPDATE` trigger on `Passage`
  and `IndexedDocument` stamps `df_*` from `DocumentFields` when CocoIndex's `SET` lands. Cascade
  triggers on `DocumentFields` carry the operator's edits. `candidateWhere` filters on `df_*`.
- **Suggestions come from Go.** A Go pass applies rules on the labels, and the invoice header
  pairs filecard now keeps (carried on `IndexedDocument` by the ingest), as suggestions.
- **The cockpit** adds a Documenti view on `@svar-ui/react-grid` beside the file manager.

**Tech Stack:**
- Go 1.27;
- ArcadeDB 26.10.1 over HTTP;
- Python 3.12 with cocoindex 1.0.25;
- React 18, `@svar-ui/react-grid` 2.7.0, vitest, Playwright.

**Spec:** `docs/superpowers/specs/2026-10-18-document-fields-design.md` (approved 2026-10-18).

## Global Constraints

- **No new environment variable.**
- **No Postgres migration.** Document fields live in the identity's ArcadeDB database, reached only
  through `arcadedb.TenantClients`, so one identity cannot address another's database.
- **Every multi-record write is one `sqlscript`.** Measured in Task 0: 4,027 records take 32.8 s one
  HTTP request each, and 0.27 s in one script.
- **The identity comes from the principal** (`principalIdentityID(r)`), never a request parameter.
- **`set_by` takes exactly these values:** `operator`, `rule:<label id>`, `extractor`, `copied`.
  `classifier` and `llm` are reserved for phases 2 and 3; the CHECK admits them and no code
  writes them.
- **Nothing below the operator changes an operator value.** A rejected value is never
  re-suggested for that document.
- **Names match case-insensitively and exactly, after trimming.** They are unique per identity
  and kind.
- **`document_search` gets exactly seven new optional arguments:** `type`, `counterparty`,
  `tags`, `tags_mode` (`any`|`all`), `date_from`, `date_to`, `include_unreviewed` (default
  `true`). Unknown fields stay refused.
- **The agent filters on fixed fields and tags only.**
- **The lists start empty.** No seed outside tests.
- **`@svar-ui/react-grid` is pinned exactly at `2.7.0`.**
- **Paperless is GPL-3.0.** Its behaviours are the reference; no code, styles or strings.
- **No file over 600 lines;** refactor on touch.
- **Every Go edit:** `go vet ./...`, `go build ./...`, `go test -race` on the touched packages.
  Every web edit: `npm run lint`, `npm run typecheck`, `npm test`.
- **Python:** the ingest suite through `scratchpad/harness/ingest-pytest.sh` and `ruff check`.

## Review Focus

1. **A name typed differently** ("gialli import ", "GIALLI IMPORT") matches the stored
   "Gialli Import" in filters, in rules and in bulk edit. An unknown name returns the closest
   names, never an empty silent result. Test in Task 6.
2. **"Select all in the filter", then Apply, after the result changed** (a new document indexed in
   between). Apply re-evaluates the filter minus exclusions, and refuses with the new count when
   it differs from the count the operator confirmed. Test in Task 8.
3. **A rename onto a key that already has a record.** The moved record replaces the target's, and
   the replaced record's values end with a `removed` event. Test in Task 4.
4. **The operator removes a value, then the pass runs again.** The value comes back neither from
   its rule nor from the extractor. Test in Task 7.
5. **An invalid regex rule.** It is refused at save with the parse error, and a pass never panics
   or stalls on any pattern; Go's RE2 is linear. Test in Task 7.

---

### Task 0: measure the mirror before building it

**Files:** `docs/superpowers/verification/2026-10-18-document-fields-mirror.md` (new). No product
code.

- [x] **Step 1. Build the mirror on a copy.** On a disposable copy of the 7,798-passage library
  (`scratchpad/adb2610`, ArcadeDB 26.10.1), create:
  `CREATE DOCUMENT TYPE DocumentFields; CREATE PROPERTY DocumentFields.source_key STRING;
  CREATE INDEX ON DocumentFields (source_key) UNIQUE`.
  Insert one row per `IndexedDocument.source_key`, with `type` set on one fifth of them.
- [x] **Step 2. Measure the cost of the filter.** Run `TestFusedQueryVariants` with an added
  variant whose `candidateWhere` carries
  `AND source_key IN (SELECT source_key FROM DocumentFields WHERE type = :type)`.
  Record its median against "shipped".
  - **Expected:** within 2× the shipped median.
  - **If not:** stop and report. The filter then moves to a document-id prefilter computed in Go,
    and the spec is amended.
- [x] **Step 3. Check that CocoIndex leaves the mirror alone.** On the fixture corpus, run an
  ingest catch-up, rename one object, and run a second catch-up.
  - **Expected:** the `DocumentFields` row count is unchanged, and so is the renamed object's old
    row, which only Go deletes.
- [x] **Step 4. Record and commit.** Write the verification file with both results and what they
  do not prove. Commit: `docs(verification): measure the DocumentFields mirror before building it`.
- **Result (2026-10-18):** the subquery failed the gate (2.9× at 325 documents, 14.6× at 4,027). A `Passage`
  property and a Go key list both passed at every width. ArcadeDB triggers kept the property on
  CocoIndex's rows. The operator moved the fields into ArcadeDB. See
  `docs/superpowers/verification/2026-10-18-document-fields-mirror.md`; Tasks 2, 3 and 6 change accordingly.

### Task 1: the question set, before the code

**Files:**
- Modify: `docs/superpowers/specs/2026-10-18-document-fields-design.md` (Ruling 2026-10-18: no
  `prd.md` amendment, the operator's standing rule; the spec records the ArcadeDB decision instead);
- Create: `scripts/fixtures/document_retrieval_eval/document_filter_questions.json`.

- [x] **Step 1. The spec amendment** (it replaces decision 2 and the filter of decision 5).
  - It covers the fields, provenance, review state, the ArcadeDB types and triggers, and the
    seven arguments.
  - It records why the fields left Postgres for the identity's ArcadeDB database, citing Task 0's
    numbers, and that 0098's object-metadata sentence still does not hold, for the spec's reasons.
  - It ends with what the amendment does not prove.
- [x] **Step 2. The question set.**
  - At least 20 questions over the office corpus, each answerable with type, counterparty, date
    and tags only. It extends q03, q09 and q18; q08 stays out.
  - Each entry: `{"qid","query","filters":{…},"gold_docs":[…],"answer"}`. `gold_docs` holds the
    answer documents themselves: the invoices, not `contabilita_2026.xlsx`.
  - Read the values from `spikes/document-routing-benchmark/generate_corpus.py`.
- [x] **Step 3. Commit:** `docs(spec): document fields live in ArcadeDB, and the question set that
  gates them`.

### Task 2: the ArcadeDB types and the store

**Files:**
- Create: `internal/arcadedb/document_fields_schema.go`, the types, indexes and triggers;
- Create: `internal/docfields/docfields.go`, `labels.go`, `records.go`, `values.go` (the diff
  semantics and the history entries), `script.go` (the sqlscript and the encoding);
- Create: `internal/docfields/rules/match.go`, `match_test.go`, the five matchers (the Interfaces of
  Task 7), here because `SaveLabel` validates with them;
- Test: `internal/docfields/docfields_test.go` (unit, no database) and
  `store_integration_test.go` (`//go:build arcadedb_integration`).

**The types**, in the identity's database, created `IF NOT EXISTS` on first use:
- `DocumentLabel`:
  - `id` UNIQUE, `kind` (`type|counterparty|tag`), `name`;
  - `name_key = lower(trim(name))`, UNIQUE on `(kind, name_key)`;
  - `match_algorithm` (`none|any|all|exact|regex|fuzzy`), `match_pattern`, `case_insensitive`, timestamps.
- `DocumentCustomField`: `id` UNIQUE, `name`, `name_key` UNIQUE, `data_type`
  (`date|monetary|integer|text|select`), `options`.
- `DocumentFields`, one per `source_key` (UNIQUE), with `record_id` (UNIQUE, the stable handle):
  - **the filterable projection,** which the triggers copy:
    - label ids `type`, `counterparty` and `tags`;
    - `document_date`;
    - `*_ok`, the same values restricted to operator-set or accepted ones;
  - **provenance,** `values`: a list of `{field, value_key, value, set_by, accepted, evidence, set_at}`;
  - **review,** `rejections`, `reviewed_at`, `reviewed_by`, timestamps.
- `DocumentFieldEvent`: `record_id`, `field`, `value_key`,
  `action` (`set|accepted|rejected|removed|reviewed|moved`), `actor`, `value_before`, `value_after`,
  `at`, `seq`. `BEFORE` and `AFTER` are reserved words of ArcadeDB SQL, refused as names in a `SET`.
- `DocumentRulePass`: the pass watermark on `IndexedDocument.indexed_at` (Task 7).

**Interfaces, produced:**
- `arcadedb.(*Client).EnsureDocumentFieldsSchema(ctx) (bool, error)`: the types, indexes and triggers,
  idempotent; it reports whether the triggers are in place (Task 3).
- `func New(tenants arcadedb.TenantClientResolver) *Store`. It ensures the schema once per identity.
- Labels: `(s *Store) Labels(ctx, identityID string, kind Kind) ([]Label, error)`, and
  `SaveLabel(ctx, identityID string, in Label) (Label, error)`, which validates the pattern
  through `rules.Compile`. Also `DeleteLabel(ctx, identityID, id string) error`.
- `(s *Store) Record(ctx, identityID, sourceKey string) (Fields, error)`, which creates on first use.
- `(s *Store) History(ctx, identityID, recordID string) ([]Event, error)`, oldest first.
- `(s *Store) ApplyDiff(ctx, identityID string, recordIDs []string, d Diff, actor string) error`,
  where `Diff{Set []Value; Remove []ValueRef}`:
  - one `sqlscript` for every record;
  - it writes events, and records a rejection when the operator removes a non-operator value;
  - it never overwrites `set_by=operator` unless the actor is the operator.
- `(s *Store) Suggest(ctx, identityID, recordID string, vs []Value) (int, error)`. It skips
  rejected values and fields that already hold an operator value.
- `(s *Store) MarkReviewed(ctx, identityID string, recordIDs []string) error`.
- `(s *Store) Move(ctx, identityID, fromKey, toKey string) error` and
  `(s *Store) CopyRecord(ctx, identityID, fromKey, toKey string) error`.
- `var ErrNotFound, ErrInvalid` as in `internal/board`.

**Steps:**
- [x] **Step 1. RED.**
  - The matcher table test for all five algorithms, with case folding, accents and
    `TestInvalidRegexRefused` (Review Focus 5).
  - Unit tests for the diff semantics and the generated script.
  - The integration tests:
    - `TestDocfieldsOperatorWins`;
    - `TestDocfieldsRejectionSticks`;
    - `TestDocfieldsMoveOntoExistingKey` (Review Focus 3);
    - `TestDocfieldsIsolation`: a second identity's database has none of the first's records;
    - `TestDocfieldsNameCaseInsensitiveUnique`.
- [x] **Step 2. Run them** against the dev stack's ArcadeDB, with the `arcadedb_integration` env:
  `go test -tags arcadedb_integration -run TestDocfields ./internal/docfields/`. They fail on the
  missing package.
- [x] **Step 3. GREEN.** The schema, then the Store.
- [x] **Step 4. Run them again, plus `go test -race ./internal/docfields/` in WSL;** all pass.
- [x] **Step 5. Commit:** `feat(docfields): store operator-owned document fields in the identity's ArcadeDB`.

### Task 3: the triggers that keep the fields on the indexed rows

**Files:** `internal/arcadedb/document_fields_triggers.go` (the trigger DDL), its unit test, and an
`arcadedb_integration` test.

**The triggers**, as measured in Task 0 (`docs/superpowers/verification/2026-10-18-document-fields-mirror.md`):
- **The stamp:** `AFTER UPDATE ON TYPE Passage` and `ON TYPE IndexedDocument`:
  - `UPDATE … SET df_* = first((SELECT … FROM DocumentFields WHERE source_key = $record.source_key)).…, df_stamped = true`;
  - `WHERE @rid = $record.@rid AND df_stamped IS NULL AND source_key IS NOT NULL`.
  - CocoIndex's `MERGE … SET n += $props` lands on the update, not the create.
- **The cascade:** `AFTER CREATE` and `AFTER UPDATE ON TYPE DocumentFields`, which write `df_*` on
  both types `WHERE source_key = $record.source_key`, one trigger per type: on 26.10.1 a SQL
  trigger body is one statement, and a second after `;` is a syntax error, though the manual
  shows one (measured 2026-10-18).
- **The clear:** `AFTER DELETE ON TYPE DocumentFields`, which sets `df_*` to null on both types.
- **Not used, because they fail on 26.10.1:**
  - a `BEFORE CREATE` SQL trigger (`$record` has no RID yet);
  - JavaScript triggers (`record.x = …` is not persisted, and parameters given as a JS object are
    not bound).
- **A failing `BEFORE` trigger aborts CocoIndex's write silently.** These are `AFTER` only.

**Interfaces, produced:** no `Backfill`. `EnsureDocumentFieldsSchema` does it, measured 2026-10-18
on 26.10.1:
- ArcadeDB refuses a trigger on a type that does not exist, so the triggers wait for ingest's
  `Passage` and `IndexedDocument`. The ensure reports `false` until then, and the Store asks again on
  its next call. Creating the types from Go would turn an empty library, which retrieval reads as
  one, into a schema with none of ingest's indexes.
- Each trigger converges on its body, read back from `SELECT FROM schema:triggers`
  (`actionCode`): one that differs is dropped and created again.
- Every record not stamped under the current bodies cascades once: `UPDATE DocumentFields SET
  stamp_version = :v ... LIMIT 500`, repeated until a batch is short, where `v` hashes the trigger
  bodies. `SET source_key = source_key` fires nothing, since an UPDATE that changes nothing is not
  written. Only records are walked: a row of a document with no record needs no stamp, its `df_*`
  being null either way. The batches resume where an interrupted pass stopped.
- The stamp names its row as `UPDATE $record.@rid`: `UPDATE <type> WHERE @rid = $record.@rid` scans
  the type (11.6 ms against 2.6 ms per row on 5,000 passages), and `UPDATE $record` is an internal
  error. The cascade finds a document's rows by `source_key`, which ingest declares but does not
  index, so the ensure adds a NOTUNIQUE index on both types.
- On the bench library (4,027 records, 7,271 passages, 4,045 documents), through the identity's
  own credential: the first ensure, re-stamp included, 1.33 s; the next 38 ms. Lowering the sentinel
  on every row instead had passed the client's 30 s timeout.

**Steps:**
- [x] **Step 1. RED.**
  - Unit tests on the generated DDL.
  - Integration `TestDocumentFieldsTriggersStampCocoIndexWrites` replays CocoIndex's
    `MERGE (n:Passage {passage_key: …}) SET n += $props` over openCypher.
  - It asserts that the stamp, the cascade, the clear and a rename move the values the way Task 0
    measured.
- [x] **Step 2. GREEN.**
- [x] **Step 3. Run the touched packages with `-race` in WSL, and the integration test.**
- [x] **Step 4. Commit:** `feat(arcadedb): triggers keep document fields on the indexed rows`.

### Task 4: rename and move carry the fields, and the new name

**Files:**
- Modify: `internal/objectstore/s3.go:286-294` (`Copy`) and the interface it implements, with
  `fake.go`, `filesystem.go` and `asset_placement.go` (`withFileName`);
- Modify: `internal/assets/filemanager_ops.go:120-180` and `browser.go` (`BrowserFields`);
- Modify: `cmd/aura/file_browser_wiring.go`, and `document_index_wiring.go`, whose tenant
  resolver the fields store shares;
- Tests: beside each.

**Measured while writing it:**
- Garage's `REPLACE` takes the destination's metadata from the request alone (`src/api/s3/copy.rs`
  at v2.4.1, `extract_metadata_headers`; its manual is silent). The copy reads the source with
  HEAD and sends back the type and every user entry, with `filename` replaced.
- `FilesystemStore.Put` dropped `PutOptions.Metadata`, and its `Copy` dropped it again: on that
  store no name survived an upload. Fixed on touch.

**Interfaces:**
- Consumes `docfields.Store.Move` and `docfields.Store.CopyRecord` (Task 2).
- Produces `objectstore.CopyOptions{FileName string}` on `Copy`. A non-empty `FileName` sends
  `MetadataDirective: REPLACE` with `filename` set and the other user metadata preserved.

**Steps:**
- [x] **Step 1. RED.**
  - `TestRenameReplacesFilenameMetadata`: after a rename, HEAD on the new key returns the new
    `filename`. Today it returns the old one (the bug the spec records).
  - `TestRenameMovesDocumentRecord` and `TestCopyCopiesDocumentRecord`.
- [x] **Step 2. GREEN.** Rename passes the new name. The ops call `Move`, or `CopyRecord` for a
  copy, after the object operation succeeds, and in the same flow as `RelocateAsset`.
- [x] **Step 3. Run with `-race`.**
- [x] **Step 4. Commit:** `fix(assets): a rename writes the new name and moves the document's fields`.

### Task 5: filecard keeps the invoice header pairs, and the ingest carries them

**Files:**
- Modify: `internal/documents/filecard/table.go:108-145` and `card.go:60-81`;
- Modify: `cmd/aura-filecard/main.go`;
- Modify: `services/ingest/app.py`, `arcade.py`;
- Tests: `internal/documents/filecard/header_test.go` (new), `services/ingest/tests/test_header_pairs.py`
  (new).

**Interfaces, produced:**
- `type HeaderPair struct{ Sheet, Key, Value string }` and `Sheet.Header []HeaderPair`, on the
  sheet the pairs were found on; `-json` gathers every sheet's, so no `Card.Header` repeats them.
- `-json` emits `"header": [...]`, always a list.
- `IndexedDocument.header_pairs`: `LIST OF STRING`, each `"<Key>: <Value>"`.

**Measured while writing it:**
- No `version=` on `_extract` or `process_file`. In cocoindex 1.0.25 a `version` replaces the
  AST in the logic fingerprint (`_internal/function.py:682`), so it would turn the automatic
  invalidation into a manual one. `_extract`'s body changes here, and a memoized parent records
  its children's logic fingerprints even on a cache hit (`function.py:836-845`), so
  `process_file` re-runs on every stored file without a bump.
- The card still calls the pairs a banner, and an invoice's client is indexed nowhere: an
  xlsx has no extracted text, and its passages are the rows under the header. Out of this
  plan's scope; recorded for the retrieval work.

**Steps:**
- [x] **Step 1. RED, Go.** `TestInvoiceHeaderPairsKept` reads a workbook built like
  `generate_corpus.py:123-139`. It asserts the five pairs (`Fattura n.`, `Cliente`, `Partita IVA`,
  `Data emissione`, `Stato`) with their values, and that rows and columns are unchanged.
- [x] **Step 2. GREEN.** In `resolveHeader`'s skip loop, collect 2-cell rows with a label in
  their first cell as `HeaderPair`s.
- [x] **Step 3. RED, Python.** `test_header_pairs_reach_indexed_document`.
- [x] **Step 4. GREEN.**
  - `Extracted.header`, then `IndexedDocument.header_pairs`.
  - The DDL property in `_document_ddl`.
  - `process_file` and `_extract` carry `version=` bumps with a WHY comment, so the memo
    re-extracts (the memo-fingerprint caveat of 2026-10-17).
- [x] **Step 5. Run** the filecard and cmd tests with `-race`, the ingest suite and ruff.
- [x] **Step 6. Commit:** `feat(filecard): keep an invoice's header pairs for the document fields`.

### Task 6: `document_search` filters

**Files:**
- Modify: `internal/arcadedb/document_retrieval.go:51-57, 225-330`;
- Modify: `internal/documents/retrieval.go:70-82`, `retrieval_cards.go`;
- Modify: `internal/agent/tools/document_search.go`;
- Tests: beside each, plus `document_retrieval_floor_test.go`.

**Interfaces:**
- `arcadedb.FieldFilter{Types, Counterparties, Tags []string; TagsAll bool; DateFrom, DateTo string; IncludeUnreviewed bool}`
  on `CandidateFilter.Fields *FieldFilter`. The values are label ids.
- `candidateWhere` appends predicates on the stamped properties:
  - `df_type IN :types` and `df_counterparty IN :counterparties`;
  - `df_tags CONTAINSANY :tags`, or `CONTAINSALL` when `TagsAll`;
  - `df_date >= :date_from` and `df_date <= :date_to`, both ISO strings;
  - the `_ok` columns when `IncludeUnreviewed` is false.
  - Every leg built on it inherits the filter: dense, full-text, lexical (`document_lexical.go:96`)
    and card.
  - Measured in Task 0: 59–168 ms, against 155–163 ms for the unfiltered query.
- `document_retrieval.go` is 558 lines: the filter goes to `document_retrieval_fields.go`.
- **Names arrive as label names.** The tool resolves them through a
  `docfields.Store.ResolveNames(ctx, identityID string, kind Kind, names []string) (ids []string, unknown map[string][]string, err error)`.
  `unknown` maps each unmatched name to up to 3 closest names, by edit distance over the lowered
  names.
- **The result gains `filters`:**
  `[{name, values, admitted_documents, rests_on_suggestions bool, unknown_names: {…}}]`.
  `name` is `type`, `counterparty`, `tags` or `date`; a date's values read `>= 2026-01-01`.
- **A filter that admits nothing is not searched under.** The admissions are counted first
  (`arcadedb.Client.FieldAdmissions`, through `docfields.Store.Admissions`); when one is zero,
  including a name on no list, the answer abstains with `filter_admits_nothing` and the report,
  without running the search. A named filter with no resolved id is reported, never dropped:
  dropping it would widen the search.

**Measured while writing it** (2026-10-18, the bench library of 4,045 documents, through the
identity's own credential): the lexical leg under `type = fattura` returned 20 candidates, all of
that type, in 12 ms; the admission count took 16 ms and reported 434 admitted, 0 reviewed, so
`rests_on_suggestions` would be true there. `CONTAINSANY` and `CONTAINSALL` on a bound list
behave as the clauses expect, live on 26.10.1.

**Steps:**
- [x] **Step 1. RED.**
  - `TestCandidateWhereFieldFilter`, clause by clause.
  - `TestDocumentSearchSevenArgumentsStrict`: unknown fields are still refused.
  - `TestResolveNamesTrimsAndFolds` (Review Focus 1).
  - `TestUnknownNameReturnsClosest`.
  - `TestIncludeUnreviewedFalseMatchesOperatorValuesOnly`.
- [x] **Step 2. GREEN.** The dense filter and `denseSpaceFilter` stay as they are, so
  `document_retrieval_floor_test.go` must pass unchanged.
- [x] **Step 3. Run `go test -race` on the three packages.**
- [x] **Step 4. Commit:** `feat(document_search): filter by type, counterparty, date and tags`.

### Task 7: rules and the suggestion pass

**Files:**
- Create: `internal/docfields/pass.go`, `pass_test.go`. The matchers already exist from Task 2;
- Modify: the serve wiring beside `cmd/aura/serve_agui.go:131`.

**Interfaces:**
- `rules.Compile(alg Algorithm, pattern string, caseInsensitive bool) (Matcher, error)` and
  `Matcher.Match(text string) bool`. The algorithms:
  - `any`: some word;
  - `all`: every word;
  - `exact`: the phrase;
  - `regex`: RE2;
  - `fuzzy`: a token-set ratio of at least 0.8 against any window of the pattern's length.
- `(s *Store) RunPass(ctx, identityID string, reader DocumentReader) (PassResult, error)`.
  - `DocumentReader` lists `IndexedDocument`s with `indexed_at` above a watermark kept in
    a `DocumentRulePass` record in the identity's database (Task 2).
  - It returns `file_name`, `card` and `header_pairs`.
- **The extractor mapping:**
  - `Data emissione` (dd/mm/yyyy or ISO) goes to `document_date`;
  - `Cliente` and `Fornitore` go to `counterparty`, matched against the list or proposed new;
  - `Fattura n.` and `Stato` go to a custom field of that name when one exists;
  - `set_by=extractor`, with the pair as evidence.
- **Saving a rule** calls `RunRule(ctx, identityID, labelID string)` over all documents.
- **The pass runs per identity** on the existing serve loop's cadence, with no new interval or
  env.

**As built** (2026-10-18):
- `RunPass(ctx, identityID)` reads the documents itself, through the identity's client: no
  `DocumentReader` interface with a single implementation. It matches the rules against the file
  name, the card, the header pairs and the passages (spec decision 2: "the card and the
  passages"), bounded at 1 MB. `RunRule(ctx, identityID, labelID)` walks the library by
  `source_key`, a page at a time.
- The pass is a second loop beside the asset worker, at `runtimeIngestionPollInterval`, so a long
  pass never starves the asset loop. An identity with nothing new costs one schema read and one
  indexed query, about 3 ms once `IndexedDocument.indexed_at` is indexed (25-37 ms without).
- `rules.Matcher.Match` takes a `*rules.Text`, which splits a document into words once per case
  mode: tokenizing 1 MB cost 58 ms per rule. With that and a length bound on the fuzzy windows,
  every rule of the table over 1 MB takes 0.30 s, from 0.87 s.
- A counterparty on no list is proposed as `new:<name>`, left out of the filterable projection.
- Writes to one identity's records are serialized in the process (`lockIdentity`): the pass and an
  operator edit read, change and write a whole record.
- Measured on 26.10.1: `UPDATE ... UPSERT` needs an index on its WHERE, so `DocumentRulePass.kind`
  is UNIQUE; `after` is a reserved word as a parameter name too; two DDL passes over one database
  at once fail inside ArcadeDB, so the Store prepares the types once and serializes the
  preparation per identity.
- On the bench library (4,045 documents) a pass of 200 documents takes about 95 ms.

**Steps:**
- [x] **Step 1. RED.**
  - `TestPassNeverStallsOnPathologicalPattern`: a pass over a 1 MB text with the patterns of
    Task 2's table finishes within 1 s (Review Focus 5).
  - `TestPassNeverResuggestsRejected` (Review Focus 4).
  - `TestPassExtractsOfficeInvoiceHeaders` on the five pairs.
  - `TestPassNeverTouchesOperatorValue`.
- [x] **Step 2. GREEN.**
- [x] **Step 3. Run `-race`.**
- [x] **Step 4. Commit:** `feat(docfields): rules on the label and header-pair suggestions`.

### Task 8: the HTTP API

**Files:** `internal/agui/documents_api.go`, `documents_bulk_api.go`, `documents_labels_api.go`
(new), with tests. Wiring: `SetDocumentFieldsStore`, beside `SetBoardStore`.

**Routes**, base `/api/documents`, behind `RequireAuth`:
- `GET /api/documents?…filters…&sort=&page=`: rows with values, `set_by` and review state, plus
  `facets` (a count per label in the current result) and `total`;
- `GET /api/documents/{id}`: the record, its suggestions, its history;
- `PATCH /api/documents/{id}`: a `Diff`;
- `POST /api/documents/{id}/review`;
- `POST /api/documents/bulk`: `{selection: {ids} | {filter, exclude, expected_count}, diff}`.
  When `expected_count` differs from the filter's count at apply time, it answers 409 with the
  new count (Review Focus 2);
- `GET|POST|PUT|DELETE /api/documents/labels`, the same for `/custom-fields`;
- `POST /api/documents/labels/{id}/run`.

**As built** (2026-10-18):
- `{id}` is the document's `IndexedDocument.search_document_id`, the `document_id` that
  `document_search` returns too: every indexed document has one, labelled or not, and its record
  is created on its first write (`docfields.Store.ApplyToDocuments`, one script for every missing
  record).
- "Da rivedere" is a stamped projection, `needs_review` → `df_needs_review`: a document holding a
  value the operator has not set or accepted, a proposed name included. The list, its filters and
  its facets read one type, `IndexedDocument`, with the same predicate the search applies
  (`arcadedb.FieldPredicate`).
- Saving a label that carries a rule runs it over the library before answering, and the answer
  says how many values it suggested.
- Measured on a disposable database of 4,000 documents and 8,000 passages, through the store: the
  list with its total and facets 47 ms, filtered 25 ms, at offset 3,000 51 ms; a bulk tag on 100
  documents 93 ms, on 1,000 2.3 s and on 4,000 5.3 s, creating the records; the type on 4,000
  documents whose records exist 1.6 s, and the cascade stamped all 8,000 passages.

**Steps:**
- [x] **Step 1. RED.** Handler tests: identity from the principal only, the 409 on a count
  mismatch, the diff semantics, and another identity's id answering 404.
- [x] **Step 2. GREEN.**
- [x] **Step 3. Run `-race`.**
- [x] **Step 4. Commit:** `feat(agui): the documents API`.

### Task 9: the Documenti view, list and filters

**Files:**
- Create: `web/src/documents/DocumentsWorkspace.tsx`, `documentsApi.ts`, `FilterBar.tsx`,
  `documentsLocale.ts`, with tests;
- Modify: `web/src/AppShell.tsx:377-378` for the Cartelle/Documenti toggle, remembered like the
  surface;
- Modify: `web/package.json`: `"@svar-ui/react-grid": "2.7.6"`.

**As built** (2026-10-18):
- `DocumentsSurface.tsx` holds the Cartelle/Documenti toggle, remembered under
  `aura.documents.view`; `AppShell` loads it in place of `FilesWorkspace`.
- The list pages on the server with SVAR's `Pager`, and sorts there: an intercept on `sort-rows`
  that answers false stops the grid's own sort (`@svar-ui/lib-state` `exec`), and `sortMarks`
  draws the arrow.
- The cells live in `DocumentsCells.tsx`, their context in `documentsCellContext.ts`. In 2.7.6
  the runtime hands a custom cell `onAction` while the published types declare `onaction`; the
  cells read both.
- The mockup's Exclude, "Non assegnata" and search box needed the list API to grow:
  `type_not`, `counterparty_not`, `tags_not`, `unassigned`, `q`, and an `unassigned` facet
  (commit `feat(docfields): the list's exclusions, unassigned fields and name search`). The search
  box matches file names; its "Significato" mode, a semantic search, is left for later.
- `web/src/i18n/resources.ts` passed 600 lines with the new namespace: the approval strings moved
  to `resources.approval.ts`.

**Steps:**
- [x] **Step 1. RED, vitest:**
  - columns and source marks rendered from API rows;
  - facet counts, Include, Exclude and "Non assegnata";
  - a click on a value adds its filter;
  - Reset;
  - the "da rivedere" counter.
- [x] **Step 2. GREEN,** following mockup 1. The grid uses `cell` components for values and
  marks; the bar sits outside the grid.
- [x] **Step 3. `npm run lint && npm run typecheck && npm test`.**
- [x] **Step 4. Commit:** `feat(web): the Documenti view with filters`.

### Task 10: bulk edit and its confirmation

**Files:** `web/src/documents/BulkBar.tsx`, `BulkConfirmDialog.tsx`, with tests.

**As built** (2026-10-18): `bulkModel.ts` holds the three states and the diff, `useBulkEdit.ts`
the selection (ticked ids, or the filter minus the unticked) through the confirmation and the
request, and `SelectionCells.tsx` the checkbox column (`select={false}`, `data-action=
"ignore-click"`, as SVAR's selection guide shows). A filter selection reads a value's three states
from the result's facets, which count the excluded documents too; clearing a single field there
removes every value the facets hold. The mockup's "Segna come rivisti" on the selection is not one
of the spec's behaviours and has no API: left out. The tests share `documentsTestServer.tsx`.

- [x] **Step 1. RED:**
  - three states per value;
  - Apply sends only the diff;
  - the confirmation names the change and N;
  - "tutti i N del filtro" sends `{filter, exclude, expected_count}`;
  - a 409 shows the new count and asks again.
- [x] **Step 2. GREEN,** following mockup 2.
- [x] **Step 3. The web gates.**
- [x] **Step 4. Commit:** `feat(web): bulk edit documents by difference`.

### Task 11: detail, suggestions and review

**Files:** `web/src/documents/DocumentDetail.tsx`, `SuggestionChip.tsx`, `DocumentHistory.tsx`, with
tests.

**As built** (2026-10-18): the name in the list opens the detail in place of the list. The form
(`detailModel.ts`) holds only what the operator set or accepted, and Save sends `formDiff` against
the document as last read, so a × written meanwhile keeps the unsaved edits. `DetailFields.tsx`
holds the controls, `detailNames.ts` the field and value names, and `useDocumentLists.ts` the lists
the list and the detail share through the query cache; the detail waits for them, so no label
reads as unknown while they load. The preview reuses the chat's renderers over the file manager's
direct route. A proposed name (`new:`) taken into the form becomes a label of its list on Save. The
mockup's "2 di 7" reads "N da rivedere", and its "+ Campo" belongs to Task 12.

- [x] **Step 1. RED:**
  - a suggestion shows its source and evidence;
  - a click puts it in the form, unsaved;
  - × records a rejection;
  - Save sends only the changed fields;
  - "Rivisto, salva e avanti" opens the next "da rivedere" document;
  - closing with unsaved changes asks first.
- [x] **Step 2. GREEN,** following mockup 3.
- [x] **Step 3. The web gates.**
- [x] **Step 4. Commit:** `feat(web): document detail with suggestions and review`.

### Task 12: the lists and their rules

**Files:** `web/src/documents/LabelsAdmin.tsx`, `CustomFieldsAdmin.tsx`, with tests.

**As built** (2026-10-18): "Liste e regole" in the Documenti header opens `ListsPage.tsx` in place
of the list, a tab per list. A value's count is the documents of the whole library holding it, read
from the unfiltered list's facets; "Filtra documenti" goes back to the list filtered on that value
alone. Custom fields have neither count nor filter: the list filters on type, counterparty and tags
only. `adminParts.tsx` holds the entry, the editor's frame, the radio choice and the delete
confirmation both pages share, and `useWrite.ts` the busy and failure state the detail shares too.
The server's sentence reaches the page as `HttpError.detail`, the `{message}` writeDocumentsError
writes beside its code.

- [x] **Step 1. RED:**
  - a list with its name, the rule in words, a count, and "filtra documenti";
  - the pattern box only for any, all, exact, regex and fuzzy;
  - an invalid regex shows the server's message.
- [x] **Step 2. GREEN.**
- [x] **Step 3. The web gates.**
- [x] **Step 4. Commit:** `feat(web): document lists and matching rules`.

### Task 13: measure, verify, review, push

- [x] **Step 1. The question set.** Run Task 1's set, first with today's search, then with
  filters on a library the operator, or the test harness standing in, has reviewed.
  - Record set recall both ways in `docs/superpowers/verification/2026-10-18-document-fields.md`.
  - **The gate:** recall up, and no gold lost that an unfiltered retry would not recover.
- [x] **Step 2. Survival and isolation, live.**
  - A rename, a move, a re-ingest and a byte edit from the cockpit keep the values and the
    history.
  - After a rename, the index shows the new name.
  - A second identity sees nothing, through the API, the search and both databases.
- [x] **Step 3. Playwright** `web/e2e/documents.spec.ts`: list, filter, bulk edit with its
  confirmation, and review with "salva e avanti". The evidence goes through `testInfo.attach`.
- [x] **Step 4. The gates.** `make quality`, and the coverage gate through
  `bash scripts/coverage_docker.sh`: the package policy, including a policy entry for
  `internal/docfields`.
- [x] **Step 5. Final review** by one fresh reviewer, on the range, this plan, the spec and the
  Review Focus. Fix its findings test-first.
- [x] **Step 6. Push. Merge to master** on the operator's word.
- [ ] **Step 7. The lab VM.** The real agent answers the set through `document_search` with
  filters, after the operator reviewed the library in the view. The score must be above 9.8, with
  the dated file. The operator runs it.

## Decisions taken while writing this plan

- **Superseded by Task 0 (2026-10-18):** the subquery failed the gate; the fields live in ArcadeDB
  and triggers stamp them on the indexed rows (Architecture above). The original ruling follows.
- **Ruling: the mirror's filter is a `source_key IN (subquery)` on `candidateWhere`,** so every leg
  inherits it from one clause. Cost if wrong: Task 0 says so before any code, and the filter
  becomes a Go-side id prefilter.
- **Ruling: one migration holds every table,** the pass watermark included. Cost if wrong: one
  more migration.
- **Ruling: fuzzy is a token-set ratio of at least 0.8.** Paperless's own fuzzy threshold is not
  copied, because its code is GPL. Cost if wrong: a constant, tuned on the office set.
- **Ruling: the extractor maps only the five header keys of the office invoices,** in Italian.
  Other layouts wait for a measured need.
- **Ruling: bulk "all" re-evaluates at apply time and refuses on a count change.** It never applies
  silently to a different set.

## File structure

| Files | Responsibility |
|---|---|
| `internal/arcadedb/document_fields_schema.go` | the ArcadeDB types, indexes and triggers |
| `internal/docfields/` | the store, diff semantics, provenance, events, the pass |
| `internal/docfields/rules/` | the five matchers |
| `internal/arcadedb/document_retrieval.go`, `internal/documents/retrieval*.go`, `internal/agent/tools/document_search.go` | the filters |
| `internal/objectstore/s3.go`, `internal/assets/filemanager_ops.go` | rename and move carry the name and the fields |
| `internal/documents/filecard/`, `cmd/aura-filecard/`, `services/ingest/` | header pairs to `IndexedDocument` |
| `internal/agui/documents_*api.go` | the HTTP API |
| `web/src/documents/` | the view, the bars, the detail, the lists |
| `prd.md` §11, `docs/superpowers/verification/` | the amendment and the records |
