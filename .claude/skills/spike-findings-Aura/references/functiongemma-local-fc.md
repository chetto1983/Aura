# functiongemma local fc

Slice-13 **local/offline function-call GENERATION** (call + arguments) using a finetuned
FunctionGemma-270M GGUF served on llama.cpp. Tool *selection* ("which tool?") is already
SHIPPED for free via granite embeddings (spikes 054-058); FunctionGemma's only defensible
slot is generating the call + arguments with no cloud, on the offline fallback path.

## Requirements

(Non-negotiable design decisions from MANIFEST Session-19 + the per-spike rows. Each is a hard constraint.)

- **Finetune is mandatory, not optional.** Base `unsloth/functiongemma-270m-it` zero-shot on Aura's tools is UNUSABLE: top-1 ≈ 1/12 (~8%), ~80% refusal ("I am sorry, but I cannot assist…") in BOTH Italian and English, identical at temp 0 / temp 1.0 / with the correct preamble. The machinery is sound (reads real JSON schemas, emits correct `<escape>` format), only alignment-to-fire is missing — exactly what a LoRA targets. (Spike 071.)
- **You cannot finetune the `-GGUF` repo — it is inference-only.** Train the **safetensors** base `unsloth/functiongemma-270m-it` (or `…-unsloth-bnb-4bit`), then *export* GGUF. The `-GGUF` repo is the output shape only. (Spikes 071/072.)
- **Finetune in Google Colab (T4 16 GB), NOT the local 4 GB host.** Operator decision mid-session. The deliverable is a ready-to-run notebook (`FunctionGemma_270M_Aura.ipynb`), regenerated deterministically by `build_notebook.py` — keep the two in sync; edit the cell sources in the generator, not the JSON. Local WSL Unsloth (`setup.sh`) is the documented offline fallback only. (Spike 072.)
- **Dataset = Aura's real registry, FULL 21-tool catalog in every row, Italian-primary ~70/30.** HF `{messages, tools}` shape; `tool_calls[].function.arguments` is an **object** (not a string); no developer message in `messages` (the template injects the preamble + declarations from `tools=`). Native schemas pulled LIVE from `internal/agent/tools`; MCP namespaces from a static catalog mirroring live mounts. Regenerate `go run ./.planning/spikes/073-fc270m-dataset-from-registry` (deterministic, seed 3407) when the registry changes. (Spike 073.)
- **Aura MUST ship a custom FunctionGemma call parser.** llama.cpp `--jinja` does NOT parse FunctionGemma's `<start_function_call>call:NAME{...}<end_function_call>` format into OpenAI `tool_calls[]` — it returns the block in `message.content`. The parser is real Slice-13 glue (the ~5-line `rawCallRe` regex + an `<escape>`-delimited arg decoder). Required regardless of finetune outcome. (Spikes 071/072.)
- **Keep FunctionGemma OFF the selection hot path; narrow it to the offline call+arg tier.** Embeddings already win selection (~85µs, free). FunctionGemma's value is generation only. VRAM forces this: it cannot permanently co-reside with the 3.7 GB primary model on the 4 GB card (3705 + 455 = 4160 > 4096). Run it CPU, time-shared, or as the offline-mode primary. If spike 074 can't beat "embedding-selects + cloud-fills-args" on a real metric, the slot may not justify the build. (Spikes 071/074.)

## How to Build It

### 0. Status gate (spike 074 is PENDING)
The whole chain's payoff verdict (074) is **PENDING the operator's Colab finetune** — the finetuned GGUF does not yet exist. Upstream is ready (071 baseline, 073 data, 072 notebook). Do NOT build the Slice-13 tier until 074 lands. Verdict criteria (074):
- **VALIDATED** → finetuned IT top-1 ≥ ~8/12, args mostly right, GPU ≤ ~1.6 s/call. Build the tier.
- **PARTIAL** → fires more but args/namespace lag, or CPU-only. Keep as offline-only fallback; revisit dataset scale (073 slot lists).
- **INVALIDATED** → still refuses/wrong-tool at this data scale. Shelve; embeddings own selection, cloud LLM owns arg-gen on the hot path.

