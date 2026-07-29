# P0 Remediation Evidence

## Verdict

**GO for the complete P0 remediation scope.**

- Open P0 findings: **0**
- Remediated and verified P0 findings: **2**
- Scoped score: **10.0 / 10**
- Findings closed: **SEC-001, SEC-002**
- Implementation revision: `30e1d8a98`
- Permanent real-agent gate revision: `3ba6fb4b4`

This verdict is deliberately scoped. The original audit's **3.4 / 10 NO-GO**
for the full Agent Memory and context system remains in force because 16 P1
findings were not part of the request to fix all P0s.

## SEC-001 closure

The sidecar boundary no longer trusts a payload-selected tenant.

1. `mcp/auth.py` verifies short-lived HS256 JWTs with issuer `aura`, audience
   `agent-memory`, and scope `memory:access`.
2. The authenticated subject overwrites any caller-supplied
   `user_identifier`.
3. Missing, malformed, expired, incorrectly signed, wrongly scoped, or
   service-subject tool credentials fail before tool execution.
4. Authenticated mode disables FastMCP resources, removing the global resource
   surface.
5. Aura's memory recipe requires a secret of at least 32 bytes and mints a new
   tenant-bound token for every tool call. Initialization and discovery use a
   non-human service subject that cannot execute tools.
6. Compose enables Agent Memory multi-tenancy and requires the same secret on
   both sides. The installer generates it rather than shipping a default.

### Live adversarial proof

The rebuilt local image was started through WSL and Docker Desktop:

- Docker Engine: 29.6.1
- Agent Memory: FastMCP 2.14.7
- Endpoint: loopback-only `127.0.0.1:8091`
- Sidecar health: `healthy`

Race-enabled live tests proved:

- an unauthenticated MCP initialize returns HTTP 401;
- Alice's JWT with Bob in the JSON body is executed as Alice;
- Alice can read the resulting fact;
- Bob's independently valid JWT cannot read Alice's fact;
- the canonical bridge remains tenant-scoped when the model supplies no
  principal argument;
- the authenticated live tool surface mounts correctly.

Result: **5/5 live P0 and mount scenarios passed**.

## SEC-002 closure

Conversation ownership is now immutable and tenant-qualified.

1. Neo4j has a composite uniqueness constraint on
   `(Conversation.user_identifier, Conversation.session_id)`.
2. creation uses one scoped `MERGE`, creates the owner edge in the same
   operation, and accepts only zero owners or the matching owner;
3. scoped reads require exactly one matching owner;
4. a foreign explicit conversation ID is rejected;
5. ambiguous or legacy global ownership is quarantined instead of repaired,
   linked, or overwritten.

### Live isolation proof

The race-enabled E2E used one identical session ID for Alice and Bob, wrote a
different message for each, and then read both contexts:

- Alice saw Alice's marker and not Bob's;
- Bob saw Bob's marker and not Alice's;
- neither call could mutate the other conversation's owner.

Result: **tenant/session isolation passed against live Neo4j**.

## Real-agent E2E

`TestMemoryRealAgentRecall` is a permanent, explicitly paid build-tagged gate.
It does not script model turns.

1. The test seeds a unique fact through the authenticated live Memory MCP.
2. It mounts the real sidecar through Aura's production MCP bridge.
3. A real `LlmAgent` calls OpenRouter using
   `deepseek/deepseek-v4-flash:nitro`.
4. The model must invoke `memory__memory_search`.
5. The test requires the seeded marker in the actual tool-result preview and
   in the agent's terminal `text_response`.
6. The seeded fact is deleted during cleanup.

Observed result:

```text
REAL AGENT E2E PASS
provider=openrouter
model=deepseek/deepseek-v4-flash:nitro
memory__memory_search start=true end=true
seeded marker present in tool result=true
seeded marker present in final answer=true
```

The paid gate passed in **13.72 s** under WSL with the race detector enabled.
The deterministic full loop gate also passed in **2.04 s**.

## Verification matrix

| Gate | Result |
|---|---|
| Vendored Python suite | 138 passed, 5 unrelated existing live tests skipped |
| Ruff on changed Python | PASS |
| `go vet ./...` in WSL | PASS |
| `go build ./...` in WSL | PASS |
| Affected Go package tests | PASS |
| Affected Go package race tests | PASS |
| Auth claim/signature/expiry unit matrix | PASS |
| Live unauthenticated and forged-identity attack | PASS |
| Live same-session two-tenant isolation | PASS |
| Deterministic authenticated `LlmAgent` loop | PASS |
| Real OpenRouter-backed `LlmAgent` loop | PASS |
| Compose rendering and sidecar health | PASS |

The later full `cmd/aura` race invocation encountered an unrelated concurrent
working-tree change in `.env.example` (`AURA_EMBED_HF_REPO` was changed from an
active assignment to a commented example). The P0 implementation's
`cmd/aura` race gate had already passed before that concurrent edit; all three
currently affected P0 packages remained race-green afterward.

## Scoped score

| P0 acceptance area | Score |
|---|---:|
| Authenticated sidecar boundary | 10.0 |
| Server-derived tenant scope | 10.0 |
| Global-resource removal | 10.0 |
| Tenant/session data model | 10.0 |
| Adversarial two-user validation | 10.0 |
| Real-agent production-path validation | 10.0 |

Overall scoped P0 remediation score: **10.0 / 10**.
