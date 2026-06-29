# Senior-Dev Agent Hardening (2026) — Research Notes

> Living document. This file aggregates evidence-backed best practices for hardening
> production-grade, multi-user, sandboxed agent runtimes. Each contributing research
> angle appends its own section.

---

## Angle: Kubernetes-native vs single-host (Docker-direct) sandbox deployment on a mini-PC appliance

**Decision-point context (Aura):** single-binary Go agent runtime on a 16-core/32 GB mini-PC
appliance (DGX Spark bundle long-term). The sub-question: is the CNCF `kubernetes-sigs/agent-sandbox`
project (and lightweight K8s like k3s/k0s) sane on a single small host, or do seniors implement the
per-user-sandbox pattern directly over Docker?

### TL;DR verdict

- The Kubernetes `agent-sandbox` project is **real, official, and the emerging standard** — it is a
  Kubernetes SIG Apps subproject launched by Google at KubeCon Atlanta (Nov 2025), with a `Sandbox`
  CRD + extension CRDs (`SandboxTemplate`, `SandboxClaim`, `SandboxWarmPool`) and pluggable isolation
  via `runtimeClassName` (gVisor / Kata). It is the canonical artifact to cite for the "K8s-native
  per-agent sandbox pattern."
- **But it explicitly does NOT provide the surrounding production infrastructure** — you bring the
  cluster, networking, storage classes, observability, secrets, multi-tenancy. It is the isolation
  *primitive + declarative API*, not a platform (Northflank).
- **Design center is multi-node, tens-of-thousands of parallel sandboxes, thousands of QPS** (Google).
  On a single small host you pay the K8s + isolation overhead without the distributed-coordination
  payoff. The `SandboxWarmPool` (its headline cold-start mitigation) trades **idle compute** for
  latency — directly counter-productive when you have one 16-core box.
- **Senior consensus for a single appliance: implement the per-user-sandbox pattern directly over
  the container runtime (Docker/containerd) or a microVM**, not over a K8s control plane. Reserve
  k3s/k0s + agent-sandbox for when you actually go multi-node or need the declarative fleet API.
  This matches Aura's existing direction (`tools.SandboxExec` over a sandbox-agent on `:2468`,
  `make sandbox-up`).

### The K8s agent-sandbox project (what to cite)

- **CRD model** (from the Kubernetes blog + project docs):
  - `Sandbox` — declarative API for a single, stateful pod with stable identity + persistent storage
    (the core "isolated, stateful, singleton workload" primitive).
  - `SandboxTemplate` — reusable blueprint: base image, resource limits, initial security policy.
  - `SandboxClaim` — transactional request for an environment; abstracts provisioning (claims from a
    warm pool when available).
  - `SandboxWarmPool` — maintains pre-warmed pods so the controller claims a ready instance instead of
    creating one cold; brings cold start to "less than one second."
- **Isolation backends are pluggable via `runtimeClassName`**: gVisor (runsc, user-space kernel,
  lower overhead, faster start, good for untrusted multi-tenant code) or Kata Containers (dedicated
  guest kernel per workload, stronger isolation, higher startup latency). This is the same
  RuntimeClass mechanism standard K8s uses — agent-sandbox does not invent new isolation, it
  standardizes the *orchestration* around it.
- **Why Google argues K8s needs this**: agent behavior is many quick iterative tool calls, each
  wanting its own isolated sandbox created "from scratch, extremely quickly"; enterprise demand is
  "tens of thousands of parallel sandboxes, thousands of queries per second." That is the scale this
  is built for — explicitly *not* a single appliance.

### Cold-start / overhead numbers (single-host relevance)

- Plain pod startup adds **~1 second** of overhead — fine for a microservice deploy, but it "breaks
  the continuity of the interaction" when an idle agent is re-invoked. `SandboxWarmPool` exists to
  hide this — at the cost of holding spare warm capacity (overkill on one mini-PC).
- gVisor: **~10–30% overhead on I/O-heavy** workloads, near-zero on compute-heavy; no VM per
  workload, faster start.
- Kata: VMM + guest kernel per workload, stronger boundary, higher cold start; warm pools "particularly
  useful for Kata where VM creation adds cold start overhead."
- Northflank's production figure: **1–2 s end-to-end sandbox creation** is achievable in a tuned K8s
  deployment.
- For contrast, the direct-runtime / microVM datapoints seniors cite when skipping K8s: Firecracker
  **<125 ms boot** (~100K LOC, 6 virtual devices, independent kernel per microVM, powers AWS Lambda;
  used by E2B / Fly Machines); Daytona (Docker containers) **sub-90 ms** cold start; E2B **~80 ms**
  same-region; Cloudflare Sandboxes **sub-50 ms**; Blaxel **~25 ms** resume-from-standby. These are
  achieved *without a K8s control plane in the request path*.

### Lightweight K8s on a mini-PC (k3s / k0s) — is it sane?

