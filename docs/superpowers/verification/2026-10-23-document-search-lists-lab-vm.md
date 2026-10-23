# Document search lists: the agent's own check on the lab VM, 2026-10-23

Plan `docs/superpowers/plans/2026-10-23-document-search-lists.md`, Task 3. Before and after: two turns
of one conversation, in which the operator asked Aura's agent to test its document tools and report
friction.

**Verdict.** The agent reports four frictions resolved and one improved, with no regression across
its repeated probes. It named four residual frictions, none a wrong result; on build `ee662da70` it
reports all four resolved (turns 3 and 4). The floors friction remains, and is plan
`2026-10-23-document-floors-calibration.md`. No 9.8 score is produced: the operator replaced the
25-question run with this check.

## Protocol

- **Who drove it.** The operator, through the cockpit. Conversation `01a168cf-…`, chat on ChatGPT
  `gpt-6.1-sol`, default route, effort high.
- **Turn 1, 12:49Z, build `d46cedab6`** (before this plan). The operator wrote "prova a cercare
  documenti con i loro tag e dimmi se trovi frizioni". The operator exported the turn.
- **Turn 2, 14:14Z to 14:17Z, build `489815bf7`.** That is this plan's code, applied by the VM's updater; the
  image's revision label was read on the VM before the turn. The same conversation was continued as the
  operator through `POST /agent/run`, with "Ho aggiornato i tool dei documenti. Rifai le stesse prove
  sui tag e dimmi se le frizioni che avevi trovato ci sono ancora, o se ne trovi di nuove."
- **The library.** The operator's own. One tag, `Cliente`, on two documents: reviewed on one, suggested
  on the other. Nothing was uploaded or changed, and the agent says it modified no document or tag.

The turn frames are kept outside the repository; they carry session identifiers and the operator's
document text.

## Turn 1, without the lists: what the agent found

The agent ran 14 `document_search` calls and one `document_open`. It reported five frictions:

1. `tags_mode: all` with `["Cliente", "__tag_test_inesistente__"]` returned the `Cliente` document. The
   unknown tag had been dropped and the filter widened.
2. "Show me the documents tagged Cliente" went through ranking. With reviewed values only, the filter
   admitted one document, and the search returned none (`no_qualified_passage`).
3. Passages that were not relevant came back under `uncalibrated_floors`.
4. A hit did not show its own tags or their review state. The agent had to compare two searches to tell
   reviewed from suggested.
5. Tags could only be discovered through the closest names a wrong name returns.

