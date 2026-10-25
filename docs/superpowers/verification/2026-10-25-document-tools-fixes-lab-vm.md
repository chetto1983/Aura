# Document floors and the agent's tool frictions, checked on the lab VM, 2026-10-25

Plan `docs/superpowers/plans/2026-10-23-document-floors-calibration.md`, Task 3, and the tool
frictions Aura's agent hit while writing the calibration questions (2026-10-23, the operator: "i
bachi sono nostri").

**Verdict.** All six checks pass:
- "fattura" abstains, alone and with the tag `Cliente`;
- a bare listing returns the whole library, 8 documents;
- Office files open with their real media type;
- the memory skill arrives whole;
- a pdfcraft document stays open across a two-minute pause;
- the transient memory fact is corrected.

The pdfcraft loss of 2026-10-23 did not reproduce.

## Protocol

- **Build.** `e1e3d0dff` (v1.2.0), applied by the updater after the VM's boot on 2026-10-25. It
  carries:
  - the per-family floors table and pplx-embed's documents row, `1b70f7ffa`;
  - the review's fixes, `52da38196`;
  - the whole-library listing, `f7b4a432a`;
  - the media type by extension, `f8cf7ca55`;
  - whole skill bodies, `ea9c5969a`.

  CI was green on `ea9c5969a`.
- **The turn.** One turn of the operator's conversation `01a168cf-…`, driven through
  `POST /agent/run` after the operator said the update had finished. It ran in 195 s with
  14 tool calls and ended `RUN_FINISHED`. The agent was asked to re-check each friction with its
  own tools and to change no document.

## Results

| # | Friction (2026-10-23) | The agent's probe | Result |
|---|---|---|---|
| 1 | "fattura" admitted an unrelated Markdown example at 0.36-0.38 | `document_search` `fattura`, then with `tags: [Cliente]` | `abstained: true`, `no_qualified_passage`, no documents. With the tag, the filter admits 2 and neither clears the floor. |
| 2 | no way to list the library; 2 of 8 documents missed | `document_search` with `limit: 50` only | `profile: field_list`, `matching_documents: 8`, all 8 listed, no `next_offset`, the video and the image included |
| 3 | `application/octet-stream` for Office files | `document_open` on `Financial_Sample.xlsx` | `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet` |
| 4 | `memory-aura` (30939 bytes) truncated at 30000 | `skill use memory-aura` | whole, through its last section, no truncation |
| 5 | pdfcraft "no open document with id 1" about 90 s after opening | `pdfcraft__doc_open`, `text_extract` page 3, `shell_exec sleep 120`, then `text_find` "sovranità" on the same `doc` | the document was still open: 2 matches, pages 4 and 6, `complete: true` |
| 6 | a memory fact saying the floors must be recalibrated | `memory_recall` on the fact | already superseded in an earlier turn: documents calibrated at 0.39 on pplx-embed, memory not |

## What this does not show

- **The calibration's quality.** Item 1 shows abstention on two queries. The agent says so itself,
  and notes that a search result does not expose the floor applied. The bands are in
  `2026-10-23-document-floors-pplx.md`.
- **The cause of the 2026-10-23 pdfcraft loss.** It did not reproduce on a quiet VM over 120 s.
  The original loss fell between 15:13Z and 15:16Z, while a parallel session was restarting
  ArcadeDB and `aura-arcadedb-mcp` on the VM. Its logs went with the container the update
  replaced at 15:18Z. A restarted pdfcraft process would explain it, but nothing here proves it.
- **Exact phrases.** A font named verbatim in the DOCX still reranks to 0.199. That is the agent's
  first request, "esattezza prima, AI dopo", and it needs a design of its own.
- **One run.** One model (`gpt-6.1-sol`), one turn.
