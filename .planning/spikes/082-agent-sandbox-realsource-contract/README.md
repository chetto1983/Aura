---
spike: 082
name: agent-sandbox-realsource-contract
type: standard
validates: "Given the REAL agent-sandbox/agent-sandbox + kubernetes-sigs/agent-sandbox Go source cloned+pinned and a live cluster run, when the actual CRD Go types + E2B-protocol surface are diffed against 079's paper SandboxSpec/Router.Resolve contract and one sandbox is created→exec→suspend→delete live, then 079's field-map is confirmed-or-corrected AND the transport-swap claim is empirically real (incl. whether E2B-protocol is the better Backend seam than hand-mapped CRDs)"
verdict: VALIDATED
related: [062, 078, 079, 081]
tags: [sandbox, agent-sandbox, k8s, crd, e2b, api-contract, phase-37, v2.0.0]
---

# Spike 082: agent-sandbox real-source contract + live run

## What This Validates

Spike **079** formalized Aura's `usersandbox` Go-over-Docker contract by mapping it onto agent-sandbox's `Sandbox`/`SandboxTemplate`/`SandboxClaim` — but from **docs/blog/research only**, never the real Go source, and never a running cluster. It left one explicit open question ("vendor agent-sandbox's CRD types now, or Aura-native structs?"). The operator dropped `github.com/agent-sandbox/agent-sandbox` mid-session to force the real check.

This spike **clones + pins both repos, reads the actual Go types, and stands up a live kind cluster** to run the full sandbox lifecycle the contract claims — proving the "transport swap not redesign" story empirically instead of on paper.

## Research (pinned source, not docs — see `SOURCES.md`)

**The first finding is that the operator's URL is a DIFFERENT project from the one 079 mapped.** There are two:

| Repo | Module | What it is | K8s? |
|---|---|---|---|
| `kubernetes-sigs/agent-sandbox` @ `0be472b7` | `sigs.k8s.io/agent-sandbox` | The **CRD project** — `Sandbox`/`SandboxTemplate`/`SandboxClaim`/`SandboxWarmPool` + a controller. 079 mapped this. | mandatory (control plane) |
| `agent-sandbox/agent-sandbox` @ `2d0df81c` (v0.7.0) | `github.com/agent-sandbox/agent-sandbox` | **The operator's link.** An **E2B-protocol-compatible REST + MCP gateway BUILT ON TOP OF** the SIG project. Its own README says the SIG one "faces Kubernetes directly… not friendly for AI Agents"; this wraps it in an E2B-SDK-drop-in API. | mandatory (`client-go`/knative; no Docker backend) |

So there are **two layers**, and 079 only saw the bottom one. This changes the forward-compat story materially (below).

### Real CRD types (`sigs.k8s.io/agent-sandbox`, v1beta1) — vs 079's paper contract

Read from `api/v1beta1/sandbox_types.go`, `extensions/api/v1beta1/{sandboxtemplate,sandboxclaim,sandboxwarmpool}_types.go`.

| 079 said | Real source says | Verdict |
|---|---|---|
| `Sandbox` = one stateful pod, stable identity + storage | ✅ `SandboxSpec` embeds `SandboxBlueprint{PodTemplate, VolumeClaimTemplates, Service}` | **confirmed** |
| `SandboxSpec ≈ SandboxTemplate` field-map | ✅ literally true in source: `SandboxSpec` and `SandboxTemplateSpec` **both embed the same `SandboxBlueprint`** (`json:",inline"`) — the "promote a field and it appears in both" comment is in the code | **confirmed, stronger than assumed** |
| `Router.Resolve(id) ≈ SandboxClaim` (get-or-create) | ⚠️ **WRONG.** `SandboxClaimSpec.WarmPoolRef` is a **required** field — a Claim is a *checkout from a warm pool*, not a get-or-create. You cannot use `SandboxClaim` without a `SandboxWarmPool`. | **CORRECTION** |
| "skip warm pools on one box" | ✅ correct — but that means Aura's `Resolve` maps onto **direct `Sandbox` creation** (idempotent by name), NOT onto `SandboxClaim`. Claim+WarmPool is the *latency* tier Aura skips; it is not the provisioning primitive. | **corrected mapping** |
| `runtimeClassName` is a CRD field | ⚠️ it lives inside `podTemplate.spec` (standard `corev1.PodSpec.RuntimeClassName`), not a top-level Sandbox field | minor correction |
| `Egress EgressPolicy` (from 009) | ✅ but the real thing is richer: `SandboxTemplateSpec.NetworkPolicy` (a restricted `NetworkPolicyIngress/Egress` subset) + `NetworkPolicyManagement`. **Secure default = allow public internet, block RFC1918 + cloud metadata server** — exactly Aura's SBX-02 posture, first-class. | confirmed + upgrade |
| idle-TTL `Stop` (hand-waved) | ✅ **real and better than 079 captured**: `OperatingMode ∈ {Running, Suspended}` + `Lifecycle{ShutdownTime, ShutdownPolicy ∈ {Delete, Retain}}`. **Suspended = pod terminated but Sandbox object + PVC retained** = restartable idle box — precisely what a per-identity box wants. | **new capability** |

