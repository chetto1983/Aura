# GraphRAG — write path and hygiene: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Aura's memory graph an actual graph — every fact whose two ends name entities becomes a traversable `RELATED_TO` edge — and clear the test residue that falsifies every measurement of it.

**Architecture:** Four seams, in dependency order. (1) The Go MCP bridge injects a `threshold` default the way it already injects `user_identifier`, so recall stops returning everything. (2) The vendored Python sidecar's two edge writers are unified onto one property name, because they currently disagree and a reader would see half the graph. (3) The live graph is purged of test residue, backup first, with the two residue classes handled by their own discriminators. (4) `memory_add_fact` gains optional endpoint types and materializes the edge at write time; a backfill turns the facts already stored into edges.

**Tech Stack:** Go 1.26 (`internal/agent/mcptools`, `internal/knowledge`, `cmd/aura`), Python 3.12 (vendored fork at `docker/agent-memory`, pytest + pytest-asyncio), Neo4j 5 Cypher, Docker Compose.

## Global Constraints

- **Design source of truth:** `docs/superpowers/specs/2026-07-31-graphrag-vero-design.md`. This plan implements sections 1, 2 (passaggio 0, steps 2–3) and 4. Sections 2-step-4 (Leiden communities), 3 (walking retrieval) and the GLiNER ONNX typing net are **out of scope** and get their own plans.
- **No new extraction stack.** Do not add spaCy, GLiNER, GLiREL, torch, or any model runtime in this plan. Types come from the LLM caller, or the endpoint stays a literal.
- **Backward compatible write path.** A caller that passes no `subject_type`/`object_type` must get byte-identical behaviour to today. Every existing pytest must pass unchanged.
- **Never mint an entity for a `LITERAL` endpoint.** `Europe/Rome`, `424410`, `programmatore`, timestamps stay fact properties.
- **The vendored sidecar is not measured under Aura's Go gates** (`docs/aura-quality-snapshot.md` covers `internal/*`). It is measured by its own pytest suite, which CI runs at `.github/workflows/ci.yml:956`. Every Python change in this plan ships with pytest coverage there.
- **Go changes obey CLAUDE.md Gate 2:** `go vet ./...`, `go build ./...`, `go test ./internal/<pkg>/`, `go test -race ./internal/<pkg>/` before moving on. Files stay ≤600 LOC.
- **Destructive live-data steps require an operator go-ahead and a fresh Neo4j dump first.** Task 3 is the only one; it stops for confirmation.
- **Measurement discipline.** Every task ends with a real measurement against the live stack (`docker compose` up, `aura-agent-memory-mcp` healthy), not a compile check. Record before/after numbers in the task's commit body.

## Baseline — live deployment, measured 2026-07-31

Re-measure these before starting; they are the yardstick for every "after" number.

| | |
|---|---:|
| `:Chunk` / `NEXT_CHUNK` | 302 / 299 |
| `:Entity` (real: `Davide`, `Caraglio`, `PmSync`) | 27 (**3**) |
| `:Fact` (real: 4 about Davide + 2 client codes) | 26 (**6**) |
| `:Fact` with `ABOUT_SUBJECT` | 4 |
| `RELATED_TO` (all four are test residue) | 4 |
| `MENTIONS` / `SAME_AS` | 3 / 3 |
| `:User` nodes | 30 |
| `RELATED_TO` edges carrying `r.relation_type` / `r.type` | 1 / 3 |

Baseline query, run through `mcp-neo4j-cypher` or `cypher-shell`:

```cypher
MATCH (n) UNWIND labels(n) AS l RETURN l, count(*) AS c ORDER BY c DESC;
MATCH ()-[r]->() RETURN type(r) AS rel, count(*) AS c ORDER BY c DESC;
MATCH ()-[r:RELATED_TO]->() RETURN count(r.relation_type) AS with_relation_type, count(r.type) AS with_type;
```

## File Structure

| File | Responsibility |
|---|---|
| `internal/agent/mcptools/bridge_memory.go` (modify) | Bridge-side memory policy. Gains `withMemoryRecallThreshold`, sibling to the existing `withMemoryUserIdentifier`. |
| `internal/agent/mcptools/bridge.go` (modify, line 113) | One added call in `Execute`. |
| `internal/agent/mcptools/bridge_memory_threshold_test.go` (create) | Unit coverage for the threshold seam. |
| `docker/agent-memory/src/neo4j_agent_memory/graph/queries.py` (modify) | `CREATE_ENTITY_RELATIONSHIP` unified onto `relation_type`; new `MERGE_TYPED_ENTITY_FOR_FACT` + `LINK_FACT_TO_OBJECT_ENTITY`. |
| `docker/agent-memory/src/neo4j_agent_memory/memory/long_term.py` (modify) | `add_fact` gains `subject_type`/`object_type`; new `_materialize_fact_edge`. |
| `docker/agent-memory/src/neo4j_agent_memory/integration.py` (modify) | `add_fact` passes the two types through and reports what it materialized. |
| `docker/agent-memory/src/neo4j_agent_memory/mcp/_tools.py` (modify) | `memory_add_fact` exposes the two type parameters + docstring the model reads. |
| `docker/agent-memory/tests/test_relation_type_property.py` (create) | Locks the single property name across both writers. |
| `docker/agent-memory/tests/test_fact_edge_materialization.py` (create) | Typed endpoints → edge; literal endpoints → no node. |
| `internal/knowledge/graphhygiene.go` (create) | Cypher for the two residue classes + the flat-fact backfill. Read-only planning + explicit apply. |
| `internal/knowledge/graphhygiene_test.go` (create) | Unit coverage of the Cypher builders + residue classification. |
| `internal/knowledge/graphhygiene_integration_test.go` (create) | `neo4j_integration`-tagged live proof against a seeded graph. |
| `cmd/aura/memory_prune.go` (create) | `aura memory prune` verb — dry-run default, `--apply` to execute. `cmd/aura/memory.go` is at 425 LOC and stays under 600. |
| `cmd/aura/memory_prune_test.go` (create) | Verb parsing + dry-run default. |
| `internal/cron/handlers/graph_backfill.go` (create) | Scheduled flat-fact → edge materialization, sibling to `retention.go`. |
| `internal/cron/handlers/graph_backfill_test.go` (create) | Handler seam coverage. |

---

### Task 1: Recall threshold default at the bridge

The design's §4, which "vale da solo, non aspetta il resto". `memory_search` documents `threshold` (default 0.7) and nothing ever passes it. Measured on the live graph: «dove vive Davide» returns 5 entities in 1267 ms at the default and `Davide + Caraglio` in 282 ms at 0.8.

The seam already exists: `bridgedTool.Execute` calls `withMemoryUserIdentifier` before dispatch. This adds a sibling. It only fills a value the caller **omitted** — a model that passes `threshold` explicitly keeps its own.

**Files:**
- Modify: `internal/agent/mcptools/bridge_memory.go`
- Modify: `internal/agent/mcptools/bridge.go:113`
- Test: `internal/agent/mcptools/bridge_memory_threshold_test.go` (create)

**Interfaces:**
- Consumes: `bridgedTool.policy` (`bridgePolicy`), `bridgedTool.Spec().Parameters` (`json.RawMessage`), `acceptsUserIdentifier`'s schema-probing pattern.
- Produces: `func (b *bridgedTool) withMemoryRecallThreshold(args map[string]any) map[string]any`; package const `defaultMemoryRecallThreshold = 0.8`; `func acceptsParameter(parameters json.RawMessage, name string) bool` (generalises the existing `acceptsUserIdentifier`, which becomes a one-line caller).

- [ ] **Step 1: Write the failing test**

Create `internal/agent/mcptools/bridge_memory_threshold_test.go`:

