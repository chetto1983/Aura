# Document fields, Task 13 step 7 on the lab VM — 2026-10-23

Plan `docs/superpowers/plans/2026-10-18-document-fields.md`, Task 13 step 7, on build `d46cedab6`.
That build carries the pass fix `2fa2df738` and the cloud credentials fix
(`2026-10-23-cloud-service-credentials.md`). The chat ran on ChatGPT `gpt-6.1-sol`, the VM's
configured primary route.

**Verdict: the gate is not met.** The score is 9.44; the gate is above 9.8.

## Protocol

- **Library.** The 23 files of `scripts/fixtures/document_retrieval_eval/corpus/` were uploaded
  through the file manager into `archivio-ufficio/` at 10:40Z, beside the operator's own 6
  documents.
  - The first run of the morning was removed before this upload: 23 files, 17 labels and 2
    conversations.
- **Review.** The harness stood in for the operator through the documents API, as the signed-in
  operator. It set the values the set's `library` declares and marked each document reviewed.
- **Turns.** One conversation per question, one message, through `POST /agent/run`, one at a
  time, default effort. The frame was `Nella mia libreria documenti: <query>. Rispondimi qui in
  chat con i nomi dei file.`, which names no filter, field or tool.
- **Scoring.** The score counts the set's documents named in the final answer. It is 10 × mean
  F1 over the 25 questions.
- **Cleanup.** Afterwards the 23 files, the 17 labels the review created and the 25
  conversations were deleted.

The scripts and the raw frames are kept outside the repository; they carry session identifiers.

## The extractor, before the review: the pass fix measured

| | Morning upload, 06:37Z, build without `2fa2df738` | This upload, 10:40Z, build `d46cedab6` |
|---|---|---|
| Invoices with an extractor counterparty proposal | 6 of 14 | **14 of 14** |
| Invoices with an extractor date proposal | 6 of 14 | **14 of 14** |
| Proposals equal to the set's declared values | — | 14/14 counterparties, 14/14 dates |

A proposal carries its evidence, for example `Cliente: ACME SpA` and `Data emissione: 2026-01-12`.
An unknown counterparty is proposed as a new name (`new:ACME SpA`).

## The run

- **Questions answered:** all 25 ended with `RUN_FINISHED`.
- **Score:** 9.44, with mean recall 0.960 and mean precision 0.937.
- **Exact sets:** 23 of 25.
- **`document_search`:** used on all 25 questions, with `status: complete` on the dense legs.
- **Filters:** on 15 questions the agent passed the set's own filters.

| qid | Question | Set's filters | What the agent passed | F1 |
|---|---|---|---|---|
| f20 | i documenti interni dell'amministrazione | `tags: [interno]` | no filter, two text searches | 0.60: all three gold documents, plus 4 others |
| f21 | la bozza interna | `tags: [interno, bozza]`, `all` | no filter, two text searches | 0.00: "nothing found" |

Every other question scored F1 1.00. Questions on type, counterparty and date found their
documents, including the three phrasings on dates (`q03f`, `q09f`, `q18f`) and the ranges
`f12`–`f17`.

## Why the two tag questions fail, read in the code

`document_search` tells the model that `type`, `counterparty` and `tags` take names "from the
operator's own lists" (`internal/agent/tools/document_search.go`), but it never shows those
lists.
- **Type and counterparty.** Questions about them carry the name in the words: "fatture",
  "ACME SpA".
- **Tags.** "Documenti interni" and "la bozza interna" do not contain the tag's name. The model
  had no way to know that `interno` and `bozza` exist, and it never tried a tag.
- **The spec's safety net cannot help.** Spec §5 says a name that matches nothing returns the
  closest names on the list, but only to a call that tries a tag.

Deciding how the agent learns the lists is a design change, and the operator decides it before
any code (PRD-first). One option: the result of every `document_search` names the library's
tags and types, which keeps the tool description static for the cache.

## Also found

- **The scanned PDF was not indexed.** `verbale_scansionato_italiano.pdf` failed: with a ChatGPT
  primary, the media indexer has no vision route and fell back to the local OCR sidecar, which
  the VM does not run. Details are in `2026-10-23-cloud-service-credentials.md`.
  - No question of the set uses that file. The review declared it absent, and it is not part of
    the score.
  - The operator's decision the same day: the chosen model reads images when it can, ChatGPT
    included.

## What this does not show

- **One run, one model.** One run of 25 questions on one model, `gpt-6.1-sol`; other primary
  models may pick tags differently.
- **A stand-in review.** The harness did it through the API; the operator did not review the
  library in the Documenti view.
- **The extractor's coverage.** Only the office invoice header is measured, which is the only
  layout the extractor maps.
