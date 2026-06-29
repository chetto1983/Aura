# Claim verification: K8s Agent Sandbox pluggable runtimes (gVisor/Kata)

**Claim:** "The Kubernetes Agent Sandbox project provides kernel and network isolation
for multi-tenant, untrusted agent code execution via pluggable runtimes (gVisor or
Kata Containers) selected on the Sandbox custom resource."

**Verdict: NOT REFUTED (supported, current, primary-sourced) — with one nuance.**

## Evidence

### 1. Primary source confirms the quote verbatim
Kubernetes blog (2026-03-20), under the heading "Strong isolation for untrusted code":
> "The Sandbox custom resource natively supports different runtimes, like gVisor or
> Kata Containers. This provides the necessary kernel and network isolation required
> for multi-tenant, untrusted execution."

The supporting quote is reproduced word-for-word on the live page. The claim restates it
faithfully. No misread or overreach in the core assertion.
Source: https://kubernetes.io/blog/2026/03/20/running-agents-on-kubernetes-with-agent-sandbox/

### 2. Mechanism nuance — "selected on the Sandbox CR" is accurate
The official project docs show selection via the standard Kubernetes `runtimeClassName`
field set inside the Sandbox CR's `podTemplate.spec` (not a dedicated top-level field):
```yaml
apiVersion: agents.x-k8s.io/v1beta1
kind: Sandbox
spec:
  podTemplate:
    spec:
      runtimeClassName: gvisor
```
> "Agent Sandbox supports gVisor through the standard Kubernetes runtimeClassName field."
Source: https://agent-sandbox.sigs.k8s.io/docs/use-cases/gvisor-isolation/

The GitHub README frames vendor-neutral multi-runtime support partly under "Desired
Sandbox Characteristics" (aspirational language: "We aim for the Sandbox to be
vendor-neutral, supporting various runtimes"). But working how-to docs exist for BOTH
gVisor and Kata Containers, so it is implemented today, not merely roadmap.
Sources: https://github.com/kubernetes-sigs/agent-sandbox ,
https://agent-sandbox.sigs.k8s.io/docs/use-cases/examples/kata-containers/

### 3. Independent corroboration (no dispute)
Northflank (2025/26): "Agent sandbox supports gVisor and Kata Containers as runtime
isolation backends. Both are configured via Kubernetes runtimeClassName... gVisor:
Intercepts system calls in user space via its runsc runtime... It provides kernel and
network isolation suitable for multi-tenant, untrusted code execution." No caveat or
dispute of the isolation claim.
Source: https://northflank.com/blog/agent-sandbox-on-kubernetes

### 4. Technical accuracy of the isolation guarantee
gVisor (runsc) intercepts syscalls in userspace (Sentry), so workloads never touch the
host kernel directly; Kata boots a per-pod microVM with its own kernel under a KVM
boundary. Both genuinely provide kernel + network isolation. This is consensus across
multiple 2025-26 sources. Caveat from the literature (not contradicting the claim, but
sharpening it): for actively adversarial workloads Kata's hardware-enforced microVM is
the stronger guarantee; gVisor's Sentry (~200k LOC Go) is itself an attack surface,
though an escape still faces the host kernel as a second boundary.
Sources: https://www.systemshardening.com/articles/kubernetes/runtimeclass-gvisor-kata/ ,
https://northflank.com/blog/kata-containers-vs-gvisor ,
https://edera.dev/stories/kata-vs-firecracker-vs-gvisor-isolation-compared

## Why not refuted
- Source is the official Kubernetes project blog (primary, authoritative for a K8s SIG
  project) — quality matches claim strength.
- Date 2026-03-20 — current, not outdated.
- Not marketing/press-release/cherry-picked benchmark; it is project documentation
  corroborated by independent engineering blogs and the project's own how-to docs.
- The only nuance (RuntimeClass mechanism vs. dedicated API field; README "desired
  characteristics" framing) does not falsify the claim — selection IS done on the
  Sandbox CR via `runtimeClassName`, and both runtimes have working docs.