- k3s: CNCF-certified full K8s in **<100 MB**, runs in **512 MB RAM**, uses containerd (not Docker),
  scales from a Raspberry Pi up. k0s: single binary **160–300 MB**, runs on **1–2 vCPU / 1–2 GB**,
  no required kernel modules/swap config; joined CNCF Sandbox in 2025. Both are technically fine on a
  16-core/32 GB box footprint-wise.
- The catch (per the comparison sources): **neither k3s nor k0s adds container-level isolation for
  agents that Docker doesn't already give you** — isolation still comes from the runtime
  (runc/gVisor/Kata). So lightweight K8s buys you the *declarative fleet API + warm pools + scheduling*,
  not stronger isolation. On a single node, "Docker's native container isolation would be more
  straightforward than configuring Kubernetes pod security policies."

### Senior recommendation for Aura's appliance

1. **Single host now → per-user sandbox directly over the container runtime** (Docker/containerd),
   selecting gVisor or Kata via `runtimeClassName`-equivalent runtime config, or a Firecracker microVM
   tier when running genuinely untrusted LLM-generated code. This is what Daytona/E2B/Modal do under
   the hood; the K8s layer is optional packaging, not the isolation.
2. **Keep the agent-sandbox CRD model as the forward-compatible contract.** If/when Aura goes
   multi-node (DGX Spark fleet), adopting agent-sandbox over k3s gives the declarative `Sandbox` /
   `SandboxClaim` / `SandboxWarmPool` API without re-architecting. Mirror its template/claim shape
   in Aura's own sandbox config so the migration is a transport swap, not a redesign.
3. **Skip warm pools on one box** — they trade idle CPU/RAM for latency you can instead win with a
   fast runtime (microVM/gVisor) + session-bound reuse (Aura already has stateless 2a + session-bound
   2b).
4. **Use RuntimeClass-grade isolation, not bare containers, for untrusted code** — the Feb-2026
   consensus is that shared-kernel runc isolation alone "isn't cutting it" for untrusted agent code;
   gVisor (Modal/Google) or Firecracker (E2B/Fly) is the senior default.

### Sources

- Running Agents on Kubernetes with Agent Sandbox — Kubernetes.io blog (2026-03-20):
  https://kubernetes.io/blog/2026/03/20/running-agents-on-kubernetes-with-agent-sandbox/
- Why Kubernetes needs a new standard for agent execution — Google Open Source Blog (2025-11):
  https://opensource.googleblog.com/2025/11/unleashing-autonomous-ai-agents-why-kubernetes-needs-a-new-standard-for-agent-execution.html
- kubernetes-sigs/agent-sandbox — GitHub: https://github.com/kubernetes-sigs/agent-sandbox
- Agent Sandbox on Kubernetes: how it works and how to run it in production — Northflank:
  https://northflank.com/blog/agent-sandbox-on-kubernetes
- Small Kubernetes for local experiments (k0s/MicroK8s/kind/k3s/Minikube) — Palark:
  https://palark.com/blog/small-local-kubernetes-comparison/
- per-user agent code execution sandbox landscape (E2B/Daytona/Modal/Fly/Firecracker cold-start) —
  AgentMarketCap / Spheron / Northflank (2026):
  https://agentmarketcap.ai/blog/2026/04/07/ai-agent-sandbox-infrastructure-e2b-modal-daytona-fly-machines-secure-code-execution

---

## Angle: Multi-user identity isolation + capability authz (decision point 4)

**Decision-point context (Aura):** multi-user without full enterprise RBAC — owner-scoped data
stores (Postgres `aura.*` + Neo4j), per-principal API/tool filtering, per-identity sandboxes,
capability-based authz, and how seniors avoid *half-done* isolation / IDOR.

### TL;DR verdict

1. **Authz must be capability-based, not identity-based.** API keys / sessions answer *"who are
   you?"*; agents need *"what can this principal do, right now, for how much, for how long?"* Use
   attenuated capability tokens (macaroon → biscuit model) that **only restrict, never amplify**
   down a delegation chain (agent → sub-agent → tool/MCP call). This is the cleanest way to get
   per-user isolation **without** building full RBAC, and it maps onto Aura's existing
   `capability_grants` (Slice 1.7).
2. **Owner-scope every data path, not just the controller.** IDOR/BOLA is the #1 multi-tenant leak
   (OWASP **A01:2025** Broken Access Control). Centralize `WHERE owner_id = :principal` so *every*
   query — sqlc PG and LLM-generated Cypher via `mcp-neo4j-cypher` — carries the principal filter.
   Never trust an LLM-supplied object ID.
3. **Model identity as distinct layers** (trigger / execution / authorization / tenant /
   attribution). Conflating "the human who initiated" with "the agent executing" with "whose data
   is touched" is exactly how isolation ends up half-done and audits become unforensic.
4. **Per-identity sandbox + per-principal token scoping are the same boundary** in two places:
   filesystem/process isolation per user *and* a capability the tool layer cannot widen. If only
   one exists, isolation is half-done.

### Capability authz beats RBAC/API keys for agents