```go
package mcptools

import (
	"encoding/json"
	"testing"
)

func memoryToolWithParams(t *testing.T, params string) *bridgedTool {
	t.Helper()
	b := &bridgedTool{name: "memory_search", policy: bridgePolicy{memory: true}}
	b.spec.Store(toolSpecWithParameters(json.RawMessage(params)))
	return b
}

func TestThresholdInjectedWhenAbsent(t *testing.T) {
	b := memoryToolWithParams(t, `{"properties":{"query":{},"threshold":{}}}`)
	got := b.withMemoryRecallThreshold(map[string]any{"query": "dove vive Davide"})
	if got["threshold"] != defaultMemoryRecallThreshold {
		t.Fatalf("threshold = %v, want %v", got["threshold"], defaultMemoryRecallThreshold)
	}
}

// A model that reasoned about its own threshold must keep it. Overwriting an explicit
// argument is the difference between a default and a policy, and this is a default.
func TestExplicitThresholdSurvives(t *testing.T) {
	b := memoryToolWithParams(t, `{"properties":{"query":{},"threshold":{}}}`)
	got := b.withMemoryRecallThreshold(map[string]any{"query": "x", "threshold": 0.5})
	if got["threshold"] != 0.5 {
		t.Fatalf("threshold = %v, want caller's 0.5", got["threshold"])
	}
}

// memory_add_fact has no threshold parameter. Injecting one would make the sidecar
// reject the call, taking down the write path to speed up the read path.
func TestThresholdNotInjectedWhenToolHasNoSuchParameter(t *testing.T) {
	b := memoryToolWithParams(t, `{"properties":{"subject":{},"predicate":{}}}`)
	got := b.withMemoryRecallThreshold(map[string]any{"subject": "Davide"})
	if _, ok := got["threshold"]; ok {
		t.Fatal("threshold injected into a tool that does not accept it")
	}
}

func TestThresholdNotInjectedForNonMemoryNamespace(t *testing.T) {
	b := &bridgedTool{name: "whatsapp_send", policy: bridgePolicy{memory: false}}
	b.spec.Store(toolSpecWithParameters(json.RawMessage(`{"properties":{"threshold":{}}}`)))
	got := b.withMemoryRecallThreshold(map[string]any{})
	if _, ok := got["threshold"]; ok {
		t.Fatal("threshold injected outside the memory namespace")
	}
}
```

`toolSpecWithParameters` is a helper this test needs. If the package already has an
equivalent (check `bridge_test.go` and `bridge_identity_test.go` for how they build a
`bridgedTool` — reuse whatever they use and delete this helper), skip it. Otherwise add
to the new file:

```go
func toolSpecWithParameters(params json.RawMessage) tools.ToolSpec {
	return tools.ToolSpec{Name: "memory_search", Parameters: params}
}
```

with `"github.com/chetto1983/aura/internal/agent/tools"` imported.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/agent/mcptools/ -run TestThreshold -v
```

Expected: FAIL — `b.withMemoryRecallThreshold undefined` and `defaultMemoryRecallThreshold undefined`.

- [ ] **Step 3: Write minimal implementation**

In `internal/agent/mcptools/bridge_memory.go`, replace `acceptsUserIdentifier` with the
generalised probe and add the threshold seam:

```go
// defaultMemoryRecallThreshold is the similarity floor the bridge supplies when the caller
// omits one. The sidecar's own default is 0.7 and nothing ever overrode it, so recall
// returned its full limit on every query regardless of how weakly anything matched.
// Measured on the live graph 2026-07-31: «dove vive Davide» returned 5 entities in 1267 ms
// at 0.7 and exactly Davide + Caraglio in 282 ms at 0.8.
//
// This buys precision and latency. It does NOT buy "I don't know" — a nonsense query still
// returns real rows, because with a graph this small everything is near everything. That is
// missing topology, not a wrong cutoff, and the fact-edge tasks below are what fix it.
const defaultMemoryRecallThreshold = 0.8

// withMemoryRecallThreshold fills a threshold the caller omitted. It never overwrites one:
// a model that passed its own has reasoned about it, and a default that overrides the
// caller is not a default.
func (b *bridgedTool) withMemoryRecallThreshold(args map[string]any) map[string]any {
	if !b.policy.memory || !acceptsParameter(b.Spec().Parameters, "threshold") {
		return args
	}
	if _, explicit := args["threshold"]; explicit {
		return args
	}
	if args == nil {
		args = make(map[string]any, 1)
	}
	args["threshold"] = defaultMemoryRecallThreshold
	return args
}

// acceptsParameter reports whether a tool's JSON schema declares name. An unparseable
// schema returns true, preserving the pre-existing fail-open behaviour of
// acceptsUserIdentifier: the sidecar is the authority on its own arguments, and refusing
// to inject on a schema we merely failed to read would silently drop user scoping.
func acceptsParameter(parameters json.RawMessage, name string) bool {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(parameters, &schema); err != nil || schema.Properties == nil {
		return true
	}
	_, ok := schema.Properties[name]
	return ok
}
```

Delete the old `acceptsUserIdentifier` and change its one call site in
`withMemoryUserIdentifier` to `acceptsParameter(b.Spec().Parameters, "user_identifier")`.

In `internal/agent/mcptools/bridge.go`, after line 113:

```go
	args = b.withMemoryUserIdentifier(ctx, args)
	args = b.withMemoryRecallThreshold(args)
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/agent/mcptools/ -v
go test -race ./internal/agent/mcptools/
go vet ./... && go build ./...
```

Expected: PASS, including every pre-existing test in the package (`bridge_identity_test.go` exercises `acceptsUserIdentifier`'s behaviour through `withMemoryUserIdentifier` — it must stay green).

- [ ] **Step 5: Measure it live**

Stack up, then compare against the design's table. Record real numbers, including latency:

```bash
docker compose up -d
docker inspect -f '{{.State.Health.Status}}' aura-agent-memory-mcp   # expect: healthy
go build -o /tmp/aura ./cmd/aura     # WSL: native build; never run .exe on the Windows host
time /tmp/aura memory search "dove vive Davide"
time /tmp/aura memory search "codice cliente ZOPPI"
time /tmp/aura memory search "xilofono quantistico marmellata"
```

Expected shape (exact counts move once Task 3 purges the fixtures — that is fine, record what you see):
- «dove vive Davide» returns fewer rows and is several times faster than the pre-change run.
- «xilofono quantistico marmellata» still returns rows. That is the design's declared honest limit, not a regression.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/mcptools/bridge_memory.go internal/agent/mcptools/bridge.go \
        internal/agent/mcptools/bridge_memory_threshold_test.go
git commit -m "feat(memory): supply the recall threshold nobody was passing

memory_search documents a similarity threshold and defaults it to 0.7; no caller
ever set it, so every recall returned its full limit no matter how weakly a row
matched. The bridge already injects user_identifier at the same seam.

Measured on the live graph before this change: <paste the two timings>.

The floor does not make recall say 'I don't know' — a nonsense query still returns
rows. With 27 entities and no edges everything is near everything; that is missing
topology and the fact-edge work is what addresses it.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: One property name for `RELATED_TO`

Not in the design — found while reading the write paths, and it invalidates the design's §3 before it is written. Two writers disagree about where an edge's type lives:

- `CREATE_ENTITY_RELATIONSHIP` (`queries.py:549`, used by `LongTermMemory.add_relationship`) writes `MERGE (e1)-[r:RELATED_TO {type: $relation_type}]->(e2)`.
- `CREATE_ENTITY_RELATION_BY_NAME` / `_BY_ID` (`queries.py:682`/`703`, used by the `memory_create_relationship` tool) write `r.relation_type = $relation_type`.

Live proof, 2026-07-31: of four `RELATED_TO` edges, one carries `relation_type` and three carry `type`. A traversal that reads either property silently sees a fraction of the graph. Today all four edges are test residue so nothing has noticed; from Task 4 on, every edge Aura mints flows through one of these writers.

`relation_type` wins: it is the name the schema documents, the name the by-name/by-id writers already use, and `type` collides with the `Entity.type` property vocabulary.

The same query also lets `source == target` — the live graph has `AuraAuditSource -[:KNOWS]-> AuraAuditSource`. A self-loop is never a fact about the world and it makes any hop-count traversal loop back on itself, so it is refused at the writer.

**Files:**
- Modify: `docker/agent-memory/src/neo4j_agent_memory/graph/queries.py:549-561`
- Modify: `docker/agent-memory/src/neo4j_agent_memory/memory/long_term.py` (`add_relationship`, ~line 1290)
- Test: `docker/agent-memory/tests/test_relation_type_property.py` (create)

**Interfaces:**
- Consumes: `queries.CREATE_ENTITY_RELATIONSHIP`, `queries.CREATE_ENTITY_RELATION_BY_ID`, `LongTermMemory.add_relationship(source, target, relationship_type, ...)`.
- Produces: every `RELATED_TO` edge carries `relation_type` and no `type`. `add_relationship` raises `ValueError` on `source == target`.

- [ ] **Step 1: Write the failing test**

Create `docker/agent-memory/tests/test_relation_type_property.py`:

```python
"""Every RELATED_TO writer must agree on where the edge type lives.

Two writers disagreed: CREATE_ENTITY_RELATIONSHIP wrote `type`, the by-name/by-id
writers wrote `relation_type`. On the live graph 2026-07-31 that produced four edges
split 3/1 across the two spellings, so a traversal reading either one saw part of the
graph and no error anywhere said so.
"""

