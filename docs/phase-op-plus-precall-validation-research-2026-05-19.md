# Pre-call tool-parameter validation — survey of external codebases

Research dump for Aura's planned US-OP12 (pre-call validator middleware in `internal/agent/executor.go`). Question: how do other agentic frameworks validate tool parameters *before* invocation, and what error shape do they feed back to the LLM?

Five archetypal shapes referenced throughout:

1. **Strong pre-call** — explicit schema validator runs in a middleware before invoke; structured error.
2. **Weak pre-call** — type-coerce + required-key check inline, no formal schema.
3. **No pre-call** — only post-call validation in the tool body.
4. **Type-system pre-call** — Pydantic / TypeBox / Zod deserialization IS the validation (raises on bad input).
5. **Retry-with-feedback** — validation error structured, fed back to the model for a retry.

---

## Per-codebase findings

### elysia (added 2026-05-19)

**Stack**: Python 3.11+, FastAPI server, DSPy for the LLM agent loop, Pydantic for typed objects, optional Weaviate retrieval. License: **BSD-3-Clause** (Weaviate B.V.).

**Architecture in one line**: a *decision tree* where each `DecisionNode` runs a DSPy `Signature` (`DecisionPrompt`) that emits `function_name: str` + `function_inputs: dict[str, Any]`; the chosen tool is then invoked as an async generator.

#### Where parameters get from the LLM to the tool