Item 1 was a bug, fixed in `5e4abb227`. The operator decided items 2 and 4 the same day ("Query
facoltativa", "Sì, nomi e stato"). Item 5 is `library_lists`. Item 3 is spec §9, the relevance floors,
and outside this plan.

## Turn 2, on `489815bf7`: the agent's comparison

28 tool calls in 154 s, `RUN_FINISHED`.

| The agent's probe | Its result | The agent's verdict |
|---|---|---|
| `all` with `Cliente` and an unknown tag | zero documents, `filter_admits_nothing`; with and without a query, and with reviewed values only | resolved |
| `any` with `Cliente` and an unknown tag | the `Cliente` documents, the unknown name reported | correct |
| a list by tag alone, no `query` | `profile: field_list`, `matching_documents: 2`, no ranking | resolved |
| each document's tags and their state | in `document_fields`, with `reviewed` true or false | resolved |
| discovering the tags | `library_lists` shows `Cliente` on 2 documents | much improved |
| `Cliente` and `cliente` | the same results | confirmed |
| reviewed values only | `Financial_Sample.xlsx` alone | confirmed |
| `fattura` with tag `Cliente` | still `esempio.md` and `Financial_Sample.xlsx`, `abstained: false`, `uncalibrated_floors` | still present |

### The residual frictions it named

1. **No paging past `limit`.** A list of more than 50 matching documents cannot be walked: there is no
   `offset` and no cursor. The agent ranks this next.
2. **Three counts, three scopes, not labelled:**
   - `library_lists` counts the whole library, suggestions included.
   - `matching_documents` honours `include_unreviewed: false`.
   - `filters[].admitted_documents` ignores `document_ids`.

   None of them is wrong, but a reader can confuse them.
3. **No list of known ids.** `tags` with `document_ids` and no query is refused, as the error says. A
   known selection cannot be listed with its fields.
4. **The closest names are not selective.** `fatture`, `contratti` and a nonsense name all get `Cliente`,
   the only tag. Now that `library_lists` exists, the agent would separate "a similar name" from "the tags
   there are".

## Turns 3 and 4, on `ee662da70`: the residuals

Plan `docs/superpowers/plans/2026-10-23-document-search-residuals.md`, Task 3. The updater applied
`ee662da70` at 15:00Z; it carries the residuals' code (`d9948dd60`, `80163e712`).

- **Turn 3, 15:04Z to 15:06Z, the operator's own**, in the cockpit: "cosa ti mancherebbe per avere un
  sistema perfetto?". Before answering, the agent re-checked two residuals with three
  `document_search` calls:
  - `tags: [Cliente]`, `limit: 1`, `offset: 0` gave `Financial_Sample.xlsx`, `matching_documents: 2`
    and `next_offset: 1`; `offset: 1` gave `esempio.md` and no `next_offset`;
  - `document_ids: [<esempio.md>]` with no query listed it with its fields, `profile: field_list`.

  It withdrew both from its list. The rest of its answer is a wish list; see below.
- **Turn 4, 15:07Z to 15:08Z, driven through `POST /agent/run`**, asking it to retry the other two.
  7 `document_search` calls in 53 s, `RUN_FINISHED`.

| Residual | The agent's probe | Its result | The agent's verdict |
|---|---|---|---|
| paging past `limit` | `offset` 0 then 1 | `next_offset` 1, then the last page | resolved (turn 3) |
| listing known ids | `document_ids` alone | listed, with `document_fields` | resolved (turn 3) |
| count scopes | `Cliente` reviewed only; `Cliente` within one id | library 2 / filter 1 / selection 1; library 2 / filter 2 / selection 1 | resolved |
| closest names | `__tag_test_inesistente__`, `fatture`, `contratti`, `Clente` | `[]`, `[]`, `[]`, `["Cliente"]` | resolved |

On the counts, the agent notes that the scope is stated in the tool's description, not repeated as
a label beside each number, and that this answers its ambiguity.

### What it asked for next (turn 3)

Its priorities, in its own order:
1. **Relevance first.** "fattura" still returns the example Markdown: a weak semantic match should
   not become evidence, and "nothing relevant" is a normal answer. This is the floors plan.
2. **Editing metadata through the tools.** Assign and remove tags; correct title, type,
   counterparty, date and custom fields; confirm or reject a suggestion; batch edits; create, rename
   and merge tags or counterparties. With a preview, conflict checks and undo.
3. **Provenance of a classification.** Who set it (user, rule, import, AI), when, why, the evidence
   for an automatic one, and its correction history.
4. **Observable ingestion.** Each document's state, retrying a failure, re-indexing, duplicates,
   and telling original, version and attachment apart. It marks these as to verify, not as proven
   gaps.
5. **Verifiable answers.** From an answer to its document, passage and position; whole-sheet data
   for exact sums.
6. **Document-system guarantees.** Permissions on search and download, isolation between users,
   an audit of changes, trash and restore, export with metadata, and a real restore from backup.
   It says these were not tested.

None of these is decided. They are the agent's, recorded for the operator.

## What this does not show

- **No score.** The 25-question set was not run, so there is no number against the 9.8 gate.
  - The 23 questions that scored F1 1.00 on 2026-10-23 were not re-checked.
  - Nothing measures whether "filter by the listed name" makes the agent add a filter that drops
    untagged documents.
- **f20 and f21 were not exercised.** The operator's library has no `interno` or `bozza` tags. The case
  the plan exists for, a tag the question's words do not spell, was not tested on the VM.
  - It is covered by `TestDocumentSearchNamesTheLibraryLists` only.
- **Not two real tags.** With one distinct tag, `all` and `any` across two existing tags were not tried.
  The agent says so itself.
- **One model, one run.** `gpt-6.1-sol` only, and each turn once.
- **The residuals rest on one library.** Paging was walked over two documents, not past 50; the
  closest names were tried against a single tag.

## Gates

- Local:
  - `make quality` passed on `04181399c`;
  - the `docfields` and `arcadedb` `arcadedb_integration` tiers passed with nothing skipped (65 and 72
    tests);
  - `go test -race` passed on `arcadedb`, `docfields` and `agent/tools`.
- The local coverage run measured the aggregate at 87.4%. It found the `docfields` package ratio at
  410/1163, below its 392/1102 baseline.
  - Two daemon-free tests were added.
  - The baseline is pinned at 414/1163, the least that keeps the ratio.
- The pre-push gates passed on `489815bf7`. On GitHub, CodeQL, Skills, the edge image, the ArcadeDB MCP
  and the appliance were green.
  - The CI run's remaining jobs, among them the coverage gate, were cancelled by a newer push from a
    parallel session (`1dfddad8d`, ArcadeDB 26.10.2). That push descends from this one, and its CI run
    measures the same code.