- **Attenuation, never amplification.** A holder can add caveats to make a token *more* restricted,
  never less; in A→B→C delegation "Agent C can NEVER spend more than $1, even if Agent A's original
  macaroon had $50." Model inverts from *"grant then restrict"* to *"define minimum necessary
  access from the start."* Caveats pin endpoints/budgets/expiry per principal, enforced at the
  gateway/protocol layer (overspend → `402`, expiry is cryptographic), not in app code.
  **Biscuit** > raw macaroons (adds public-key sigs + offline attenuation, closes the
  proof-of-possession gap; each token is "a capability scoped to specific tools and arguments").
  (SatGate; DEV/SatGate)
- **Standardization:** IETF OAuth WG `draft-niyikiza-oauth-attenuating-agent-tokens` — an agent
  "receives authority scoped to a session, principal, or workflow and carries it unchanged across
  every tool call and every sub-agent." (IETF; AIP arXiv)
- **Runtime authorization beyond identity (Microsoft):** identity/OAuth scopes "cannot answer
  whether *this action should be executed now, by this agent, for this user, under the current
  context*." Authorize **externally, at runtime, scoped to the task, tied to the initiating
  human** — exactly Aura's ToolGateway seam.
- **Least privilege caution:** OAuth 2.1 doesn't constrain which tools / how often / against what
  data; needs agent-level scope enforcement. **ForcedLeak** (CVSS 9.4, Salesforce AgentForce, Jul
  2025) — prompt injection → over-broad tool authority → CRM + credential exfiltration. Default-deny
  + attenuate-only is the structural defense. (Okta; Cequence; Resilient Cyber)

### Multi-tenant agent access control: 5 identity layers + config-only scope translation

Most directly applicable senior playbook (Scalekit):

- **Model five identity layers explicitly** (trigger/execution/authorization/tenant/attribution) —
  "or access-control bugs surface silently months later." Separate **execution** from
  **attribution** identity in audit trails.
- **Scope translation is explicit, never inferred** — every parameter to a downstream API/tool
  "should come from config, not be inferred from the triggering message." Defeats parameter
  injection (the agent equivalent of IDOR via the prompt).
- **Tenant boundary at three layers**: config isolation (one channel → one resource), named-
  connection binding, code boundaries (all lookups via config only).
- **Anti-patterns**: resource IDs from message payloads; token reuse across tenants (validate
  mappings ACTIVE at startup); stale mappings (shared store, never per-process memory). Org-level
  checks from trusted signals (verified email domain) **never** user-supplied input.

### Owner-scoped stores + per-principal filtering (the IDOR/BOLA discipline)

