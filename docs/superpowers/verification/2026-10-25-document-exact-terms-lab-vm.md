# Exact terms on the lab VM, 2026-10-25

Plan `docs/superpowers/plans/2026-10-25-document-named-terms.md`, Task 6, Steps 4 and 5. Spec
`docs/superpowers/specs/2026-10-25-document-exactness-and-judge-design.md`, decision 1 and acceptance 1.

**Verdict: acceptance 1 passes.** The complex-corpus measurement was waived by the operator ("basta
aura e te"): Aura's agent and Claude validate on this library.
- s04 with the name quoted, and the bare `"Ubuntu Mono"`, return `calibre_demo.docx` first on the
  exact evidence, where the floor used to drop it.
- Nothing that abstained before admits now: the five out-of-corpus questions and "fattura" still
  abstain, and the hard negatives admitted are 7 of 12, the count of 2026-10-23.
- Aura's agent, asked to find the font without being told how, quoted the name on its first call.
- A search with 16 identifiers costs what a search with none does.
- `complex_qrels.json` was not run. The complex identity no longer exists, neither on the
  workstation nor on the VM, seeding it needs the whole ingest pipeline, and the operator waived it.

## Protocol

- **Build.** `3ed81a0b2`, the aura image's revision label. The updater applied it at 17:51Z. It
  carries slice 1 (`d92a5b921`…`4614dc874`) and the review's fixes (`11998bdb5`…`a062464f8`).
- **The library.** The operator's own, unchanged since 2026-10-23: 8 documents. The space is
  `es1-b604719dc5fc0ce4`, pplx-embed-v1-4b at 768 dimensions.
- **Each question** is one `aura docs search "<q>" --limit 8` inside the `aura` container, with the
  production floors (relevance 0.39, distance 0.72) and no override.
- **The questions** are the 29 of `2026-10-23-document-floors-pplx.md` plus 11 for this slice. The
  set quotes the operator's documents and stays outside the repository; only ids and the slice's own
  questions appear here.

## The floors set, with exact terms

| Group | n | Result | 2026-10-23 at 0.39 |
|---|---:|---|---|
| Answerable, top-1 right | 11 of 12 | unchanged; s04 abstains | 11 of 12, s04 wrong |
| Out of corpus | 5 | 5 abstain | 5 abstain |
| "fattura" | 1 | abstains | abstains |
| Other hard negatives admitted | 7 of 12 | h02–h05, h10–h12 | 7 of 12 |

Four answerable questions now carry exact evidence besides their legs:
- s03, by the identifier `AZW3`;
- s08, by its quoted slide title;
- i01 and i02, by their file names.

Their top-1 did not change. o05, "vitamina B12", holds the identifier `b12`, which no document
carries, and it still abstains.

## The slice's questions

| Id | Query | Result |
|---|---|---|
| a01 | s04 with `"Ubuntu Mono"` quoted | `calibre_demo.docx` first, exact evidence alone, score 0 |
| a02 | `"Ubuntu Mono"` | `calibre_demo.docx` first, exact evidence alone |
| a03 | `Ubuntu Mono`, unquoted | abstains: the burial, as designed |
| a04 | `apri calibre_demo.docx` | `calibre_demo.docx` first, exact evidence beside its legs (review C1) |
| q01 | `"Aura"` | 3 documents, each naming Aura in its own text; no unread file comes back for its caveat |
| x01 | `quanto costa la carta A4 su Amazon?` | `esempio_excel.xlsx` at 0.3930, by its card and passage legs |
| x02 | `quali sono i risultati del Q3 2026 di Apple?` | abstains |
| x03 | `farmacia aperta 24h a Cuneo` | abstains |
| x04 | `quali sono i sintomi del COVID-19?` | abstains |
| e01 | `"Italia è una Repubblica democratica"` | the Constitution first, by its legs, with no exact evidence |
| e02 | the same, unquoted | the Constitution first, by its legs |

- **q01.** The three are `esempio.md` ("creato da Aura"), `SampleShow.pptx` ("collaudo Aura") and
  `Financial_Sample.xlsx`, whose first row passage ends "Collaudo Aura 2026-10-20".
- **x01, review I4.** `A4` is in no document, so the exact lane admitted nothing. The spreadsheet
  cleared the floor on its own cosine, 0.0030 above it. That is the floor's thin margin, not the
  exact rule. No document carries `Q3`, `24h` or `COVID-19` either, so this library cannot show
  whether an everyday alphanumeric it does hold would admit a document that does not answer.
- **e01, review I5.** The passage says "L’Italia è una Repubblica democratica", with a typographic
  apostrophe. Go's check splits on it and would accept the text, so the engine is what misses it:
  StandardAnalyzer keeps "l’italia" one token. The document still ranks first, by its legs. A
  quoted name behind an elided article loses the floor bypass, not its place.

## The cost of a search with exact terms (Step 4)

Wall time of the whole `docker exec … aura docs search`, query embedding included. The identifiers
`ZQ1001`… are held by no document. Three interleaved runs:

| Identifiers | Runs (ms) | Median (ms) |
|---:|---|---:|
| 0 | 902, 792, 901 | 901 |
| 1 | 848, 804, 823 | 823 |
| 4 | 923, 869, 792 | 869 |
| 16 | 1167, 904, 828 | 904 |

The lane is 3 full-text queries whatever the number of terms (review I6), and its cost is inside
the run-to-run spread. No cap on the number of terms is proposed.

## The agent (the operator's validation form)

One turn in the operator's conversation `01a168cf-…`, through the cockpit route, with the
operator's account. It asked Aura to find the document naming the font Ubuntu Mono, then vitamin
B12, then "fattura", and to report friction. The message used no quotation marks.

- **Its first call was `document_search {"query": "\"Ubuntu Mono\""}`**, quoted, as the tool's
  description teaches. It returned `calibre_demo.docx` on the exact evidence. Aura checked the
  sentence in the passage.
- **B12 and "fattura" abstained.**
- **It then tried the forms a person would type.** `Ubuntu Mono` and "Quale documento nomina il font
  Ubuntu Mono?" abstain; the same question with the name quoted finds the file.
- **The friction it reported:**
  - "Devo riconoscere io il nome composto e aggiungere le virgolette": without them the abstention
    looks the same as B12's. That is decision 1 as amended (structured signals only, the agent
    picks the words). The message's "the names you write now count" overstated it.
  - A document only the exact lane found has `score` 0 while its exact evidence carries the BM25,
    8.95; "zero" can read as irrelevant to a caller that does not read the evidence. Decision 1
    sets no score on purpose. Saying so in the tool's description is the operator's call.