from __future__ import annotations

import re

import pytest

from neo4j_agent_memory.graph import queries

_RELATED_TO_WRITERS = [
    "CREATE_ENTITY_RELATIONSHIP",
    "CREATE_ENTITY_RELATION_BY_NAME",
    "CREATE_ENTITY_RELATION_BY_ID",
]


@pytest.mark.parametrize("name", _RELATED_TO_WRITERS)
def test_writer_sets_relation_type(name: str) -> None:
    cypher = getattr(queries, name)
    assert "relation_type" in cypher, f"{name} does not write relation_type"


@pytest.mark.parametrize("name", _RELATED_TO_WRITERS)
def test_writer_never_sets_bare_type_on_the_edge(name: str) -> None:
    cypher = getattr(queries, name)
    # `r.type` or `{type: ...}` inside the RELATED_TO pattern — the two shapes the
    # disagreement took. Entity `type` properties elsewhere in a query are fine.
    assert not re.search(r"\br\.type\b", cypher), f"{name} writes r.type"
    assert not re.search(r"RELATED_TO\s*\{[^}]*\btype\s*:", cypher), (
        f"{name} keys RELATED_TO on a bare `type`"
    )
```

Add to the same file a self-loop guard test:

```python
from uuid import uuid4

from neo4j_agent_memory.memory.long_term import LongTermMemory


@pytest.mark.asyncio
async def test_add_relationship_refuses_a_self_loop() -> None:
    entity_id = uuid4()
    memory = LongTermMemory.__new__(LongTermMemory)  # no I/O: the guard precedes every call
    with pytest.raises(ValueError, match="self"):
        await memory.add_relationship(entity_id, entity_id, "KNOWS")
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
docker run --rm -v "$PWD/docker/agent-memory:/app" -w /app aura-agent-memory-mcp:local \
  sh -lc 'pip install --quiet pytest pytest-asyncio && python -m pytest tests/test_relation_type_property.py -q'
```

Expected: FAIL — `CREATE_ENTITY_RELATIONSHIP does not write relation_type`, `CREATE_ENTITY_RELATIONSHIP keys RELATED_TO on a bare `type``, and `DID NOT RAISE ValueError`.

- [ ] **Step 3: Write minimal implementation**

In `queries.py`, replace `CREATE_ENTITY_RELATIONSHIP`:

```python
# MERGE keys on relation_type, the same property the by-name and by-id writers set. It
# used to key on `type`, so the same logical edge written through two entry points landed
# under two property names and no reader could see both.
CREATE_ENTITY_RELATIONSHIP = """
MATCH (e1:Entity {id: $source_id})
MATCH (e2:Entity {id: $target_id})
MERGE (e1)-[r:RELATED_TO {relation_type: $relation_type}]->(e2)
ON CREATE SET
    r.id = $id,
    r.description = $description,
    r.confidence = $confidence,
    r.valid_from = $valid_from,
    r.valid_until = $valid_until,
    r.created_at = datetime()
ON MATCH SET
    r.confidence = CASE WHEN $confidence > r.confidence THEN $confidence ELSE r.confidence END,
    r.updated_at = datetime()
RETURN r
"""
```

In `long_term.py`, at the head of `add_relationship`, before `source_id` is derived:

```python
        source_id = source.id if isinstance(source, Entity) else source
        target_id = target.id if isinstance(target, Entity) else target
        if str(source_id) == str(target_id):
            # A self-loop is never a claim about the world, and a hop-bounded traversal
            # walks it forever. The live graph had one — AuraAuditSource KNOWS itself —
            # written straight through this method by an audit script.
            raise ValueError("add_relationship: refusing a self relationship")
```

(and delete the two now-duplicated `source_id`/`target_id` assignments further down).

- [ ] **Step 4: Run the tests to verify they pass**

```bash
docker run --rm -v "$PWD/docker/agent-memory:/app" -w /app aura-agent-memory-mcp:local \
  sh -lc 'pip install --quiet pytest pytest-asyncio && python -m pytest tests/ -q'
```

Expected: PASS, whole suite. Any pre-existing test asserting on `r.type` is asserting the
bug — fix the test alongside, and say so in the commit body.

- [ ] **Step 5: Migrate the three live edges**

They are all test residue that Task 3 deletes, so no data migration ships. Verify after
Task 3 that the split is gone:

```cypher
MATCH ()-[r:RELATED_TO]->() RETURN count(r.relation_type) AS with_relation_type, count(r.type) AS with_type;
```

Expected once Task 3 has run: `with_type = 0`.

- [ ] **Step 6: Commit**

```bash
git add docker/agent-memory/src/neo4j_agent_memory/graph/queries.py \
        docker/agent-memory/src/neo4j_agent_memory/memory/long_term.py \
        docker/agent-memory/tests/test_relation_type_property.py
git commit -m "fix(memory): make both RELATED_TO writers name the edge type the same way

add_relationship wrote {type: ...}; the by-name and by-id writers wrote
relation_type. Same logical edge, two property names, no error anywhere. Live graph
2026-07-31: of four RELATED_TO edges, three carried type and one carried
relation_type, so a traversal reading either saw part of the graph.

relation_type wins: the schema documents it, two of the three writers already used
it, and 'type' collides with the Entity type vocabulary.

Also refuses source == target. The live graph had AuraAuditSource KNOWS itself; a
self-loop is not a claim about the world and a hop-bounded walk never leaves it.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Purge the test residue — **stops for operator confirmation**

The design's passaggio 0. Measured 2026-07-31: 24 of 27 entities and 20 of 26 facts are
test residue. `Quasar Walrus Almanac` and `Tungsten Turbine Apparatus` occupied two of
four slots in the answer to «dove vive Davide». Until they are gone every quality number
in this plan is measuring the fixtures.

The residue is two classes with two different discriminators, and the design's single
"via le fixture" hides that:

**Class A — foreign test scopes (15 entities, ~14 facts, 27 `:User` nodes).** Written under
identities like `atomic-profile-1785336537977209590`, `codex-live-…`, `short-alice-…`,
`foreign-…`, `aura-e2e-…`. Discriminator: the `:User.identifier` is not a row in Postgres
`aura.identities`. Principled and repeatable — E2E runs keep producing these.

