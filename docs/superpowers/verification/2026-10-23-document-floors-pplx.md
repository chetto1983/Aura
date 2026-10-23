# Document relevance floors for pplx-embed on the lab VM, 2026-10-23

Plan `docs/superpowers/plans/2026-10-23-document-floors-calibration.md`, Task 2. Space
`es1-b604719dc5fc0ce4`: `openrouter perplexity/pplx-embed-v1-4b, 768d, recipe 1`, the lab VM's
embedding route for both families.

**Verdict.** The document floors for this space are a distance of **0.72** (unchanged) and a
relevance of **0.39** (operator's decision, the same day). On 29 questions over the operator's
8 documents, 0.39 keeps all 11 true top-1 matches and abstains on all 5 out-of-corpus
questions and on "fattura", the case the agent reported. The margin is thin: 0.0214 between
"fattura" and the worst true match. Memory was not measured.

## Protocol

- **Build.** `95514022d`, applied by the updater at 15:18Z; it carries
  `AURA_DOCUMENT_DENSE_MAX_DISTANCE_RATIO` and `AURA_DOCUMENT_MIN_RELEVANCE`.
- **Two passes per question**, each one `aura docs search "<q>" --limit 3` inside the `aura`
  container, set with `docker exec -e` on that process only. The daemon kept the table's floors.
  - Pass 1, both floors off: `AURA_DOCUMENT_MIN_RELEVANCE=0.0001`,
    `AURA_DOCUMENT_DENSE_MAX_DISTANCE_RATIO=2`.
  - Pass 2, relevance floor off alone, with the distance at 0.72.
- **The score.** The top-1 document's score is the higher of its two reranked cosines: the passage
  leg (`fused`) or the card leg. A document is admitted when either leg clears the floor, so that
  is the number the floor decides on.
- **The library.** The operator's own, as it stands: 8 documents, namely a PDF, a DOCX, a PPTX,
  two spreadsheets, a Markdown file, a video and an image. Nothing was uploaded or changed.
- **The questions.** Aura's agent wrote 24 of them, in the operator's conversation `01a168cf-…`, on
  the operator's request to collaborate ("se collaborate te ed Aura sistemate tutto"):
  - 12 the library answers, each with the right document. 8 are prose, 2 quote a value, and
    2 are bare file names, which Aura asked to keep apart.
  - 12 hard negatives. Each is close to a document's topic, and the document does not hold the
    answer.

  The 5 out-of-corpus questions of the 2026-09-09 calibration were added. The set quotes the
  operator's documents, so it is kept outside the repository; only its ids appear below.

## Pass 1: the bands

| Group | n | Top-1 score | Notes |
|---|---:|---|---|
| True matches, top-1 right | 11 | 0.4047 … 0.6523 | worst s02, a prose question on one article of the PDF; next 0.4624 |
| True match, top-1 wrong | 1 | 0.2564 (wrong document) | s04; the right document ranked third at 0.199 in pass 2 |
| Out of corpus | 5 | 0.2637 … 0.2961 | carbonara 0.2961, ulivi 0.2960, B12 0.2769, mondiale 0.2683, traghetti 0.2637 |
| Hard negatives | 12 | 0.2466 … 0.4916 | "fattura" 0.3833 on `esempio.md`, by its card |

The hard negatives that score high are the topically right document:
- an invoice number for the laptops → the laptop spreadsheet, 0.4807;
- a LaTeX formula in Markdown → the Markdown file, 0.4852;
- removing DRM with calibre → the calibre document, 0.4916.

Returning these is correct retrieval: the document is relevant, and saying that the value is
absent is the reader's job. No floor can separate "about the topic" from "holds the answer", and
none is asked to.

"fattura" is the one hard negative that is not about its document's topic. `esempio.md` is a
Markdown example, and its card scores 0.3833 against the single word, above its passage (0.3616).

## Pass 2: the distance

With the distance at 0.72, the top-1 document and its score were the same for all 12 answerable
questions. One leg changed:
- **The `SampleShow.pptx` file-name question.** Its passage left the distance, and its card held
  at 0.6523.
- **"vitamina B12".** It abstained on the distance alone.

The distance drops no true match in this library, so it stays at 0.72.

## The floor

| Relevance floor | True top-1 kept | Out of corpus abstained | "fattura" | Hard negatives admitted |
|---|---:|---:|---|---:|
| 0.32 (EmbeddingGemma's, in force) | 11/11 | 5/5 | admitted | 9/12 |
| 0.35 (midpoint of true and out-of-corpus) | 11/11 | 5/5 | admitted | 8/12 |
| **0.39** (midpoint of true and "fattura") | **11/11** | **5/5** | **abstains** | **7/12** |

The operator chose 0.39 over 0.35.
- **What it adds.** "fattura" abstains, with no measured true match lost. It is the only
  measured question between 0.35 and 0.39.
- **What it costs.** Any document between 0.35 and 0.39 is no longer admitted; none was
  measured there but "fattura". Raising the floor from today's 0.32 costs one topically
  relevant document at either floor: the VAT question went to the sales spreadsheet at 0.3312.
- **The midpoint.** (0.3833 + 0.4047) / 2 = 0.394, rounded down toward recall.

## What this does not show

- **A claim beyond this set.** 29 questions, 8 documents, one day, one run, top-1 only, one
  embedder.
- **A wide margin.** The band that decides 0.39 is 0.3833..0.4047. It rests on one
  single-word query and one prose question.
- **Short queries.** On this model a single word scores high against a card. EmbeddingGemma's
  identifiers ran low; that is the opposite direction, and one sample does not establish it.
- **Independent questions.** The agent that wrote them could read the documents, so the
  positives may be easier than an operator's.
- **Memory.** Not measured here; its floors for this space stay uncalibrated.
- **Exact phrases.** s04 names a font that appears verbatim in the DOCX, and the passage
  holding it reranks to 0.199. The cosine rerank buries an exact match. No floor fixes that: it
  is ranking, and it is the agent's first request ("esattezza prima, AI dopo").
