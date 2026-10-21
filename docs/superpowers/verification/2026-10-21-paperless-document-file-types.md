# Document file categories — 2026-10-21

Task 1b of `docs/superpowers/plans/2026-10-21-paperless-documents.md`.
Base: `3546f815f1e987e329250f748c33395871de7724`; checkout: `D:/Repo/Aura`,
branch `feat/paperless-documents`. No worktree, extraction change or new dependency.

## Contract and measured scope

The operator added Code and then explicitly rejected the compatibility default of All files.
The final contract is `file_type=all|documents|pdf|word|spreadsheet|presentation|text|image|video|audio|code`.
Omitted/empty filters mean Documents in GET, bulk JSON, saved queries and zero-valued
`ListQuery`. Only explicit `all` includes every indexed category. Documents contains PDF,
Word, spreadsheet, presentation and prose text, excluding pictures, video, audio, code and
unknown extensions. Unknown extensions report `file_type: other` and are accessible under All.

One inventory supplies both bound SQL suffix predicates and response classification. Code
reuses `internal/documents/filecard.SourceExtensions()` rather than another source-language
list; JSON/XML/HTML/HTM are added because the existing ingestion handlers treat them separately.
Office/ODF/macro/template families and common media formats are represented. Library grouping
is deliberately broader than `internal/assets` upload admission and does not promise a viewer,
editor or extractor for every recognized format.

Official SQL references read before implementation:
[SQL methods](https://docs.arcadedb.com/arcadedb/reference/sql/sql-methods) and
[SQL syntax](https://docs.arcadedb.com/arcadedb/reference/sql-syntax).
The predicates reuse the existing `file_name.toLowerCase() LIKE :parameter` query form.

## RED → GREEN

- Before production changes, the new unit tests failed because saved queries rejected
  `file_type`, unknown values were accepted by the parser, and serialized rows omitted the
  category. New API tests failed because bulk JSON rejected `file_type` and invalid GET values
  returned 200. New live tests failed because every category returned all fixture records.
- After the operator changed the default, a separate RED run required omitted/empty queries
  to return canonical `documents`; the then-current implementation returned an empty value.
  This intentional contract change explains why initial All-default expectations were replaced.
- GREEN: full package unit/race suites and the live `arcadedb_integration` suite passed.

The live mixed fixture has 27 records: 12 documents, two pictures, one video, one audio,
seven code/config files and four unknown/binary-extension files. It checks every exact group,
explicit All, empty/default queries, page size/total, List/Select membership, row/detail
classification, saved-view persistence through a fresh Store and the default of an empty saved
query. Uppercase/multipart names, source suffixes containing `+`/`-` and final suffix boundaries
are included. A second fixture composes file type with content and filename search, label,
custom exact value, source-key selection, Inbox and selection exclusions. The existing full
workspace suite also ran, including content pagination past 50 documents/passages.

## Commands and results

All Go commands ran sequentially in WSL. Live credentials were injected from `.env` by the
existing local `run-task-1.py` helper without printing their values.

| Command | Result |
|---|---|
| `go vet ./...` | PASS, exit 0 |
| `go build ./...` | PASS, exit 0 |
| `go test ./internal/docfields ./internal/agui ./internal/documents/filecard -count=1` | PASS; 0.355 s / 27.265 s / 1.463 s |
| `go test -race ./internal/docfields -count=1` | PASS; 7.089 s |
| `go test -race ./internal/agui -count=1` | PASS; 37.245 s |
| `go test -race -tags arcadedb_integration -coverprofile=/tmp/aura-documents-task1b.cover ./internal/docfields -count=1` | PASS; 22.889 s; 90.6% statement coverage |
| `mutate-task-1b.py` with temporary Go overlays | PASS; 9/9 valid mutants killed (100%) |

Every new file-type helper measured 100% statement coverage; this is the combined unit/live
package profile, not a whole-repository coverage claim.
Mutation cases covered the default, response and SQL case folding, final suffix boundary,
Documents membership, enum validation, query propagation, saved allowlist and bulk propagation.
Overlays left production files untouched.

## Limits

Classification uses filename extensions only. It cannot distinguish SQLite bytes from a
Siemens source named `.db`, or TypeScript from another file named `.ts`; the canonical source
registry supplies those names. Ingestion's existing binary checks and extraction paths remain
authoritative and unchanged. These synthetic fixtures do not establish extraction fidelity,
Craft preview quality, production corpus latency, browser acceptance or the final workspace
release gates. Independent review, merge, post-merge verification and publication belong to
the controller's subsequent tasks.