**Class B — residue under the operator's own identity (9 entities).** `Quasar Walrus
Almanac 38489d13dc6f`, `Tungsten Turbine Apparatus 96c61047b6fb`, `AuraAuditSource1785425779497`,
`AuraAuditSource1785422592683`, `Aura Audit Source 1935`, `CalderaWitness1785422592683`,
`NimbusHarbor1785422592683`, `ObsidianCove1785425100754`, `ZephyrArchivist1785425092835`,
`RoboManual AURA-P15-IT-entity-1785399260675299150-319763`. Same `deduplication_scope` as
`Davide` — audit and E2E scripts ran against the live deployment as the real operator.
**No provenance discriminator exists.** These go by an explicit, operator-reviewed name
list, never by a heuristic.

The root cause of class B is that live E2E and audit scripts authenticate as the operator.
Fixing that is a separate concern and is **not** in this plan; note it in the commit body
so it is on the record.

**Files:**
- Create: `internal/knowledge/graphhygiene.go`
- Create: `internal/knowledge/graphhygiene_test.go`
- Create: `internal/knowledge/graphhygiene_integration_test.go`
- Create: `cmd/aura/memory_prune.go`
- Create: `cmd/aura/memory_prune_test.go`
- Modify: `cmd/aura/memory.go` (`memoryVerbToTool` gains the `prune` verb dispatch)

**Interfaces:**
- Consumes (verified 2026-07-31, do not re-derive):
  - `func (c *Client) Read(ctx context.Context, query string, params map[string]any) ([]map[string]any, error)` — `internal/knowledge/client.go:113`
  - `func (c *Client) Write(ctx context.Context, query string, params map[string]any) ([]map[string]any, error)` — `client.go:121`
  - `func asString(v any) string` — **already exists** at `graphview_normalize.go:196`. Do NOT redeclare it; the package will not compile.
  - the identity list from Postgres via the store `cmd/aura` already uses.
- Produces:
  - `type ResidueReport struct { ForeignScopeUsers []string; ForeignScopeEntities int; ForeignScopeFacts int; NamedEntities []string; NamedFacts int }`
  - `func PlanResidue(ctx context.Context, c *Client, realIdentities []string, namedResidue []string) (ResidueReport, error)`
  - `func ApplyResidue(ctx context.Context, c *Client, report ResidueReport) (deleted int, err error)`

- [ ] **Step 1: Write the failing unit test**

Create `internal/knowledge/graphhygiene_test.go`:

```go
package knowledge

import (
	"strings"
	"testing"
)

// The whole safety argument rests on this: a graph :User is residue exactly when no
// Postgres identity claims it. If the identity list arrives empty the predicate matches
// EVERY user, including the operator, so an empty list must refuse rather than plan a
// full wipe.
func TestPlanResidueRefusesAnEmptyIdentityList(t *testing.T) {
	if _, err := foreignScopeQuery(nil); err == nil {
		t.Fatal("empty identity list produced a query instead of an error")
	}
}

func TestForeignScopeQueryExcludesKnownIdentities(t *testing.T) {
	q, err := foreignScopeQuery([]string{"e343c45d-81b8-4229-9cd8-00d0cf9e34b5"})
	if err != nil {
		t.Fatalf("foreignScopeQuery: %v", err)
	}
	if !strings.Contains(q, "$real_identities") {
		t.Fatal("query does not parameterise the identity list")
	}
	if !strings.Contains(q, "NOT u.identifier IN $real_identities") {
		t.Fatalf("query does not exclude known identities:\n%s", q)
	}
}

// Class B has no provenance discriminator, so it is a literal name list and must be
// matched exactly. A prefix or contains match would take "Davide" with "Davide Test".
func TestNamedResidueMatchesExactNamesOnly(t *testing.T) {
	q := namedResidueQuery()
	if !strings.Contains(q, "e.name IN $names") {
		t.Fatalf("named residue is not an exact-name match:\n%s", q)
	}
	for _, forbidden := range []string{"STARTS WITH", "CONTAINS", "=~"} {
		if strings.Contains(q, forbidden) {
			t.Fatalf("named residue query uses %s — must be exact match only:\n%s", forbidden, q)
		}
	}
}

func TestApplyRefusesAnUnplannedReport(t *testing.T) {
	if _, err := ApplyResidue(t.Context(), nil, ResidueReport{}); err == nil {
		t.Fatal("apply accepted an empty report")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
go test ./internal/knowledge/ -run TestPlanResidue -v
go test ./internal/knowledge/ -run TestForeignScope -v
go test ./internal/knowledge/ -run TestNamedResidue -v
go test ./internal/knowledge/ -run TestApplyRefuses -v
```

Expected: FAIL — `undefined: foreignScopeQuery`, `undefined: namedResidueQuery`, `undefined: ApplyResidue`.

- [ ] **Step 3: Write the implementation**

Create `internal/knowledge/graphhygiene.go`:

```go
package knowledge

import (
	"context"
	"errors"
	"fmt"
)

// ResidueReport is what a purge WOULD delete. Nothing deletes without one, and it is
// printed in full before anything is applied: the operator's own memory is on the other
// side of this query and a wrong predicate here is not recoverable from the graph.
type ResidueReport struct {
	ForeignScopeUsers    []string
	ForeignScopeEntities int
	ForeignScopeFacts    int
	NamedEntities        []string
	NamedFacts           int
}

// Empty reports whether the report would delete nothing.
func (r ResidueReport) Empty() bool {
	return len(r.ForeignScopeUsers) == 0 && len(r.NamedEntities) == 0
}

var errNoIdentities = errors.New("graph hygiene: refusing to plan with an empty identity list")

// foreignScopeQuery finds :User nodes no Postgres identity claims, and everything hanging
// off them. Test runs create a fresh :User per case and never remove it — 27 of the 30
// users on the live graph 2026-07-31 were residue of exactly this kind.
//
// The identity list is the ONLY thing standing between this and a full wipe, so an empty
// one is an error, not a permissive default.
func foreignScopeQuery(realIdentities []string) (string, error) {
	if len(realIdentities) == 0 {
		return "", errNoIdentities
	}
	return `
MATCH (u:User)
WHERE NOT u.identifier IN $real_identities
OPTIONAL MATCH (u)-[:HAS_ENTITY]->(e:Entity)
OPTIONAL MATCH (u)-[:HAS_FACT]->(f:Fact)
OPTIONAL MATCH (u)-[:HAS_PREFERENCE]->(p:Preference)
RETURN u.identifier AS identifier,
       count(DISTINCT e) AS entities,
       count(DISTINCT f) AS facts,
       count(DISTINCT p) AS preferences
ORDER BY identifier`, nil
}

// namedResidueQuery counts what an explicit name list would remove. Class B residue was
// written by audit and E2E scripts authenticating AS the operator, so it carries the
// operator's deduplication_scope and is indistinguishable from real memory by provenance.
// An exact-name list reviewed by the operator is the only honest discriminator; a prefix
// match would eventually take a real entity whose name starts the same way.
func namedResidueQuery() string {
	return `
MATCH (e:Entity)
WHERE e.name IN $names
OPTIONAL MATCH (f:Fact)-[:ABOUT_SUBJECT]->(e)
RETURN e.name AS name, count(f) AS facts
ORDER BY name`
}

// PlanResidue reads what a purge would remove. It writes nothing.
func PlanResidue(ctx context.Context, c *Client, realIdentities, namedResidue []string) (ResidueReport, error) {
	foreign, err := foreignScopeQuery(realIdentities)
	if err != nil {
		return ResidueReport{}, err
	}
	var report ResidueReport
	rows, err := c.Read(ctx, foreign, map[string]any{"real_identities": realIdentities})
	if err != nil {
		return ResidueReport{}, fmt.Errorf("plan foreign-scope residue: %w", err)
	}
	for _, row := range rows {
		report.ForeignScopeUsers = append(report.ForeignScopeUsers, asString(row["identifier"]))
		report.ForeignScopeEntities += asCount(row["entities"])
		report.ForeignScopeFacts += asCount(row["facts"])
	}
	if len(namedResidue) > 0 {
		named, err := c.Read(ctx, namedResidueQuery(), map[string]any{"names": namedResidue})
		if err != nil {
			return ResidueReport{}, fmt.Errorf("plan named residue: %w", err)
		}
		for _, row := range named {
			report.NamedEntities = append(report.NamedEntities, asString(row["name"]))
			report.NamedFacts += asCount(row["facts"])
		}
	}
	return report, nil
}

// ApplyResidue deletes exactly what the report names. It re-derives nothing: the operator
// approved a list, and this executes that list.
func ApplyResidue(ctx context.Context, c *Client, report ResidueReport) (int, error) {
	if report.Empty() {
		return 0, errors.New("graph hygiene: refusing to apply an empty report")
	}
	deleted := 0
	if len(report.ForeignScopeUsers) > 0 {
		rows, err := c.Write(ctx, `
MATCH (u:User)
WHERE u.identifier IN $identifiers
OPTIONAL MATCH (u)-[*1..2]->(owned)
WHERE owned:Entity OR owned:Fact OR owned:Preference
WITH collect(DISTINCT u) + collect(DISTINCT owned) AS doomed
UNWIND doomed AS n
DETACH DELETE n
RETURN count(n) AS deleted`, map[string]any{"identifiers": report.ForeignScopeUsers})
		if err != nil {
			return deleted, fmt.Errorf("apply foreign-scope residue: %w", err)
		}
		deleted += countRows(rows, "deleted")
	}
	if len(report.NamedEntities) > 0 {
		rows, err := c.Write(ctx, `
MATCH (e:Entity)
WHERE e.name IN $names
OPTIONAL MATCH (f:Fact)-[:ABOUT_SUBJECT]->(e)
WITH collect(DISTINCT e) + collect(DISTINCT f) AS doomed
UNWIND doomed AS n
DETACH DELETE n
RETURN count(n) AS deleted`, map[string]any{"names": report.NamedEntities})
		if err != nil {
			return deleted, fmt.Errorf("apply named residue: %w", err)
		}
		deleted += countRows(rows, "deleted")
	}
	return deleted, nil
}
```

Two helpers this file needs, added alongside the existing `asString` in
`graphview_normalize.go` (`asString` and `asMap` are already there — reuse, don't redeclare):

```go
// asCount coerces a Cypher count() result. Rows arrive as decoded JSON, so an integer
// count is a float64 — reading it as int yields zero and a purge that reports nothing to
// delete looks exactly like a clean graph.
func asCount(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0
		}
		return int(i)
	default:
		return 0
	}
}

