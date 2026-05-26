# OH3 Research — AWS Strands Swarm + LangGraph Swarm (2026-05-25)

Scout #2 of 4 for Wave OH3 (peer-mesh agent swarm with runtime communication loop).
Focus: two production multi-agent frameworks that ship a `Swarm` primitive in 2026.
All API names, defaults, and code shapes below are verified from the upstream source on `main` (May 2026), not from blog posts.

---

## 1. TL;DR — most lift-worthy primitives for Aura

1. **Strands `SwarmState.should_continue()` is the cleanest Go-portable termination contract.** Five orthogonal stop conditions in one function: `max_handoffs`, `max_iterations`, `execution_timeout`, `repetitive_handoff_detection_window`, `repetitive_handoff_min_unique_agents`. Returns `(bool, reason)`. We can lift this 1:1 into a `swarmpolicy.go` and reuse the cycle-detector pattern OH2 already shipped.
2. **Strands' tool-injection model for the handoff primitive maps perfectly onto Aura's tool registry.** At swarm init, Strands runs `_inject_swarm_tools()` to add a `handoff_to_agent(agent_name, message, context)` tool to every node's registry. Aura's `internal/agent/tools/registry` already supports per-loop tool sets — same pattern, no new substrate.
3. **Termination = absence of handoff, not a "done" signal.** Both frameworks converged on the same answer: if the active agent finishes its turn *without* calling the handoff tool, the swarm is COMPLETED. No `complete_swarm_task` tool. No explicit nominate-self-as-terminal. This is one fewer concept than the prompt assumed.
4. **LangGraph's "active_agent" sticky state + `Command(goto=..., graph=Command.PARENT, update=...)` is the right blackboard shape**, but the Pregel/superstep machinery underneath does NOT port to Go cheaply. Lift the *contract* (typed shared state + reducer for `messages`, scalar pointer for `active_agent`), not the runtime.
5. **Parallel fan-out is NOT in either Swarm.** Both ship a *separate* primitive for it: Strands has `Graph` (DAG with conditional edges), LangGraph has the `Send` API + supersteps. For OH3 gap #3 (parallel fan-out), reach for a DAG/Graph primitive separate from the peer-mesh loop — don't try to bolt concurrency onto the handoff loop.

---

## 2. Strands Swarm (AWS, `strands-agents/sdk-python`)

Source: `src/strands/multiagent/swarm.py` on `main` (May 2026). Verified via `gh api`.

### 2.1 API surface (verified signatures)

```python
class Swarm(MultiAgentBase):
    def __init__(
        self,
        nodes: list[Agent],
        *,
        entry_point: Agent | None = None,
        max_handoffs: int = 20,
        max_iterations: int = 20,
        execution_timeout: float = 900.0,        # seconds
        node_timeout: float = 300.0,             # seconds
        repetitive_handoff_detection_window: int = 0,   # 0 = disabled
        repetitive_handoff_min_unique_agents: int = 0,
        session_manager: SessionManager | None = None,
        hooks: list[HookProvider] | None = None,
        id: str = "default_swarm",
        trace_attributes: Mapping[str, AttributeValue] | None = None,
        plugins: list[MultiAgentPlugin] | None = None,
    ) -> None: ...

    def __call__(self, task, invocation_state=None, **kw) -> SwarmResult: ...
    async def invoke_async(self, task, invocation_state=None, **kw) -> SwarmResult: ...
    async def stream_async(self, task, invocation_state=None, **kw) -> AsyncIterator[dict]: ...
    def serialize_state(self) -> dict[str, Any]: ...
    def deserialize_state(self, payload: dict[str, Any]) -> None: ...
```

Construction does NOT use `add_agent()` / `set_entry_agent()` (that signature only exists in some early blog posts and the `strands-agents/tools` *swarm tool*). The production `Swarm` class takes `nodes: list[Agent]` + optional `entry_point=` kwarg. No mutator methods after construction.

### 2.2 Supporting dataclasses

```python
@dataclass
class SwarmNode:
    node_id: str
    executor: Agent
    swarm: Optional["Swarm"] = None
    # captured at __post_init__:
    _initial_messages: Messages
    _initial_state: AgentState
    _initial_model_state: dict[str, Any]
    def reset_executor_state(self) -> None: ...   # called every node entry

@dataclass
class SharedContext:
    """The blackboard. Two-level dict: {node_id: {key: json_value}}."""
    context: dict[str, dict[str, Any]] = field(default_factory=dict)
    def add_context(self, node: SwarmNode, key: str, value: Any) -> None: ...
    # Validates: key is non-empty str; value is JSON-serializable.

@dataclass
class SwarmState:
    current_node: SwarmNode | None
    task: MultiAgentInput
    completion_status: Status = Status.PENDING        # PENDING, EXECUTING, COMPLETED, FAILED, INTERRUPTED
    shared_context: SharedContext
    node_history: list[SwarmNode]
    start_time: float
    results: dict[str, NodeResult]
    accumulated_usage: Usage
    accumulated_metrics: Metrics
    execution_time: int = 0                            # ms
    handoff_node: SwarmNode | None = None
    handoff_message: str | None = None

    def should_continue(self, *, max_handoffs, max_iterations, execution_timeout,
                        repetitive_handoff_detection_window,
                        repetitive_handoff_min_unique_agents) -> tuple[bool, str]: ...
```

