---
spike: 073
name: fc270m-dataset-from-registry
type: standard
validates: "Given Aura's real tool registry + grounded traces, when a generator builds a FunctionGemma-format training JSONL, then every tool (incl. the mail gravity-well) is represented, schema-valid, with a held-out split, ready to drop into the official Unsloth FunctionGemma-270M Colab notebook"
verdict: VALIDATED
related: [071-fc270m-baseline-and-slot, 054-semantic-toolsearch-vs-bm25, 057-toolselection-oracle-signal]
tags: [functiongemma, dataset, finetune, tool-calling, slice-13, colab]
---

# Spike 073: FunctionGemma training dataset from Aura's registry

## What This Validates

Can we mechanically turn Aura's real tool registry into a quality FunctionGemma
SFT dataset that (a) drops into the official Unsloth notebook with a one-cell swap,
(b) covers every capability, (c) is schema-valid, and (d) directly targets the
spike-071 failure modes (Italian refusal, bitcoin→mail gravity well, fs_glob→fs_read)?

## Research

- **Target format = the official Unsloth notebook** (operator-chosen):
  `FunctionGemma_(270M).ipynb`. It formats each row with
  `tokenizer.apply_chat_template(row["messages"], tools=row["tools"],
  add_generation_prompt=False, tokenize=False)` → a `text` field, then
  `train_on_responses_only(instruction_part="<start_of_turn>user\n",
  response_part="<start_of_turn>model\n")` masks loss to the model turn only.
- **Therefore each row = HF `{messages, tools}`.** No developer message in
  `messages` — the FunctionGemma template injects the `developer` preamble +
  `<start_function_declaration>` block from `tools=`. The assistant turn carries
  `tool_calls[].function.arguments` as an **object** (the recipe's `function: call`
  dict shape).
- **Operator decisions:** FULL tool catalog in every example (teach
  selection-under-distractors); Italian-primary ~70/30.

## How to Run

```bash
go run ./.planning/spikes/073-fc270m-dataset-from-registry
# → train.jsonl, eval.jsonl, COVERAGE.md  (deterministic, seed 3407)
```

## What to Expect

`train.jsonl` (101) + `eval.jsonl` (33), 21-tool catalog in every row, IT/EN ≈ 76/24.
See `COVERAGE.md` for per-tool counts.

## Investigation Trail

1. **Native schemas pulled from the REAL registry** (`internal/agent/tools`,
   zero-value `Spec()` — spike 054 convention) so descriptions + JSON-schema params
   are the production ones, not invented. MCP namespaces (mail/whatsapp/calendar/
   memory) come from a static catalog mirroring the live surfaces (spikes 054/063).
2. **Schema-validity by construction:** every template's gold arguments satisfy the
   tool's `required` fields (verified the real keys: web_search→query, web_fetch→url,
   fs_read→path, fs_glob/fs_grep→pattern, shell_exec→command, fs_edit→path+old+new,
   mail__send_email→to+body, …).
3. **First gen = 115 examples**, but the thin-tail tools (calendar/memory/whatsapp/
   mail-mark/delete) had only ~1 training example after the stratified split — too
   thin to learn. Boosted their slot lists → **134 examples, every tool ≥3** (≥2
   train). web_search is intentionally the largest class (23) — it is the
   gravity-well counterweight.
4. **Failure-mode targeting:** web_search owns bitcoin/price/meteo/news (NOT mail);
   document_search vs web_fetch are split by "my documents" vs a URL; fs_glob
   (find-by-pattern) is kept distinct from fs_read (read-one-file); mail/whatsapp/
   calendar siblings co-occur as distractors to teach namespace disambiguation.
5. **A few no-tool chat rows** (greetings → assistant text, no `tool_calls`) curb
   the over-firing the model would otherwise learn from an all-positive set.

## Results

**VALIDATED ✓** — a deterministic generator produces a notebook-ready, schema-valid,
fully-covering FunctionGemma SFT dataset from Aura's real registry.

| Metric | Value |
|---|---|
| Total examples | 134 (train 101 / eval 33) |
| Catalog (tools/example) | 21 (full catalog every row) |
| Language mix | it 102 (76%) / en 32 (24%) |
| Tools covered | 21/21, each ≥3 examples |
| Format | HF `{messages, tools}`, arguments as object — drops into the Unsloth notebook |
| Hard cases | bitcoin/price→web_search; doc vs web_fetch; fs_glob vs fs_read; mail/whatsapp/calendar namespace siblings as distractors |

Verified the emitted JSON visually: `messages` = user + assistant-with-`tool_calls`
(no content on the call turn), `tools` = the 21-tool catalog with real schemas.

## Signal for the Build

- **Drop-in for Colab** (spike 072): swap the notebook's dataset cell to load these
  two JSONLs and format with `apply_chat_template(..., tools=row["tools"])`.
- **The slot lists are the scale lever.** 134 is a first-finetune size; adding slot
  values multiplies examples with zero new code — re-run after the first eval (074)
  shows which tools still miss.
- **Regenerate when the registry changes** — native schemas are pulled live, so a
  new/renamed tool flows in automatically; the MCP catalog is the one hand-maintained
  part (keep it in sync with the live mounts).
- **This is the dataset the whole idea rests on** — base FunctionGemma's machinery
  is sound (071), so closing the gap is a data problem, and this is the data.