func countRows(rows []map[string]any, key string) int {
	total := 0
	for _, row := range rows {
		total += asCount(row[key])
	}
	return total
}
```

- [ ] **Step 4: Run the unit tests**

```bash
go test ./internal/knowledge/ -run 'TestPlanResidue|TestForeignScope|TestNamedResidue|TestApplyRefuses' -v
go vet ./... && go build ./...
```

Expected: PASS.

- [ ] **Step 5: Write the live integration test**

Create `internal/knowledge/graphhygiene_integration_test.go` with `//go:build neo4j_integration`.
It seeds a throwaway `:User` with a known non-identity name plus one entity and one fact,
plans, applies, and asserts both the seeded nodes are gone and a control node owned by a
listed identity survives. Follow the seeding and env conventions in
`internal/knowledge/graphview_integration_test.go` and `integration_env_test.go` — including
the `t.Fatal`-under-`$CI` skip-helper rule from CLAUDE.md.

```bash
go test -tags neo4j_integration ./internal/knowledge/ -run TestResidue -v
```

Expected: PASS, with a runtime well above a second (a sub-second "integration" run is the skip tell).

- [ ] **Step 6: Wire the CLI verb**

Create `cmd/aura/memory_prune.go` exposing `aura memory prune` — **dry-run by default**,
printing the full `ResidueReport`; `--apply` executes. The named residue list is a
`--names` flag or a file, never compiled in, so the operator reviews it every time.
Add the verb to `memoryVerbToTool` in `cmd/aura/memory.go` (which stays under 600 LOC).
Cover verb parsing and the dry-run default in `cmd/aura/memory_prune_test.go`.

- [ ] **Step 7: STOP — back up, show the operator, get a go-ahead**

Do not proceed without an explicit yes. Take the dump first:

```bash
docker compose stop neo4j
docker compose run --rm neo4j neo4j-admin database dump neo4j --to-path=/backups
docker compose start neo4j
```

Then show the plan and wait:

```bash
/tmp/aura memory prune --names-file docs/graph-residue-2026-07-31.txt
```

Present: the full list of `:User` identifiers to be deleted, the entity/fact counts, and the
nine class-B names. Confirm `Davide`, `Caraglio`, `PmSync` and their six real facts are **not**
in it. Only then run with `--apply`.

- [ ] **Step 8: Verify and measure**

```cypher
MATCH (n) UNWIND labels(n) AS l RETURN l, count(*) AS c ORDER BY c DESC;
MATCH ()-[r:RELATED_TO]->() RETURN count(r.relation_type) AS with_relation_type, count(r.type) AS with_type;
```

Expected: `:Entity` 3, `:Fact` 6, `:User` 1 (plus any real identities), `RELATED_TO` 0,
`with_type` 0. `:Chunk` still 302 — the document corpus is untouched.

Then re-run Task 1's live queries. «dove vive Davide» must now return `Davide` and
`Caraglio` and nothing else.

- [ ] **Step 9: Commit**

```bash
git add internal/knowledge/graphhygiene.go internal/knowledge/graphhygiene_test.go \
        internal/knowledge/graphhygiene_integration_test.go \
        cmd/aura/memory_prune.go cmd/aura/memory_prune_test.go cmd/aura/memory.go \
        docs/graph-residue-2026-07-31.txt
git commit -m "feat(memory): remove the test residue that was answering the operator's questions

24 of 27 entities and 20 of 26 facts on the live graph were test fixtures. Measured:
'Quasar Walrus Almanac' and 'Tungsten Turbine Apparatus' held two of the four slots
in the answer to «dove vive Davide». Every quality number about this graph was
measuring the fixtures.

Two classes, two discriminators, because they are not the same problem. Foreign test
scopes hang off :User nodes no Postgres identity claims — repeatable, and E2E keeps
making more. The rest was written by audit and E2E scripts authenticating AS the
operator, so it carries the operator's own deduplication_scope and no provenance
tells it from real memory; those go by an exact name list the operator reviewed.

Root cause of the second class is that live scripts run as the real operator. Not
fixed here.

Backed up before applying: neo4j-admin database dump.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Typed endpoints materialize the edge at write time

The design's §1. `memory_add_fact` takes three strings and writes a flat `Fact`. It already
links the fact to the entity its **subject** names, when the caller owns one
(`_link_fact_to_subject_entity`), and deliberately mints nothing. The object end has
nothing at all.

Adding optional `subject_type` and `object_type` lets the caller — the LLM, which just
wrote the triple and knows what the ends are — say which ends are entities. An end typed
`PERSON`/`ORGANIZATION`/`LOCATION`/`CONCEPT` gets `MERGE`d and joined by `RELATED_TO`. An
end typed `LITERAL`, or untyped, mints nothing and stays a fact property. The `Fact` node
survives either way: it is the evidence, with provenance and confidence; the edge is the
navigable view.

**Files:**
- Modify: `docker/agent-memory/src/neo4j_agent_memory/memory/long_term.py` (`add_fact`, new `_materialize_fact_edge`)
- Modify: `docker/agent-memory/src/neo4j_agent_memory/integration.py` (`add_fact`, ~line 286)
- Modify: `docker/agent-memory/src/neo4j_agent_memory/mcp/_tools.py` (`memory_add_fact`, ~line 308)
- Modify: `docker/agent-memory/src/neo4j_agent_memory/mcp/_instructions.py` (the guidance the model reads)
- Test: `docker/agent-memory/tests/test_fact_edge_materialization.py` (create)

**Interfaces:**
- Consumes: `LongTermMemory.add_entity(name, entity_type, *, user_identifier, ...) -> tuple[Entity, DeduplicationResult]`; `LongTermMemory.add_relationship(source, target, relationship_type, *, confidence)` (as fixed in Task 2); `LongTermMemory._resolve_owned_entity_id(user_identifier, name) -> str | None`.
- Produces:
  - `LongTermMemory.add_fact(..., subject_type: str | None = None, object_type: str | None = None)`.
  - `LongTermMemory._materialize_fact_edge(user_identifier, subject, subject_type, predicate, obj, object_type, confidence) -> str | None` returning the created edge's target entity id, or `None`.
  - `MemoryIntegration.add_fact(..., subject_type=None, object_type=None)` whose result dict gains `"materialized_edge": bool` and `"linked_object_entity": str | None`.
  - MCP tool `memory_add_fact(..., subject_type: str | None = None, object_type: str | None = None)`.
  - Module constant `_ENTITY_ENDPOINT_TYPES = {"PERSON", "ORGANIZATION", "LOCATION", "CONCEPT"}` and `_LITERAL_ENDPOINT = "LITERAL"`.

- [ ] **Step 1: Write the failing test**

Create `docker/agent-memory/tests/test_fact_edge_materialization.py`. Follow the fake-client
and fixture style already used by `tests/test_fact_entity_link.py` — read that file first
and reuse its harness rather than building a second one.

```python
"""Typed fact endpoints become a traversable edge; literal endpoints stay properties.

Before this, memory_add_fact wrote a flat Fact and at most an ABOUT_SUBJECT edge to an
entity that already existed. Live graph 2026-07-31: 22 of 26 facts had no edge at all,
and `Davide works_for PmSync` sat beside an :Organization named PmSync with nothing
joining them, so a two-hop question fell back to vector search over single facts.
"""