`should_continue()` is the entire termination policy. It checks:
- `len(node_history) >= max_handoffs` → stop.
- `len(node_history) >= max_iterations` → stop.
- elapsed wall-clock > `execution_timeout` → stop.
- If window > 0 and `len(set(node_history[-window:])) < min_unique_agents` → stop. **This is the ping-pong / cycle detector.**

### 2.3 Handoff mechanics — TOOL CALL, not return value (production); return-value variant proposed

The current production handoff is **a tool injected into every agent's tool registry at swarm init.** From `_inject_swarm_tools()` and `_create_handoff_tool()`:

```python
@tool
def handoff_to_agent(agent_name: str,
                     message: str,
                     context: dict[str, Any] | None = None) -> dict[str, Any]:
    """Transfer control to another agent in the swarm for specialized help."""
    target_node = swarm_ref.nodes.get(agent_name)
    if not target_node:
        return {"status": "error", "content": [{"text": f"Error: Agent '{agent_name}' not found"}]}
    swarm_ref._handle_handoff(target_node, message, context or {})
    return {"status": "success", "content": [{"text": f"Handing off to {agent_name}: {message}"}]}
```

`_handle_handoff()` mutates two fields on `SwarmState`: `handoff_node` (target) and `handoff_message` (string), and writes any `context` payload into `SharedContext` keyed under the *current* node. **No code-path teleports execution mid-turn.** The swarm only consults `state.handoff_node` *after* the current node's `_execute_node` generator finishes producing its result event. This means a node can call the handoff tool, keep running, and even produce text after — and the handoff still fires at end-of-turn.

GitHub issue #913 (open) tracks adding a parallel **result-based** handoff: any `MultiAgentResult` could carry `handoff: HandoffRequest(agent_name, message, context)`. This lets non-`Agent` custom nodes (and A2A remote agents that don't have a local tool_registry) participate. **Not yet shipped — important for Aura because we'd want both paths from day 1** (a Go child agent should be able to return a typed handoff struct without round-tripping through the LLM tool-call interface).

### 2.4 Completion signaling — *absence* of handoff

End of the main loop in `_execute_swarm()`:

```python
if self.state.handoff_node:
    # swap current_node, emit MultiAgentHandoffEvent, continue
    ...
else:
    logger.debug("no handoff occurred, marking swarm as complete")
    self.state.completion_status = Status.COMPLETED
    break
```

So termination = the active agent's turn ended without setting `handoff_node`. No `complete_swarm_task` tool exists (the blog post calling it out is inaccurate vs current source). The agent doesn't have to "vote done" — it just stops handing off.

### 2.5 Blackboard / shared state

Two layers:
- **`SharedContext`** — agent-writable, agent-readable. Each `handoff_to_agent` call's `context` dict gets merged into `shared_context.context[current_node_id]`. The shared context is then *serialized into the next agent's prompt* via `_build_node_input()`:

```text
Handoff Message: <message>
User Request: <original task>
Previous agents who worked on this: a → b → c
Shared knowledge from previous agents:
  • a: {key: val, ...}
  • b: {...}
Other agents available for collaboration:
  Agent name: a. Agent description: ...
You have access to swarm coordination tools ...
```

- **`invocation_state`** — caller-supplied dict that's threaded through but NOT shown to the LLM (used for tooling config, request IDs, etc.). Mentioned in the dev.to article but rarely surfaces in user code.

The shared context is **read every turn** at node-input-build time, so it functions as a true blackboard — but reads are *cold* (LLM sees stringified snapshot), not live observers. Agents don't subscribe; they pull on entry.

### 2.6 Parallel fan-out — NOT supported in Swarm

The Swarm loop is strictly sequential: one `current_node` at a time, `await`ed in `_execute_swarm`'s `while True`. The `node_history` is a `list[SwarmNode]`, not a tree. No `asyncio.gather()`, no fan-out.

