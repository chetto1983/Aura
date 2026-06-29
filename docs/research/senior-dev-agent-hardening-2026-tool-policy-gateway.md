# Tool Policy Gateway + Durable Action Ledger — Senior-Dev Patterns (2025–2026)

Research angle: decision point 2 of the Aura hardening question — the centralized
**ToolGateway**: one enforceable policy + approval + durable mutating-action ledger +
idempotency point, kept *minimal* (not an over-engineered "atomic bomb").

Context: Aura is a Go single-binary agentic runtime (agent loop + full-host shell/fs/network
tools + MCP servers + Postgres/Neo4j + AG-UI/SSE cockpit) on a 16-core/32GB mini-PC.
The relevant Aura surface is `internal/agent/tools` (deferred-tool pattern) + `tool_invocations`
migration 0011 + `ask_user` pause/resume — i.e. Aura already has the seams a ToolGateway snaps into.

## TL;DR for Aura

The 2025–2026 consensus is a **single in-band interception point that every tool call passes
through before execution**, expressing policy in a declarative language (YAML / OPA Rego / Cedar),
emitting a tamper-resistant audit/evidence record, and routing high-stakes mutating calls to a
human approval gate. The *minimal* industrial form is a thin middleware (sub-millisecond / single-binary),
not a separate distributed control plane. Idempotency + exactly-once for mutating actions is the one
genuinely hard part, and the mature answer is **durable execution (event-sourced ledger + replay)** —
but you only need its primitives, not the full Temporal cluster, on a single appliance.

Concretely Aura should:
1. Make the existing tool-dispatch path the single PEP (policy enforcement point). Every tool call —
   built-in, deferred, or MCP-proxied — routes through one `Authorize(ctx, principal, toolCall)` hook.
2. Express rules declaratively (start with YAML allow/deny + "requires_approval", graduate to Cedar/Rego
   only if needed). Default-deny destructive ops (drop/delete/truncate, shell rm, fs writes outside workspace).
3. Reuse `ask_user` pause/resume as the human-in-the-loop approval primitive; persist the approval
   *before* the mutating call returns (durable signal pattern).
4. Promote migration 0011 `tool_invocations` into a true **action ledger**: principal, tool, args hash,
   idempotency key, decision (allow/deny/approved-by), outcome — append-only, exportable.
5. Make mutating tool calls idempotent via a stable idempotency key + a recorded result, so a crash/retry
   replays the recorded outcome instead of re-invoking (the durable-execution insight, applied minimally).

---

## Sources & findings

### 1. Microsoft Agent Governance Toolkit — the reference "policy gateway" shape (HIGH)
- Blog: https://opensource.microsoft.com/blog/2026/04/02/introducing-the-agent-governance-toolkit-open-source-runtime-security-for-ai-agents/
- Repo: https://github.com/microsoft/agent-governance-toolkit
- Core: **"Agent OS"** = a *stateless policy engine that intercepts every agent action before execution*
  at **<0.1ms p99** — kernel-style interception of all tool calls, not post-hoc monitoring. This is the
  exact ToolGateway shape Aura wants, and the latency number proves "minimal/in-band" is achievable.
- Policy languages: **YAML rules, OPA Rego, and Cedar** — declarative, no vendor lock-in. Validates
  Aura starting with YAML allow/deny and graduating to Cedar/Rego.
- Approval: **"Approval workflows with quorum logic"** (multi-stakeholder) + a semantic intent classifier
  for goal-hijack/prompt-injection-to-tool-execution.
- Ledger/evidence: automated compliance verification, regulatory mapping (EU AI Act, HIPAA, SOC2) and
  **OWASP Agentic AI Top 10 evidence collection across all 10 categories** — the audit/evidence ledger angle.
- Minimal/framework-agnostic: hooks into native extension points (LangChain callbacks, CrewAI task
  decorators, Google ADK plugin system); install = "pip install + a few lines of config" — i.e. governance
  added without rewriting agent code. For Aura the analog is a single `Authorize` hook in tool dispatch.

### 2. Temporal — "Why agentic flows need distributed-systems discipline" (HIGH)
- https://temporal.io/blog/from-ai-hype-to-durable-reality-why-agentic-flows-need-distributed-systems
- Idempotency / exactly-once via **event-sourced history + replay**: a crash mid-tool-call replays the
  workflow from the stored event log — "no lost progress, no double charges." Each activity is effectively
  run once even across worker crashes/retries.
- **Durable action ledger**: every tool invocation becomes a workflow step captured in history; you can
  trace every hop (agent call → business logic → external API). This is the durable mutating-action ledger.