OWASP **2025** folds IDOR under **A01 Broken Access Control** (#1 risk); API equivalent = **BOLA**;
same bug — trusting client/LLM-supplied IDs without server-side ownership checks. Senior rules:

- Scope every query to the principal (`... WHERE user_id = :current_user AND id = :order_id`) in the
  **data-access layer, not just controllers**; centralize so no path bypasses the owner predicate.
- Validate object ownership on **every** request (session/token principal vs record owner).
- Runtime backstop: SQL-parse + reject any query missing the tenant filter (Aikido Zen).

**Aura mapping:** sqlc repository wrapper (or PG row-level security) that injects `owner_id`/identity
so a tool can never read another user's conversations / paused_states / skills / tool_invocations.
For Neo4j, the `mcp-neo4j-cypher` path must **template the principal into the Cypher** — LLM-generated
Cypher is precisely the BOLA-via-supplied-ID risk; enforce a principal-scoped sub-graph, don't trust
node IDs in the model's query.

### Sources (this angle)

- Scalekit — Access Control for Multi-Tenant AI Agents: Identity & Isolation:
  https://www.scalekit.com/blog/access-control-multi-tenant-ai-agents
- SatGate — Macaroon Tokens vs API Keys for Agent Access:
  https://satgate.io/blog/macaroon-tokens-vs-api-keys
- SatGate — Agent Capability Tokens (Scoped Authority for AI Agents):
  https://satgate.io/agent-capability-tokens
- DEV/SatGate — Why Capability-Based Auth Beats Identity-Based Auth for AI Agents:
  https://dev.to/mattdeangit/macaroon-tokens-vs-api-keys-why-capability-based-auth-beats-identity-based-auth-for-ai-agents-4nkl
- Microsoft Security — Authorization and Governance for AI Agents: Runtime Authorization Beyond Identity:
  https://techcommunity.microsoft.com/blog/microsoft-security-blog/authorization-and-governance-for-ai-agents-runtime-authorization-beyond-identity/4509161
- Okta — How to implement least privilege for AI agents:
  https://www.okta.com/identity-101/how-to-implement-least-privilege-for-ai-agents/
- Cequence — Least Privilege Access for AI Agents:
  https://www.cequence.ai/blog/ai/ai-agent-least-privilege-access/
- Resilient Cyber — Identity Is the Agentic AI Problem Nobody Has Solved Yet:
  https://www.resilientcyber.io/p/identity-is-the-agentic-ai-problem
- IETF OAuth WG — Attenuating Authorization Tokens for Agentic Delegation Chains:
  https://datatracker.ietf.org/doc/html/draft-niyikiza-oauth-attenuating-agent-tokens-00
- arXiv — AIP: Agent Identity Protocol for Verifiable Delegation Across MCP and A2A:
  https://arxiv.org/pdf/2603.24775
- OWASP — Insecure Direct Object Reference (IDOR):
  https://owasp.org/www-community/attacks/insecure_direct_object_reference
- IntelligenceX — Broken Access Control (A01:2025) Complete Guide:
  https://blog.intelligencex.org/broken-access-control-owasp-a01-2025-complete-guide
- Palo Alto Networks — What Is Broken Object Level Authorization?:
  https://www.paloaltonetworks.com/cyberpedia/broken-object-level-authorization-api1
- Aikido — Zen Stops IDOR at Runtime:
  https://www.aikido.dev/blog/zen-stops-idor-vulnerabilities
- Security Boulevard — Tenant Isolation in Multi-Tenant Systems: Architecture, Identity, and Security:
  https://securityboulevard.com/2025/12/tenant-isolation-in-multi-tenant-systems-architecture-identity-and-security/

---

## Angle: Production-readiness gates as a validated contract (decision points 3 & 6)

**Decision-point context (Aura):** the proposed `dev / local_trusted / hardened / server_production`
deployment profiles, a `aura readiness`-style fail-fast validation command, and the honest
"production-ready / 10/10 = evidence not score-gaming" bar. The canonical primary source for this
exact pattern predates the agent era: Google's Launch Coordination Engineering (LCE), Production
Readiness Review (PRR), and Launch Coordination Checklist.

### Source: Google SRE Book — "Reliable Product Launches at Scale" (Ch. 27) + Launch Coordination Checklist (Appendix E)

- **URLs:**
  - https://sre.google/sre-book/launch-checklist/
  - https://sre.google/sre-book/reliable-product-launches/
- **Type:** Primary (institutional engineering practice; Google SRE, O'Reilly 2017, CC BY-NC-ND 4.0).
- **Date:** Book 2017; checklist artifact is "Google's original Launch Coordination Checklist, circa 2005, slightly abridged."

### Extracted claims

1. **A launch checklist is a deliberate completeness/consistency control, not bureaucracy.**
   > "Checklists are used to reduce failure and ensure consistency and completeness across a variety of disciplines."
   → Justifies a first-class `aura readiness` command over ad-hoc per-deploy judgement. (central)

2. **Every checklist item must be evidence-justified by a real past failure, and be actionable —
   the anti-score-gaming / anti-"atomic-bomb" discipline.**
   > "Every question's importance must be substantiated, ideally by a previous launch disaster. Every instruction must be concrete, practical, and reasonable for developers to accomplish."
   → Each profile check (secrets present, auth on, CORS locked, health green) must trace to a
   concrete failure mode and be fixable. This is the honest-bar test: evidence per gate, no
   speculative gates. (central — decision point 6)

3. **A dedicated review function gates launches and signs off only those determined "safe."**
   > LCEs serve as "gatekeepers and signing off on launches determined to be 'safe.'"
   → Validates a fail-fast gate before `server_production` boots; the profile contract is the
   automated LCE stand-in on a single appliance. (supporting — decision point 3)

4. **The review drives convergence on already-hardened infrastructure instead of bespoke solutions.**
   > "Rather than implementing a custom solution, LCE can recommend existing infrastructure as building blocks—infrastructure that is already hardened through years of experience."
   → Reinforces choosing battle-tested building blocks (gVisor, OTel, Prometheus) over bespoke
   mechanisms for sandbox + telemetry. (supporting — decision points 1 & 5)

5. **Production-readiness is a multi-domain checklist spanning ~9–10 themes.**
   Domains: Architecture; Machines/Datacenters (N+2 redundancy, DNS); Volume & Capacity (traffic
   estimates, load testing, storage); System Reliability (failure modes, failover); Monitoring
   (internal state, end-to-end, alerts); Security (design review, code audit, auth, SSL); Automation
   (deployment, canaries, staged rollouts); Growth (spare capacity, 10x planning, scaling
   bottlenecks); External Dependencies (third-party, graceful degradation); Schedule & Rollout.
   > "Google's original Launch Coordination Checklist, circa 2005, slightly abridged for brevity."
   → Concrete template for what each Aura profile validates: not just secrets/auth/CORS/health, but
   capacity/load, failover, monitoring+alerts, security audit, staged rollout+rollback, and graceful
   degradation of MCP/LLM dependencies. (central — decision points 3 & 6, supports 5)

### Limitations

- The pages do **not** self-characterize the checklist as "lightweight" or "battle-tested" — those
  are inferences, not quotes. Avoid attributing them.
- Pre-cloud / pre-agent and scaled to multi-datacenter Google services. The *principles*
  (evidence-justified gates, convergence on hardened building blocks, gate-before-launch) transfer
  cleanly; the *N+2 datacenter* specifics do not map to a 16-core mini-PC.

---

## Angle: Observability, SLOs, DR & the honest production bar (decision points 5 & 6)

**Decision-point context (Aura):** production observability + SLOs + DR + load/chaos +
supply-chain (DP5), and what an honest "production-ready / 10/10" bar requires as *evidence*,
not a self-assigned score (DP6). Aura emits an agent loop (LLM calls + tools incl. full-host
shell/fs + MCP servers) over AG-UI/SSE on a single 16-core/32 GB appliance.

### 1. Instrument the agent loop with OpenTelemetry GenAI semantic conventions

Ride the **OpenTelemetry GenAI semantic conventions** (GenAI SIG, formed Apr 2024; now six
layers: LLM calls, agent orchestration, MCP tool calling, content capture, evaluation) rather
than a bespoke vendor format.

- **Span tree** (maps 1:1 to Aura's loop): `invoke_agent` (top-level, per run) -> `chat` (per
  LLM call) -> `execute_tool` (per tool / MCP call).
- **Two required metric histograms first**: `gen_ai.client.operation.duration` (latency; filter
  by `gen_ai.request.model` to compare DeepSeek vs local vLLM fallback) and
  `gen_ai.client.token.usage` (filter by `gen_ai.token.type` input/output -> cost).
- **Key attributes**: `gen_ai.request.model`, `gen_ai.usage.input_tokens`/`output_tokens`,
  `gen_ai.response.finish_reasons` (detects retry/tool loops), content attrs
  `gen_ai.input.messages`/`output.messages` (capture-gated for privacy).
- **Maturity caveat**: as of spec **v1.41** the spans/metrics exist but nearly all `gen_ai.*`
  attributes still carry "Development" stability badges -- names can change without a major bump;
  keep a thin mapping layer you own. Instrument MCP calls (`mcp-neo4j-cypher`, PIM sidecar) as
  `execute_tool` children carrying the server/tool identity so a slow sidecar is a distinct span.

### 2. Agent SLIs -- keep operational vs quality separate

Cross-vendor 2026 guidance: do **not** mix operational telemetry with quality metrics (noisy,
un-actionable alerts). Export, separately:

- **Operational**: TTFT, total response time (incl. tool time), per-tool latency, P50/P95/P99,
  error rate, throughput, **cost per trace** (cumulative -- surfaces looping branches like a
  planner re-calling a tool), cost per user/feature.
- **Quality**: task/goal success rate, workflow adherence, **tool-call accuracy & recall** (right
  tools, right order, right args, none missed), **hallucination/groundedness rate** (rule-based +
  LLM-as-judge + sampled human annotation).

The agent-observability differentiator: telemetry must explain the **chain of decisions** between
request and response, not just the output -- hence the span tree, not just metrics.

### 3. SLOs -> Prometheus burn-rate alerts (the minimal industrial pack)

Define SLI -> set SLO -> alert on **error-budget burn rate**, not raw thresholds (Google SRE).

- Normalize thresholds against (1 - target); use **multi-window, multi-burn-rate** alerting (the
  recommended default).
- **Fast-burn**: burn rate >= 14 over a 1-hour window -> page immediately (severe outage); slower
  windows catch gradual budget exhaustion without noise pages.
- Generate the alert pack with **Sloth** (SLO-as-YAML -> recording + burn-rate rules) or Grafana
  SLO rather than hand-rolling -- the "minimal industrial" form for Aura's health/readiness +
  agent error-rate SLOs.

### 4. DR drills, chaos & load testing as *evidence*

A DR plan never executed is not evidence -- **practice it**.

- Chaos engineering gives **data-driven confidence in SLOs under stress**: controlled experiments
  (DB failover, regional/endpoint outage) produce tangible proof the plan + runbook work.
- For Aura: kill the Postgres/Neo4j container, drop the OpenRouter endpoint, saturate the sandbox
  -- confirm the loop degrades and recovers; simulate rate limits / packet loss / corrupted data
  and verify recovery paths.
- **AI-specific DR**: back up + version models and prompt templates; document restore/retrain from
  clean datasets (maps to pg_dump + neo4j-admin dump + Agent.md/skills backup).
- Capture during drills: infra (CPU/mem/**GPU** -- they share the single host), networking, and AI
  indicators (token counts, cache hit rate, hallucination frequency).

### 5. Supply-chain hardening (SBOM, pinned actions, SLSA)

By 2026 SBOM/SLSA/Sigstore are "operational requirements" (EO 14028, EU CRA). Do-now for CI:

- **Pin GitHub Actions by commit SHA, not tag** (and base images by digest). The early-2025
  **GhostAction** attack compromised a popular action; every repo pinning the mutable tag
  auto-pulled the malicious version -- tag-pinned = critical vuln.
- **Generate SLSA Level 3 provenance** via the official SLSA GitHub Generator / reusable workflows
  (source repo, commit, build steps, builder identity, out of the box).
- **Verify before deploy** with slsa-verifier; mismatched signature/builder identity -> block
  deploy.
- **Emit + attest an SBOM** in the same workflow (marginal cost once attestation infra runs).
- Complements Aura's existing govulncheck job: vuln scan = "are deps known-bad";
  provenance/SBOM = "is this artifact the one I built from my source."

### 6. The honest "production-ready / 10/10" bar -- evidence, not score-gaming

Release readiness is a **reviewed checklist backed by artifacts** (a production-readiness review),
not a self-assigned number. Each item must point to a run / dashboard / dump / drill log:

- **Observability**: the invoke_agent/chat/execute_tool span tree + metric histograms +
  structured logs demonstrably wired to a backend (not code that *could* emit).
- **SLOs + alerts**: defined SLOs with burn-rate rules that have actually fired in a test.
- **DR**: plan executed in a drill, with runbook timing recorded.
- **Resilience**: chaos experiments run, recovery proven (evidence artifacts).
- **Supply chain**: SHA-pinned actions, SBOM + SLSA provenance, verify-on-deploy gate.
- **Performance**: load test under realistic concurrency with infra + AI metrics captured.
- **AI-specific**: hallucination-rate baseline, tool-call accuracy baseline, cost-per-task ceiling.

The "evidence not score" framing mirrors Aura's own "no-skip-as-green" / "verify artifact, not
reply" rules: a green checkbox with no linked artifact is the audit equivalent of a t.Skip that
fires under $CI -- a falsely-green job exercising nothing.

### Sources (this angle)

- GenAI Observability with OpenTelemetry -- opentelemetry.io blog (2026):
  https://opentelemetry.io/blog/2026/genai-observability/
- How OpenTelemetry Traces LLM Calls, Agent Reasoning, and MCP Tools -- Greptime (2026-05-09):
  https://greptime.com/blogs/2026-05-09-opentelemetry-genai-semantic-conventions
- OpenTelemetry for AI Agents: Observability in MCP Workflows -- MintMCP:
  https://www.mintmcp.com/blog/opentelemetry-ai-agents
- AI Agent Monitoring: 2026 Observability Guide (agent SLIs) -- Augment Code:
  https://www.augmentcode.com/guides/ai-agent-monitoring
- AI observability tools buyer's guide 2026 (operational vs quality SLIs) -- Braintrust:
  https://www.braintrust.dev/articles/best-ai-observability-tools-2026
- Alerting on SLOs (multi-window multi-burn-rate) -- Google SRE Workbook:
  https://sre.google/workbook/alerting-on-slos/
- Defining SLOs -- Google SRE Book: https://sre.google/sre-book/service-level-objectives/
- Sloth for SLO monitoring & alerting with Prometheus -- Mattermost:
  https://mattermost.com/blog/sloth-for-slo-monitoring-and-alerting-with-prometheus/
- 8 Production Readiness Checklist for Every AI Agent (chaos/DR/AI-specific) -- Galileo:
  https://galileo.ai/blog/production-readiness-checklist-ai-agent-reliability
- Using chaos engineering to test DR plans -- Google Cloud Blog:
  https://cloud.google.com/blog/products/devops-sre/using-chaos-engineering-to-test-dr-plans
- Testing disaster recovery with Chaos Engineering -- Gremlin:
  https://www.gremlin.com/community/tutorials/testing-disaster-recovery-with-chaos-engineering
- Software Supply Chain Security: SBOM, SLSA & Actions (GhostAction, SHA-pinning) -- Trantor:
  https://www.trantorinc.com/blog/software-supply-chain-security-sbom-slsa-engineering-teams
- SLSA Provenance Hands-on (Generate with GitHub Actions, Verify with slsa-verifier) -- DEV:
  https://dev.to/kanywst/slsa-provenance-hands-on-generate-with-github-actions-verify-with-slsa-verifier-56ka
- Production Readiness Review Checklist & Best Practices -- Cortex:
  https://www.cortex.io/post/how-to-create-a-great-production-readiness-checklist
- Essential Production Readiness Checklist -- SigNoz:
  https://signoz.io/guides/production-readiness-checklist/

---

## Claim verification log (adversarial, voter 3/3)

### Claim: "The K8s Agent Sandbox project provides kernel & network isolation for multi-tenant, untrusted agent code via pluggable runtimes (gVisor or Kata) selected on the Sandbox custom resource."

**Verdict: NOT REFUTED (well-supported, current, primary-sourced). Confidence: high.**

- **Quote support — exact, not paraphrased.** The Kubernetes.io blog
  (2026-03-20) states verbatim: "The Sandbox custom resource natively supports
  different runtimes, like gVisor or Kata Containers. This provides the necessary
  kernel and network isolation required for multi-tenant, untrusted execution."
  The claim restates this faithfully — no overreach on the isolation/multi-tenant
  language.
- **Runtime-selection mechanism corroborated by 2 independent sources.** Northflank
  ("both are configured via Kubernetes `runtimeClassName`, making the project
  backend-agnostic by design") and Google Cloud's GA announcement ("natively
  supports gVisor and default-deny Kubernetes network policy ... pluggable
  interfaces for open source sandboxes like Kata Containers"). The claim's phrase
  "selected on the Sandbox custom resource" is accurate: the runtime is chosen via
  the standard K8s `runtimeClassName` surfaced through the Sandbox pod spec.
- **Implementation maturity, not just aspiration.** The sigs.k8s.io docs list the
  isolation goals under "Desired Sandbox Characteristics" (could read as
  aspirational), BUT Google Cloud's blog presents gVisor support + default-deny
  network policy as **current GA capabilities** (GA announced 2026-05-21), and
  Northflank's independent production write-up treats both backends as implemented.
  The one honest qualifier (does not defeat the claim): agent-sandbox ships the
  isolation primitive + declarative API, NOT the surrounding production
  infrastructure (Northflank).
- **Source quality matches claim strength.** Primary = official Kubernetes blog +
  official Google Cloud product blog (the project's originator/maintainer);
  secondary = Northflank (independent vendor analysis). No credible source disputes
  or heavily qualifies the kernel/network-isolation-via-gVisor/Kata mechanism.
- **Not outdated / not marketing fluff.** Dated Mar–May 2026, current as of this
  research (Jun 2026). The isolation mechanism (gVisor=runsc user-space syscall
  interception; Kata=per-pod microVM with dedicated guest kernel) is standard,
  well-understood CNCF technology, not a vendor benchmark claim.

Refutation attempts that FAILED: (1) "isolation is only aspirational" — refuted by
GA announcement + independent production analysis; (2) "runtime not selected on the
CR" — refuted, it is via `runtimeClassName`; (3) "low-quality source" — refuted,
primary official sources.

---

## Claim verification — agent-sandbox CRDs (Sandbox/SandboxTemplate/SandboxClaim) — voter 1/3 (adversarial)

**Date:** 2026-06-29 · **Verdict: NOT REFUTED** (well-supported; one minor numeric undercount) · **Confidence: high**

### Claim

> "The Kubernetes community is standardizing agent execution via a new SIG project
> (kubernetes-sigs/agent-sandbox) built on three CRDs — Sandbox, SandboxTemplate, and
> SandboxClaim — providing a persistent, isolated instance for single-container, stateful,
> singleton workloads, addressing the gap of safely running untrusted agent-generated code."

Primary source: [Google OSS Blog, 2025-11](https://opensource.googleblog.com/2025/11/unleashing-autonomous-ai-agents-why-kubernetes-needs-a-new-standard-for-agent-execution.html)

### Element-by-element verification

| Claim element | Verdict | Evidence |
|---|---|---|
| `kubernetes-sigs/agent-sandbox` is a Kubernetes SIG project | CONFIRMED | "a formal subproject of Kubernetes SIG Apps" (Google blog); docs "under the umbrella of SIG Apps". |
| Sandbox = "core resource ... running an isolated instance" | CONFIRMED (verbatim) | Google blog; docs: core CRD = "single, stateful pod with stable identity and persistent storage". |
| SandboxTemplate = "secure blueprint ... resource limits, base image, security policies" | CONFIRMED (verbatim) | Google blog; docs "reusable templates for creating Sandboxes". |
| SandboxClaim = "transactional resource ... request an execution environment" (ADK/LangChain) | CONFIRMED (verbatim) | Google blog; docs "issue a SandboxClaim against a SandboxTemplate ... pre-warmed, fully isolated environment". |
| "persistent, isolated, single-container, stateful, singleton workloads" | CONFIRMED (verbatim) | Google blog exact phrasing; repo tagline "isolated, stateful, singleton workloads". |
| "safely running untrusted agent-generated code" | CONFIRMED | Google blog "safely allow agents to run untrusted, unverified generated code"; README "Isolated environments for executing untrusted, LLM-generated code". |
| "standardizing agent execution" / "new standard" | CONFIRMED (with nuance) | Blog title literally "Why Kubernetes needs a new standard for agent execution"; "designed to standardize ... Kubernetes ... for agentic workloads." It is a de-facto in-ecosystem standardized API, not a ratified external standard. |

### The one defect — CRD count is an UNDERCOUNT, not a misstatement
The project defines **four** CRDs, not three: Sandbox (`agents.x-k8s.io`, core) +
SandboxTemplate + SandboxClaim + **SandboxWarmPool** (all `extensions.agents.x-k8s.io`, extension).
The claim names three correctly and omits SandboxWarmPool. This is a completeness gap, not a
contradiction — the three named CRDs all exist and are described accurately, and the claim does not
assert "only three." Verdict stands.

### Nuances (do not change verdict, but matter for Aura's "production-ready" reasoning)

- **Maturity is moving:** Nov-2025 announcement gave no GA tag (`v1alpha1`); by the kubernetes.io
  blog (2026-03-20) and current docs the getting-started YAML shows `agents.x-k8s.io/v1beta1`
  (alpha→beta). The claim makes no maturity assertion. Do not treat agent-sandbox as GA/stable.
- **"Standard" ≠ isolation primitive:** agent-sandbox is a lifecycle/declarative-API layer; it
  *delegates* kernel isolation to the chosen `runtimeClassName` (gVisor / Kata). Security research
  (Northflank, Zylos, ARMO) stresses gVisor suits compute-heavy/limited-I/O while Kata/Firecracker
  is the right default for multi-tenant untrusted LLM code, and that IAM/token/data-access gaps
  persist *above* the sandbox layer. "Addressing the gap" is defensible but is not "fully solving."

### Adversarial checklist

1. Supported by quote? Yes — supporting quote is verbatim from the primary source.
2. Contradicting evidence? None. The only divergence is *more* CRDs than claimed (strengthens, not refutes).
3. Source quality vs strength? Sufficient — Google OSS blog + official SIG repo + docs + kubernetes.io blog.
4. Outdated? No (Nov-2025 → Mar-2026 → current). Evolving alpha→beta, but facts hold at every point.
5. Marketing/forum speculation? Vendor-authored origin (Google) but CRD facts independently confirmed by neutral kubernetes.io blog + the SIG repo.

Refutation attempts that FAILED: (1) "three CRDs is wrong" — it's an undercount (4 exist), the
named three are accurate; (2) "not a real SIG project" — refuted, SIG Apps subproject in
kubernetes-sigs; (3) "marketing claim" — CRD descriptions verbatim-match neutral primary sources.

---

## Angle: Tool-policy gateway supporting multiple declarative policy languages (research question #2)

**Claim under review:** "A production tool-policy gateway can support multiple declarative policy
languages (YAML rules, OPA Rego, and Cedar) for deterministic enforcement rather than a single
hard-coded ruleset."

**Primary source:** Microsoft Open Source Blog, "Introducing the Agent Governance Toolkit:
Open-source runtime security for AI agents" (2026-04-02).

### Verdict: NOT REFUTED (claim stands)

Supported by multiple first-party primary sources, current (April 2026), source quality matches
claim strength.

### Evidence

1. **Microsoft Open Source Blog (primary)** — verbatim: "Supports YAML rules, OPA Rego, and Cedar
   policy languages." Describes the policy engine ("Agent OS") as "deterministic, sub-millisecond
   policy enforcement" (`<0.1ms p99`), intercepting agent actions before execution.
   <https://opensource.microsoft.com/blog/2026/04/02/introducing-the-agent-governance-toolkit-open-source-runtime-security-for-ai-agents/>
2. **GitHub `microsoft/agent-governance-toolkit`** — architecture: "Policy Engine ──► (YAML/OPA/Cedar)";
   "Stateless, deterministic, fail-closed policy decision runtime"; denied actions are "structurally
   impossible." <https://github.com/microsoft/agent-governance-toolkit>
3. **Project docs, tutorial 08 (OPA / Rego / Cedar Policies)** — all three co-evaluate in one pipeline
   via a single `PolicyEvaluator.evaluate()` call, order: YAML (highest) → OPA → Cedar → defaults.
   <https://microsoft.github.io/agent-governance-toolkit/tutorials/08-opa-rego-cedar-policies/>

### Qualifier (does not refute, but matters for Aura)

OPA/Rego and Cedar are functional but rely on external tooling (OPA CLI / remote OPA server; Cedar
tooling) OR built-in fallback evaluators with limited coverage. OPA built-in supports only default
value / equality / inequality / negation / truthy / nested paths — NOT set comprehensions, built-in
Rego functions, partial rules, or package imports. Cedar built-in handles only common permit/forbid
patterns. OPA CLI mode adds ~50–200 ms subprocess overhead; the sub-millisecond figure is the
YAML/built-in fast path. So "deterministic enforcement across three languages" is accurate, but
**full** OPA/Cedar coverage requires the external engines.

### Relevance to Aura ToolGateway

Validates the single-chokepoint, fail-closed, pluggable-policy-language pattern from a first-party
Microsoft implementation. Aura's minimal-industrial form should NOT ship three engines up front — a
single declarative (YAML) ruleset at one fail-closed chokepoint, with the seam left open for an
OPA/Cedar backend, avoids the "atomic bomb" while matching the validated architecture.

### Adversarial checklist

1. Supported by quote? Yes — supporting quote is verbatim from the primary source.
2. Contradicting evidence? None found. Qualifier (fallback evaluators have subset coverage) narrows
   the *depth* of OPA/Cedar support but does not refute that all three are supported for deterministic
   enforcement.
3. Source quality vs strength? Sufficient — Microsoft OSS blog + official repo + project docs/tutorial.
4. Outdated? No (2026-04-02, current).
5. Marketing/forum speculation? Vendor-authored origin (Microsoft) but the policy-language facts are
   confirmed by the neutral docs tutorial and corroborated by Help Net Security and Microsoft Tech
   Community deep-dive.