For parallel work Strands directs you to `Graph` (`src/strands/multiagent/graph.py`):
- `GraphBuilder().add_node(agent, "id").add_edge("a", "b", condition=...).set_entry_point("a").set_max_node_executions(N).set_execution_timeout(s).build()`
- Python implementation uses **OR semantics**: a node fires when any incoming edge completes.
- TypeScript implementation uses **AND semantics** + `maxConcurrency` for scheduler-bounded parallelism.
- Supports cycles (via `set_max_node_executions`) and conditional edges.

So the answer for Strands on OH3 gap #3 (parallel fan-out) is: don't extend Swarm — ship a Graph alongside it.

### 2.7 Code example (real shape, from the Strands docs)

```python
from strands import Agent
from strands.multiagent import Swarm

researcher = Agent(name="researcher", system_prompt="You research topics deeply...")
writer     = Agent(name="writer",     system_prompt="You write blog posts based on research...")
reviewer   = Agent(name="reviewer",   system_prompt="You critique and improve writing...")

swarm = Swarm(
    [researcher, writer, reviewer],
    entry_point=researcher,
    max_handoffs=10,
    max_iterations=15,
    execution_timeout=600.0,
    node_timeout=120.0,
    repetitive_handoff_detection_window=4,
    repetitive_handoff_min_unique_agents=2,
)

result = swarm("Write a 500-word blog about Go's GC in 1.22")
print(result.status)         # Status.COMPLETED
print(result.node_history)   # [researcher, writer, reviewer, writer]
print(result.results["writer"].result)
```

### 2.8 Iteration cap and cycle protection — full picture

Hard caps (always on):
- `max_handoffs` (default 20)
- `max_iterations` (default 20)
- `execution_timeout` (900 s)
- `node_timeout` (300 s) — wraps each `_execute_node` via `_stream_with_timeout` using `asyncio.timeout` on 3.11+.

Soft cap (off by default, must opt in by setting both > 0):
- **Repetitive handoff detector.** Looks at the last `window` entries in `node_history`. If number of unique agents < `min_unique_agents`, stop with `Status.FAILED`. Catches A→B→A→B ping-pong, but also catches "researcher→writer→researcher→writer→researcher" loops with `window=4, min=2`.

### 2.9 Gotchas

- **Single global `SwarmNode.executor` per swarm**. Strands deep-copies the executor's initial `messages`/`state`/`_model_state` at `__post_init__` and resets every node entry via `reset_executor_state()`. If you mutate the underlying Agent outside the swarm, you'll desync the snapshot. For Aura: every swarm-run should clone its agents from the registry, never mutate a shared definition.
- **`Session persistence is not supported for Swarm agents yet`** — explicit `_validate_swarm` rejection. The swarm has its own `session_manager` but child agents can't carry their own. Important for Aura's existing conversation-archive logic.
- **JSON-only blackboard values** — `SharedContext._validate_json_serializable` rejects non-JSON types. Good for our SQLite/JSON archive but means agent-to-agent passing of e.g. raw bytes needs envelope encoding.
- **No A2A handoff yet** — issue #913 is open, the production `_inject_swarm_tools` skips non-Agent nodes silently because they don't have `tool_registry`.

### 2.10 Source links

- Strands Swarm class: https://github.com/strands-agents/sdk-python/blob/main/src/strands/multiagent/swarm.py
- Strands Graph (parallel DAG): https://github.com/strands-agents/sdk-python/blob/main/src/strands/multiagent/graph.py
- User guide: https://strandsagents.com/docs/user-guide/concepts/multi-agent/swarm/
- Multi-agent patterns overview: https://strandsagents.com/docs/user-guide/concepts/multi-agent/multi-agent-patterns/
- Tools-package swarm tool (different from Swarm class): https://github.com/strands-agents/tools/blob/main/src/strands_tools/swarm.py
- Open issue tracking result-based handoff for custom/A2A nodes: https://github.com/strands-agents/sdk-python/issues/913
- AWS blog (Strands 1.0 release): https://aws.amazon.com/blogs/opensource/introducing-strands-agents-1-0-production-ready-multi-agent-orchestration-made-simple/

---

## 3. LangGraph Swarm (`langchain-ai/langgraph-swarm-py`)

Source: `langgraph_swarm/swarm.py` + `langgraph_swarm/handoff.py` on `main` (May 2026). Verified via `gh api`.

### 3.1 Architecture — sits on top of LangGraph, NOT a self-contained engine

LangGraph Swarm is a thin (~250 LOC) helper that **assembles a LangGraph `StateGraph`** whose nodes are individual agents. All the real machinery (supersteps, checkpointer, reducers, interrupt/resume) is LangGraph's underlying Pregel runtime. The Swarm package contributes three things:

