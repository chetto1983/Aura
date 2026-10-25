# Exact terms and decision-model judges on the floors set, lab VM, 2026-10-25

A spike for the agent's first request, "esattezza prima, AI dopo"
(`2026-10-23-document-search-lists-lab-vm.md`, turn 3). It measures three things on the candidates
the shipped fused query returns:
- why a font named verbatim in a DOCX is buried;
- whether a rule on the terms a question names would surface it without breaking abstention;
- whether a decision model, asked whether each document holds what the question asks, separates
  answers from non-answers better than the reranked cosine.

The operator named the models: `typesafe/jev-1.13` ("prova anche"), `typesafe/jev-1.14` and
`liquid/d1-ultra`.

**Verdict: PARTIAL.**
- All three judges put the right document first for 12 of the 12 answerable questions, against the
  cosine's 11. The cosine misses s04.
- The widest gap is jev-1.14's. Its worst true match scores 0.94 and its best negative 0.28, a
  margin of 0.66. The cosine's margin was 0.0214.
- The named-term rule admits only the gold document wherever it fires (10 of 12) and no document
  for any out-of-corpus question.
- It is PARTIAL because this is one library, one run, and questions written by an agent that had
  read the documents.

## Protocol

- **Build.** `e1e3d0dff` (v1.2.0) on the lab VM. The space is `es1-b604719dc5fc0ce4`,
  pplx-embed-v1-4b at 768 dimensions.
- **Candidates.** For each of the 29 questions of `2026-10-23-document-floors-pplx.md`, one
  `aura docs search "<q>" --limit 8` ran inside the `aura` container with both floors off:
  `AURA_DOCUMENT_MIN_RELEVANCE=0.0001` and `AURA_DOCUMENT_DENSE_MAX_DISTANCE_RATIO=2`, set with
  `docker exec -e` on that process only.
  - The library has 8 documents, so every question returned all 8: 232 (question, document) pairs.
  - The cosine ordering reproduces the 2026-10-23 bands: 11 of 12 answerable top-1, s04 wrong.
  - The set quotes the operator's documents and stays outside the repository, as before; only its
    ids appear here.
- **Judges.** OpenRouter's Decisions API, `POST https://openrouter.ai/api/alpha/decisions`
  (`openrouter.ai/docs/guides/community/jev-tutorial.md`; the request and answer schema is
  TypeSafe's, `docs.typesafe.ai/api/system-one.md`).
  - The snapshots that answered were `typesafe/jev-1.13-20260917`, `typesafe/jev-1.14-20261014` and
    `liquid/d1-ultra-20261015`.
  - The key was the operator's own OpenRouter key; the cockpit route was not used.
- **One request per pair.**
  - `state`: `{query, document: {title, card, passages}}`, with at most the document's first
    three returned passages.
  - Two `noul` questions, verbatim:
    - `answers`: "Does `document` contain the information that answers `query`?"
      - true: "The document's text states the fact, value, name or passage that `query` asks for.
        A term that `query` quotes verbatim appears in the document."
      - false: "The document does not state what `query` asks for, even if it is about a related
        subject."
    - `topical`: "Is `document` about the subject that `query` asks about?"
  - Four requests ran in parallel.
  - A document's judge score is its `answers` probability. The top-1 is the highest.
- **Named terms**, computed over the same candidate texts:
  - the candidates: quoted strings, file names (`name.ext`), tokens holding a letter and a digit,
    and runs of capitalised words not at the start of the sentence;
  - a document carries a term when its title, card or returned passages contain it as a
    case-insensitive whole-word sequence;
  - lowercase words are never named terms, however rare.

## The burial

| Query | `calibre_demo.docx` rank | Its passage's reranked cosine | Top-1 |
|---|---:|---:|---|
| s04, the Italian question naming "Ubuntu Mono" | 4 of 8 | 0.199 (card 0.228) | `SampleShow.pptx` 0.256 |
| `Ubuntu Mono`, the bare term | 6 of 8 | 0.146 (card 0.201) | `esempio.md` 0.252 |

- The document is the only one that contains "Ubuntu Mono". Its passage is one chunk of 5,037
  characters, and the term is one line inside it.
- `vector.rerank` re-scores the fused candidates by cosine alone. Read from
  `SQLFunctionVectorRerank.java`: it emits one `score`, the cosine.
- `vector.fuse` keeps no per-source provenance. Read from `SQLFunctionVectorFuse.java`: one fused
  `score`.
- So the lexical leg's hit is invisible after the rerank. The relevance floor (0.39 for this
  space) then drops it inside the statement, for both queries.