### 1. Generate the dataset (spike 073, SHIPPED as a runnable generator)
`go run ./.planning/spikes/073-fc270m-dataset-from-registry` → `train.jsonl` (101), `eval.jsonl` (33), `COVERAGE.md`. Deterministic, seed 3407. Source: `sources/073-fc270m-dataset-from-registry/main.go`.

Row shape (the exact contract the notebook consumes):
```json
{"messages":[{"role":"user","content":"quanto costa il bitcoin adesso?"},
             {"role":"assistant","tool_calls":[{"type":"function",
               "function":{"name":"web_search","arguments":{"query":"il bitcoin prezzo attuale","category":"general"}}}]}],
 "tools":[<FULL 21-tool catalog with real schemas>]}
```
Key generator mechanics:
- Native schemas pulled from the REAL registry via zero-value `Spec()` (spike 054 convention): `&tools.WebSearch{}`, `&tools.WebFetch{}`, `&tools.DocumentSearch{}`, `tools.CurrentTime{}`, `&tools.SendFile{}`, `&tools.FSRead{}`, `&tools.FSGlob{}`, `&tools.FSGrep{}`, `&tools.FSWrite{}`, `&tools.FSEdit{}`, `&tools.ShellExec{}`.
- MCP namespaces (`mail__*`, `whatsapp__*`, `calendar__*`, `memory__*`) come from `mcpCatalog()` — a static catalog mirroring live surfaces (the one hand-maintained part; keep in sync with live mounts).
- Failure-mode targeting (the 071 gaps the data must move): bitcoin/price/meteo/news → `web_search` (NOT mail); `document_search` ("my documents") vs `web_fetch` (a URL); `fs_glob` (find-by-pattern) vs `fs_read` (read-one); mail/whatsapp/calendar siblings co-occur as distractors to teach namespace disambiguation.
- A few no-tool chat rows (greetings → assistant text, no `tool_calls`) curb over-firing.
- Stratified split holds out ~1 + 15% per gold tool so eval covers every tool. Every tool ≥3 examples (≥2 train); `web_search` is intentionally the largest class (23) as the gravity-well counterweight.
- Final mix: 134 ex, 21-tool catalog/row, it 102 (76%) / en 32 (24%). Per-tool counts in `sources/073-.../COVERAGE.md`.
- Scale lever = the template `vals` slot lists (add values → multiply examples with zero new code). Re-run after the first 074 eval shows which tools still miss. Scale via slot lists, NOT more epochs.

### 2. Finetune in Colab (spike 072, VALIDATED — notebook authored & drop-in verified)
Open `sources/072-fc270m-finetune-toolchain-fit/FunctionGemma_270M_Aura.ipynb` (regenerable via `build_notebook.py`, stdlib-only, 26 cells, nbformat-4 valid) on a Colab **GPU** runtime. Run top-to-bottom; upload `train.jsonl` + `eval.jsonl` at the upload cell; download the exported GGUF. Pipeline shape (proven correct):
```
base safetensors → LoRA → apply_chat_template(messages, tools=row["tools"], add_generation_prompt=False, tokenize=False)
  → train_on_responses_only(instruction_part="<start_of_turn>user\n", response_part="<start_of_turn>model\n")
  → save_pretrained_gguf(quantization_method="q8_0") → llama.cpp serve
```
Load (cell 2): `FastLanguageModel.from_pretrained(model_name="unsloth/functiongemma-270m-it", max_seq_length=MAX_SEQ_LEN, load_in_4bit=False, load_in_16bit=True, full_finetuning=False)` — 270M in 16-bit is tiny, QLoRA unnecessary.

LoRA (cell 3): `r=16, lora_alpha=16, lora_dropout=0, bias="none"`, target `["q_proj","k_proj","v_proj","o_proj","gate_proj","up_proj","down_proj"]`, `use_gradient_checkpointing="unsloth"`, `random_state=3407`.