1. A `SwarmState` schema (extends `MessagesState`) that adds a sticky `active_agent: str | None` field.
2. A factory `create_handoff_tool(agent_name=..., ...)` that produces a LangChain `Tool` whose execution returns a `Command(goto=agent_name, graph=Command.PARENT, update={"messages": ..., "active_agent": agent_name})`.
3. A router helper `add_active_agent_router(builder, route_to=[...], default_active_agent=...)` that wires a conditional edge from `START` to the agent named in `state["active_agent"]`. This is what makes the active-agent pointer *sticky across turns* — on every new `app.invoke(...)`, the checkpointed state's `active_agent` decides which agent handles the new user message.

### 3.2 API surface (verified signatures)

```python
class SwarmState(MessagesState):
    active_agent: str | None        # turned into Literal[<agent_names>] at create_swarm time

def create_handoff_tool(*,
                        agent_name: str,
                        name: str | None = None,
                        description: str | None = None) -> BaseTool: ...
    # default tool name: f"transfer_to_{snake_case(agent_name)}"
    # default description: f"Ask agent '{agent_name}' for help"

def add_active_agent_router(builder: StateGraph,
                            *,
                            route_to: list[str],
                            default_active_agent: str) -> StateGraph: ...

def create_swarm(agents: list[Pregel],
                 *,
                 default_active_agent: str,
                 state_schema: StateSchemaType = SwarmState,
                 context_schema: type[Any] | None = None) -> StateGraph: ...

def get_handoff_destinations(agent: CompiledStateGraph,
                             tool_node_name: str = "tools") -> list[str]: ...
```

### 3.3 Handoff mechanics — `Command(goto=..., graph=Command.PARENT, update=...)`

The handoff tool body (from `handoff.py`):

```python
@tool(name, description=description)
def handoff_to_agent(
    state: Annotated[Any, InjectedState],
    tool_call_id: Annotated[str, InjectedToolCallId],
) -> Command:
    tool_message = ToolMessage(
        content=f"Successfully transferred to {agent_name}",
        name=name,
        tool_call_id=tool_call_id,
    )
    return Command(
        goto=agent_name,
        graph=Command.PARENT,
        update={
            "messages": [*state["messages"], tool_message],
            "active_agent": agent_name,
        },
    )

handoff_to_agent.metadata = {METADATA_KEY_HANDOFF_DESTINATION: agent_name}
```

Key contract bits:
- **Tool returns `Command` instead of a string** — the LangGraph `ToolNode` recognizes this and routes execution, not the LLM. This is structurally different from Strands (where the tool is a side-effect setter and routing happens at end-of-turn).
- **`graph=Command.PARENT`** — routing happens at the swarm-graph level, not inside the child agent's own subgraph. This is the key to multi-graph composition.
- **`update={"messages": ..., "active_agent": ...}`** — atomic write under the parent's state schema. The `messages` reducer (`add_messages` from LangGraph) is a *list-merge with id-dedup* reducer, not a clobber.
- **Custom handoff tools must also return `Command`** and the destination must be in `agent.metadata[METADATA_KEY_HANDOFF_DESTINATION]` (= `"__handoff_destination"`) for `get_handoff_destinations()` to wire `add_node(..., destinations=(...,))` correctly.

### 3.4 Active-agent tracking across turns

```python
def add_active_agent_router(builder, *, route_to, default_active_agent):
    ...
    def route_to_active_agent(state: dict) -> str:
        return cast("str", state.get("active_agent", default_active_agent))
    builder.add_conditional_edges(START, route_to_active_agent, path_map=route_to)
```

The router is a **conditional edge from `START`** to one of the agent names. On every `app.invoke({"messages": [...]}, config=...)`:
1. Checkpointer loads the prior state by `thread_id`.
2. `active_agent` is already set (because the last handoff updated it).
3. Router edge dispatches to that agent.
4. That agent processes the new user message *as if it were always in the conversation*.
5. It may or may not call a handoff tool.

This makes "the swarm remembers who you were talking to" effectively free — no Aura-level pointer to maintain, the graph's state carries it.

### 3.5 Shared state model

