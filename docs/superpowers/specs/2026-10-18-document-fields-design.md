# Document fields: type, counterparty, date, tags, reviewed by the operator

Proposed on 2026-10-18 at `ba4b03b`. It is item D5 of `2026-10-09-consolidation-best-of-design.md`.
The operator asked for it ("Andiamo avanti con le funzioni di classificazione dei documenti"),
proposed taking Paperless-ngx's interface as the example, chose a new Documenti view on SVAR's
grid over a fork of the file manager ("Va bene"), and approved the three mockups ("Vanno bene,
scrivi la spec").
- **The mockups:** https://claude.ai/artifact/XsQ73gmMzmHC6STSqekmZN. They are private to the
  operator: list and filters, bulk edit, detail and review.
- **The readings:**
  - `docs/paperless-ngx-behaviours-2026-10-17.md` (backend);
  - `docs/paperless-ngx-ui-behaviours-2026-10-18.md` (interface);
  - `docs/svar-filemanager-inventory-2026-10-18.md` (SVAR).
- **Paperless is GPL-3.0.** Behaviours only, no code.

Risk tier: **medium.** It adds data the operator owns and that nothing can rebuild, and it adds
seven optional arguments to an agent tool.

## The problem, measured and read

1. **A question over many documents has no handle.**
   - Office q09 ("tutte le fatture del cliente Gialli Import") and q18 ("fatture emesse a marzo
     2026") ask for a set of documents.
   - `document_search` can filter only by document id, by file and by folder
     (`CandidateFilter`, `internal/arcadedb/document_retrieval.go:51-57`).
   - On 2026-10-17 q18 ranked fourth, "a question over every document, which no ranking
     answers" (`2026-10-17-workbook-row-passages.md:67-68`).
2. **The fields are in the files, and Aura drops them.**
   - filecard reads an invoice's top rows ("Fattura n.", "Cliente", "Partita IVA",
     "Data emissione", "Stato") as a banner and skips them (`internal/documents/filecard/table.go:129-132`).
   - So "Cliente | Gialli Import" is in neither the card nor the row passages. This is read from
     the code; no test pins it.
3. **Nothing about a document survives its key.**
   - `IndexedDocument` belongs to CocoIndex, keyed by `search_document_id`, which hashes the
     object key (`services/ingest/identity.py:38-57`).
   - A rename or a move in the file manager is a copy then a delete
     (`internal/assets/filemanager_ops.go:120-180`). CocoIndex then runs `DETACH DELETE` on the old
     vertex (cocoindex 1.0.25 `connectors/neo4j/_cypher.py:117`).
   - A property written on it by anyone else is gone too. Only `raw_sha256` survives a move, and
     only the key survives an edit.
4. **0098 retired the old catalog on purpose.**
   - `0098_retire_document_catalog.up.sql:30-31`: "document titles and tags stop existing. A
     document's name is its object key, and anything richer has to live in S3 object metadata."
   - A new table of document fields reverses that, unless it is measured to be needed.

## Goal

The operator sees, filters, corrects and reviews a few fields on every document, in a Documenti
view modelled on Paperless's list. The agent filters `document_search` by the same fields.
- Every value records who set it: the operator, a rule, the extractor, a classifier or an LLM.
- A value nobody reviewed is never treated like one the operator set.
- The fields survive a rename, a move and a re-ingest.

## Non-goals

- **A classifier, and LLM suggestions.** They are phases 2 and 3 below, each behind its own
  measurement. This spec ships the fields, the rules, the extractor, the review loop, the view
  and the filters.
- **Saved views, nested tags, permissions per document, a full-text query language.**
- **Versioning**, which the operator dropped on 2026-10-17.
- **Questions over every document** (D4). Filters narrow a search; they do not sweep a library.

## Anti-goals

The slice has failed, even if it works, if any of these happens:
- a filter hides a gold document the unfiltered search found, without the agent being able to
  search again unfiltered;
- a rename or a move loses a value the operator set;
- an unreviewed value is shown or used with the same weight as an operator's;
- one identity sees or changes another's fields.

## Decisions

### 1. The fields

- **Fixed fields:**
  - `type`, one value from the operator's list (fattura, contratto, registro contabile…);
  - `counterparty`, one value from the operator's list;
  - `document_date`, a date;
  - `tags`, a set from the operator's list.
- **Typed custom fields:** date, monetary (an ISO 4217 currency plus an amount), integer, text,
  and select. A field's type is fixed once it is created.
- **The lists** (types, counterparties, tags, custom fields) belong to the identity, with names
  unique per kind.
- **Every value carries:**
  - `set_by`: `operator`, `rule:<name>`, `extractor`, `classifier`, `llm`;
  - `set_at`;
  - for anything but `operator`, the evidence: the rule's pattern, or the header line read.
- **Every document carries `reviewed_at` and `reviewed_by`,** null until the operator marks it
  reviewed.

### 2, amended 2026-10-18: the fields live in ArcadeDB, kept on the indexed rows by triggers

**Measured first, in Task 0** (`docs/superpowers/verification/2026-10-18-document-fields-mirror.md`):
- **The subquery on a Go-written mirror fails the plan's 2× gate.** On a 4,045-document library of
  real labelled documents its cost grows with the documents it admits: 2.9× the unfiltered query
  at 325 documents and 14.6× at 4,027.
- **Copying the fields onto the `Passage` rows passes at every width,** 59–168 ms against
  155–163 ms unfiltered, with identical answers. That is Paperless's shape: the index carries the
  fields.
- **ArcadeDB keeps that copy itself.** An `AFTER UPDATE` SQL trigger stamps the passages when
  CocoIndex's `MERGE … SET n += $props` lands: 407 of 407 stamped, at no measurable ingest cost.
  Cascade triggers carry the operator's edits, and a rename is handled once Go moves the key.

**The operator's call:** the fields leave Postgres. Everything decision 2 below put in `aura.*`
lives instead in the identity's own ArcadeDB database:
- the lists, the values with provenance, the review state, the rejections and the events;
- `DocumentFields`, one per `source_key`, the truth.

Isolation is the database per identity. Triggers keep `df_*` on `Passage` and `IndexedDocument`,
and the search filters on them (decision 5). Go still owns every write, and moves the record on a
rename. 0098's sentence about object metadata still does not hold, for the reasons below.

What this does not prove: a library larger than 4,045 documents, the triggers under the live
ingest loop with concurrent writers, and the cockpit's facet and bulk queries on ArcadeDB SQL.
Those are measured by the tasks that build them.

### 2. Where the fields live: Postgres, mirrored into ArcadeDB by Go (the operator's call, superseded above)

**Proposed, after the builder check of 2026-10-18.**

**Postgres is the truth.** Everything lives in new `aura.*` tables with fail-closed RLS like
`aura.assets` (`0090_rls_fail_closed_assets.up.sql`):
- the identity's lists (types, counterparties, tags, custom-field definitions), each with its
  matching rule;
- one row per document, keyed by `(identity_id, object_key)`, with its own id. That id is the
  stable handle the history and the UI use;
- the current values, each with `set_by`, `set_at` and evidence; the review state; the rejected
  suggestions;
- `aura.document_field_events`, one row per change. It is the audit and phase 2's training data.

**Go owns every write,** which removes the ingest from the write path:
- **A rename or a move in the file manager** already runs `RelocateAsset`
  (`internal/assets/filemanager_ops.go:120-180`). The same transaction moves the document row to
  the new key, so its values follow the file.
- **A copy** gets a new row, with the values copied and marked `set_by` as copied.
- **An object renamed or moved outside the cockpit**, straight in the bucket, loses its row. This
  is recorded as a limit, not handled.

**ArcadeDB gets a Go-owned mirror:** a `DocumentFields` document type in the identity's database,
keyed by `source_key`, holding the current values and whether each is reviewed.
- **Not on `IndexedDocument`.** That vertex belongs to CocoIndex and is `DETACH DELETE`d on every
  rename. `DocumentFields` belongs to no CocoIndex target, so a reconcile never touches it.
- **Go writes it in the same request as Postgres.** A failed mirror write fails the save, and a
  repair pass rebuilds the mirror from Postgres.
- **The search filters with a subquery on `DocumentFields`**, inside the statement, so there is
  no 100-id cap (decision 5).

**Why not object metadata, which 0098 points to** (my first proposal, dropped):
- **A metadata-only write may never reach the index.** `process_file` is memoized on the S3
  file, mtime then ETag (`services/ingest/app.py:294`; cocoindex 1.0.25
  `resources/file.py:183-202`, `connectors/amazon_s3/_source.py:35-44`). A copy onto itself with
  replaced metadata usually keeps the content's ETag, so the mirror would stay stale.
- **The lists, the rules and the rejections need a store anyway.** The ingest has no Postgres
  connection (`app.py:31-41`).
- **About 1 KiB of user metadata is left** once the filename is stored
  (`internal/objectstore/asset_placement.go:15-19`).
- **The Documenti view needs counts and filters over the library,** and metadata cannot be
  queried.

**It reverses 0098's sentence "anything richer has to live in S3 object metadata."** The PRD
amendment records why, citing these four reasons.

**Task 0, before the code, measures two things on a disposable stack:**
- the fused query's latency with the `DocumentFields` subquery on the 7,798-passage library,
  against today's;
- that a reconcile, a rename and a re-ingest leave `DocumentFields` intact.

**Found while checking this: a rename keeps the old name in the index.**
- `S3Store.Copy` sends no `MetadataDirective` (`internal/objectstore/s3.go:289-294`), so the old
  `filename` metadata is copied.
- The ingest prefers that name (`app.py:315-316`).
- So the index shows a renamed file under its old name. This is read, not run.
- The rename path is touched here anyway, so it is fixed test-first in this slice (NOT MY WORK).

### 3. Assignment, in order, never overwriting the operator

1. **The operator.** A value the operator set is never changed by anything below. Their removal
   of a value is recorded, and that value is never re-suggested.
2. **Rules on the label**, as in Paperless: Any, All, Exact, Regex and Fuzzy over the document's
   text and file name.
   - A rule lives on its type, counterparty or tag, case-insensitive by default.
   - **One matcher, in Go.** It runs in two cases:
     - when the operator saves a rule, over every document;
     - for each newly indexed document, found by `IndexedDocument.indexed_at` since the last pass.
       **Amended 2026-10-23:** found by a per-document mark instead, `IndexedDocument.df_passed_at`,
       the `indexed_at` the pass read it at; a document whose mark is missing or older is read. A
       watermark assumed documents become visible in `indexed_at` order, and the ingest breaks it:
       on the lab VM 8 of 14 invoices uploaded together became visible after a pass had read later
       ones, and were never read (Task 13 step 7; `TestPassReadsADocumentThatBecameVisibleLate`).
   - It reads the card and the passages from ArcadeDB. The ingest runs no rules.
3. **The extractor.** filecard keeps an invoice's header pairs. They are derived from the bytes,
   so they ride on `IndexedDocument` as a CocoIndex-owned property, as the card does. Go's pass
   turns them into candidates:
   - the date, for `document_date`;
   - the counterparty, matched against the list, or proposed as a new name;
   - the number and the status, for custom fields when the operator has defined them.
   - **This is bespoke: STOP BEFORE BESPOKE applies, and the inventory is below.**
   - Paperless never extracts a value from content, except a date found by a regex sweep
     (`paperless-ngx-behaviours-2026-10-17.md`).
   - filecard already reads the rows and only discards them (`table.go:108-145`). Keeping them is
     a change of what it returns, not a new parser.
4. **Nothing else in this spec.** No classifier and no LLM.

A rule's or the extractor's value is a **suggestion** until the operator accepts it, edits it, or
marks the document reviewed.
- Until then the document counts as "da rivedere".
- **A filter matches it only when the agent asks for unreviewed values too** (decision 5).

### 4. The Documenti view

**It lives where the operator already looks.** The cockpit's `documents` surface renders
`FilesWorkspace` today (`web/src/AppShell.tsx:377-378`). It gains a toggle between **Cartelle**,
the existing file manager unchanged, and **Documenti**, the new view. The choice is remembered
like the surface itself.

**The new view is built on `@svar-ui/react-grid` 2.7.0,** added as a direct dependency (MIT). It
is already resolved through react-filemanager (`web/package-lock.json:6089`). It follows the
three mockups and the five behaviours of the UI brief:
1. **List and filters.**
   - Columns: name, type, counterparty, date, the custom fields the operator switches on, tags,
     and review state. Next to a value its operator did not set, a small mark names the source.
   - Above the list, a bar with a search box, then dropdowns for type, counterparty and tags.
     Each item shows its count in the current result, plus Include, Exclude and "Not assigned";
     tags add Any or All.
   - Dates take ranges and presets. A "da rivedere" counter sits at the top, and Reset clears
     the filters.
   - Clicking a value in a row filters by it.
2. **Bulk edit.**
   - While documents are selected, the filter bar becomes the edit bar.
   - Each value shows three states: on all, on some, on none.
   - Apply sends only the difference, after a confirmation naming the change and the number of
     documents.
   - "All" means the whole filter minus exclusions, not a list of ids.
3. **Detail and review.**
   - A preview, the field form, and every suggestion under its field with its source and
     evidence. A click accepts it, and × rejects it; a rejection is recorded.
   - The document's history.
   - Save, plus "Rivisto, salva e avanti" over the "da rivedere" list.
4. **The lists.** One page each for types, counterparties, tags and custom fields: name,
   matching rule shown in words, document count, and "filter documents". The pattern box appears
   only for the algorithms that need one.

**No field editor goes inside the file manager.** Its root keydown handler would turn typing
into file actions (`docs/svar-filemanager-inventory-2026-10-18.md`).

### 5. `document_search` gains filters

The new arguments, all optional, strictly decoded like the existing ones
(`internal/agent/tools/document_search.go:130-146`):
- `type`, `counterparty`: a list of names;
- `tags`: a list, with `tags_mode` `any` or `all`;
- `date_from`, `date_to`;
- `include_unreviewed`: a boolean, default `true`, so a library nobody reviewed still answers.

**Where the filters apply.** They join `candidateWhere`
(`internal/arcadedb/document_retrieval.go:309-330`) as a subquery on `DocumentFields`, so every
leg built on it applies them: dense, full-text, lexical (`document_lexical.go:96`) and card.

**"Unreviewed" is judged per value.** A value is unreviewed while its `set_by` is not `operator`
and the operator has not accepted it.

**Names match case-insensitively and exactly** against the identity's lists.

**The tool's result reports, for each filter it applied:**
- whether the match rests on operator values or on suggestions;
- how many documents of the library the filter admits;
- when a name matches nothing, the closest names on the list, so the agent can correct itself or
  retry without the filter. That answers the first anti-goal at no second query.

The agent filters on the fixed fields and tags only. Custom fields are shown, edited and filtered
in the cockpit; the agent gets them in a later slice.

## Shape

| Part | Files |
|---|---|
| Task 0, the measurement | a dated file in `docs/superpowers/verification/`; no product code |
| PRD | `prd.md` §11: the fields, where they live (per Task 0), the provenance, the filters, before the code |
| Storage | migration `<next>` from `ls internal/db/migrations/ \| tail -1` (today 0141): the lists, the document rows, the values, the rejections and `aura.document_field_events`, with RLS; `DocumentFields` in ArcadeDB, created by Go |
| Ingest | `services/ingest/app.py` and `arcade.py`: the header pairs as an `IndexedDocument` property, with `process_file` versioned so the memo re-extracts |
| Rename | `internal/assets/filemanager_ops.go`, `internal/objectstore/s3.go`: move the document row and the mirror, and replace the `filename` metadata on a rename |
| Rules | a Go package: the matcher, the pass over new documents, and the pass on a rule save |
| filecard | `internal/documents/filecard/table.go` and neighbours: keep the header pairs as candidates, test-first |
| Go API | `internal/agui/`: list documents with fields, facet counts, bulk edit as a diff, the lists and rules; identity from the principal, never a parameter |
| Search | `internal/arcadedb/document_retrieval.go`, `document_cards.go`, `internal/documents/retrieval*.go`, `internal/agent/tools/document_search.go` |
| Cockpit | `web/src/documents/` (new): the view, the bar, the detail and the lists, beside `web/src/files/`; `web/package.json` gains `@svar-ui/react-grid` |

Every file stays under 600 lines.

### New env vars

None.

## Phases 2 and 3, gated

- **Phase 2: a classifier**, only for a label the operator opts into ("Auto"), trained only on
  reviewed documents.
  - It ships only if, out of sample on the operator's own reviewed documents, it beats the rules
    alone (BASELINE OR DROP).
  - It abstains when unsure, and it never sets a value: it suggests one.
- **Phase 3: an LLM suggestion,** last, opt-in, on the appliance's local model.
  - It picks only among the names the operator's lists already hold, or proposes a new name the
    operator must create.
  - The document's content goes in marked untrusted.
  - It must beat phase 2, or the rules where phase 2 was dropped, on the same set.

Each phase gets its own spec after its measurement.

## Acceptance

1. **Task 0** is recorded: the latency with the `DocumentFields` subquery, and `DocumentFields`
   intact after a reconcile, a rename and a re-ingest.
2. **A "which documents" set.** At least 20 questions over the office corpus, extending q03, q09
   and q18, with the gold *answer documents* (the invoices themselves, not the ledger that lists
   them) written down before the code.
   - Every question can be answered with the agent's filters: type, counterparty, date, tags.
   - q08 ("non pagate") needs a status the agent cannot filter yet, so it stays out until custom
     fields reach the tool.
   - Measured with today's search, then with the filters the questions imply, set by the
     operator on a reviewed library.
   - **The gate:** set recall up, and no gold lost that the agent could not recover by an
     unfiltered retry.
3. **The extractor.** Every office invoice yields its counterparty, date and number as
   candidates, matching the generator's values (`spikes/document-routing-benchmark/generate_corpus.py:123-139`).
4. **Survival.** An operator value and its history survive a rename, a move, a re-ingest and an
   edit of the bytes, made in the cockpit, on the live stack. After a rename the index shows the
   new name.
5. **Isolation.** A second identity sees none of the first identity's fields, lists or events,
   through the API, the search and the database.
6. **The cockpit.**
   - Component tests per part.
   - A Playwright run of list, filter, bulk edit with its confirmation, and review with "salva e
     avanti", with its evidence attached to the result.
7. **Go and Python units, race, coverage policy;** `make quality` green.
8. **E2E** on the lab VM: the operator reviews a library in the view, and the real agent answers
   the set through `document_search` with filters. Score above 9.8, with a dated file in
   `docs/superpowers/verification/`.

## Open questions

1. **Decision 2.** A Postgres table plus a Go-owned ArcadeDB mirror, which reverses 0098's
   sentence on object metadata. This is the operator's call.
2. **Does "reviewed" need a reviewer other than the operator?** One operator per identity today.

## Calls made while writing, for the operator to check

- **The lists start empty.** The extractor proposes new counterparties, and the operator creates
  them. The office corpus seeds them only in tests.
- **Seven optional arguments** on `document_search`, with `include_unreviewed` defaulting to
  `true`. The result marks matches that rest on suggestions.
- **The agent filters on fixed fields and tags only** in this slice.
- **Renames outside the cockpit** lose the fields: recorded as a limit.
- **The rename bug is fixed in this slice,** because its path is touched anyway.

## What this spec does not prove

- **That filters help on libraries other than office.** The set is office only.
- **That the agent picks the right filter from a question.** Acceptance 8 shows it on one set,
  not in general.
- **The classifier and the LLM,** until their phases run.