from __future__ import annotations

import pytest

from neo4j_agent_memory.memory.long_term import (
    _ENTITY_ENDPOINT_TYPES,
    _LITERAL_ENDPOINT,
)


@pytest.mark.asyncio
async def test_typed_endpoints_create_the_edge(memory, user_identifier):
    await memory.add_fact(
        "Davide", "works_for", "PmSync",
        subject_type="PERSON", object_type="ORGANIZATION",
        user_identifier=user_identifier,
    )
    edges = await memory_edges(memory, "Davide")
    assert ("works_for", "PmSync") in edges


@pytest.mark.asyncio
async def test_literal_object_mints_no_node(memory, user_identifier):
    await memory.add_fact(
        "Davide", "timezone", "Europe/Rome",
        subject_type="PERSON", object_type=_LITERAL_ENDPOINT,
        user_identifier=user_identifier,
    )
    assert await entity_named(memory, "Europe/Rome") is None
    assert await fact_property(memory, "Davide", "timezone") == "Europe/Rome"


@pytest.mark.asyncio
async def test_untyped_call_behaves_exactly_as_before(memory, user_identifier):
    """The compatibility guarantee: no types, no new nodes, no new edges."""
    before = await graph_counts(memory)
    await memory.add_fact("Davide", "role", "programmatore", user_identifier=user_identifier)
    after = await graph_counts(memory)
    assert after["entities"] == before["entities"]
    assert after["related_to"] == before["related_to"]
    assert after["facts"] == before["facts"] + 1


@pytest.mark.asyncio
async def test_unknown_endpoint_type_is_refused_not_guessed(memory, user_identifier):
    with pytest.raises(ValueError, match="endpoint type"):
        await memory.add_fact(
            "Davide", "works_for", "PmSync",
            subject_type="PERSON", object_type="EMPLOYER",
            user_identifier=user_identifier,
        )


@pytest.mark.asyncio
async def test_the_fact_survives_materialization(memory, user_identifier):
    """The edge is the view; the fact is the evidence. Losing the fact loses provenance."""
    fact = await memory.add_fact(
        "Davide", "located_in", "Caraglio",
        subject_type="PERSON", object_type="LOCATION",
        user_identifier=user_identifier,
    )
    assert fact.id is not None
    assert fact.confidence == 1.0


def test_literal_is_not_an_entity_type():
    assert _LITERAL_ENDPOINT not in _ENTITY_ENDPOINT_TYPES
```

`memory`, `user_identifier`, `memory_edges`, `entity_named`, `fact_property` and
`graph_counts` are fixtures/helpers — take them from `tests/test_fact_entity_link.py`
if it has equivalents, otherwise add them to the new file against the same fake graph
client that file uses.

- [ ] **Step 2: Run to verify it fails**

```bash
docker run --rm -v "$PWD/docker/agent-memory:/app" -w /app aura-agent-memory-mcp:local \
  sh -lc 'pip install --quiet pytest pytest-asyncio && python -m pytest tests/test_fact_edge_materialization.py -q'
```

Expected: FAIL — `cannot import name '_ENTITY_ENDPOINT_TYPES'`.

- [ ] **Step 3: Write the implementation**

In `long_term.py`, near the module's other constants:

```python
# The endpoint vocabulary a fact may declare. These are the POLE+O entity labels the
# schema already carries — nothing new is invented here.
_ENTITY_ENDPOINT_TYPES = frozenset({"PERSON", "ORGANIZATION", "LOCATION", "CONCEPT"})

# LITERAL is how a caller says "this end is a value, not a thing": a timezone, a client
# code, a job title, a timestamp. It mints nothing. Without a way to say this, typing the
# ends would fill the graph with a node per string.
_LITERAL_ENDPOINT = "LITERAL"
```

Add the two parameters to `add_fact`'s signature (`subject_type: str | None = None`,
`object_type: str | None = None`, keyword-only, after `metadata`), validate them at the top
of the method before any I/O:

```python
        for label, value in (("subject_type", subject_type), ("object_type", object_type)):
            if value is None:
                continue
            if value not in _ENTITY_ENDPOINT_TYPES and value != _LITERAL_ENDPOINT:
                # Guessing here is how a typo becomes a node. Refuse and say what is valid.
                raise ValueError(
                    f"add_fact: unknown endpoint type {value!r} for {label}; "
                    f"valid: {sorted(_ENTITY_ENDPOINT_TYPES)} or {_LITERAL_ENDPOINT!r}"
                )
```

and call the materializer at the end, beside the existing subject link:

```python
        if user_identifier is not None:
            await self._link_user_to_fact(user_identifier, str(fact.id))
            await self._link_fact_to_subject_entity(
                user_identifier, str(fact.id), fact.subject
            )
            await self._materialize_fact_edge(
                user_identifier,
                subject=fact.subject,
                subject_type=subject_type,
                predicate=fact.predicate,
                obj=fact.object,
                object_type=object_type,
                confidence=confidence,
            )
```

Then the materializer itself:

```python
    async def _materialize_fact_edge(
        self,
        user_identifier: str,
        *,
        subject: str,
        subject_type: str | None,
        predicate: str,
        obj: str,
        object_type: str | None,
        confidence: float,
    ) -> str | None:
        """Turn a fact whose two ends are entities into a traversable edge.

        The Fact node stays: it carries provenance, confidence and temporal bounds, and it
        is the evidence behind the edge. The edge is what a traversal can walk — which is
        the thing the graph did not have. Live graph 2026-07-31: `Davide works_for PmSync`
        was stored beside an :Organization named PmSync with no edge between them, so
        «dove lavora la persona che vive a Caraglio» could not be answered by structure.

        Both ends must be declared entities. One typed end is not enough for an edge, and
        inferring the other is the guess this design refuses to make.
        """
        if subject_type not in _ENTITY_ENDPOINT_TYPES:
            return None
        if object_type not in _ENTITY_ENDPOINT_TYPES:
            return None

        source_id = await self._resolve_owned_entity_id(user_identifier, subject)
        if source_id is None:
            source, _ = await self.add_entity(
                subject, subject_type, user_identifier=user_identifier
            )
            source_id = str(source.id)

        target_id = await self._resolve_owned_entity_id(user_identifier, obj)
        if target_id is None:
            target, _ = await self.add_entity(
                obj, object_type, user_identifier=user_identifier
            )
            target_id = str(target.id)

        if source_id == target_id:
            # `X same_as X` after resolution collapses both ends onto one node. Task 2's
            # guard would raise; there is nothing wrong to report, so stop quietly.
            return None

        await self.add_relationship(
            UUID(source_id), UUID(target_id), predicate, confidence=confidence
        )
        return target_id
```

In `integration.py`'s `add_fact`, thread both parameters through to
`self.client.long_term.add_fact` and extend the returned dict:

```python
            return {
                "stored": True,
                "type": "fact",
                "id": str(fact.id) if hasattr(fact, "id") else None,
                "triple": f"{subject} -> {predicate} -> {object_value}",
                "linked_subject_entity": linked_subject_entity,
                "materialized_edge": bool(materialized),
                "linked_object_entity": materialized,
            }
```

`long_term.add_fact` returns a `Fact`, not a tuple, and changing that return type would
break every existing caller. So the materializer's result rides on the fact it belongs to:
in `add_fact`, assign it before returning —

```python
            materialized = await self._materialize_fact_edge(...)
            if materialized is not None:
                fact.metadata["materialized_object_entity"] = materialized