The state schema is a Python `TypedDict` with **per-key reducers** (LangGraph's pattern). Default `SwarmState` only knows:

```python
class SwarmState(MessagesState):     # MessagesState gives: messages: Annotated[list, add_messages]
    active_agent: str | None         # last-write-wins (no reducer)
```

To add a blackboard, user defines a subclass:

```python
class MySwarmState(SwarmState):
    scratch: Annotated[dict, merge_dicts]   # user-defined reducer
    findings: Annotated[list[str], add]     # operator.add for list-append
```

This is the most powerful aspect of LangGraph for OH3 — **a typed blackboard with merge semantics declared at schema time**, not at write time. Concurrent branches can write to the same key without overwriting if a reducer is declared.

### 3.6 Loop termination — no iteration cap in Swarm itself

The swarm has **no `max_iterations` field.** It inherits LangGraph's `recursion_limit` (default 25 — the number of supersteps; configurable per-invoke via `config={"recursion_limit": N}`). When the limit is hit, LangGraph raises `GraphRecursionError`.

Termination = the active agent's turn ends without calling any handoff tool. Same shape as Strands. No `complete_swarm_task`. The agent simply stops emitting tool-calls and the LangGraph React-agent state machine transitions to END.

**No built-in ping-pong / repetitive-handoff detector** — that's a footgun for OH3. Aura would need to layer this in.

### 3.7 Parallel fan-out — NOT in swarm, but available via LangGraph `Send`

Like Strands, the Swarm primitive is sequential. For fan-out LangGraph provides:
- **`Send(node_name, state_payload)`** — returned from a conditional-edge function. Multiple `Send` returns in one superstep launch all branches in parallel.
- **Supersteps** — LangGraph executes all `Send`s in a superstep concurrently, then synchronizes (Pregel-style) before the next step.
- **Deferred nodes** (`add_node(..., defer=True)`) — barrier nodes that wait for all upstream branches to finish, even those with asymmetric completion times.
- **Reducers** — state keys with concurrent writers must declare a reducer or LangGraph raises `InvalidUpdateError`.

For Aura's OH3 gap #3 the lift is **the Send-fanout pattern + per-key reducers**, not a separate Graph primitive.

### 3.8 Checkpointer / persistence

```python
checkpointer = InMemorySaver()             # or SQLite / Postgres saver
store        = InMemoryStore()             # long-term cross-thread memory
app = workflow.compile(checkpointer=checkpointer, store=store)

config = {"configurable": {"thread_id": "abc"}}
app.invoke({"messages": [...]}, config)    # state persisted under thread_id
```

Checkpointing is **required** for multi-turn swarms — without it the `active_agent` pointer is forgotten between calls, defeating the whole sticky-routing premise.

### 3.9 Anti-patterns / footguns

- **Forgetting `add_messages` reducer** when subclassing state → every node's `update={"messages": ...}` clobbers prior history.
- **Not putting handoff tools' destinations in `add_node(destinations=(...,))`** → LangGraph's static graph validation can't see the target nodes, and the graph visualization is wrong (`destinations` doesn't change runtime behavior since `goto` does, but it breaks static analysis and warnings).
- **Custom handoff tools that return non-`Command` values** → routed back to the calling LLM instead of teleporting; debug nightmare. Always return `Command(goto=..., graph=Command.PARENT, update=...)`.
- **Mixing per-agent state schemas without wrapper functions** — the README warns that if Alice uses `alice_messages` instead of the shared `messages`, you need a wrapper that translates parent state ↔ subgraph state.
- **No recursion_limit override** for long swarms → silent `GraphRecursionError` at superstep 25.

### 3.10 Code example (canonical from README)

```python
from langgraph.checkpoint.memory import InMemorySaver
from langchain.agents import create_agent
from langgraph_swarm import create_handoff_tool, create_swarm

def add(a: int, b: int) -> int:
    return a + b

alice = create_agent(
    "openai:gpt-4o",
    tools=[add, create_handoff_tool(agent_name="Bob", description="Transfer to Bob")],
    system_prompt="You are Alice, an addition expert.",
    name="Alice",
)
bob = create_agent(
    "openai:gpt-4o",
    tools=[create_handoff_tool(agent_name="Alice", description="Transfer to Alice")],
    system_prompt="You are Bob, you speak like a pirate.",
    name="Bob",
)

workflow = create_swarm([alice, bob], default_active_agent="Alice")
app = workflow.compile(checkpointer=InMemorySaver())

config = {"configurable": {"thread_id": "1"}}
app.invoke({"messages": [{"role": "user", "content": "i'd like to speak to Bob"}]}, config)
app.invoke({"messages": [{"role": "user", "content": "what's 5 + 7?"}]}, config)
# Second call automatically routes to Bob because active_agent="Bob" was persisted.
# Bob calls handoff_to_alice → Alice computes 12 → response.
```

### 3.11 Source links

- README: https://github.com/langchain-ai/langgraph-swarm-py
- swarm.py: https://github.com/langchain-ai/langgraph-swarm-py/blob/main/langgraph_swarm/swarm.py
- handoff.py: https://github.com/langchain-ai/langgraph-swarm-py/blob/main/langgraph_swarm/handoff.py
- PyPI: https://pypi.org/project/langgraph-swarm/
- API reference: https://reference.langchain.com/python/langgraph-swarm/swarm
- LangGraph Send API + Pregel supersteps: https://langchain-ai.github.io/langgraph/concepts/multi_agent/

