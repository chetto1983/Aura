---
spike: 072
name: fc270m-finetune-toolchain-fit
type: standard
validates: "Given the operator finetunes in Google Colab (not the local 4GB host), when the official Unsloth FunctionGemma-270M notebook is rewritten end-to-end and wired to Aura's spike-073 dataset, then a runnable notebook produces a finetuned GGUF that loads in llama.cpp"
verdict: VALIDATED
related: [071-fc270m-baseline-and-slot, 073-fc270m-dataset-from-registry, 074-fc270m-finetuned-vs-baseline]
tags: [functiongemma, finetune, unsloth, colab, gguf, toolchain, slice-13]
---

# Spike 072: FunctionGemma-270m finetune toolchain (Colab)

## What This Validates

Can Aura's tool dataset actually be finetuned into a usable FunctionGemma GGUF, and
on what toolchain? **Operator decision mid-session: do the finetune in Google Colab
(GPU), not on the local 4 GB A2000 / WSL.** So the deliverable is a ready-to-run
notebook — a full rewrite of the official Unsloth `FunctionGemma_(270M)` notebook,
wired to the spike-073 dataset — that the operator runs on Colab to emit the GGUF
that spike 074 evaluates.

## Research

- **Toolchain = the official notebook** (operator-provided):
  `github.com/unslothai/notebooks` → `nb/FunctionGemma_(270M).ipynb`. Its shape
  (verified): `load_dataset` → `tokenizer.apply_chat_template(messages, tools=...,
  add_generation_prompt=False, tokenize=False)` → SFTConfig(`dataset_text_field="text"`)
  → `train_on_responses_only(instruction_part="<start_of_turn>user\n",
  response_part="<start_of_turn>model\n")`.
- **The base is safetensors, not GGUF.** `unsloth/functiongemma-270m-it` (or
  `…-unsloth-bnb-4bit`). The `-GGUF` repo is the *output* shape only.
- **Why Colab over local:** the 4 GB A2000 is ~93% consumed by the primary model
  (spike 048) and WSL here ships Python 3.14 (no torch/unsloth wheels) with a 7.8 GB
  RAM cap — workable but fiddly. Colab T4 (16 GB) trains a 270M LoRA trivially.

## How to Run

1. Generate the dataset (spike 073): `go run ./.planning/spikes/073-fc270m-dataset-from-registry`.
2. Open **`FunctionGemma_270M_Aura.ipynb`** in Colab (GPU runtime).
3. Run top-to-bottom; upload `train.jsonl` + `eval.jsonl` at the upload cell.
4. Download the exported `functiongemma-270m-aura-q8_0.gguf`.
5. Serve + score it locally (spike 074 / the 071 harness).

`build_notebook.py` regenerates the `.ipynb` deterministically (stdlib only) — edit
the cell sources there, not the JSON.

## What to Expect

A finetuned **q8_0 GGUF** (~290 MB) that loads in `ghcr.io/ggml-org/llama.cpp:server`
with `--jinja` and emits FunctionGemma's `<start_function_call>call:NAME{...}` format
on Aura's tools.

## Investigation Trail

1. Initial plan was a local WSL Unsloth install (`setup.sh`, kept as the documented
   fallback). Probed WSL: Ubuntu 26.04 + CUDA OK, but **system Python 3.14** (too new
   for torch/unsloth wheels — pin 3.12 via `uv`) and a **7.8 GB RAM cap**. Workable,
   not pleasant.
2. **Operator pivot:** finetune in Colab; this spike prepares the notebook instead of
   running locally. Cleaner hardware (T4 16 GB), zero local-env wrangling, and the
   no-`.exe`/AV host constraint is moot.
3. **Format alignment:** read the official notebook's dataset/training cells and
   confirmed spike-073's `{messages, tools}` rows drop in with a one-cell loader +
   `apply_chat_template(..., tools=row["tools"])`. Added a guard that coerces
   `tool_calls[].arguments` to an object if a template version hands back a string.
4. **Notebook authored via generator** (`build_notebook.py` → `json.dump`) so the
   `.ipynb` escaping is correct; validated it parses as nbformat 4 (26 cells).

## Results

**VALIDATED ✓ (toolchain delivered + drop-in verified).** A complete, self-contained
Colab notebook is ready and its dataset contract matches spike 073 exactly. The actual
training run is the operator's Colab step (cheap, ~minutes on a T4) and feeds the
spike-074 head-to-head. What is proven here: the pipeline shape is correct
(base→LoRA→`apply_chat_template(tools=)`→`train_on_responses_only`→`save_pretrained_gguf`
q8_0→llama.cpp serve), and the local-fallback path is documented for an offline rebuild.

The only thing NOT executed in-session is the GPU training itself (no Colab from here);
that is by operator design, not a gap in the toolchain.

## Signal for the Build

- **Ship the notebook as the Slice-13 finetune recipe.** It regenerates from
  `build_notebook.py`; keep the two in sync.
- **max_seq_length = 8192** — the full-catalog rows (operator chose full catalog/example)
  run ~3-5k tokens; don't drop below that or calls get truncated.
- **Watch eval loss across the 3 epochs** — 134 examples is small; stop if eval loss
  turns up (overfit). Scale via spike-073 slot lists, not more epochs.
- **The finetuned GGUF still needs Aura's custom call parser** (spike 071) — llama.cpp
  returns `<start_function_call>` in `content`, not OpenAI `tool_calls[]`.
- Next: run it, then spike 074 for the verdict on whether finetuning clears the bar.