1. **Tool registration** ([`elysia/objects.py:374-417`](file:///D:/tmp/elysia/elysia/objects.py#L374)) — the `@tool` decorator introspects the function signature (`inspect.signature` + `function.__annotations__`) and synthesises a metadata dict:
   ```python
   inputs={
       input_key: {
           "description": "",
           "type": ("Not specified" if input_value is None else input_value),
           "default": defaults_mapping.get(input_key, None),
           "required": defaults_mapping.get(input_key, None) is not None,
       }
       …
   }
   ```
   Note: `required` is set when a default **exists**, which looks inverted vs. the usual convention — but the field is informational, never enforced as a gate.

2. **Schema shown to the LLM** ([`elysia/tree/util.py:293-320`](file:///D:/tmp/elysia/elysia/tree/util.py#L293)) — `_options_to_json` flattens each tool to `{function_name, description, inputs}`. If an input's `type` is a Pydantic model (`hasattr(input_dict["type"], "model_json_schema")`), the JSON Schema properties are inlined into the prompt as a string:
   ```python
   type_overwrite = f"A JSON object of the following properties: {input_dict['type'].model_json_schema()['properties']}"
   ```
   So the LLM sees the schema, but no validator runs against the LLM's output.

3. **LLM call** ([`elysia/tree/prompt_templates.py:5-145`](file:///D:/tmp/elysia/elysia/tree/prompt_templates.py#L5)) — `DecisionPrompt(dspy.Signature)` declares `function_name: str = dspy.OutputField(...)` and `function_inputs: dict[str, Any] = dspy.OutputField(...)`. DSPy's adapter parses the LLM response into those types. **That is the only "validation" of `function_inputs`** — a coarse `dict[str, Any]` cast. There is no per-tool input-schema validator.

4. **Wrapped in an assertion loop** ([`elysia/tree/util.py:152-215`](file:///D:/tmp/elysia/elysia/tree/util.py#L152)) — `AssertedModule` re-runs the DSPy module up to `max_tries=2` times if a user-supplied `assertion(kwargs, pred) -> (bool, feedback_str)` returns False. The retry passes `previous_feedbacks` and `previous_attempts` back into the module on the next call. This is a generic retry-with-feedback harness — but the only assertion wired in for tool dispatch is `_tool_assertion`:
   ```python
   def _tool_assertion(self, kwargs, pred):
       return (
           pred.function_name in self.options,
           f"You picked the action `{pred.function_name}` - that is not in `available_actions`! "
           f"Your output MUST be one of the following: {list(self.options.keys())}",
       )
   ```
   ([`elysia/tree/util.py:377-382`](file:///D:/tmp/elysia/elysia/tree/util.py#L377)) — it only checks `function_name` membership, **NOT** that `function_inputs` matches the tool's declared schema.

5. **Pre-call massage, not validation** ([`elysia/tree/tree.py:461-479`](file:///D:/tmp/elysia/elysia/tree/tree.py#L461) and [`tree.py:1619-1622`](file:///D:/tmp/elysia/elysia/tree/tree.py#L1619)) — `_get_function_inputs(tool_name, inputs)` does two things before the invocation:
   - fills missing keys with the function's Python defaults
   - if the model emitted `{key: {"description": …, "type": …, "default": …, "value": X}}` (echoing the schema back), unwrap to `X`
   No type-checking, no required-key enforcement, no enum check.

6. **Invocation** ([`elysia/tree/tree.py:1664-1684`](file:///D:/tmp/elysia/elysia/tree/tree.py#L1664)) — the chosen tool is called directly:
   ```python
   async for result in action_fn(
       tree_data=self.tree_data,
       inputs=self.current_decision.function_inputs,
       …
   ):
       action_result, error = await self._evaluate_result(result, self.current_decision)
   ```
   The tool's body is responsible for handling/raising on bad input. If it yields an `Error`, that error is captured.

7. **Post-call retry-with-feedback** ([`elysia/tree/tree.py:1273-1297`](file:///D:/tmp/elysia/elysia/tree/tree.py#L1273)) — `_add_error` accumulates errors per `function_name` into `tree_data.errors[function_name]`. On the next decision-tree turn, those go back to the LLM through `previous_errors: list[dict] = dspy.InputField(...)` ([`prompt_templates.py:95-105`](file:///D:/tmp/elysia/elysia/tree/prompt_templates.py#L95)) with one of two prefix strings ("Avoidable error: …" if the tool returned structured feedback, "Unknown error: …" if it raised plain). This IS retry-with-feedback — but the retry happens on the **next turn**, not in the same turn, and it triggers only after a wasted tool execution.

#### Pattern classification

| Aspect | elysia's shape |
|---|---|
| Tool-name validation | **Strong pre-call** (`_tool_assertion`, same-turn retry via `AssertedModule`, max_tries=2) |
| Tool-input validation | **Type-system pre-call (very weak)** — only `dict[str, Any]` cast by DSPy; no per-tool schema validator |
| Default-filling / shape-coercion | Inline in `_get_function_inputs`, runs before invoke |
| Post-call error feedback | **Retry-with-feedback across turns** — `previous_errors` field in next LLM call |
| Error shape | Free-text feedback string + tool name; not structured per-field |

elysia is a **3 (No pre-call for inputs) + 5 (Retry-with-feedback) hybrid**, with strong pre-call only for the tool-name choice.

#### Notable patterns Aura could borrow

1. **`AssertedModule` is a clean reusable retry harness** ([`util.py:152-197`](file:///D:/tmp/elysia/elysia/tree/util.py#L152)) — generic `(assertion_fn, max_tries)` wrapper that loops a DSPy call while threading `previous_feedbacks` + `previous_attempts` back into the next call as additional context. This is structurally what US-OP12 wants for the validation-feedback case. The model gets to see *its own previous wrong attempt* alongside the feedback, which is more than just an error string.

2. **Per-tool Pydantic schema rendered in the LLM-visible prompt** ([`util.py:311-316`](file:///D:/tmp/elysia/elysia/tree/util.py#L311)) — when a tool's input type is a Pydantic model, the JSON Schema is flattened into the action description the LLM sees. Aura already exposes JSON Schema to the LLM via tool definitions; the elysia twist is that the same schema object that's shown to the LLM is the one the codebase *could* validate against — though elysia doesn't, the wiring is one line away.

3. **Per-tool error history keyed by `function_name`** ([`tree.py:1273-1297`](file:///D:/tmp/elysia/elysia/tree/tree.py#L1273)) — accumulated in `tree_data.errors[function_name]: list[str]`, surfaced as `previous_errors` input field next turn. The "Avoidable" vs "Unknown" prefix is a tiny but useful signal: avoidable = "model's fault, can re-try", unknown = "external, maybe pivot". Aura's structured ValidationError could carry the same `recoverable: bool` hint.

#### What elysia does NOT do

- No `jsonschema.validate()` or equivalent against `function_inputs` before invoke.
- No Pydantic `model_validate()` on the LLM's emitted args even when the declared `type` is a Pydantic model — the schema is rendered to the prompt but not enforced on the way back.
- No per-field error shape. The post-call error is a single free-text string.
- No same-turn retry on bad inputs — only on bad `function_name`. Bad inputs cost a full tool execution.

#### Verdict for Aura's US-OP12

elysia does **not** suggest pivoting US-OP12 — it suggests it's a real gap. elysia has the schema metadata in hand (Pydantic models attached to tool inputs) and even shows it to the LLM, yet skips the obvious pre-call `model_validate()`. The cost: every malformed input pays a full tool execution + a turn delay before the model sees the error. This is exactly the wasted-roundtrip Aura wants to avoid.

The reusable pattern is **`AssertedModule`'s shape**, not its content: a generic `(predict, assert, max_tries, thread_previous_attempts)` loop where the assertion is plugged in per-call. For Aura's executor, the equivalent would be: validate args with the tool's JSON Schema, and on failure feed back `{tool, field, expected, got, previous_attempt}` to the model *in the same turn* (not next turn — elysia's weakness). The structured per-field shape is what elysia is missing, and what US-OP12 should ship.

One small concrete steal: the **"Avoidable error" vs "Unknown error" labeling** at [`tree.py:1284-1297`](file:///D:/tmp/elysia/elysia/tree/tree.py#L1284) is a nice cheap signal. Aura's ValidationError can carry `recoverable: true` (schema mismatch the model can fix) vs `recoverable: false` (HTTP 500 from a downstream service, model can't fix by re-emitting args).

---

### openhuman (Rust) — GPLv3 (added 2026-05-19)

**Pattern: hybrid #4 (typed deserialize, per-tool opt-in) + #3 (no centralized pre-call).** No single middleware. The tool loop dispatches `serde_json::Value` directly to `tool.execute(args)`.

#### Tool trait

The `Tool` trait ([`tools/traits.rs:114-126`](file:///D:/tmp/openhuman/src/openhuman/tools/traits.rs#L114)) exposes `parameters_schema() -> serde_json::Value` for LLM-side advertisement but has NO `validate` method. `execute(args: Value) -> Result<ToolResult>` is the only entry — whatever JSON the model emits arrives raw.

```rust
async fn execute(&self, args: serde_json::Value) -> anyhow::Result<ToolResult>;
```

#### Tool loop — no validation step

In [`agent/harness/tool_loop.rs:648-654`](file:///D:/tmp/openhuman/src/openhuman/agent/harness/tool_loop.rs#L648):

```rust
let outcome =
    tokio::time::timeout(tool_deadline, tool.execute(call.arguments.clone())).await;
```

Look up the tool by name, time-bound it, invoke. No `validate()` between dispatch and execute.

#### Per-tool: TWO distinct styles co-exist

**Style A — typed-struct deserialize (pattern #4):**
[`tools/impl/whatsapp_data/list_chats.rs:51-56`](file:///D:/tmp/openhuman/src/openhuman/tools/impl/whatsapp_data/list_chats.rs#L51):
```rust
async fn execute(&self, args: serde_json::Value) -> anyhow::Result<ToolResult> {
    let req: ListChatsRequest = serde_json::from_value(args).map_err(|e| {
        anyhow::anyhow!("invalid arguments for whatsapp_data_list_chats: {e}")
    })?;
    ...
}
```
serde does field-presence, type, and (with `#[serde(deserialize_with)]`) custom validators in one pass. Error messages like `missing field 'account_id'` or `invalid type: integer 5, expected a string` come for free, but they are not customised for LLM consumption.

**Style B — ad-hoc inline (pattern #3):**
[`tools/impl/filesystem/file_read.rs:48-52`](file:///D:/tmp/openhuman/src/openhuman/tools/impl/filesystem/file_read.rs#L48):
```rust
async fn execute(&self, args: serde_json::Value) -> anyhow::Result<ToolResult> {
    let path = args
        .get("path")
        .and_then(|v| v.as_str())
        .ok_or_else(|| anyhow::anyhow!("Missing 'path' parameter"))?;
    ...
}
```
Hand-rolled per-arg check. Most file/shell/network tools follow this style; only the typed-struct-heavy modules (whatsapp_data, memory/tree) use Style A.

#### Error shape back to model

[`tool_loop.rs:779`](file:///D:/tmp/openhuman/src/openhuman/agent/harness/tool_loop.rs#L779):
```rust
(format!("Error executing {}: {e}", call.name), false)
```
Then formatted via the dispatcher as `<tool_result name="…" status="error">…</tool_result>` ([`dispatcher.rs:108-116`](file:///D:/tmp/openhuman/src/openhuman/agent/dispatcher.rs#L108)). String passthrough, no structured event, no machine-readable error code, no "retry hint" text. The model corrects from prose.

#### Note on `tools/schema.rs`

The 540-LOC [`tools/schema.rs`](file:///D:/tmp/openhuman/src/openhuman/tools/schema.rs) is NOT a parameter validator — it's a schema-CLEANER that strips provider-unsupported JSON Schema keywords (Gemini rejects `minLength`, `pattern`, etc.) from the outbound tool definition. Different concern entirely.

#### Verdict

openhuman shows that pattern #4 works in Rust but is **per-tool opt-in**, not a framework rule. In Go this idiom is awkward (no serde equivalent in the stdlib; `mapstructure` adds a dependency + per-tool decoder boilerplate). Concept is portable, code is not (GPLv3 anyway).

---

### nanobot (Python) — MIT (added 2026-05-19)

**Pattern: #1 strong pre-call + #5 retry-with-feedback combined.** This is the textbook implementation.

#### Validator lives on the `Tool` ABC

[`agent/tools/base.py:243-250`](file:///D:/tmp/nanobot/nanobot/agent/tools/base.py#L243):
```python
def validate_params(self, params: dict[str, Any]) -> list[str]:
    if not isinstance(params, dict):
        return [f"parameters must be an object, got {type(params).__name__}"]
    schema = self.parameters or {}
    if schema.get("type", "object") != "object":
        raise ValueError(f"Schema must be object type, got {schema.get('type')!r}")
    return Schema.validate_json_schema_value(params, {**schema, "type": "object"}, "")
```

The real walker `Schema.validate_json_schema_value` ([`base.py:48-101`](file:///D:/tmp/nanobot/nanobot/agent/tools/base.py#L48)) is **~55 lines** and supports:
- `type`: string / integer / number / boolean / array / object with `["T", "null"]` nullable unions
- `required` (object members)
- `enum` (value must be in list)
- `minimum` / `maximum` (numeric range)
- `minLength` / `maxLength` (string length)
- `minItems` / `maxItems` (array length)
- recursive walk into `properties` and `items`
- field-path errors like `request.filters[2].id should be string`

#### Coercion before validation

Paired with `Tool.cast_params` ([`base.py:198-241`](file:///D:/tmp/nanobot/nanobot/agent/tools/base.py#L198)) — **safe schema-driven casts BEFORE validate**. Examples:
- `"5"` → `5` when schema says `integer` or `number`
- `"true"` / `"yes"` / `"1"` → `True` when schema says `boolean`
- recursive into nested objects and arrays

Without this, LLMs would false-fail validation on every stringified numeric. (Empirically a high-frequency failure on llama.cpp, GPT-OSS, Mistral.)

#### Where in dispatch — the canonical "prepare_call" shim

[`agent/tools/registry.py:73-114`](file:///D:/tmp/nanobot/nanobot/agent/tools/registry.py#L73):
```python
def prepare_call(self, name: str, params: dict[str, Any]) -> tuple[Tool | None, dict[str, Any], str | None]:
    """Resolve, cast, and validate one tool call."""
    if not isinstance(params, dict) and name in ('write_file', 'read_file'):
        return None, params, (
            f"Error: Tool '{name}' parameters must be a JSON object, got {type(params).__name__}. "
            "Use named parameters: tool_name(param1=\"value1\", param2=\"value2\")"
        )
    tool = self._tools.get(name)
    if not tool:
        return None, params, (
            f"Error: Tool '{name}' not found. Available: {', '.join(self.tool_names)}"
        )
    cast_params = tool.cast_params(params)
    errors = tool.validate_params(cast_params)
    if errors:
        return tool, cast_params, (
            f"Error: Invalid parameters for tool '{name}': " + "; ".join(errors)
        )
    return tool, cast_params, None

async def execute(self, name: str, params: dict[str, Any]) -> Any:
    _HINT = "\n\n[Analyze the error above and try a different approach.]"
    tool, params, error = self.prepare_call(name, params)
    if error:
        return error + _HINT
    try:
        assert tool is not None
        result = await tool.execute(**params)
        ...
        return result
    except Exception as e:
        return f"Error executing {name}: {str(e)}" + _HINT
```

Three-step prepare: name lookup → cast → validate → return early on error. Otherwise call `tool.execute(**params)`. Same hint string `"[Analyze the error above and try a different approach.]"` appended to EVERY error path (validation, name-unknown, runtime).

#### Runner side: dual-channel error reporting

[`agent/runner.py:801-825`](file:///D:/tmp/nanobot/nanobot/agent/runner.py#L801):
```python
prepare_call = getattr(spec.tools, "prepare_call", None)
tool, params, prep_error = None, tool_call.arguments, None
if callable(prepare_call):
    with suppress(Exception):
        prepared = prepare_call(tool_call.name, tool_call.arguments)
        if isinstance(prepared, tuple) and len(prepared) == 3:
            tool, params, prep_error = prepared
if prep_error:
    event = {
        "name": tool_call.name,
        "status": "error",
        "detail": prep_error.split(": ", 1)[-1][:120],
    }
    ...
    return prep_error + hint, event, ...
```

Two parallel error channels:
- **Tool-result message (LLM-visible)** — prose with `_HINT` suffix. The model self-corrects.
- **Progress event (callback-visible)** — structured `{"name": tool, "status": "error", "detail": "..."}`. The UI / telemetry shows it; the LLM doesn't.

Excellent split: structured for ops, prose for the model.

#### Per-tool override pattern

Subclasses extend by calling `super()` then adding domain checks. [`agent/tools/cron.py:125-126`](file:///D:/tmp/nanobot/nanobot/agent/tools/cron.py#L125):
```python
def validate_params(self, params: dict[str, Any]) -> list[str]:
    errors = super().validate_params(params)
    # ... add cron-expression validity check, returns extra errors
```

#### Verdict

This is the canonical reference. The pattern is small (~55 LOC walker + ~40 LOC coercion + ~30 LOC prepare_call), composable (per-tool overrides), and pairs validation with a fixed retry-hint string. The "Analyze the error" hint at [`registry.py:101`](file:///D:/tmp/nanobot/nanobot/agent/tools/registry.py#L101) is a 30-LOC standalone win Aura could ship today.

---

### picobot (Go) — MIT (added 2026-05-19)

**Pattern: #3 (no pre-call) + ad-hoc post-call.** Aura's closest peer in Go shape.

#### Tool interface — identical to Aura today

[`internal/agent/tools/registry.go:15-22`](file:///D:/tmp/picobot/internal/agent/tools/registry.go#L15):
```go
type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]interface{}
    Execute(ctx context.Context, args map[string]interface{}) (string, error)
}
```

No `Validate` method.

#### Registry executor — bare dispatch

[`registry.go:65-91`](file:///D:/tmp/picobot/internal/agent/tools/registry.go#L65):
```go
func (r *Registry) Execute(ctx context.Context, name string, args map[string]interface{}) (string, error) {
    if name == "" { return "", errors.New("tool name is required") }
    r.mu.RLock()
    t, ok := r.tools[name]
    r.mu.RUnlock()
    if !ok { return "", errors.New("tool not found") }
    log.Printf("[tool] → %s %s", name, argsJSON)
    result, err := t.Execute(ctx, args)
    ...
}
```

Look up, log, call. No validate step.

#### Per-tool: hand-rolled inline checks

[`internal/agent/tools/filesystem.go:62-83`](file:///D:/tmp/picobot/internal/agent/tools/filesystem.go#L62):
```go
func (t *FilesystemTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
    actionRaw, ok := args["action"]
    if !ok {
        return "", fmt.Errorf("filesystem: 'action' is required")
    }
    action, ok := actionRaw.(string)
    if !ok {
        return "", fmt.Errorf("filesystem: 'action' must be a string")
    }
    pathRaw := args["path"]
    ...
}
```

Required + type assertion duplicated across every tool. Aura's current pattern exactly.

#### Error shape back to model

[`internal/agent/loop.go:359-364`](file:///D:/tmp/picobot/internal/agent/loop.go#L359):
```go
result, err := a.tools.Execute(ctx, tc.Name, tc.Arguments)
if err != nil {
    result = "(tool error) " + err.Error()
}
messages = append(messages, providers.Message{Role: "tool", Content: result, ToolCallID: tc.ID})
```

Bare string `(tool error) ...`. No hint. No structured channel. The model self-corrects from prose, but the wording is whatever the tool author wrote inline.

#### Verdict

picobot is the **baseline Aura inherits from**. It works but has all the cost Aura wants to eliminate: HTTP roundtrip wasted on bad args, error wording inconsistent, duplicated boilerplate. The shape to move toward is nanobot's, on top of an interface that already looks like picobot's.

---

### mem0 (Python) — Apache-2.0 (added 2026-05-19)

**Skipped — not relevant.** mem0 is a memory store/retrieval library, not an agent harness. It exposes LLM provider adapters (`mem0/llms/*.py`) and cookbooks demonstrating integration with autogen / langchain / etc. Tool-dispatch + parameter validation are delegated to whichever host runs the tools. No pre-call pattern of its own to extract.

---

### codex (OpenAI Codex CLI) — only `D:/tmp/codex.md` blog snapshot in tree

**Pattern: #1 implied via OpenAI Responses API platform-level enforcement.** No source tree present. The blog ([`codex.md:90-98`](file:///D:/tmp/codex.md#L90)) describes the `tools` field as "a list of tool definitions that conform to a schema defined by the Responses API." When the model is configured with `strict: true`, the OpenAI server constrains tool-call output to schema-valid JSON server-side. Client-side pre-call validation is redundant under strict mode.

**Aura cannot rely on this.** Aura uses an OpenAI-compatible HTTP shim that talks to llama.cpp / GPT-OSS / Mistral / etc. — none of which enforce strict-mode schemas. The codex pattern is "trust the provider" and only works for the OpenAI-native flow.

No code citations available; blog is high-level.

---

### cli-printing-press (Go) — MIT (added 2026-05-19)

**Skipped — not relevant.** This is a Go program that manipulates MCP/skill descriptor JSON (rewriting descriptions, normalizing argument schemas, etc.) but doesn't run an agent loop or dispatch tools. Confirmed: zero hits for `validate|invoke|Execute` in `internal/mcpdesc/`. No relevant pattern to extract.

---

## Synthesis across all six codebases

| Codebase | Pattern | Validator location | Error shape to model | License |
|----------|---------|---------------------|----------------------|---------|
| elysia | 3 + 5 (cross-turn retry) | tool-name only, `_tool_assertion` | free-text `previous_errors` next turn | BSD-3 |
| openhuman | 4 (per-tool opt-in) + 3 (default) | inside each tool.execute | `format!("Error executing {name}: {e}")` prose | GPLv3 |
| nanobot | **1 + 5 (same-turn)** | `prepare_call` in registry | structured prose + fixed retry hint + structured event channel | **MIT** |
| picobot | 3 | inside each tool.Execute | `(tool error) ...` prose, no hint | MIT |
| mem0 | n/a | n/a | n/a | Apache-2.0 |
| codex (CLI blog) | 1 (server-side strict mode) | OpenAI Responses API | platform reject before client sees it | OpenAI platform |

### Field reality

- Pattern #5 (retry-with-feedback) is **universal** — every framework feeds errors back to the model in some shape.
- Pattern #1 (strong pre-call) is **clean in one codebase out of six** (nanobot). It's not a field default — but it IS the most polished design in the survey.
- Pattern #4 (type-system) is **Rust-specific in practice**; in Go you'd need `mapstructure` + per-tool decoder structs, which is more surface area than a JSON Schema walker.
- Pattern #3 (no pre-call) is the **dominant default** (openhuman, picobot, elysia for inputs). Works but pays the wasted-roundtrip cost on every malformed call.

## Verdict for Aura US-OP12

**Ship US-OP12 mostly as-designed, with two pivots:**

### Pivot 1 — Location

**Move the validator from `internal/agent/executor.go` to the tool registry's execute path** (Aura's `Registry.Execute` in `internal/tools/registry.go` — or wherever `Execute(ctx, name, args)` lives). Nanobot puts validation in `prepare_call` at the registry layer. Reasons:

- Every Tool consumer benefits, not just the agent executor (CLI, RPC, MCP-side calls).
- Single point of enforcement = single point of telemetry.
- Doesn't couple validation to executor internals; the executor just calls `registry.Execute` and inherits the new behavior.

### Pivot 2 — Add coercion before validation

LLMs emit stringified numerics roughly 1 in 30 turns (Mistral, GPT-OSS, llama.cpp). Without coercion, US-OP12 will regress by false-failing those. Port nanobot's `cast_params` shape ([`base.py:198-241`](file:///D:/tmp/nanobot/nanobot/agent/tools/base.py#L198)):

```go
// coerceArgs walks schema + args, attempts safe casts (string→int, string→bool, ...)
func coerceArgs(schema, args map[string]any) map[string]any
```

Then call `validate(schema, coerced)`. If validation fails, return error WITH the coerced args attached for telemetry (so we can see whether the LLM emitted the wrong shape or whether coercion still couldn't save it).

### Concrete shape

```go
// internal/tools/registry.go
type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]any
    Execute(ctx context.Context, args map[string]any) (string, error)

    // Optional. Default impl walks Parameters() schema.
    // Returns list of field-path-prefixed error strings.
    // Tools needing custom rules (cron-expr, glob, etc.) override.
    Validate(args map[string]any) []string
}

func (r *Registry) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
    t, ok := r.tools[name]
    if !ok { return "", fmt.Errorf("Tool %q not found. Available: %s", name, r.listNames()) }

    coerced := coerceArgs(t.Parameters(), args)
    if errs := t.Validate(coerced); len(errs) > 0 {
        return formatValidationError(name, errs), nil // err==nil; payload IS the error message
    }

    return t.Execute(ctx, coerced)
}

func formatValidationError(name string, errs []string) string {
    return fmt.Sprintf(
        "Error: Invalid parameters for tool %q: %s\n\n[Analyze the error above and try a different approach.]",
        name, strings.Join(errs, "; "),
    )
}
```

The validator subset (~150-200 LOC):
- `type` (string / integer / number / boolean / array / object), nullable union
- `required` for object members
- `enum` membership
- `minimum` / `maximum` (numeric)
- `minLength` / `maxLength` (string)
- recursive walk into `properties` and `items`
- field-path threading (`request.filters[2].id should be string`)

DO NOT enforce `additionalProperties: false` unless schema explicitly says so — open schemas dominate Aura, and the LLM commonly emits stray keys (chain-of-thought hints, provider metadata) that don't break execute. Strict mode would false-fail.

### AC changes vs. the original US-OP12 plan

The plan in [phase-op-plus-plan-2026-05-19.md](./phase-op-plus-plan-2026-05-19.md) §2 should be updated:

| Original AC | Revised |
|---|---|
| Location: `internal/agent/executor.go` middleware | **Location: `internal/tools/registry.go` `Registry.Execute`** |
| Validate against `tool.Parameters()` | + **Coerce first** via `coerceArgs(schema, args)` before validate |
| Return structured `ValidationError` to LLM | Return a string tool-result message in nanobot's shape: `Error: Invalid parameters for tool 'X': field1 reason; field2 reason\n\n[Analyze the error above and try a different approach.]` |
| Reject hard on schema mismatch | Soft-fail: if schema itself is malformed (Aura author bug, not LLM bug), log warning + skip validation, proceed to Execute. Don't brick the tool because the schema is broken. |
| (new) | Add an opt-out `Validate(args) []string` method on `Tool` with registry-driven default — mirrors nanobot's [`cron.py:125`](file:///D:/tmp/nanobot/nanobot/agent/tools/cron.py#L125). Tools with domain-specific rules override; everyone else inherits the default. |
| (new) | Add structured log fields `{tool, errors[], coerced_args}` for telemetry — matches nanobot's progress-event channel. |

### Bonus side-slice — US-OP12b: universal retry hint

Independent of US-OP12 proper, ship a ~30 LOC change appending nanobot's `_HINT = "\n\n[Analyze the error above and try a different approach.]"` to ALL tool errors (validation, execution, timeout, MCP). This is a field-proven self-correction primer. Worth a separate commit because the win is universal and doesn't require schema infrastructure.

### What NOT to do

- **Don't go pattern #4** (serde-style typed structs via `mapstructure` or generics). Adds per-tool boilerplate; error messages need re-wrapping for LLM consumption. Pattern #1 is cleaner for Aura's `map[string]any` reality.
- **Don't pull in a full JSON Schema validator** (`xeipuuv/gojsonschema`, ~2k LOC + 8 transitive deps). Aura's schemas use a tiny subset; ~200 LOC clean-room walker covers 100% of in-tree usage.
- **Don't enforce `additionalProperties: false`** unless schema explicitly says so.
- **Don't break tools with empty `Parameters()`.** Short-circuit on `len(schema) == 0`.
- **Don't merge validation feedback across turns** (elysia's `previous_errors` cross-turn pattern). Same-turn return is cheaper and clearer — the model sees the error in the same tool_result slot.

### Estimated effort

| Component | LOC |
|---|---|
| `validateArgs(schema, args) []string` walker | ~150 |
| `coerceArgs(schema, args) map[string]any` | ~80 |
| `Tool.Validate` interface extension + default impl | ~30 |
| Wire into `Registry.Execute` | ~30 |
| Tests (table-driven walker + coercer) | ~120 |
| Cleanup: drop now-redundant inline checks in 2-3 existing tools | -40 |
| **Net** | **~370 added, ~40 removed, fits original US-OP12 budget** |

## License audit

| Pattern source | License | Port mode |
|---|---|---|
| nanobot `validate_json_schema_value` (55-LOC walker) | **MIT** | **Code-portable.** Small enough to clean-room rewrite from the Python; MIT-attribution paste is also legal. |
| nanobot `cast_params` / `_cast_value` | MIT | Same. |
| nanobot `prepare_call` registry shape | MIT | Concept + 30-line structure; trivial Go translation. |
| nanobot `_HINT` retry-hint string | MIT | Copy-paste safe (with attribution in commit msg). |
| openhuman serde-from_value pattern | **GPLv3** | **Concepts only.** Don't copy code. Pattern doesn't map to Go's `map[string]any` anyway. |
| openhuman tool-loop error wrapping | GPLv3 | Concepts only. Aura already does similar wrapping. |
| picobot post-call inline validation | MIT | Already in Aura today — no port needed. |
| elysia `AssertedModule` retry harness | BSD-3 | Concepts only — Aura's same-turn shape is cleaner than elysia's cross-turn shape. |
| elysia `previous_errors` field | BSD-3 | Concept reject (cross-turn delay = the cost Aura wants to remove). |
| codex Responses-API strict mode | OpenAI platform | Not portable — server-side. |

**Recommendation**: Implement US-OP12 as a clean-room Go translation of nanobot's three pieces:
1. `Schema.validate_json_schema_value` → `validateArgs` (~150 LOC)
2. `Tool.cast_params` → `coerceArgs` (~80 LOC)
3. `Registry.prepare_call` → integration into `Registry.Execute` (~30 LOC)

Both the algorithm and call site are MIT and well within port-ability. Nanobot is the single best reference doc for the implementation; openhuman/picobot/elysia are useful as negative space (what NOT to do — leave bad args to runtime).

## Cross-doc references

- [phase-op-plus-plan-2026-05-19.md](./phase-op-plus-plan-2026-05-19.md) §2 US-OP12 — the original design; AC updates above.
- [phase-op-plus-openhuman-research-2026-05-19.md](./phase-op-plus-openhuman-research-2026-05-19.md) — focused on memory dedup / post-turn hooks; orthogonal axis, no overlap with this doc.
- [phase-op-plus-web-research-2026-05-19.md](./phase-op-plus-web-research-2026-05-19.md) — priority pinning / recency; orthogonal.
- [phase-op-plus-mem0-research-2026-05-19.md](./phase-op-plus-mem0-research-2026-05-19.md) — mem0 memory model; orthogonal.