---

## 4. Comparison table

| Dimension | Strands `Swarm` | LangGraph `create_swarm` |
|---|---|---|
| **Handoff mechanism** | Tool call mutates `state.handoff_node` as side-effect; routing applied AFTER current node finishes its turn | Tool call returns `Command(goto=..., graph=Command.PARENT, update=...)`; routing applied by LangGraph's `ToolNode` mid-turn |
| **Handoff payload** | `(agent_name, message, context_dict)` — `context` written to `SharedContext` keyed by sender | `Command.update` is a free-form state delta; default tool injects only `messages` + `active_agent`; custom tool can add task descriptions |
| **State / blackboard model** | `SharedContext` = `{node_id: {key: json_value}}`. Read-on-entry, serialized into the next agent's prompt as plaintext. JSON-only values. | `SwarmState` extends `MessagesState`. Typed `TypedDict` with per-key reducers. Concurrent writers safe if reducer declared. State persisted by checkpointer. |
| **Active-agent tracking** | Implicit — `state.current_node` lives in-memory during one `invoke_async` call. `serialize_state()` / `deserialize_state()` for cross-process resume. | Explicit — `active_agent: str | None` in shared state, persisted by checkpointer. Sticky across `app.invoke` calls via `add_active_agent_router` conditional edge from `START`. |
| **Parallel fan-out in Swarm itself** | No (sequential `while True` over `current_node`). Use `Graph` primitive instead. | No (sequential — one active agent per turn). Use `Send` API + supersteps + reducers, or `langgraph-supervisor`. |
| **Iteration cap mechanism** | `SwarmState.should_continue()` checks 5 conditions: max_handoffs, max_iterations, execution_timeout, node_timeout, repetitive_handoff_detector | Inherits LangGraph's `recursion_limit` (default 25 supersteps, raises `GraphRecursionError`). No built-in ping-pong detector. |
| **Cycle / ping-pong detection** | Built-in: `repetitive_handoff_detection_window` + `min_unique_agents` over recent `node_history` slice | None — must be layered manually |
| **Completion signal** | Absence of handoff at end of turn → `Status.COMPLETED` | Absence of further tool calls → React agent transitions to END |
| **Observability** | `stream_async` yields typed events: `MultiAgentNodeStartEvent`, `MultiAgentNodeStreamEvent`, `MultiAgentHandoffEvent`, `MultiAgentNodeStopEvent`, `MultiAgentResultEvent`. Hooks via `HookProvider`. OpenTelemetry tracing. | LangSmith integration; `app.stream()` yields per-node state updates; full superstep events; built-in interrupt/resume |
| **Persistence model** | `serialize_state()` → dict (status, node_history, results, handoff_node, shared_context). Caller persists. | `Checkpointer` (in-memory / SQLite / Postgres) — automatic, threaded by `thread_id`. Also a `Store` for cross-thread long-term memory. |
| **Language / runtime** | Python (sdk-python) + TypeScript (sdk-typescript). Pure async-Python; uses `asyncio.timeout`. | Python only. Built on Pregel/superstep runtime — non-trivial to port. |
| **A2A / remote agent support** | Partial — `a2a/` subpackage exists; issue #913 tracks result-based handoff for non-Agent nodes (not yet shipped) | First-class via LangGraph subgraphs + `Command.PARENT`; any `Pregel` object (compiled graph, functional workflow) can be a node |
| **Lines to read for full understanding** | ~1100 (single file) | ~250 (`swarm.py` + `handoff.py`) but Pregel runtime ~50k LOC underneath |

---

## 5. Mapping to Aura's 4 OH3 gaps

Aura is **Go**, currently at the agentdef registry + delegate synth stage (commits `02d390a7..d0b24989`). The agentdef tier system + cycle detector + sync hierarchical delegation is the substrate; OH3 layers peer-mesh on top.

### Gap 1 — Peer hand-off (every agent nominates next)

**Lift from Strands.** The tool-injection pattern is a 1:1 fit for Aura's `internal/agent/tools/registry`. Plan:

- Define `handoff_to_agent(agent_name, message, context)` as a Go tool synthesized by a new `swarmpolicy` package, parallel to today's `agentdef` delegate synth.
- Inject it into each child agent's per-loop tool set at swarm-run start (Aura already has per-loop tool sets — this is `executor.go`'s `loopOptions`).
- The Strands "absence of handoff = COMPLETED" rule is the right termination contract. No `complete_swarm_task` needed.
- **Also adopt issue #913's not-yet-shipped result-based path** — let Go child agents that aren't LLM-backed (deterministic worker structs) emit a `HandoffRequest{TargetID, Message, Context}` struct without round-tripping through an LLM. This avoids a class of problems Strands hasn't solved yet.

