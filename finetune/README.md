# Aura × FunctionGemma — multi-turn tool-calling fine-tune (spike)

A self-contained pipeline that teaches Google's **FunctionGemma-270M** to drive
**Aura's exact tool-calling protocol**, reproducing the
[distil labs result](https://www.distillabs.ai/blog/making-functiongemma-work-multi-turn-tool-calling-at-270m-parameters/):
a 270M model that goes from **10–39% → 90–97%** multi-turn tool-call equivalence
after task-specific SFT, matching a ~120B teacher at 445× smaller (~288 MB
quantized, ~10–50 ms/call, runs on a single CPU core).

> **Status: spike.** Goal is to *measure* whether a 270M model can learn Aura's
> tool surface before committing architecture. No production Go is touched. The
> only Go here is a read-only spec exporter. If the numbers land, the natural
> home is **Slice 13 (local LLM fallback)** — see *Next steps*.

## Why this fits Aura

Aura's tool layer is OpenAI-style function calling (`internal/llm/client.go`)
with one twist that makes a tiny specialist *especially* attractive: the
**deferred-tool + `tool_search`** pattern. On a fresh turn the model sees only
`name + summary` for deferred tools; to use one it must first call
`tool_search({"query":"select:Name1,Name2"})` to load the schema, then call the
tool. That is a narrow, highly-structured behavior — exactly what a 270M model
can be drilled on, and exactly where the stock model fails.

## The methodology (what the blog does, applied here)

1. **Export the real tool surface.** `go run ./finetune/exporter` dumps every
   in-tree tool's spec (the same registry `cmd/aura/main.go` wires) to
   `data/aura_tools.json`. Single source of truth — no hand-maintained copy.
2. **Build a hybrid dataset.**
   - *Synthetic (teacher distillation):* DeepSeek-V4 (Aura's production model)
     writes multi-turn traces over the real tools, including the
     deferred→`tool_search`→call dance. Every trace is **verified** (tool names
     exist, arguments validate against the JSON schema) before it's kept.
   - *Real:* harvest actual traces from Aura's Postgres
     (`aura.conversation_turns`), read-only — the realistic half.
3. **SFT the student.** LoRA on `unsloth/functiongemma-270m-it` via Unsloth +
   TRL `SFTTrainer`, with `train_on_responses_only` (loss on model turns only,
   using Gemma's `<start_of_turn>` markers).
4. **Measure pre vs post.** Tool-call-equivalence on a held-out test split:
   feed the model each conversation prefix and check its predicted call(s)
   against gold (name + order-/whitespace-insensitive arguments).

## Layout

```
finetune/
├── exporter/main.go          # Go: dump Aura's real tool specs (read-only)
├── config/
│   ├── pipeline.yaml         # all knobs; override with AURA_FT_<KEY> env
│   └── seeds.jsonl           # seed goals for synthetic generation
├── aura_finetune/
│   ├── config.py             # yaml + env config
│   ├── tools.py              # load exported specs; default vs full tooldefs
│   ├── format.py             # normal-form trace ↔ FunctionGemma chat template
│   ├── harvest.py            # real traces from Postgres (read-only)
│   ├── synth.py              # teacher distillation + verification
│   ├── build_dataset.py      # merge + dedup + stratified split
│   ├── train.py              # Unsloth + TRL LoRA SFT
│   └── evaluate.py           # tool-call-equivalence metric (base vs finetuned)
├── scripts/run_pipeline.sh   # end-to-end driver
├── Makefile
└── requirements.txt
```

## Quickstart

```bash
cd finetune
pip install -r requirements.txt

# 1. export the real tool surface (writes data/aura_tools.json)
make tools

# 2. generate synthetic traces  (needs OPENROUTER_API_KEY)
make synth

# 3. (optional) harvest real traces  (needs AURA_DB_URL, read-only)
make harvest

# 4. build train/val/test
make dataset

# 5. measure BEFORE, train, measure AFTER
make eval-base
make train
make eval-finetuned
```

Or the whole thing: `./scripts/run_pipeline.sh --train`.

### Compute

FunctionGemma-270M LoRA fits the 4 GB-VRAM constraint (PRD Amendment #59) and
trains on CPU for this small spike set. Export GGUF for local inference with
`python -m aura_finetune.train --gguf` (q4_k_m).

## The result to fill in

| Task surface | Base (before) | Fine-tuned (after) |
|---|---|---|
| Aura tools (overall) | _run `make eval-base`_ | _run `make eval-finetuned`_ |
| — synthetic split | | |
| — real split | | |

(The blog's reference: 10–39% → 90–97% across three task families.)

## Design notes & honest gaps

- **Spec drift is impossible:** the exporter compiles against the live `tools`
  package, so the dataset always reflects the real manifest.
- **`task`/`skill` specs** are exported from their zero-value `Spec()` (static
  metadata); their production constructors aren't invoked, which is fine for the
  spec but means runtime-derived manifest text (e.g. the live skill list) isn't
  captured. Acceptable for a spike.
- **Predicted-call parsing** in `evaluate.py` is best-effort over FunctionGemma's
  `name{k:v}` block *and* a JSON arguments block, so the metric survives
  template/quantization differences. If you pin a specific template, tighten it.
- **Real-trace volume** is whatever Aura has logged; synthetic data carries the
  spike. Privacy: harvested traces stay under `data/` (git-ignored) and may
  contain operator content — don't commit them.

## Next steps (if the numbers land)

Wire the GGUF/merged model as a local **OpenAI-compatible sidecar** that Aura's
`internal/llm` client can route to — the Slice 13 (local LLM fallback) path —
with DeepSeek-V4 as the escalation tier for turns the 270M can't handle. That is
a separate, deliberate Go change behind the existing `llm.Config` provider seam;
**out of scope for this spike.**