Other real fields 079 didn't have: `EnvVarsInjectionPolicy` / `VolumeClaimTemplatesPolicy` (Allowed/Overrides/Disallowed — govern what a Claim may inject), `AutomountServiceAccountToken` defaulted **false** (secure-by-default), `EmbeddedObjectMetadata` propagation controls.

### The E2B gateway layer (`agent-sandbox/agent-sandbox`) — the piece 079 completely missed

This is the operator's actual link, and it is the **stronger forward-compat lever**. Read from `pkg/api/e2b/{sandbox,api/types}.go`, `pkg/handler/{handler,mcp_handler}.go`, `main.go`, `install.yaml`.

- **E2B-SDK drop-in.** REST surface (`pkg/handler/handler.go`): `POST/GET/DELETE /sandbox`, `POST /sandbox/{pause,resume}/{name}`, `POST /terminal/sandbox/{name}` (+ `/ws` exec stream), `/sandbox/files/{name}/{upload,download}`, metrics, per-sandbox **port-proxy router** (`/sandboxes/router/{id}/{port}/` and `{port}-{id}.{domain}`). `pkg/api/e2b/api/types.go` is the E2B OpenAPI-generated model set.
- **Per-user tenancy is native.** `CreateSandbox` reads `auth.GetUserTokenFromContext` → `sb.User = user`; `List(user)` filters by owner; `GET /sandboxes?metadata=user=abc` is a documented filter. **The API key IS the tenant key** — this is 078/081's per-identity model already implemented upstream.
- **`NewSandbox` request carries exactly Aura's per-identity knobs**: `TemplateID`, `Metadata`, `EnvVars`, `Timeout`, `AutoPause`/`AutoResume`, `Network{AllowOut, DenyOut, AllowInternetAccess}` (= 009 egress), **`Mcp map[string]interface{}`** (per-sandbox MCP config = 081's per-identity MCP scoping), `Secure`.
- **It is itself an MCP server.** `pkg/handler/mcp_handler.go` mounts a **Stateless streamable-HTTP MCP server at `/mcp`** with 5 tools: `createSandbox`, `getSandbox`, `listSandbox`, `deleteSandbox`, `sandboxExecutor`. That is the same transport shape Aura already mounts for `agent-memory :8091` (spike 032) — **Aura could mount agent-sandbox's `/mcp` into its agent loop unmodified**, giving the agent per-identity box lifecycle tools for free.

## How to Run (live proof — kind)

```bash
export PATH="$HOME/go/bin:$PATH"          # kind installed via: go install sigs.k8s.io/kind@latest (v0.32.0)
kind create cluster --name aura-spike082
K="kubectl --context kind-aura-spike082"
$K apply -f https://github.com/kubernetes-sigs/agent-sandbox/releases/download/v0.5.0/manifest.yaml
$K -n agent-sandbox-system rollout status deploy/agent-sandbox-controller
$K apply -f sandbox-cr.yaml                # Sandbox CR, public alpine:3.20 image (no GCP push)
$K wait --for=condition=Ready sandbox/aura-spike082-box --timeout=120s
MSYS_NO_PATHCONV=1 $K exec aura-spike082-box -c box -- cat /tmp/marker
$K patch sandbox aura-spike082-box --type=merge -p '{"spec":{"operatingMode":"Suspended"}}'   # idle-Stop
$K patch sandbox aura-spike082-box --type=merge -p '{"spec":{"operatingMode":"Running"}}'      # resume
$K delete sandbox aura-spike082-box        # owner-ref cascade GC of the Pod
kind delete cluster --name aura-spike082
```

## What to Expect

Sandbox reconciles to `Ready` (reason `DependenciesReady`, "Pod is Ready"); a backing Pod appears; exec runs as root and reads the marker; Suspend terminates the Pod while retaining the Sandbox object; Delete removes the Sandbox and cascade-GCs the Pod; an empty-spec Sandbox is rejected by CRD schema validation.

## Investigation Trail