```

— and in `integration.add_fact` read it back with
`materialized = fact.metadata.get("materialized_object_entity")`. No second round-trip.

In `_tools.py`, add both parameters to `memory_add_fact` and extend the docstring with
the guidance the model actually acts on:

```
            subject_type: What the subject IS, when it is a thing rather than a value:
                'PERSON', 'ORGANIZATION', 'LOCATION' or 'CONCEPT'. Pass 'LITERAL' for a
                value. Declaring BOTH ends as entities is what makes the fact walkable —
                'Davide' PERSON works_for 'PmSync' ORGANIZATION becomes an edge you can
                traverse later; the same triple untyped stays a string.
            object_type: Same vocabulary, for the object. Use 'LITERAL' for codes,
                timezones, dates, job titles and other values — they belong on the fact,
                not as nodes.
```

Mirror the same two sentences into `_instructions.py` beside the existing
`memory_add_fact` guidance.

- [ ] **Step 4: Run the tests**

```bash
docker run --rm -v "$PWD/docker/agent-memory:/app" -w /app aura-agent-memory-mcp:local \
  sh -lc 'pip install --quiet pytest pytest-asyncio && python -m pytest tests/ -q'
```

Expected: PASS, whole suite. `test_untyped_call_behaves_exactly_as_before` is the
compatibility gate — if it fails, the change is not backward compatible.

- [ ] **Step 5: Rebuild the sidecar and prove it end to end on the live stack**

```bash
docker compose build aura-agent-memory-mcp
docker compose up -d aura-agent-memory-mcp
docker inspect -f '{{.State.Health.Status}}' aura-agent-memory-mcp
```

Verify the image the container actually runs matches the tag just built (compare
`docker inspect -f '{{.Image}}' aura-agent-memory-mcp` against
`docker image inspect -f '{{.Id}}' aura-agent-memory-mcp:local`) — a stale container is
the classic way this looks like it worked.

Then write a real fact through the real bridge and read the edge back:

```bash
/tmp/aura memory add-fact Davide works_for PmSync --subject-type PERSON --object-type ORGANIZATION
```

(extend `memoryAddFactArgs` in `cmd/aura/memory.go` with the two flags as part of this
step — the CLI is how this gets exercised without a model in the loop)

```cypher
MATCH (:Person {name:"Davide"})-[r:RELATED_TO]->(o:Organization {name:"PmSync"})
RETURN r.relation_type, r.confidence;
```

Expected: one row, `relation_type = "works_for"`. And the fact still exists:

```cypher
MATCH (f:Fact {subject:"Davide", predicate:"works_for"}) RETURN f.object, f.confidence;
```

- [ ] **Step 6: Measure the cost**

The write path now does up to two extra `MERGE`s and one relationship write. Time it:

```bash
for i in 1 2 3 4 5; do
  /usr/bin/time -f '%e' /tmp/aura memory add-fact "Probe$i" knows "Target$i" \
    --subject-type PERSON --object-type PERSON
done
```

Compare against five untyped writes. Record both in the commit body. The extra work is two
indexed `MERGE`s on `{name, type, deduplication_scope}`; if it costs more than a few tens
of milliseconds, `EXPLAIN` the `MERGE` and check the constraint is being used before
accepting the number.

- [ ] **Step 7: Commit**

```bash
git add docker/agent-memory/src/neo4j_agent_memory/memory/long_term.py \
        docker/agent-memory/src/neo4j_agent_memory/integration.py \
        docker/agent-memory/src/neo4j_agent_memory/mcp/_tools.py \
        docker/agent-memory/src/neo4j_agent_memory/mcp/_instructions.py \
        docker/agent-memory/tests/test_fact_edge_materialization.py \
        cmd/aura/memory.go
git commit -m "feat(memory): let a fact declare its ends so it becomes an edge

memory_add_fact took three strings and wrote a flat Fact. Live graph 2026-07-31: 22
of 26 facts had no edge at all, and 'Davide works_for PmSync' sat beside an
:Organization named PmSync with nothing joining them — so «dove lavora la persona che
vive a Caraglio» was not answerable by traversal and fell back to vector search over
single facts.

Both ends may now declare a type. Two entity ends get MERGEd and joined by
RELATED_TO; a LITERAL end mints nothing and stays a fact property, which is what
keeps Europe/Rome and 424410 from becoming nodes. The type comes from the LLM that
just wrote the triple and already knows. An unknown type is refused, not guessed.

The Fact node stays either way: it holds provenance and confidence and is the
evidence behind the edge. Untyped callers are byte-identical to before.

Write cost: <paste typed vs untyped timings>.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Backfill the facts already stored

The design's §2 steps 2–3, for the corpus that exists. After Task 3 the graph holds six
real facts, four of them about `Davide`, and two of those name entities on both ends
(`located_in Caraglio`, `works_for PmSync`). Task 4 only helps facts written from now on.

This is deliberately **not** typing anything. It materializes an edge exactly when the
object string already matches an entity the same user owns — an exact name match against
an existing node, which is evidence, not inference. Facts whose object matches nothing stay
flat and are counted in the report, so the gap is visible rather than guessed at. Automatic
typing of the remainder is what the GLiNER work is for, and it is not in this plan.

**Files:**
- Modify: `internal/knowledge/graphhygiene.go` (add the backfill query + runner)
- Modify: `internal/knowledge/graphhygiene_test.go`
- Modify: `internal/knowledge/graphhygiene_integration_test.go`
- Create: `internal/cron/handlers/graph_backfill.go`
- Create: `internal/cron/handlers/graph_backfill_test.go`

**Interfaces:**
- Consumes: `knowledge.Client`, `handlers.Handler`, `handlers.TaskKind`, `newCountingSweep` (the pattern `identity_purge.go` and `retention.go` both use).
- Produces:
  - `func BackfillFactEdges(ctx context.Context, c *Client) (materialized int, skipped int, err error)`
  - `const KindGraphBackfill TaskKind = "graph_backfill"`
  - `func NewGraphBackfillHandler(backfiller GraphBackfiller) Handler` with `type GraphBackfiller interface { BackfillFactEdges(ctx context.Context) (int, error) }`

- [ ] **Step 1: Write the failing test**

Add to `internal/knowledge/graphhygiene_test.go`:

```go
// The backfill materializes an edge only where the object string already names an entity
// the SAME user owns. Anything looser turns "role -> programmatore" into a node.
func TestBackfillQueryRequiresBothEndsOwnedByTheSameUser(t *testing.T) {
	q := backfillFactEdgesQuery()
	if !strings.Contains(q, "HAS_ENTITY") {
		t.Fatalf("backfill does not root on ownership:\n%s", q)
	}
	if strings.Count(q, "HAS_ENTITY") < 2 {
		t.Fatalf("backfill checks ownership on only one end:\n%s", q)
	}
	if !strings.Contains(q, "relation_type") {
		t.Fatalf("backfill does not set relation_type:\n%s", q)
	}
	// No CREATE of entities. The backfill connects what exists; it does not mint.
	if strings.Contains(q, "MERGE (o:Entity") || strings.Contains(q, "CREATE (o:Entity") {
		t.Fatalf("backfill mints entities:\n%s", q)
	}
}

func TestBackfillRefusesSelfEdges(t *testing.T) {
	q := backfillFactEdgesQuery()
	if !strings.Contains(q, "subject <> object") && !strings.Contains(q, "s <> o") {
		t.Fatalf("backfill does not exclude self edges:\n%s", q)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
go test ./internal/knowledge/ -run TestBackfill -v
```

Expected: FAIL — `undefined: backfillFactEdgesQuery`.

- [ ] **Step 3: Write the implementation**

In `internal/knowledge/graphhygiene.go`:

```go
// backfillFactEdgesQuery materializes an edge for every stored fact whose subject AND
// object each already name an entity the same user owns. It mints nothing: an exact name
// match against a node that exists is evidence, and anything looser turns
// "role -> programmatore" into a :Concept nobody asked for.
//
// Rooted on HAS_ENTITY at both ends — the strictest scope edge — for the same reason
// FIND_SCOPED_ENTITY_BY_NAME is: a name-keyed match that accepted MENTIONS would attach
// one user's fact to whatever node happens to carry that name.
func backfillFactEdgesQuery() string {
	return `
