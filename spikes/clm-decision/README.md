# CLM as Aura's decision model — spike

Question: can a Contrastive Language Model ([Contrastive-LM/CLM](https://github.com/Contrastive-LM/CLM),
Apache-2.0) make Aura's two per-turn decisions better than what ships today?

- **Effort.** Pick none/low/high for a user turn. Today it uses the seed bank (3 nearest per tier), plus
  the router teacher in the turn recall design (docs/superpowers/specs/2026-10-06-turn-recall-design.md).
- **Tools.** Rank the deferred tools for a request. Today this is BM25; a dense leg is planned.

CLM is a frozen LLM encoder (Qwen3-8B, last-token pooling, 4096-d) plus two 20M-parameter projection
heads trained contrastively. A typed choice is the state against each option's text, scored by a
softmax over scaled cosines. Only the Qwen3-8B head is published (`Contrastive-LM/CLM-v0.1-8B`), so
a smaller encoder needs a head of its own. The operator chose to measure the published model zero-shot
first. Stage 2, a head for a small encoder, runs only if stage 1 wins.

## Stage 1 — zero-shot CLM-8B (no training)

Runs on Modal, on one L4. vLLM 0.31.0 serves `Qwen/Qwen3-8B` with `--runner pooling`, as CLM's own
`serve_qwen3_8b.sh` does, and `contrastive-lm` 0.1.0 provides the reference head.

`data/` was exported from the repo on 2026-10-07 by throwaway tests that were not committed:

| File | Content | Source |
|---|---|---|
| `effort_gate.json` | 58 prompts with their accepted tiers (47 strict) | `liveCorpus`, `internal/agent/prompt/reasoning_classifier_live_test.go` |
| `tiers.json` | tier definitions and seeds | `reasoningTierDefs` / `reasoningTierSeeds` |
| `tools.json` | 85 deferred tools (name, summary, `searchDocument`), gate 26, held-out 24, blind 60 (30 EN + 30 IT) | `deferred_manifest.json`, `gateCases`, `heldOutCases`, `blind_eval_2026-10-06.json` |

### Pre-registered variants (fixed before any run)

Effort uses the router's own instruction, "Classify the latest user request into one reasoning tier
for the next assistant turn.", as the question.

| Id | Options the action head sees |
|---|---|
| E1 | `reasoningTierDefs`, the Italian definitions behind the seed bank |
| E2 | the router prompt's tier descriptions (English), verbatim |
| E3 | every definition and seed as its own option; a tier scores the mean log-probability of its 3 best options (the shipped classifier's rule, in CLM space) |
| E1-raw, E2-raw | E1 and E2 through `clm-raw` (cosine in the encoder space, no head) |

Each effort variant is reported with and without the production greeting fast path
(`trivialGreetings`). Tools use the question "Which tool should handle this request?".

| Id | Candidate text per tool |
|---|---|
| T-A | `searchDocument(spec)`, BM25's retrieval document, as in the dense-leg measurement |
| T-B | `name: summary` |
| T-A-raw | T-A through `clm-raw` |

### Metrics and baselines (reported 2026-10-06, see the spec's "What was measured")

| Measure | Baseline |
|---|---|
| Effort accuracy over 58, against accepted tiers | seeds 54/58, teacher 55/58, seeds + teacher below margin 58/58 |
| none-vs-rest over 58 (the Go gate's rule) | gate floor in the test |
| Effort accuracy over the VM traffic group (last 13 cases) | seeds 14/15 on the 15-case variant of that group |
| Tool top-1 / recall@5 per set | BM25: gate 26/26, held-out 12/19, blind EN 18/23, blind IT 19/19; RRF: 26/26, 17/22, 21/30, 21/28 |

A sanity check reproduces the README's example before any measurement counts. Latency is measured
warm on the GPU and is not representative of the appliance's CPU.

Decision rule: stage 2 is worth proposing only if an effort variant beats seeds alone (54/58) without
new hard-to-`none` errors, or a tool variant beats RRF on the held-out and blind sets without losing
the gate. These sets are regression data, already inspected, so a win here is a reason to build a
frozen independent set, not a release result.

Run: `modal run spikes/clm-decision/modal_clm.py` (writes `results/stage1.json`).

## Stage 1b — the effort question framed as an intent and actions (registered before its run)

Stage 1 asked CLM to classify the request, with category descriptions as options. CLM is trained
on state → the action taken, so the operator asked whether it was given the intent of what to do.
Registered 2026-10-07, after stage 1 and before this run. It runs once, and its variants are not
iterated on these sets.

The state is the user message. The question states the assistant's intent. Each option is the
action the assistant takes, rewritten from `reasoningTierDefs` and the router descriptions with
nothing added.

| Id | Question | Options |
|---|---|---|
| A1 (EN) | "You are Aura, a personal assistant. Before you reply to this message, decide how much you need to think." | none: "Reply right away, briefly: a greeting, a thank-you, a stable fact you already know, a small calculation or a short translation." · low: "Look up current information that changes over time (weather, news, prices, opening hours, timetables, traffic, sports results) or use a tool for a small task, then reply." · high: "Think it through step by step before replying: write or debug code, design a schema or system, prove something, optimise an algorithm, scrape, or analyse in several steps." |
| A2 (IT) | "Sei Aura, un'assistente personale. Prima di rispondere a questo messaggio, decidi quanto devi ragionare." | none: "Rispondi subito e in breve: un saluto, un ringraziamento, un fatto stabile che conosci già, un piccolo calcolo o una traduzione breve." · low: "Cerca un'informazione corrente che cambia nel tempo (meteo, notizie, prezzi, orari di apertura, orari dei mezzi, traffico, risultati sportivi) oppure usa uno strumento per un compito piccolo, poi rispondi." · high: "Ragiona passo per passo prima di rispondere: scrivi o correggi codice, progetta uno schema o un sistema, dimostra qualcosa, ottimizza un algoritmo, fai scraping o analizza in più passaggi." |

Same metrics and decision rule as stage 1. A win here would be measured on an already-inspected set,
so it would justify a fresh frozen set, not a conclusion.

Run: `modal run spikes/clm-decision/modal_clm.py::framing` (writes `results/stage1b.json`).