**Why not LangGraph's `Command(goto=..., graph=Command.PARENT)`** — `Command` only makes sense inside Pregel's superstep model where routing is a graph-engine concern. Aura's loop is a sequential `for-each-turn`; there's no engine to teleport into. The tool-as-side-effect pattern matches Aura's existing executor shape.

### Gap 2 — Shared blackboard (typed workspace per swarm-run)

**Lift from LangGraph (the *contract*), not the runtime.** Pregel's reducer machinery doesn't port cheaply, but the *concept* of "typed shared state with declared merge semantics per key" is exactly right for a Go port. Plan:

- Define `type SwarmState struct { ... }` per swarm-run with explicit fields (not a generic `map[string]any` — that's Strands' `SharedContext` and the JSON-validation pain isn't worth it in Go).
- For fields that *will* see concurrent writes (gap 3), explicit reducer funcs: `type ReducerFn[T any] func(old, new T) T`. Aura's gap-2 won't see concurrency on day 1 (gap 3 introduces it), so day-1 reducers can all be last-write-wins.
- Strands' `_build_node_input()` pattern — *serialize the blackboard into the next agent's system prompt* — is the right read model. Don't try to make agents subscribe; just render the snapshot at turn start.
- Persist via Aura's existing SQLite conversations archive — same shape as Strands' `serialize_state()`.

### Gap 3 — Parallel fan-out (concurrent agents)

**Don't extend the peer-mesh loop. Build a separate primitive.** Both frameworks converged on this: Strands has `Graph`, LangGraph has `Send`. The peer-mesh loop is fundamentally sequential because the handoff contract is "next single agent gets the floor".

For Aura, this maps to a separate `agentdef.Group` or `agentdef.Fanout` primitive that:
- Takes N child agentdefs + a single shared task.
- Runs them via `errgroup.Group` (Go's natural superstep equivalent).
- Each child writes results into the shared blackboard at a typed slot.
- A `barrier` step (LangGraph's `defer=True` pattern) waits for all and feeds the merged result back into the parent loop.

The Go advantage here is real: `errgroup` + channels handles what LangGraph needs Pregel for. **This is the gap that doesn't need a framework to copy.**

### Gap 4 — Iteration loop + convergence (self-signal "done")

**Lift Strands' `should_continue()` verbatim.** This is the cleanest piece in either framework. Plan:

```go
// internal/agent/swarmpolicy/policy.go
type Policy struct {
    MaxHandoffs                int           // default 20
    MaxIterations              int           // default 20
    ExecutionTimeout           time.Duration // default 15m
    NodeTimeout                time.Duration // default 5m
    RepetitiveHandoffWindow    int           // 0 = disabled
    RepetitiveHandoffMinUnique int
}

func (p Policy) ShouldContinue(history []string, elapsed time.Duration) (bool, string) {
    // 1:1 port of SwarmState.should_continue
    if len(history) >= p.MaxHandoffs { return false, fmt.Sprintf("max_handoffs=%d", p.MaxHandoffs) }
    if len(history) >= p.MaxIterations { return false, fmt.Sprintf("max_iterations=%d", p.MaxIterations) }
    if elapsed > p.ExecutionTimeout { return false, fmt.Sprintf("execution_timeout=%s", p.ExecutionTimeout) }
    if p.RepetitiveHandoffWindow > 0 && len(history) >= p.RepetitiveHandoffWindow {
        recent := history[len(history)-p.RepetitiveHandoffWindow:]
        unique := uniqueCount(recent)
        if unique < p.RepetitiveHandoffMinUnique {
            return false, fmt.Sprintf("repetitive: %d unique in last %d", unique, p.RepetitiveHandoffWindow)
        }
    }
    return true, "continuing"
}
```

The repetitive-handoff detector is a real lift — neither LangGraph nor most homegrown loops bother, and it catches the A↔B ping-pong case that the OH2 cycle detector currently lets through (OH2's cycle detector is for *delegation* recursion: A→B→A *during the same call stack*. The repetitive-handoff detector is for *peer mesh* loops: A→B→A→B *over time, all at peer tier*).

"Self-signal done" = no separate primitive. The peer-mesh loop terminates when the active agent finishes without calling `handoff_to_agent`. Same as both frameworks.

### Patterns that DON'T port cleanly to Go

- **Python `@tool` decorators** with `Annotated[..., InjectedState]` and `Annotated[..., InjectedToolCallId]` (LangGraph). Aura already solved this — tool argument injection lives in the executor's `runTool` path. Don't try to mimic the decorator surface.
- **LangChain's `Command(goto=..., graph=Command.PARENT)`** — assumes Pregel runtime. Use Aura's existing tool-return + setter-side-effect pattern instead.
- **LangGraph's reducer-per-state-key auto-merge.** Doable in Go via generics, but adds a layer of indirection per state read/write. Day 1, just hand-code merge funcs in the swarm orchestrator.
- **Strands' `_inject_swarm_tools()` mutates the agent's tool_registry in place.** Aura's per-loop tool set is *appended at loop start* (functional pattern) — keep that, don't mutate the agentdef.

---

## 6. Open questions (for other scouts or source-code follow-ups)

1. **How does Strands' `_handle_handoff` interact with parallel tool calls?** Today Aura runs independent tool calls in the same turn in parallel. If the LLM calls `handoff_to_agent("a", ...)` and `handoff_to_agent("b", ...)` in the same response, what wins? In Strands the answer is "last write wins on `state.handoff_node`", which feels accidental. Aura should explicitly forbid >1 handoff per turn (return an error from the second handoff tool call).
2. **Does LangGraph's `Command.PARENT` route through interrupt/resume cleanly when the parent graph is itself a subgraph?** Worth checking if we ever nest swarm-runs (one swarm-run calls a child that *is itself* a swarm).
3. **Strands' `serialize_state` for an INTERRUPTED swarm includes a `_internal_state.interrupt_state` blob.** Aura should mirror this in its archive schema if we want resumable swarms. Worth a separate doc on interrupt semantics.
4. **What's the latency cost of `_build_node_input()` re-serializing the full `SharedContext` every turn?** For a long-running swarm with many handoffs, this grows linearly. Strands hasn't profiled this publicly. Aura should bench before adopting (likely fine for <20 handoffs, but the prompt size matters).
5. **Issue #913's `HandoffRequest` dataclass shape** — not yet merged in Strands. We should design Aura's equivalent NOW so we don't paint into a corner when Strands ships theirs and we want to mirror the shape for ecosystem hand-shakes.
6. **LangGraph's `defer=True` barrier node + `Send` API** — worth a separate scout for OH3 gap #3 specifically. The fan-out + barrier pattern has subtleties around partial failure that this scout didn't deep-dive.
7. **Does either framework let an agent *observe* the blackboard mid-turn (not just at entry)?** This scout's reading says no for both — observation is cold-snapshot-at-entry. If Aura wants live observation (gap 2 stretch goal), neither framework has prior art to copy.

---

## Sources

- [Strands Swarm source — `swarm.py`](https://github.com/strands-agents/sdk-python/blob/main/src/strands/multiagent/swarm.py)
- [Strands Graph source — `graph.py`](https://github.com/strands-agents/sdk-python/blob/main/src/strands/multiagent/graph.py)
- [Strands Swarm user guide](https://strandsagents.com/docs/user-guide/concepts/multi-agent/swarm/)
- [Strands Graph user guide](https://strandsagents.com/docs/user-guide/concepts/multi-agent/graph/)
- [Strands multi-agent patterns overview](https://strandsagents.com/docs/user-guide/concepts/multi-agent/multi-agent-patterns/)
- [Strands tools-package swarm tool (separate primitive)](https://github.com/strands-agents/tools/blob/main/src/strands_tools/swarm.py)
- [Strands issue #913 — result-based handoff for custom/A2A nodes](https://github.com/strands-agents/sdk-python/issues/913)
- [AWS blog — Strands 1.0 release](https://aws.amazon.com/blogs/opensource/introducing-strands-agents-1-0-production-ready-multi-agent-orchestration-made-simple/)
- [dev.to — Strands multi-agent patterns walkthrough](https://dev.to/aws-builders/understanding-multi-agent-patterns-in-strands-agent-graph-swarm-and-workflow-4nb8)
- [langgraph-swarm-py README](https://github.com/langchain-ai/langgraph-swarm-py)
- [langgraph-swarm-py — `swarm.py`](https://github.com/langchain-ai/langgraph-swarm-py/blob/main/langgraph_swarm/swarm.py)
- [langgraph-swarm-py — `handoff.py`](https://github.com/langchain-ai/langgraph-swarm-py/blob/main/langgraph_swarm/handoff.py)
- [langgraph-swarm PyPI](https://pypi.org/project/langgraph-swarm/)
- [LangChain reference — langgraph-swarm module](https://reference.langchain.com/python/langgraph-swarm)
- [LangGraph multi-agent concepts](https://langchain-ai.github.io/langgraph/concepts/multi_agent/)
- [LangGraph Send API + parallel branches walkthrough](https://medium.com/@gmurro/parallel-nodes-in-langgraph-managing-concurrent-branches-with-the-deferred-execution-d7e94d03ef78)
