---
spike: 077
name: catalog-injection-recall
type: standard
validates: "Given a doc ingested earlier with NO attachment in the current turn, when a knowledge catalog is injected into context, then the real agent calls document_search (and does not over-trigger on generic turns)"
verdict: VALIDATED
related:
  - 075-image-ocr-searchable-chunks
  - internal/assets/context.go
  - internal/agent/llm_agent.go
  - internal/agent/tools/document_search.go
tags: [item-2, catalog, recall, document-search, agent-loop, tool-choice]
---

# Spike 077 — Catalog-Injection Recall

## What This Validates

**Item 2**: knowledge-catalog injection for the **no-attachment** recall case. `document_search`
is already always-visible, so the open question is purely *behavioural*: when the user asks about
a previously-ingested document **without re-attaching it**, does the agent know a relevant doc
**exists** to search? Hypothesis: injecting a knowledge-catalog block (the thread/identity's
indexed docs — `document_id` + filename + summary) makes the real agent call `document_search`,
without over-triggering on unrelated turns.

## How to Run

```bash
# WSL, stack up, OPENROUTER_API_KEY in ~/aura.env. PAID (real model, ~7 bounded turns).
wsl -d Ubuntu -e bash /mnt/d/Aura/.planning/spikes/077-catalog-injection-recall/run.sh
```

The harness ingests a G220 markdown doc (sync embed → real searchable Neo4j document), then drives
Aura's **real `LlmAgent` + real OpenRouter client** (`document_search` wired to live two-stage
retrieval; `tool_search`, `web_search`, `web_fetch`, `text_response` also registered so the model
has genuine alternatives) across three conditions, capturing which tools the model autonomously
calls. The catalog is injected the way the attachment block is — prepended to the user turn,
`messages[0]` untouched. Cleans up Neo4j on exit.

## Results (live, 2026-06-28, real DeepSeek agent)

| Condition | runs | document_search called | retrieved answer | tools observed |
|---|---|---|---|---|
| **catalog-doc** | 3 | **3/3** | **3/3** | `[document_search]` |
| **catalog-generic** (greeting) | 2 | **0/2** | — | `[]` |
| baseline-doc (no catalog) | 2 | **1/2** | 1/2 | one run → `[tool_search, web_search]` |

- **Recall lift (gated):** with the catalog, the agent called `document_search` in **3/3** runs and
  the retrieval returned the actual spec (`47`/`IP54`/`torque`) every time — reliable and correctly
  scoped to the user's own document.
- **No over-trigger (gated):** with the catalog present, a greeting (`"Ciao! Come stai oggi?"`)
  triggered **0/2** `document_search` calls — the catalog informs without coercing.
- **Baseline (context):** with **no** catalog the same doc question was a coin-flip (**1/2**), and
  the miss reached for **`web_search`** — exactly the failure the catalog fixes (the agent doesn't
  know the doc exists, so it guesses, sometimes searching the public web for the user's own file).

## Investigation Trail

- Confirmed `NewLlmAgent` pins `messages[0]` to the canonical system prompt and appends `UserTurns`;
  context (Agent.md/skills) injects via the Runner's `ContextBlock`/`AlwaysBlock` providers. The
  spike injects the catalog by prepending it to the user turn (the attachment-block pattern), which
  is sufficient to test the behavioural effect.
- Harness artifact: `web_search`/`web_fetch` were registered as zero-value structs to populate the
  manifest (so the model has alternatives). Their `Spec()` is zero-value-safe but their `Execute`
  panics on nil internals; the agent's `execute_batch` **recover** caught it and the turn completed
  — `recovered panic site=execute_batch` in the logs is this, not a retrieval/agent defect. It does
  not affect the tool-*choice* measurement (and the recovered call IS the baseline's wrong web reach).

## Verdict

**VALIDATED.** A knowledge-catalog block reliably makes the real agent call `document_search` for a
no-attachment document question (3/3), with zero over-triggering on generic turns (0/2), where the
no-catalog baseline is unreliable (1/2) and sometimes wrong-tools to the web. **Item 2 closes the
no-attachment recall gap.**

### Build notes (binding for the Item 2 plan)

- **Source the catalog from the assets store** (`assets.Service.ListForThread` → `document_id`,
  filename, summary), thread-scoped, plus identity-scoped promoted assets.
- **Inject via the cache-safe dynamic seam, NOT `messages[0]`.** Production must keep `messages[0]`
  byte-stable (prompt cache); the catalog is per-conversation and changes as docs are added, so it
  belongs in the dynamic context/tail-inject path (the `ContextBlock` provider / search-context
  tail), the same family of seams the attachment block and agent notes already use — never the
  static prefix.
- **Frame it as operator-pinned context** ("not a request, not untrusted tool output") and include
  the `document_id` so the agent scopes `document_search` — pairs with spike 075's finding that a
  native `document_id` **pre-filter** keeps the scoped seed from being crowded out.
- Keep the block compact (id + filename + 1-line summary per doc); summaries already exist on
  processed assets (`Asset.Summary`).