MATCH (u:User)-[:HAS_FACT]->(f:Fact)
MATCH (u)-[:HAS_ENTITY]->(s:Entity)
WHERE s.name = f.subject
MATCH (u)-[:HAS_ENTITY]->(o:Entity)
WHERE o.name = f.object AND s.name <> o.name
MERGE (s)-[r:RELATED_TO {relation_type: f.predicate}]->(o)
ON CREATE SET
    r.confidence = f.confidence,
    r.created_at = datetime(),
    r.materialized_from_fact = f.id
ON MATCH SET
    r.confidence = CASE WHEN f.confidence > r.confidence THEN f.confidence ELSE r.confidence END,
    r.updated_at = datetime()
RETURN count(r) AS materialized`
}

// backfillSkippedQuery counts the facts the backfill could NOT materialize, so the gap is
// a number in the log rather than an assumption. These are the facts a typing pass would
// have to reach; nothing here guesses at them.
func backfillSkippedQuery() string {
	return `
MATCH (u:User)-[:HAS_FACT]->(f:Fact)
WHERE NOT EXISTS {
  MATCH (u)-[:HAS_ENTITY]->(o:Entity) WHERE o.name = f.object
}
RETURN count(f) AS skipped`
}

// BackfillFactEdges runs both and reports what moved and what did not.
func BackfillFactEdges(ctx context.Context, c *Client) (int, int, error) {
	rows, err := c.Write(ctx, backfillFactEdgesQuery(), nil)
	if err != nil {
		return 0, 0, fmt.Errorf("backfill fact edges: %w", err)
	}
	materialized := countRows(rows, "materialized")
	skippedRows, err := c.Read(ctx, backfillSkippedQuery(), nil)
	if err != nil {
		return materialized, 0, fmt.Errorf("count unmaterialized facts: %w", err)
	}
	return materialized, countRows(skippedRows, "skipped"), nil
}
```

Then `internal/cron/handlers/graph_backfill.go`, modelled line-for-line on
`identity_purge.go` (read it first): a `KindGraphBackfill` constant, a consumer-declared
`GraphBackfiller` seam, a bounded duration, and `newCountingSweep`. A nil backfiller
yields the disabled no-op, not an error.

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/knowledge/ ./internal/cron/handlers/ -v
go test -race ./internal/knowledge/ ./internal/cron/handlers/
go vet ./... && go build ./...
```

Expected: PASS.

- [ ] **Step 5: Extend the live integration test and run it**

Add a case to `internal/knowledge/graphhygiene_integration_test.go`: seed a user owning two
entities and a fact naming both, plus a fact whose object matches no entity. Run
`BackfillFactEdges`. Assert one edge with the right `relation_type`, and `skipped == 1`.

```bash
go test -tags neo4j_integration ./internal/knowledge/ -run TestBackfill -v
```

- [ ] **Step 6: Run it against the live graph and measure**

```bash
/tmp/aura memory backfill-edges
```

(or invoke `BackfillFactEdges` through the cron handler's manual trigger — whichever the
scheduler already supports; do not add a second entry point)

Then the numbers that matter:

```cypher
MATCH ()-[r:RELATED_TO]->() RETURN count(r) AS edges;
MATCH (:Person {name:"Davide"})-[r:RELATED_TO]->(n) RETURN r.relation_type, labels(n), n.name;
```

Expected: `Davide -[located_in]-> Caraglio` and `Davide -[works_for]-> PmSync`. That is the
design's opening complaint closed: two hops now exist where there were none.

Then the design's own two-hop question, through the agent:

```bash
/tmp/aura chat "dove lavora la persona che vive a Caraglio?"
```

Expected: `PmSync`, reached through the graph. Record the answer and the tool trace
(`· <tool>` lines) in the commit body — that trace is the ground truth for what was
actually used.

Note honestly: with §3 (walking retrieval) not yet built, recall may still reach this by
vector search rather than traversal. If so, say that in the commit body — the edges are a
precondition for §3, and claiming traversal that did not happen is worse than shipping the
precondition.

- [ ] **Step 7: Commit**

```bash
git add internal/knowledge/graphhygiene.go internal/knowledge/graphhygiene_test.go \
        internal/knowledge/graphhygiene_integration_test.go \
        internal/cron/handlers/graph_backfill.go internal/cron/handlers/graph_backfill_test.go
git commit -m "feat(memory): connect the facts that were already there

Typed write only helps facts written from now on. The facts already stored name
entities that already exist and nothing joined them: Davide located_in Caraglio and
Davide works_for PmSync were three unconnected nodes and two strings.

The backfill materializes an edge exactly where the object string already matches an
entity the same user owns — an exact match against a node that exists, not inference.
Facts whose object matches nothing stay flat and are COUNTED, so the remaining gap is
a number in the log instead of an assumption. Typing those is the GLiNER work and is
not here.

Live result: <paste edge count before/after and the Davide neighbourhood>.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Phase close

After Task 5, before any push:

- [ ] Full local gate: `make quality` (WSL, `~/.local/bin:~/go/bin` on PATH).
- [ ] Coverage against the stricter of the two CI jobs — the Skills gate runs `db_integration` only: `bash scripts/coverage_docker.sh`. Owned-surface floor ≥85%; `internal/knowledge` and `internal/cron/handlers` both gain code here and must clear it.
- [ ] Mutation spot-check ≥70% on `internal/knowledge/graphhygiene.go` (WSL, `go-mutesting`). Its predicates decide what gets deleted; a surviving mutant there is a deletion that could go wrong.
- [ ] Quality snapshot: `docs/aura-quality-snapshot.md` — bump `Last measured` and prepend a re-attestation note for every row whose glob matches a changed file. Verify first: `AURA_QUALITY_CHANGED_FILES="$(git diff --name-only origin/master..HEAD)" AURA_QUALITY_BASE_DATE="$(git log -1 --format=%cs origin/master)" bash scripts/quality_snapshot_gate.sh` must print `ok: … checked N row(s)`.
- [ ] Update the design doc's census table with the post-purge, post-backfill numbers. The
      2026-07-31 figures in it are now history and should be labelled as such.
- [ ] Push and confirm every CI job green.

## Spec coverage

| Design section | Where |
|---|---|
| §1 — archi alla scrittura | Task 4 |
| §2 passaggio 0 — pulizia | Task 3 |
| §2.1 — tipizzazione automatica | **deferred** — needs GLiNER and the evaluation set the design says does not exist yet |
| §2.2 — risoluzione (`MERGE` canonico + `SAME_AS`) | **already shipped** in the fork: provenance-safe scoped dedup writes `SAME_AS` with `match_type`/`status`/`confidence`, covered by `tests/test_entity_dedup_safety.py`, and `memory_forget` on a relationship breaks a wrong merge. Task 3 removes the residue that made it look absent. Nothing new to build. |
| §2.3 — materializzazione | Task 5 |
| §2.4 — comunità (Leiden) | **deferred** — plan 2 below |
| §3 — recupero che cammina | **deferred** — plan 1 below |
| §4 — la soglia | Task 1 |
| *(not in the design)* two writers disagreeing on the edge-type property | Task 2 |

## Not in this plan

Each gets its own plan, in this order:

1. **Walking retrieval (design §3)** — anchor by exact name, then vector with floor; 1–2 hop traversal with fan-out cap; RRF fusion of the two legs using the same `rrfK` as `internal/documents/seed_fusion.go`. This is the task that makes the edges pay. It is a different subsystem (read path, in `integration_context.py`) from everything above.
2. **Communities (design §2 step 4)** — `gds.leiden` over the entity graph, one summary per community, invalidated by `MemoryCorpusRevision.epoch`. Needs a graph with enough edges to have communities, which is why it follows.
3. **GLiNER ONNX typing net (design §1 fallback)** — fp16 only (int8 measured at zero entities), ~700 MB budget, ~100 lines of own pre/post-processing or a `transformers.js` sidecar. It types what the LLM left untyped. It is not on the critical path for any of the above and the design says its evaluation set does not exist yet — build that first.
4. **Live E2E stops writing as the operator** — the root cause of Task 3's class-B residue. Without it the purge is a recurring chore.