1. **Cloned both repos, pinned commits.** Immediately found they are distinct projects with distinct modules — 079's "agent-sandbox/agent-sandbox + kubernetes-sigs/agent-sandbox" phrasing had silently merged them. The operator's link is the E2B gateway, not the CRD project.
2. **Read the real CRD types.** `SandboxClaim.WarmPoolRef` being **required** was the sharpest correction — 079's `Router.Resolve ≈ SandboxClaim` mapping is not viable; `Resolve` maps onto direct `Sandbox` creation. `OperatingMode: Suspended` turned out to be the real, superior form of the idle-`Stop` 079 hand-waved.
3. **Read the E2B gateway.** Per-user token scoping, the `Network`/`Mcp`/`Metadata` request knobs, and the 5-tool `/mcp` server reframed the forward-compat story: the swap target isn't "translate SandboxSpec into a CRD" — it's "speak the E2B protocol," which is a stable, SDK-backed, already-MCP-exposed contract.
4. **Live kind run.** Installed the v0.5.0 released controller (prebuilt image, no local `ko` build), created a Sandbox from a plain `alpine:3.20` image, and drove the full lifecycle. Every stage behaved as the types promise.

### Live-run evidence (2026-07-04, kind v0.32.0 / k8s v1.36.1 / Docker 29.6.1)

```
CREATE   sandbox.agents.x-k8s.io/aura-spike082-box created
READY    condition met — {"type":"Ready","status":"True","reason":"DependenciesReady","message":"Pod is Ready"}
POD      aura-spike082-box  1/1  Running   (controller-created backing Pod, IP 10.244.0.6)
EXEC     uid=0 host=aura-spike082-box alpine=3.20.10        (root exec into the box)
MARKER   AURA-SPIKE-082-READY                                (startup command's file, read via exec)
SUSPEND  conditions=Ready,Suspended=SandboxSuspended/PodTerminated ; pod -> Terminating ; Sandbox RETAINED
RESUME   OperatingMode:Running -> condition met again
DELETE   sandbox deleted ; get sandbox -> NotFound ; Pod cascade-GC (owner ref)
NEGCTRL  empty spec -> "The Sandbox \"bad\" is invalid: spec.podTemplate: Required value"  (CRD schema gate)
```

## Results

**VALIDATED ✓** — the contract is real and 079's *pattern* holds, but the *mapping* needed three corrections and the *forward-compat seam* is better than 079 proposed.

**Corrections to 079 / SBX-05 ADR (binding for Phase-37 `usersandbox`):**
1. **`Router.Resolve` maps onto direct idempotent `Sandbox` creation, NOT `SandboxClaim`.** `SandboxClaim.WarmPoolRef` is required; Claim+WarmPool is the cold-start-latency tier Aura explicitly skips on one box. Do not model `Resolve` as a Claim.
2. **Aura's `Stop` = `OperatingMode: Suspended` (retain box + volume), distinct from `ShutdownPolicy: Delete`.** Add a `Suspend`/`Resume` verb to the Aura `Sandbox` interface — an idle identity's box should suspend (pod gone, PVC + identity retained, fast resume), not be destroyed. 079's single `Stop` under-modeled this.
3. **Egress is a first-class `NetworkPolicy` with a secure default** (public internet allowed, RFC1918 + metadata server blocked). Aura's `EgressPolicy` should mirror that default posture, not just an allowlist toggle.

**The forward-compat lever is the E2B protocol, not hand-mapped CRD structs (answers 079's open question):**
- 079 asked "vendor CRD types or Aura-native structs?" → **neither.** Define the Aura `Sandbox` **`Backend` seam to speak the E2B protocol** (`NewSandbox`/lifecycle/exec). Then: (a) `DockerBackend` implements it locally now (078); (b) the **DGX tier is `agent-sandbox/agent-sandbox` dropped in unmodified** — it already exposes this exact API + a `/mcp` server, over K8s, with per-user tenancy. No CRD translation layer, no vendored `sigs.k8s.io/agent-sandbox` types in Aura's tree.
- Bonus: because the gateway is an MCP server, a future Aura could even mount its `/mcp` (createSandbox/exec/delete) into the agent loop like any other MCP sidecar (032 pattern), rather than calling it as a Go backend.

**Confirmed unchanged from 079:** K8s is mandatory for *both* upstream projects (hard `client-go`/knative deps, no Docker backend in either) → Aura genuinely must own the Docker-direct backend (078); the SIG `SandboxSpec`/`SandboxTemplate` shared-blueprint shape is real and is the right thing for Aura's `SandboxSpec` to resemble. The K8s-mandatory / Docker-now decision stands; only the *interface* Aura implements changes from "CRD-shaped structs" to "E2B-protocol verbs."

**Open (small, for the plan):** the E2B `envd` (in-sandbox agent daemon the gateway's terminal/files endpoints talk to) is its own contract — if Aura adopts the E2B protocol wholesale, `DockerBackend` must either run `envd` in the box or implement the subset of endpoints Aura uses (exec + files). Scoping that subset is a Phase-37 planning task, not a kill risk.
