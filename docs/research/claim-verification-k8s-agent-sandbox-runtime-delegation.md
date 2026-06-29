# Claim verification — K8s agent-sandbox delegates isolation to container runtimes

**Date:** 2026-06-29
**Verifier role:** Adversarial claim verifier (voter 3/3)
**Verdict:** NOT REFUTED (claim is well-supported, current, primary-sourced)

## Claim under review

> "The Kubernetes-native agent-sandbox approach delegates isolation enforcement to underlying
> container runtimes (gVisor or Kata Containers) rather than implementing isolation natively, and
> aims to be runtime-vendor-neutral — meaning K8s-native sandboxing is a CRD/orchestration layer on
> top of the same runtime primitives a senior could use over Docker directly."

## Evidence

### 1. Primary source — kubernetes-sigs/agent-sandbox README
> "Supporting different runtimes like gVisor or Kata Containers to provide enhanced security and
> isolation between the sandbox and the host, including both kernel and network isolation."
> "This capability is a feature of the specific runtime, and users should select a runtime that
> aligns with their security and performance requirements."

The README explicitly states isolation "is a feature of the specific runtime" — i.e. delegated, not native.
The project is a CRD + controller managing isolated, stateful, singleton workloads (orchestration layer).

### 2. Official Agent Sandbox docs — gVisor isolation page
> "Agent Sandbox supports gVisor through the standard Kubernetes `runtimeClassName` field."
> "a kustomize overlay patches the base sandbox manifest to inject `runtimeClassName: gvisor`"
> "gVisor achieves this by intercepting application system calls and handling them in a sandboxed
> kernel — the container never directly touches the host kernel."

Confirms the delegation mechanism: the standard Kubernetes `runtimeClassName` field. Isolation enforcement
"happens at the runtime level, not within agent-sandbox itself." This is exactly the "CRD/orchestration
layer on top of runtime primitives" described in the claim.

### 3. Official Kubernetes blog (2026-03-20)
> "The Sandbox custom resource natively supports different runtimes, like gVisor or Kata Containers.
> This provides the necessary kernel and network isolation required for multi-tenant, untrusted
> execution."

NOTE ON POSSIBLE MISREAD: the blog says "natively supports different runtimes." This is NOT a claim that
agent-sandbox implements isolation natively. "Natively supports" = the CRD exposes runtime selection as a
first-class field; the runtimes (gVisor/Kata) "provide" the kernel/network isolation. This reinforces
rather than contradicts the claim.

### 4. Third-party corroboration (Northflank engineering blog, Google OSS blog)
- "Agent Sandbox is runtime-agnostic. You can pair it with gVisor for kernel-level sandboxing or Kata
  Containers for VM-grade isolation." (Northflank)
- "Both are configured via Kubernetes runtimeClassName, making the project backend-agnostic by design."
  (Northflank) — confirms runtime-vendor-neutrality.

## Assessment against the claim's two sub-assertions

1. "Delegates isolation enforcement to runtimes rather than implementing natively" — SUPPORTED by all
   three primary sources. README: "feature of the specific runtime." Docs: enforcement at runtime level
   via `runtimeClassName`.
2. "Runtime-vendor-neutral; CRD/orchestration layer on top of the same primitives a senior could use over
   Docker directly" — SUPPORTED. Configured via the standard `runtimeClassName` field; "backend-agnostic
   by design." gVisor (runsc) and Kata are equally usable as a Docker/containerd runtime directly, so the
   underlying primitives are indeed the same — K8s agent-sandbox adds orchestration (lifecycle, warm pools,
   stable identity), not a new isolation primitive.

## Minor qualification (does not refute)

The claim's phrasing "the same runtime primitives a senior could use over Docker directly" is an
inference, but a correct one: gVisor (runsc) ships as an OCI runtime usable directly with Docker
(`--runtime=runsc`) and Kata likewise integrates at the containerd/CRI layer. agent-sandbox does not wrap
or modify these runtimes; it selects them via `runtimeClassName`. The orchestration value-add (warm pools,
pause/resume, singleton identity) is real and is what K8s contributes beyond raw Docker — but that does not
contradict the isolation-delegation point of the claim.

## Sources
- https://github.com/kubernetes-sigs/agent-sandbox (primary README)
- https://agent-sandbox.sigs.k8s.io/docs/use-cases/gvisor-isolation/ (official docs)
- https://kubernetes.io/blog/2026/03/20/running-agents-on-kubernetes-with-agent-sandbox/ (official K8s blog)
- https://northflank.com/blog/agent-sandbox-on-kubernetes (third-party corroboration)
- https://opensource.googleblog.com/2025/11/unleashing-autonomous-ai-agents-why-kubernetes-needs-a-new-standard-for-agent-execution.html (Google OSS blog)