**A rule on rare words fails.** Treating a query word that only one candidate carries as exact
admits `Costituzione_Italiana.pdf` for o01 and o03: "della" and "degli" occur in no other
document. That is the 2026-08-06 finding again: an exact leg that matches anything breaks
abstention.

## The judges

| | Cosine (floor 0.39) | jev-1.13 | **jev-1.14** | d1-ultra |
|---|---:|---:|---:|---:|
| answerable, right top-1 | 11/12 | 12/12 | **12/12** | 12/12 |
| worst true top-1 | 0.4047 | 0.78 | **0.94** | 0.70 |
| best negative, 12 hard and 5 out of corpus | 0.3833 | 0.29 | 0.28 | **0.13** |
| margin | 0.0214 | 0.49 | **0.66** | 0.57 |
| median / p90 per pair | — | 308 / 373 ms | 311 / 367 ms | 378 / 477 ms |
| input tokens, cost of the set | — | 335,601, $0.0141 | 335,601, $0.0141 | 261,514, $0.0110 |

- **s04.** jev-1.14 scores `calibre_demo.docx` at 0.96 and every other document at 0.06 or lower.
  jev-1.13 gives 0.96 and d1-ultra 0.976.
- **"fattura" (h01).** jev-1.13 puts the sales spreadsheet top at 0.29 and d1-ultra the PDF at
  0.098. The cosine admitted the Markdown example at 0.3833.
- **Topical but not answering.** On h05, the sales of one product in one month, jev-1.14 scores
  `Financial_Sample.xlsx` 0.50 on `topical` and 0.11 on `answers`; jev-1.13 gives 0.65 and 0.12.
  The second question tells the topical document apart from the one that answers.
- **Bare file names** were the least certain answers. For jev-1.13, i02 is 0.78 and i01 0.90; for
  d1-ultra, i01 is 0.70. jev-1.14 scores both 0.96 or higher.
- **One threshold.** A single threshold of 0.5 on `answers` separates every true top-1 from every
  negative, for all three judges. It was not tuned, and it is in-sample for this spike.

## Named terms

| Group | Fired | Carriers |
|---|---:|---|
| answerable (12) | 10 | the gold document alone, each time |
| answerable, not fired | 2 | s02 names nothing; s08's quoted phrase does not match the extracted text verbatim |
| out of corpus (5) | 2 ("Sardegna", "B12") | none: all 5 still abstain |
| hard negatives (12) | 7 | h03 and h05 → the topical spreadsheet; h12 "Aura" → all 8 documents; h06, h07, h10 and h11 → none |

- **h12** names "Aura". Every card carries the sentence Aura writes at ingest ("Written by Aura at
  ingest from the file itself"), so every document carries the term. Two documents carry it in
  their own content: `SampleShow.pptx` and `esempio.md`.
- That shows two things:
  - a name that every document carries selects nothing;
  - text Aura wrote is not the document's content.

## What this does not show

- **A claim beyond this set.** 8 documents, 29 questions, one run per judge, top-1 only.
- **Independent questions.** Aura's agent wrote 24 of the questions after reading the documents, so
  the positives are probably easier than an operator's.
- **Out-of-sample thresholds.** The 0.5 above is read off this set.
- **Spreadsheets.** A judge sees only the passages returned. For a workbook those are one row
  each, and a question about another row would read "no" even though the file answers it.
- **Memory.** Not measured: no fact or turn was judged.
- **A library larger than 8 documents.** Every document was a candidate here. In a larger one, the
  rerank's candidate cut decides what a judge ever sees.
- **The named-term rule beyond Italian question syntax.** Capitalisation was read from Italian
  sentences only.