## The frictions fixed, checked by Aura

`98ab451a4` ("fix(documents): say which terms a search checked word for word") adds `exact_terms`
to the response, absent when no term was checked, and says in `document_search`'s description what
an absent list and a score of 0 mean. The operator applied it by hand at 18:23Z. From the CLI:
`Ubuntu Mono` and `fattura` abstain with no `exact_terms`, `"vitamina B12"`'s abstention shows
`["b12"]`, and `"Ubuntu Mono"` shows `["Ubuntu Mono"]` and finds the file.

A second turn in the same conversation asked Aura to repeat its checks, the unquoted forms included.
- **The diagnostic friction is gone.** In Aura's words: "adesso l'assenza di `exact_terms` … mi dice
  che il nome non è stato controllato alla lettera … devo riprovare quotando il nome".
- **The score is understood:** "Non lo considero più un'incoerenza né un attrito irrisolto."
- **B12 and "fattura" still abstain.**
- **One friction remains, operational:** a compound name written without quotes still abstains,
  and Aura has to spot the name and call again with quotes. That is decision 1's boundary: only
  structured signals make an exact term. In the first turn Aura quoted the name on its own; this
  turn asked it for the unquoted forms on purpose.

## What this does not show

- **`complex_qrels.json` before and after.** Not run, waived by the operator. The bench needs the complex identity, which
  the workstation's stack no longer holds (its ArcadeDB has only `aura_memory`) and the VM never
  held. Seeding it means rebuilding the ingest pipeline on the workstation (aura-ingest image,
  Garage, CocoIndex state, EmbeddingGemma). The bench's "exact terms ignored" arm gives the
  before-and-after in one run once the identity exists.
- **The lane's pool (review I3).** 8 documents never fill it.
- **Memory on the VM.** Recall and the fact search were measured only by the live tests on the
  workstation's ArcadeDB 26.10.2.
- **I4 on a library that holds an everyday alphanumeric.** See x01.
- **Timing beyond this library.** One library of 8 documents, on a VM sharing the workstation's
  CPU.
- **More than one agent turn.** One turn, one model route.
