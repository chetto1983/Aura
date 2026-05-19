# mem0 LLM-judged memory writes — porting research for Aura US-OP06

Research date: 2026-05-19
Repo snapshot: `D:/tmp/mem0/` (commit `54a03cc7` at HEAD; pre-V3 reference commit `a488e190^`).
Trigger: Phase-OP+ planning — close the dedup/contradiction gap identified in [`docs/self-improvement-patterns-2026-05-18.md`](self-improvement-patterns-2026-05-18.md) §Pattern A.
License verdict: **Apache-2.0** ([`LICENSE`](../../tmp/mem0/LICENSE#L1)) — concepts AND code may be ported with attribution + NOTICE preservation.

---

## TL;DR for the porter

1. **The canonical ADD/UPDATE/DELETE/NONE prompt is `DEFAULT_UPDATE_MEMORY_PROMPT`** in [`mem0/configs/prompts.py:176-324`](../../tmp/mem0/mem0/configs/prompts.py#L176-L324). It is **still shipped** in the current repo but **no longer invoked by mem0 itself** — only by tests. The live code path uses an ADD-only pipeline (`ADDITIVE_EXTRACTION_PROMPT`, line 468).
2. **mem0 explicitly abandoned the four-event design in April 2026.** Their README publishes benchmark numbers (LoCoMo 71.4 → 91.6, LongMemEval 67.8 → 94.8) attributing the gain to ADD-only single-pass extraction with hash dedup + entity linking. This is direct, public evidence that LLM-judged UPDATE/DELETE was their bottleneck. The Aura porter **must read §5 before deciding to copy the four-event pattern verbatim**.
3. **What to copy verbatim**: the prompt text + the `{"memory": [{"id","text","event","old_memory"}]}` JSON schema + the UUID→integer mapping trick (anti-hallucination) + the `_resolve_mapped_id` guard.
4. **What to adapt**: synchronous vs async, the retrieval cap (mem0: top_k=5 per new fact, deduped; Aura's `compact_memory_documents` is small enough to send all rows), and the fact-extraction stage (Aura already has structured patches — no extraction needed, only judgement).
5. **What to skip**: mem0's V3 entity linking, multi-signal fusion, BM25 keyword search. Out of scope for US-OP06.

---

## 1. The DEFAULT_UPDATE_MEMORY_PROMPT (verbatim)

Source: [`mem0/configs/prompts.py:176-324`](../../tmp/mem0/mem0/configs/prompts.py#L176-L324). Quoted exactly as in the file:

```text
You are a smart memory manager which controls the memory of a system.
You can perform four operations: (1) add into the memory, (2) update the memory, (3) delete from the memory, and (4) no change.

Based on the above four operations, the memory will change.

Compare newly retrieved facts with the existing memory. For each new fact, decide whether to:
- ADD: Add it to the memory as a new element
- UPDATE: Update an existing memory element
- DELETE: Delete an existing memory element
- NONE: Make no change (if the fact is already present or irrelevant)

There are specific guidelines to select which operation to perform:

1. **Add**: If the retrieved facts contain new information not present in the memory, then you have to add it by generating a new ID in the id field.
- **Example**:
    - Old Memory:
        [
            {
                "id" : "0",
                "text" : "User is a software engineer"
            }
        ]
    - Retrieved facts: ["Name is John"]
    - New Memory:
        {
            "memory" : [
                {
                    "id" : "0",
                    "text" : "User is a software engineer",
                    "event" : "NONE"
                },
                {
                    "id" : "1",
                    "text" : "Name is John",
                    "event" : "ADD"
                }
            ]

        }

2. **Update**: If the retrieved facts contain information that is already present in the memory but the information is totally different, then you have to update it.
If the retrieved fact contains information that conveys the same thing as the elements present in the memory, then you have to keep the fact which has the most information.
Example (a) -- if the memory contains "User likes to play cricket" and the retrieved fact is "Loves to play cricket with friends", then update the memory with the retrieved facts.
Example (b) -- if the memory contains "Likes cheese pizza" and the retrieved fact is "Loves cheese pizza", then you do not need to update it because they convey the same information.
If the direction is to update the memory, then you have to update it.
Please keep in mind while updating you have to keep the same ID.
Please note to return the IDs in the output from the input IDs only and do not generate any new ID.
- **Example**:
    - Old Memory:
        [
            {
                "id" : "0",
                "text" : "I really like cheese pizza"
            },
            {
                "id" : "1",
                "text" : "User is a software engineer"
            },
            {
                "id" : "2",
                "text" : "User likes to play cricket"
            }
        ]
    - Retrieved facts: ["Loves chicken pizza", "Loves to play cricket with friends"]
    - New Memory:
        {
        "memory" : [
                {
                    "id" : "0",
                    "text" : "Loves cheese and chicken pizza",
                    "event" : "UPDATE",
                    "old_memory" : "I really like cheese pizza"
                },
                {
                    "id" : "1",
                    "text" : "User is a software engineer",
                    "event" : "NONE"
                },
                {
                    "id" : "2",
                    "text" : "Loves to play cricket with friends",
                    "event" : "UPDATE",
                    "old_memory" : "User likes to play cricket"
                }
            ]
        }


3. **Delete**: If the retrieved facts contain information that contradicts the information present in the memory, then you have to delete it. Or if the direction is to delete the memory, then you have to delete it.
Please note to return the IDs in the output from the input IDs only and do not generate any new ID.
- **Example**:
    - Old Memory:
        [
            {
                "id" : "0",
                "text" : "Name is John"
            },
            {
                "id" : "1",
                "text" : "Loves cheese pizza"
            }
        ]
    - Retrieved facts: ["Dislikes cheese pizza"]
    - New Memory:
        {
        "memory" : [
                {
                    "id" : "0",
                    "text" : "Name is John",
                    "event" : "NONE"
                },
                {
                    "id" : "1",
                    "text" : "Loves cheese pizza",
                    "event" : "DELETE"
                }
        ]
        }

4. **No Change**: If the retrieved facts contain information that is already present in the memory, then you do not need to make any changes.
- **Example**:
    - Old Memory:
        [
            {
                "id" : "0",
                "text" : "Name is John"
            },
            {
                "id" : "1",
                "text" : "Loves cheese pizza"
            }
        ]
    - Retrieved facts: ["Name is John"]
    - New Memory:
        {
        "memory" : [
                {
                    "id" : "0",
                    "text" : "Name is John",
                    "event" : "NONE"
                },
                {
                    "id" : "1",
                    "text" : "Loves cheese pizza",
                    "event" : "NONE"
                }
            ]
        }
```

### Prompt wrapper / user-side framing

The prompt is wrapped by `get_update_memory_messages()` ([`prompts.py:406-460`](../../tmp/mem0/mem0/configs/prompts.py#L406-L460)). Verbatim wrapper:

```text
{custom_update_memory_prompt}

    {current_memory_part}      # → "Below is the current content of my memory ... ``` {retrieved_old_memory_dict} ```"
                               # OR "Current memory is empty." when no candidates exist

    The new retrieved facts are mentioned in the triple backticks. You have to analyze the new retrieved facts and determine whether these facts should be added, updated, or deleted in the memory.

    ```
    {response_content}     # list[str] of new facts
    ```

    You must return your response in the following JSON structure only:

    {
        "memory" : [
            {
                "id" : "<ID of the memory>",                # Use existing ID for updates/deletes, or new ID for additions
                "text" : "<Content of the memory>",         # Content of the memory
                "event" : "<Operation to be performed>",    # Must be "ADD", "UPDATE", "DELETE", or "NONE"
                "old_memory" : "<Old memory content>"       # Required only if the event is "UPDATE"
            },
            ...
        ]
    }

    Follow the instruction mentioned below:
    - Do not return anything from the custom few shot prompts provided above.
    - If the current memory is empty, then you have to add the new retrieved facts to the memory.
    - You should return the updated memory in only JSON format as shown below. The memory key should be the same if no changes are made.
    - If there is an addition, generate a new key and add the new memory corresponding to it.
    - If there is a deletion, the memory key-value pair should be removed from the memory.
    - If there is an update, the ID key should remain the same and only the value needs to be updated.

    Do not return anything except the JSON format.
```

### Input format the LLM sees

- **System prompt**: the static `DEFAULT_UPDATE_MEMORY_PROMPT` (or a user-supplied `custom_update_memory_prompt` overriding it).
- **User prompt fields** (assembled by `get_update_memory_messages`):
  - `retrieved_old_memory_dict`: `list[{"id": "0", "text": "..."}, ...]` where `id` is a **stringified integer** (the UUID→int mapping, see §2).
  - `response_content`: `list[str]` — the new facts to judge.
- The call is sent as a **single user message** (no system role); the model is invoked with `response_format={"type": "json_object"}` ([pre-V3 main.py:601-613](#pre-v3-main-py-601-613)).

### Output format expected

```json
{
  "memory": [
    {"id": "<existing-int or new-int>", "text": "<content>", "event": "ADD|UPDATE|DELETE|NONE", "old_memory": "<only if UPDATE>"}
  ]
}
```

The four events are **exhaustive**. No other events are emitted by the prompt or handled by the dispatch loop.

---

## 2. Control flow

All line numbers are from the **pre-V3 commit** `a488e190^` (still the canonical reference for the four-event flow — current HEAD removed this path).

### 2.1 Where the prompt is invoked

`Memory._add_to_vector_store(messages, metadata, filters, infer=True)`:
- Pre-V3: defined at `mem0/memory/main.py:485-720` (commit `a488e190^`).
- Post-V3 (current HEAD): defined at [`mem0/memory/main.py:662-971`](../../tmp/mem0/mem0/memory/main.py#L662-L971) — but the four-event flow is gone; the new flow is ADD-only.

### 2.2 Model defaults

- Default LLM `temperature = 0.1` ([`mem0/configs/llms/base.py:19`](../../tmp/mem0/mem0/configs/llms/base.py#L19)). NOT 0. mem0 chooses near-deterministic but not strictly deterministic.
- `max_tokens = 2000`, `top_p = 0.1`, `top_k = 1` ([`mem0/configs/llms/openai.py:41-46`](../../tmp/mem0/mem0/configs/llms/openai.py#L41-L46)).
- `model` defaults to `None` (provider-specific resolution). For OpenAI no hard-coded default; configured by the user.
- One LLM client is shared for fact-extraction AND ADD/UPDATE/DELETE judgement — no separate "cheap model" hook.

### 2.3 Pre-V3 step-by-step flow

Step | Code (pre-V3) | What happens
--- | --- | ---
0 | `add(messages, infer=True)` calls `_add_to_vector_store` | Entry point
1 | `parse_messages(messages)` | Conversation → flat string
2 | `get_fact_retrieval_messages(parsed_messages, is_agent_memory)` | Picks `USER_MEMORY_EXTRACTION_PROMPT` or `AGENT_MEMORY_EXTRACTION_PROMPT`
3 | First LLM call: `response_format={"type":"json_object"}` | LLM returns `{"facts": ["...", "..."]}`
4 | `normalize_facts(...)` ([`memory/utils.py:84-106`](../../tmp/mem0/mem0/memory/utils.py#L84-L106)) | Coerces `{"fact": "..."}` / `{"text": "..."}` shapes to strings (small-LLM defensiveness)
5 | **Per fact**: `vector_store.search(query=fact, vectors=embed(fact), top_k=5, filters=user/agent/run)` | Retrieve **5 nearest** existing memories per new fact
6 | Dedup by `id` → flat list of candidates | `unique_data[item["id"]] = item`
7 | **UUID → integer mapping** (anti-hallucination): `temp_uuid_mapping[str(idx)] = real_uuid; retrieved_old_memory[idx]["id"] = str(idx)` | Crucial: LLM never sees UUIDs, only `"0"`, `"1"`, ...
8 | Second LLM call: `get_update_memory_messages(retrieved_old_memory, new_retrieved_facts, custom_prompt)` | The ADD/UPDATE/DELETE/NONE judgement
9 | Parse response | `remove_code_blocks` → `json.loads(..., strict=False)` → fallback to `extract_json(...)` on `JSONDecodeError`
10 | For each event in `response["memory"]` → dispatch | See §2.4

### 2.4 What happens on each event

Dispatch loop (pre-V3 `main.py:625-712`):

#### `ADD`
- New embedding for `action_text` (cached if already embedded during fact retrieval).
- Calls `_create_memory(data=action_text, ...)` → `vector_store.insert([embedding], [new_uuid], [metadata])` + `db.add_history(uuid, None, data, "ADD", ...)`.
- Current-HEAD impl: [`main.py:1586-1616`](../../tmp/mem0/mem0/memory/main.py#L1586-L1616). Adds `hash = md5(data)`, `created_at`, `updated_at`.

#### `UPDATE`
- Resolves the int ID back to a UUID via `temp_uuid_mapping` (`_resolve_mapped_id` returns None + WARNING log if the LLM hallucinated an unknown id; the update is then skipped).
- Calls `_update_memory(memory_id, data=action_text, ...)` → `vector_store.update(memory_id, embedding, metadata)` + `db.add_history(uuid, prev_value, data, "UPDATE", ...)`.
- Implementation: [`main.py:1657-1720`](../../tmp/mem0/mem0/memory/main.py#L1657-L1720). **Overwrites** the existing row (no merge of old + new strings — the LLM is expected to have produced the merged text in its `text` field, as shown in the Example 2 prompt). The old text is preserved only in `db.add_history.prev_value`.
- Note: the LLM is given full latitude. In Example 2(a) of the prompt: `"User likes to play cricket"` + new fact `"Loves to play cricket with friends"` → LLM is expected to output `"Loves to play cricket with friends"` (replaces) — but at Example 2 the LLM merged `"I really like cheese pizza"` + `"Loves chicken pizza"` → `"Loves cheese and chicken pizza"`. So mem0 **lets the LLM decide whether to merge or replace**.

#### `DELETE`
- Resolves int → UUID (same hallucination guard).
- Calls `_delete_memory(memory_id)` → `vector_store.delete(uuid)` + `db.add_history(..., "DELETE", is_deleted=1)`.
- Implementation: [`main.py:1722-1750`](../../tmp/mem0/mem0/memory/main.py#L1722-L1750). **Deletes the EXISTING row** that was in the candidate set, never the new fact (the new fact is discarded by virtue of not appearing in any other branch).

#### `NONE`
- Special case: if `metadata` contains `agent_id` or `run_id` AND the int-id maps to a real memory, the existing payload is **re-stamped** with the new `agent_id`/`run_id` (session-affiliation refresh). No content change. ([`main.py:686-712` pre-V3](#pre-v3-main-py-686-712)).
- Otherwise: pure no-op (`logger.info("NOOP for Memory.")`).
- The new fact corresponding to a NONE judgement is **not stored** anywhere. The semantics: "this fact is already covered by the existing memory `id=N`, don't duplicate it."

### 2.5 Per-fact vs batch

**Batch**. The second LLM call receives:
- ALL new facts at once (in `response_content` = `list[str]`).
- ALL existing candidates at once (in `retrieved_old_memory_dict`, deduped across the per-fact searches).

The model returns a single `{"memory": [...]}` array covering every existing+new pair. Typical batch size is dominated by retrieval: ≤ `5 × len(new_facts)` candidates plus all new facts, deduped by ID. With 3 new facts and 5 existing in DB, the LLM judges up to 15 (deduped, often ≤8) candidates.

---

## 3. Edge cases

| Edge case | Behavior | Source |
| --- | --- | --- |
| **0 existing candidates** (empty DB or no vector match) | `get_update_memory_messages` substitutes `"Current memory is empty."` in the user prompt. The LLM is then expected to emit pure ADD events. The wrapper still calls the LLM (no shortcut). | [`prompts.py:412-425`](../../tmp/mem0/mem0/configs/prompts.py#L412-L425), confirmed by `test_get_update_memory_messages_empty_memory` |
| **0 new facts** (extraction returned `[]`) | `_add_to_vector_store` short-circuits: skips the second LLM call entirely. `logger.debug("No new facts retrieved from input. Skipping memory update LLM call.")` | pre-V3 `main.py:561-562` |
| **Malformed JSON from LLM** | Three-stage parsing: (1) `remove_code_blocks` strips ```` ``` ```` fences and `<think>...</think>`; (2) `json.loads(..., strict=False)`; (3) on `JSONDecodeError`, `extract_json` finds the first `{` and last `}` and retries. On total failure, `new_memories_with_actions = {}` and the whole batch is dropped (no insert, no error to caller). | pre-V3 `main.py:613-624`; helpers in [`memory/utils.py:109-142`](../../tmp/mem0/mem0/memory/utils.py#L109-L142) |
| **Empty LLM response** (zero-length string) | `logger.warning("Empty response from LLM, no memories to extract")`; batch dropped. | pre-V3 `main.py:614-616` |
| **LLM hallucinates an ID** (e.g. returns `id="12"` when only IDs 0/1 exist) | `_resolve_mapped_id` returns `None` and logs `WARNING: {EVENT} skipped: LLM returned unknown id 12`. The hallucinated UPDATE/DELETE is **silently skipped** (no exception, no row affected). Added in PR #4674 / commit `081eca6d` fixing issue [#3931](https://github.com/mem0ai/mem0/issues/3931). | [`main.py:64-74`](../../tmp/mem0/mem0/memory/main.py#L64-L74) |
| **Empty `text` field on an event** | Skipped with `logger.info("Skipping memory entry because of empty 'text' field.")`. | pre-V3 `main.py:631-633` |
| **Infinite loop prevention** | None **explicit**. Each `add()` call runs exactly two LLM rounds (extract → judge). UPDATE events do not re-trigger another judgement. The pipeline is per-call, not per-row, so an UPDATE writing `"X"` won't recursively re-judge `"X"` against itself. | (absence of recursion — confirmed by reading the dispatch loop end-to-end) |
| **Token budget cap** | Implicit only — `top_k=5` per fact bounds candidate count; no character or token limit on the prompt itself. With 3 new facts × 5 candidates = up to 15 entries plus the new-facts list, the prompt body is typically <2000 tokens. No truncation logic in the path. | pre-V3 `main.py:582`; current HEAD V3 uses `top_k=10` ([`main.py:712`](../../tmp/mem0/mem0/memory/main.py#L712)) |
| **Cost tracking / always-on gating** | mem0 does NOT track LLM cost separately for the judgement call; it is **always on** when `infer=True` (the default). Setting `infer=False` ([`main.py:663-697`](../../tmp/mem0/mem0/memory/main.py#L663-L697)) bypasses both LLM calls entirely and stores raw messages verbatim. No heuristic gate; no "skip judgement if confidence high." | confirmed by reading entry points |
| **Concurrent writes** | No locking visible in `_add_to_vector_store`. mem0 assumes the vector store + SQLite handle their own concurrency. | (absence of mutex) |
| **`infer=False`** mode | The judgement call is skipped entirely. Each input message becomes an ADD (one row per message). Used for raw ingestion. | [`main.py:663-697`](../../tmp/mem0/mem0/memory/main.py#L663-L697) |

---

## 4. Test fixtures

### 4.1 Test that the prompt is wired correctly

[`tests/configs/test_prompts.py:4-19`](../../tmp/mem0/tests/configs/test_prompts.py#L4-L19) — confirms that providing a custom prompt overrides DEFAULT, and that absence falls back to DEFAULT. Confirms the prompt is treated as a string template prefix.

### 4.2 Hallucinated-ID guard tests (best end-to-end examples)

From commit `081eca6d` ([`tests/memory/test_main.py`](../../tmp/mem0/tests/memory/test_main.py)) — the cleanest fixtures showing the two-call flow + parsing of a real judgement output:

**Example A — hallucinated UPDATE skipped (no row touched):**

```python
# Existing memory: 1 row with int-id "0" → uuid "uuid-aaa"
memory.vector_store.search.return_value = [existing_mem]  # 1 candidate
memory.llm.generate_response.side_effect = [
    '{"facts": ["User likes tea"]}',                              # 1st LLM call: extract
    '{"memory": [{"id": "12", "text": "User likes tea",           # 2nd LLM call: judge
                  "event": "UPDATE", "old_memory": "User likes coffee"}]}',
]
# Expected: result == []; warning "UPDATE skipped: LLM returned unknown id 12"
# vector_store.update NOT called
```

**Example B — hallucinated DELETE skipped (no row touched):**

```python
memory.llm.generate_response.side_effect = [
    '{"facts": ["Remove coffee preference"]}',
    '{"memory": [{"id": "9", "text": "User likes coffee", "event": "DELETE"}]}',
]
# Expected: result == []; warning "DELETE skipped: LLM returned unknown id 9"
# vector_store.delete NOT called
```

**Example C — valid UPDATE on a real id (round-trip works):** same file, `test_sync_valid_id_still_processes_normally`. The LLM is fed `'{"memory": [{"id": "0", "text": "<merged>", "event": "UPDATE", "old_memory": "<old>"}]}'` and `vector_store.update` is called exactly once with `vector_id="uuid-aaa"`.

### 4.3 Pre-V3 worked-example from the prompt itself

For a porter who wants a "fresh eyes" tracing example, the prompt's own Example 2(b) is the cleanest demonstration of UPDATE-as-merge:

```
Old Memory:
    [{"id": "0", "text": "I really like cheese pizza"},
     {"id": "1", "text": "User is a software engineer"},
     {"id": "2", "text": "User likes to play cricket"}]
Retrieved facts:
    ["Loves chicken pizza", "Loves to play cricket with friends"]

Expected LLM output:
    {"memory": [
        {"id": "0", "text": "Loves cheese and chicken pizza", "event": "UPDATE",
         "old_memory": "I really like cheese pizza"},
        {"id": "1", "text": "User is a software engineer", "event": "NONE"},
        {"id": "2", "text": "Loves to play cricket with friends", "event": "UPDATE",
         "old_memory": "User likes to play cricket"}
    ]}
```

Note three things from this example:
- The LLM **merged** `"cheese pizza"` + `"chicken pizza"` → `"cheese and chicken pizza"` (not simple replace).
- It **replaced** `"likes to play cricket"` → `"Loves to play cricket with friends"` (richer phrasing wins).
- The unchanged row (`id=1`) is returned as `event=NONE` — mem0 still expects every input ID to appear in the output array (i.e. the LLM acks each one).

---

## 5. mem0's known failure modes

### 5.1 The biggest signal: mem0 themselves abandoned this design

[`README.md:48-66`](../../tmp/mem0/README.md#L48-L66) (April 2026):

> **What changed:**
> - **Single-pass ADD-only extraction** — one LLM call, no UPDATE/DELETE. Memories accumulate; nothing is overwritten.
> - **Agent-generated facts are first-class** — when an agent confirms an action, that information is now stored with equal weight.
> - **Entity linking** — entities are extracted, embedded, and linked across memories for retrieval boosting.

The benchmark deltas they publish on the same page:

| Benchmark | Old (4-event) | New (ADD-only) | Δ |
| --- | --- | --- | --- |
| LoCoMo | 71.4 | 91.6 | +20.2 |
| LongMemEval | 67.8 | 94.8 | +27.0 |

mem0 attributes these gains primarily to **removing the four-event judgement**. The implicit failure modes they call out (paraphrasing the README's framing):

1. **UPDATE loses information** — when an LLM rewrites an existing memory based on a partial new fact, nuance is destroyed. ADD-only preserves both rows; downstream retrieval reranks.
2. **Latency** — two LLM calls per write (extract + judge) → 0.88s p50 in the new design vs higher in the old (not numerically published but implied by "single-pass" framing).
3. **DELETE-based contradiction handling is too eager** — facts get killed that should have been kept as historical records (the model has no temporal awareness in the judgement prompt).

### 5.2 In-code failure modes

| Mode | Evidence |
| --- | --- |
| **LLM hallucinates an ID** | `_resolve_mapped_id` was retrofitted at PR #4674 to guard against this (issue [#3931](https://github.com/mem0ai/mem0/issues/3931)). The fix logs a warning and skips the operation — i.e. the prompt is **known to produce unreliable IDs** even with the explicit "Please note to return the IDs in the output from the input IDs only" instruction. |
| **Small LLMs return wrong shape for `facts`** | `normalize_facts` ([`memory/utils.py:84-106`](../../tmp/mem0/mem0/memory/utils.py#L84-L106)): comment says "Smaller LLMs (e.g. llama3.1:8b) sometimes return facts as objects like `{"fact": "..."}` or `{"text": "..."}` instead of plain strings." Indirect evidence that the same shape-drift happens to the judgement output, though `_resolve_mapped_id` and `event_type` dispatch quietly handle missing keys via `.get(...)`. |
| **Chatty LLMs wrap JSON in prose** | Three-stage parser (`remove_code_blocks` → direct `json.loads` → `extract_json` substring match) explicitly handles this. Commit message of `2a59c9fd`: "handle chatty LLM responses in JSON parsing". |
| **`<think>...</think>` blocks** in reasoning model output | `remove_code_blocks` strips them with `re.sub(r"<think>.*?</think>", "", ...)`. ([`memory/utils.py:121`](../../tmp/mem0/mem0/memory/utils.py#L121)). Failure mode observed in practice for reasoning models. |
| **`json_object` response_format requires the word "json"** in the prompt | `ensure_json_instruction` ([`memory/utils.py:36-58`](../../tmp/mem0/mem0/memory/utils.py#L36-L58)): "OpenAI's API requires the word 'json' to appear in the messages when response_format is set to {'type': 'json_object'}. When users provide a custom_instructions that doesn't include 'json', this causes a 400 error." |
| **NONE leaks session metadata reassignments** | The pre-V3 NONE branch quietly re-stamps `agent_id`/`run_id` on the matched memory ([`main.py:686-712` pre-V3](#pre-v3-main-py-686-712)). This is an undocumented side effect of NONE — a porter copying the prompt verbatim must NOT assume NONE is a pure no-op. |

### 5.3 Cost discipline

- **No cost tracking** for the judgement call separately.
- **No gating heuristic** — every `add()` runs both LLM calls when `infer=True`.
- **Caching**: the embedding is cached per-fact (`new_message_embeddings` dict, pre-V3 `main.py:579`) but the **prompt template itself is not cached** for prompt-caching headers.

---

## 6. Recommended Aura port — concrete file paths

(Facts only — no code proposed.)

| Aura concern | mem0 reference | Aura location |
| --- | --- | --- |
| Where the judgement prompt should land | [`mem0/configs/prompts.py:176-324`](../../tmp/mem0/mem0/configs/prompts.py#L176-L324) | A new file e.g. `internal/agent/tools/registry/propose_patch_judge.go` next to existing [`propose_patch.go`](../internal/agent/tools/registry/propose_patch.go), or a constants file under `internal/prompts/` |
| Where the routing happens | pre-V3 `_add_to_vector_store:601-712` | [`internal/agent/tools/registry/propose_patch.go:225`](../internal/agent/tools/registry/propose_patch.go) — `executeOperational` is the entry point; the judgement should run **after** the LLM-extracted patch is normalized but **before** the current direct `Insert` |
| New dependency? | None — mem0 reuses the same `self.llm.generate_response` client | **None for Aura** — `internal/llm.Client` already exposes a non-streaming completion path with `response_format` support |
| Schema change to `compact_memory_documents`? | mem0 uses `vector_store.update(id, vec, payload)` — overwrites row, keeps id | Aura: UPDATE can rewrite the `body` (and bump `updated_at`); DELETE marks `is_deleted=1` (audit trail) or hard-deletes. Most likely **no DDL change** — the existing row already has `id`, `body`, `updated_at`. Audit-trail table optional (mem0 has `db.add_history` recording old_memory + new_memory + event). |
| Cost discipline | mem0: temperature=0.1, max_tokens=2000, top_p=0.1 | Per Aura wiki convention (deterministic writes), use **temperature=0** (Aura's wiki convention from [CLAUDE.md](../CLAUDE.md) §Key Conventions). Cache the prompt template via prompt caching (Anthropic header) if porter routes through Sonnet — this is the static system prompt + sliding existing-memory context. |
| UUID→int mapping anti-hallucination | pre-V3 `main.py:594-599` | Mandatory in port. Aura's `compact_memory_documents.id` is likely an int already; even so, **renumber** to compact 0..N for the LLM to prevent it inventing IDs outside the candidate set. |
| Hallucination guard | [`main.py:64-74`](../../tmp/mem0/mem0/memory/main.py#L64-L74) | Aura must implement equivalent: `WARN` log + skip the UPDATE/DELETE silently. Do not raise to the agent — mem0's pattern is "fail closed: do nothing rather than corrupt." |

---

## 7. Open decisions for Aura

### 7.1 Per-fact vs batch
mem0 does **batch** (all new facts × all retrieved candidates in one second LLM call). Aura should follow — `propose_patch` produces one patch per call, but the judgement should still load all candidates and judge in one call to keep latency low. **Recommendation: batch with single fact = single new patch.**

### 7.2 Synchronous (block `propose_patch` return) vs async (post-turn)

mem0 is **synchronous** within `add()` — two LLM calls block the caller.

For Aura:
- The Telegram agent loop is streaming. A 1-2s additional LLM call for judgement would visibly delay the assistant's reply.
- mem0's published p50 latency for the new (single-call) pipeline is 0.88s. Their old (two-call) pipeline was higher.
- **Recommendation**: run the judgement **asynchronously after** the tool returns. `propose_patch.Execute` returns "queued for judgement" / inserts a tentative row; a background goroutine reruns the LLM judgement and may UPDATE or DELETE later. This decouples agent UX from memory-write quality.
- Risk: the same lesson may appear briefly until the async pass runs. Acceptable if the system-prompt top-10 selector tolerates a few seconds of duplication.

### 7.3 Which model?

mem0 uses **the same model** for extraction, judgement, and main agent (it's the user's configured LLM). No tiering.

For Aura:
- Sonnet 4.6 (canonical) **is overkill** for a structural classification task with a tightly constrained prompt and JSON output schema.
- Aura's project memory notes that **mini-LLM CPU is not viable for tool retrieval** (≥1500ms decoding on CPU 4t for gemma-1B/qwen-0.5B). For this judgement we're **not** doing per-tool retrieval — it's a single 1-call write-time judgement, latency budget can be a few hundred ms.
- **Open question for the porter**: candidates are (a) Sonnet 4.5 / Sonnet 4.6 (same as agent loop, costs ~$3/MTok input), (b) Haiku 4.5 (cheaper, possibly faster, but Aura doesn't currently wire it in), (c) a local model via the embedding sidecar's adjacent slot (cheapest but adds infra complexity).
- **Recommendation**: start with the same Sonnet as the agent (zero new wiring), measure p50 + cost-per-write, then decide whether to add a cheaper tier.

### 7.4 How many existing candidates to show the LLM?

mem0: per-fact `top_k=5` from vector search, deduped across facts. With 3 facts that's ≤15 candidates → typically 5-8 deduped.

Aura's `compact_memory_documents` total size after 30 days of conversations: per the trigger description, **the top-10 surfaced lessons are 10 near-duplicates** — implying the table itself has at most a few dozen distinct rows, and possibly only ~10-30 total entries.

- **Aura is small enough to send ALL rows** to the LLM in the judgement prompt with zero retrieval step. This avoids needing a vector index for `compact_memory_documents` (which Aura doesn't currently maintain — the wiki has vector embeddings, but compact memory is separate).
- **Recommendation**: send ALL rows (renumbered 0..N) up to a cap of e.g. 50. Above 50, fall back to a simple lexical filter (BM25 / token-overlap) on the new patch's `body` to pre-trim.

### 7.5 Token budget — per-judgement cap

- mem0 implicit cap: `max_tokens=2000` for the response, no input cap.
- 50 rows × ~200 chars/row ≈ 10 KB input prompt. Plus the 5 KB DEFAULT_UPDATE_MEMORY_PROMPT scaffold ≈ 15 KB total → ~4000 input tokens.
- **Recommendation**: cap candidates at 50 rows OR 8 KB of `body` text combined (whichever hits first). Response capped at 2000 tokens (mem0's default — sufficient for ~30 events).

### 7.6 Pattern E (ADD-only) — should Aura skip the four-event design entirely?

mem0's own benchmarks (LoCoMo +20pts, LongMemEval +27pts on ADD-only with hash dedup + entity linking) strongly suggest the answer is **maybe yes**. But:

- Aura's use case is **operational lessons**, not user facts. Operational lessons benefit from explicit DELETE (a lesson can become wrong as Aura's code evolves) and explicit UPDATE (lessons can be refined).
- mem0's gain came partly from **retrieval-side** improvements (entity linking, multi-signal fusion) that Aura does not need to copy.
- **Decision for the porter**: keep the four-event pattern for US-OP06 because the "stale lesson should be deleted" semantics matter for operational memory in a way they don't for user-fact memory.

### 7.7 Auditability

mem0 keeps a `db.add_history` table with `(memory_id, prev_value, new_value, event, created_at, updated_at, actor_id, role)`. Aura should mirror this — DELETE without history is destructive and the lesson dedup loop becomes unverifiable. The Aura porter should add a small `compact_memory_history` table (or `is_deleted` soft-delete flag on the existing table) when the port lands.

---

## Appendix A — File:line index of everything cited

| Concept | File:line |
| --- | --- |
| DEFAULT_UPDATE_MEMORY_PROMPT | [`mem0/configs/prompts.py:176-324`](../../tmp/mem0/mem0/configs/prompts.py#L176-L324) |
| `get_update_memory_messages` wrapper | [`mem0/configs/prompts.py:406-460`](../../tmp/mem0/mem0/configs/prompts.py#L406-L460) |
| FACT_RETRIEVAL_PROMPT | [`mem0/configs/prompts.py:15-60`](../../tmp/mem0/mem0/configs/prompts.py#L15-L60) |
| USER_MEMORY_EXTRACTION_PROMPT | [`mem0/configs/prompts.py:63-121`](../../tmp/mem0/mem0/configs/prompts.py#L63-L121) |
| `_resolve_mapped_id` guard | [`mem0/memory/main.py:64-74`](../../tmp/mem0/mem0/memory/main.py#L64-L74) |
| `_create_memory` | [`mem0/memory/main.py:1586-1616`](../../tmp/mem0/mem0/memory/main.py#L1586-L1616) |
| `_update_memory` | [`mem0/memory/main.py:1657-1720`](../../tmp/mem0/mem0/memory/main.py#L1657-L1720) |
| `_delete_memory` | [`mem0/memory/main.py:1722-1750`](../../tmp/mem0/mem0/memory/main.py#L1722-L1750) |
| Pre-V3 dispatch loop | `mem0/memory/main.py:485-720` at commit `a488e190^` |
| Current ADD-only V3 pipeline | [`mem0/memory/main.py:662-971`](../../tmp/mem0/mem0/memory/main.py#L662-L971) |
| `remove_code_blocks` / `extract_json` | [`mem0/memory/utils.py:109-142`](../../tmp/mem0/mem0/memory/utils.py#L109-L142) |
| `normalize_facts` | [`mem0/memory/utils.py:84-106`](../../tmp/mem0/mem0/memory/utils.py#L84-L106) |
| Default temperature 0.1 | [`mem0/configs/llms/base.py:19`](../../tmp/mem0/mem0/configs/llms/base.py#L19) |
| Default max_tokens 2000 | [`mem0/configs/llms/openai.py:43`](../../tmp/mem0/mem0/configs/llms/openai.py#L43) |
| README ADD-only switch | [`README.md:48-66`](../../tmp/mem0/README.md#L48-L66) |
| Hallucinated-ID guard tests | tests added in commit `081eca6d` to `tests/memory/test_main.py` |
| Apache-2.0 license | [`LICENSE`](../../tmp/mem0/LICENSE) |