Trainer (cell 6): `SFTConfig(dataset_text_field="text", max_seq_length=MAX_SEQ_LEN, per_device_train_batch_size=1, gradient_accumulation_steps=8, warmup_steps=5, num_train_epochs=3, learning_rate=2e-4, optim="adamw_8bit", weight_decay=0.01, lr_scheduler_type="linear", seed=3407)`.

Two load-bearing notebook details that silently break training if dropped:
- **`MAX_SEQ_LEN = 16384`.** Full-catalog rows measured ~12.9k–13k tokens (the static catalog dominates; the query+call is ~30 tokens). `8192` filtered EVERY row → `num_samples=0`. 16k fits (Gemma-3 ctx = 32k). A length-probe assert in the format cell catches regressions. (The MANIFEST row 243's "max_seq 8192" is a stale summary; the authored notebook and the README Signal both use **16384** — that is authoritative.)
- **`remove_columns=ds[...].column_names` after `.map(to_text)`.** TRL must see a pure-text dataset; if `messages`/`tools` columns survive, TRL takes its conversational branch and silently empties the split. Also: capture `CATALOG = ds["train"][0]["tools"]` BEFORE dropping columns (the inference cell needs it).
- **Arguments-string→object guard** in `to_text`: some template versions hand back `tool_calls[].function.arguments` as a JSON string; coerce back to an object (`json.loads`) since Aura emits objects.

Export q8_0 (cell 10): `model.save_pretrained_gguf("functiongemma-270m-aura-gguf", tokenizer, quantization_method="q8_0")` → ~290 MB GGUF.

Watch eval loss across the 3 epochs — 134 examples is small; stop if eval loss turns up (overfit).

### 3. Serve + evaluate (spike 074, eval harness = spike 071 re-pointed)
After downloading `functiongemma-270m-aura-q8_0.gguf` into the 074 dir, serve from **PowerShell** (not Git-Bash — MSYS path mangling):
```bash
docker run -d --name fc270m-aura --gpus all -p 127.0.0.1:8097:8097 \
  -v "$PWD:/models" ghcr.io/ggml-org/llama.cpp:server-cuda \
  -m /models/functiongemma-270m-aura-q8_0.gguf \
  --jinja --host 0.0.0.0 --port 8097 --ctx-size 8192 -ngl 99
```
Score with the unchanged 071 harness (IT + EN), directly comparable to baseline:
```bash
FC_BASE_URL=http://127.0.0.1:8097 FC_TAG=aura-ft-IT             go run ./.planning/spikes/071-fc270m-baseline-and-slot
FC_BASE_URL=http://127.0.0.1:8097 FC_TAG=aura-ft-EN FC_LANG=en  go run ./.planning/spikes/071-fc270m-baseline-and-slot
```
Harness (`sources/071-.../main.go`) builds Aura's real ToolSpecs (promoting deferred specs to FULL form via `specToOpenAI` since `Render()` blanks deferred description/params), adds the `mail__*` gravity-well cluster, sends them as OpenAI `tools[]`, and per query logs `[CASE]` verdict (OK/TOOL-ONLY/WRONG-TOOL/NO-CALL) + `[SCORE]` emitted/top1/arg-correct + `[PARSE-VIA]` (`tool_calls` vs `raw`) + `[LATENCY]` p50/p95. Env knobs: `FC_BASE_URL`, `FC_TAG`, `FC_LANG=en`, `FC_TEMP=1.0`, `FC_SYS=1`.

### 4. The custom call parser (ship in Slice 13)
llama.cpp returns FunctionGemma's call in `message.content`. The proven parser (from `sources/071-.../main.go`):
```go
// FunctionGemma raw format: <start_function_call>call:NAME{args}<end_function_call>
var rawCallRe = regexp.MustCompile(`(?s)<start_function_call>\s*call:\s*([A-Za-z0-9_]+)\s*\{(.*?)<end_function_call>`)
```
Parse order: prefer `message.tool_calls[]` if present (in case a future template version parses it), else regex `content`; strip a trailing `}` from the arg capture. String values are `<escape>`-delimited (`category:<escape>news<escape>`) → write a small `<escape>` decoder. `sameTool` treats `name` and `ns__name` as equal (namespace-suffix match).

### 5. Wire format reference (FunctionGemma, non-OpenAI)
- Developer preamble (template-injected when `tools[]` present): `<start_of_turn>developer\nYou are a model that can do function calling with the following functions`.
- Declarations: `<start_function_declaration>declaration:NAME{description:<escape>…<escape>,parameters:{…}}<end_function_declaration>`.
- Calls: `<start_function_call>call:web_search{query: "…"}<end_function_call>`, optional `<think>…</think>` CoT, `<escape>` delimits string values.
- Google sampling rec: `temp=1.0, top_k=64, top_p=0.95`.

## What to Avoid

- **Do NOT try to finetune `unsloth/functiongemma-270m-it-GGUF`.** GGUF is inference-only; LoRA/QLoRA needs the safetensors base. (071/072.)
- **Do NOT ship the base model unfinetuned.** ~80% refusal, top-1 ~8% on Aura tools — measured identical across IT/EN, temp 0, temp 1.0 (Google sampling top_k=64/top_p=0.95), and with the correct `FC_SYS=1` preamble (byte-identical to default — the GGUF template already injects the function-calling preamble when `tools[]` is present). Refusal is intrinsic to the base, not a config bug. Observed failure modes: refuse-instead-of-call (dominant), wrong tool (`fs_glob → fs_read`), **hallucinated** tool name (`search(query:TODO)` — not in corpus). (Spike 071.)
- **Do NOT expect English to rescue the base model.** EN eval was marginally better (3/12 emitted vs 2/12) but top-1 still 1/12. English-centric model, but English does not fix it. (071.)
- **Do NOT put FunctionGemma on the tool-SELECTION hot path.** It loses to the shipped embedding ranker (top1 1/12 base vs ~8/15 embeddings, slower, not free). Selection is solved. (071/074.)
- **Do NOT plan permanent GPU co-residence with the primary model.** Primary Gemma-4 E2B = ~3705 MiB peak (spike 048) + FunctionGemma Q8 455 MiB = 4160 > 4096 ceiling, zero desktop headroom. (071.)
- **Do NOT rely on CPU latency for an interactive path.** CPU Q8 (`-t 4`) is p50 ~750 ms / p95 ~3.5 s / max ~3.8 s — at/over the `minillm_cpu_not_viable` edge. CPU is fallback-only. (071.)
- **Do NOT set `max_seq_length=8192` in the notebook.** It filters every full-catalog row (~13k tokens) → empty split / `num_samples=0`. Use 16384. (072.)
- **Do NOT leave `messages`/`tools` columns on the dataset before TRL.** `remove_columns` after `.map(to_text)` or TRL silently empties the split via its conversational branch. (072.)
- **Do NOT launch the docker server from Git-Bash.** MSYS path mangling — use PowerShell. (071/074, CONVENTIONS.)
- **Local WSL finetune is a fiddly fallback, not the path.** WSL Ubuntu 26.04 ships system Python 3.14 (no torch/unsloth wheels — must pin 3.12 via `uv`) and a 7.8 GB RAM cap, and the 4 GB A2000 is ~93% consumed by the primary model. Workable, not pleasant; Colab T4 is the chosen route. (072 — `setup.sh` is the documented fallback.)
- **`Spec()`-safe ≠ `Execute()`-safe** in the harness corpus (a zero-value web tool `Execute` panics; only `Spec()` is safe to call on zero values). Relevant if extending the harness. (cross-spike, 077.)

## Constraints

- **Model:** base trainable = `unsloth/functiongemma-270m-it` (safetensors) — a Gemma-3 270M variant trained for text-only function calling, 32K ctx. Output served = q8_0 GGUF (~290 MB). Base GGUF quants: 180 MB (IQ2) → 543 MB (BF16); Q8_0 = 292 MB; ~550 MB RAM on CPU.
- **Accuracy (base, zero-shot):** emitted-call 2/12 (IT temp0), 3/12 (EN); top1 1/12 (~8%); arg-correct 0–1/12; ~80% refusal. Only stable win: `news → web_search`.
- **Latency (per call; refusals fast, full calls slow):** GPU Q8 `-ngl 99` p50 ~300 ms / p95 ~1.1 s / max ~1.6 s; CPU Q8 `-t 4` p50 ~750 ms / p95 ~3.5 s / max ~3.8 s. Real tool-call generation is decode-bound on the verbose `<escape>` arg format. Latency budget = GPU (~1.6 s/call); CPU is fallback-only.
- **VRAM:** FunctionGemma Q8 full-offload = **455 MiB** at ctx 8192 (~350 MiB at ctx 2048). Card = 4 GB A2000 (4096 MiB). Primary Gemma-4 E2B = ~3705 MiB peak (spike 048) → no permanent co-residence.
- **Serving images:** CPU = `ghcr.io/ggml-org/llama.cpp:server`; GPU = `ghcr.io/ggml-org/llama.cpp:server-cuda`. Flags: `--jinja` (tool support), `--ctx-size 8192`, `-ngl 99` (GPU full offload) or `-t 4` (CPU). Base-model pull route: `--hf-repo unsloth/functiongemma-270m-it-GGUF --hf-file functiongemma-270m-it-Q8_0.gguf`. Finetuned route: `-m /models/<file>.gguf` with `-v "$PWD:/models"`.
- **Ports:** 071 CPU = 8099, 071 GPU = 8098, 074 finetuned = 8097.
- **Colab:** T4 16 GB; `pip install unsloth` pulls a compatible torch/trl/peft stack; bsz=1 + grad-accum 8 on the 13k-token rows; ~minutes to train a 270M LoRA.
- **Determinism:** dataset + notebook seed = **3407**.
- **Dataset:** 134 ex (train 101 / eval 33), 21-tool catalog/row, it 76% / en 24%, every tool ≥3 examples.
- **Env knobs (071/074 harness):** `FC_BASE_URL`, `FC_TAG`, `FC_LANG` (`en` for the English set, default IT), `FC_TEMP` (`1.0` for Google sampling, default greedy 0), `FC_SYS` (`1` injects the FunctionGemma activation preamble). No `AURA_*` env exists for this yet — it is pre-build.
- **License:** FunctionGemma = Google Gemma terms; Unsloth wrappers Apache-2.0. (Not flagged as a blocker in the spikes.)
- **Module path:** harness/generator import `github.com/chetto1983/aura/internal/agent/tools` (the spike module path).

## Origin

Synthesized from spikes: **071, 072, 073, 074** (Session-19, 2026-06-21).
Source files in:
- `sources/071-fc270m-baseline-and-slot/` — README.md + `main.go` (the baseline/eval harness + custom `rawCallRe` parser; reused unchanged as the 074 eval harness).
- `sources/072-fc270m-finetune-toolchain-fit/` — README.md + `FunctionGemma_270M_Aura.ipynb` (Colab notebook) + `build_notebook.py` (deterministic generator) + `setup.sh` (WSL fallback).
- `sources/073-fc270m-dataset-from-registry/` — README.md + `main.go` (dataset generator) + `COVERAGE.md`. (`train.jsonl`/`eval.jsonl` omitted — >256 KB; regenerate via the generator.)
- `sources/074-fc270m-finetuned-vs-baseline/` — README.md (run-book + scorecard).

Verdicts: **071 PARTIAL** (gate PASSED — base unusable → finetune motivated, machinery sound, slot narrow); **072 VALIDATED** (Colab notebook authored + drop-in verified, training run is the operator's Colab step by design); **073 VALIDATED** (134-ex notebook-ready dataset from the real registry); **074 PENDING** (eval harness = 071 re-pointed, scorecard + verdict criteria written, awaiting the operator's Colab GGUF — DO NOT build the Slice-13 tier until this lands).