- **HITL via signals**: CFO clicks "Approve" → backend *persists the approval to Temporal before it returns
  200 OK*. The approval is durably recorded first, so downstream mutating calls can retry without losing
  approval context or double-acting. Directly maps to Aura's `ask_user` pause/resume.
- Minimal guidance: "keep tool logic simple; let built-in retries/timeouts handle resilience." Each tool
  becomes a workflow with retry policy + timeout, isolating failure.
- For Aura: you do NOT need a Temporal cluster on a mini-PC. Steal the *primitives* — idempotency key +
  recorded outcome + persist-approval-before-act — implemented over Postgres.

### 3. Temporal/Olmec — 2026 shift to durable agentic workflows (MEDIUM)
- https://olmecdynamics.com/news/temporal-durable-execution-agentic-workflows-2026
- Frames durable execution as the 2026 mainstream answer to agent failure points (orchestration,
  probabilistic LLM behavior, tool calling, HITL) that naive retry logic can't handle: automatic state
  persistence, automatic retries, workflow resumption. Useful as the "why a durable action ledger matters
  for agents specifically" citation. Corroborated by Inngest
  (https://www.inngest.com/blog/durable-execution-key-to-harnessing-ai-agents) noting AWS/Cloudflare/Vercel
  all shipped durable-execution offerings in 2025 driven by agent infra.

### 4. Tigera/Calico — Multi-layer policy for securing AI agents (MEDIUM)
- https://www.tigera.io/blog/multi-layer-policy-for-securing-ai-agents/
- Key senior insight that tempers "one gateway to rule them all": **policy belongs in multiple places,
  ideally one policy language**. The gateway layer naturally enforces delegation rules, per-hop token
  issuance with scope reduction, agent→MCP-tool authorization, agent→LLM constraints, HITL hooks for
  high-stakes actions, and attribute-based decisions. Good guard against the "atomic bomb" failure mode:
  the ToolGateway is the *primary* PEP but network/sandbox layers still enforce — defense in depth.

### 5. LangChain — LangSmith LLM Gateway: runtime governance in the agent lifecycle (MEDIUM)
- https://www.langchain.com/blog/introducing-llm-gateway
- A production gateway as control plane between consumers (apps/agents/users/CI) and providers/tools:
  policy-driven access control evaluated per-request against contextual attributes → allow/deny/reroute,
  fine-grained controls (restrict high-cost models to teams, block agents from sensitive tools), and
  audit logging capturing identity, params, model, tokens, cost, outcome — exportable to SIEM. Reinforces
  the "gateway = authoritative source of audit data" pattern. (TrueFoundry "Top Agent Gateways 2025"
  https://www.truefoundry.com/blog/top-agent-gateways and Pomerium
  https://www.pomerium.com/blog/best-llm-gateways-in-2025 corroborate.)

### 6. Lunar.dev — Best open-source MCP gateways 2026 (MEDIUM, single-binary relevance)
- https://www.lunar.dev/post/the-best-open-source-mcp-gateways-in-2026
- Directly relevant because Aura proxies MCP servers and ships as ONE binary. Highlights **Bifrost**:
  operates as both MCP client and MCP server in a **single binary**, one deployment handling tool
  discovery + routing + governance, **+11µs overhead at 5,000 req/s** — proof a governed single-binary
  tool layer is cheap. **MCPX (Lunar.dev)**: one governed entry point for all agent→tool interactions
  enforcing access control + auditability + policy across every MCP server. Production MCP-gateway
  requirements list: centralized identity/access control, cost/budget governance, **immutable audit
  records**, minimal-latency performance. (Corroborated by Tyk
  https://tyk.io/learning-center/mcp-gateway-architecture-technical-guide/.)

---

## Anti-over-engineering checklist (the "minimal industrial form")
- ONE interception point in tool dispatch; do not build a separate service/cluster for a mini-PC.
- Declarative rules in a file (YAML first); default-deny destructive ops; everything else allow + log.
- Idempotency = stable key + recorded outcome in `tool_invocations`; replay recorded result on retry.
- Approval = persist-before-act, reusing `ask_user`; no new bespoke approval engine.
- Audit ledger = append-only rows you already write; add decision + idempotency-key columns, make exportable.
- Defense in depth: gateway is primary PEP, but sandbox + network allowlist still enforce (Tigera point).
- Measure overhead and assert it (Bifrost 11µs, Agent OS <0.1ms p99 are the bar — sub-ms is non-negotiable).
