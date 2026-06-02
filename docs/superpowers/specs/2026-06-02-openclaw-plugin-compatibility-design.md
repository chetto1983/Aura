# OpenClaw Plugin Compatibility Host Design

Date: 2026-06-02
Status: Approved design, pre-implementation

## Goal

Make OpenClaw plugins installable and usable in Aura without turning Aura into a
wrapper around the OpenClaw Gateway.

Aura should gain the leverage of the OpenClaw plugin ecosystem, including tools,
providers, channels, hooks, and services. Aura must still own the agent loop,
configuration policy, audit trail, human approval gates, context management,
and runtime governance.

## Non-goals

- Do not run a full OpenClaw Gateway as Aura's production plugin substrate.
- Do not make OpenClaw plugin code part of Aura's Go process.
- Do not bypass Aura's `ask_user`, audit, budget, tool-result, or capability
  checks.
- Do not implement public marketplace semantics ahead of Aura's local-first
  governance model.

## Industrial Pattern

Aura should use an out-of-process compatibility host with a manifest-first
control plane.

This follows a mature extension-system pattern:

- OpenClaw treats `openclaw.plugin.json` as cheap control-plane metadata and
  runtime modules as data-plane behavior.
- OpenClaw plugin modules register into a central registry, and core surfaces
  consume registry entries instead of talking to modules directly.
- HashiCorp and Vault use out-of-process plugin RPC so plugin crashes do not
  crash the host and plugin access is limited to explicit interfaces.
- VS Code keeps extensions in extension hosts and activates them lazily to
  protect startup and core stability.

References:

- OpenClaw plugin manifest:
  https://docs.openclaw.ai/plugins/manifest
- OpenClaw plugin architecture internals:
  https://docs.openclaw.ai/plugins/architecture-internals
- OpenClaw plugin guide:
  https://docs.openclaw.ai/tools/plugin
- HashiCorp go-plugin:
  https://github.com/hashicorp/go-plugin
- Vault plugin architecture:
  https://developer.hashicorp.com/vault/docs/plugins/plugin-architecture
- VS Code extension host:
  https://code.visualstudio.com/api/advanced-topics/extension-host

## Architecture

Aura adds an OpenClaw Compatibility Host subsystem.

High-level shape:

```text
Aura Go runtime
  owns config, audit, install policy, lifecycle, capability grants,
  prompt/cache discipline, tool results, and HITL approval
    |
    | typed RPC
    v
aura-plugin-host Node sidecar
  loads enabled OpenClaw-compatible plugin modules
  exposes registered tools/providers/channels/hooks/services
```

Core Go packages:

- `internal/plugins/manifest`: parse `openclaw.plugin.json`, compatible bundle
  manifests, package metadata, static contracts, and install records without
  executing plugin runtime code.
- `internal/plugins/policy`: apply enabled/allow/deny rules, path safety, source
  trust, API compatibility, capability allowlists, and approval requirements.
- `internal/plugins/host`: supervise the Node sidecar, handle health checks,
  restart behavior, RPC calls, timeouts, and diagnostics.
- `internal/plugins/registry`: store cold and live plugin snapshots, including
  tools, providers, channels, hooks, services, routes, and diagnostics.
- `plugin-host/`: Node ESM runtime that imports OpenClaw-compatible plugins and
  provides the Aura facade for plugin registration APIs.

The boundary is intentionally asymmetric:

- Go owns the control plane.
- Node owns plugin runtime loading.
- Plugin modules register into a sidecar registry.
- Aura consumes registry snapshots and adapts them into native Aura surfaces.

## Lifecycle And Data Flow

The lifecycle is split into cold metadata and live runtime.

Cold path:

1. `aura plugins install <spec>` resolves a source such as local path, git ref,
   npm package, or a future ClawHub locator.
2. Aura copies or links the plugin into `~/.aura/plugins/installed/<id>/`.
3. Aura reads `openclaw.plugin.json` and `package.json`.
4. Aura validates config schema, path safety, source policy, declared contracts,
   and OpenClaw/Aura compatibility.
5. Aura writes a durable install record to Postgres.
6. The plugin remains disabled or pending approval unless policy permits
   immediate enablement.
7. `aura plugins inspect <id>` works without loading runtime code.

Live activation path:

1. Aura derives an activation plan from the requested surface: tool catalog,
   provider resolution, channel startup, hook phase, service startup, or runtime
   inspection.
2. Aura starts or reuses `aura-plugin-host`.
3. Aura sends a restricted activation request containing plugin ids, config
   snapshot, allowed capabilities, and runtime context.
4. The sidecar imports only the requested plugin modules.
5. Plugin modules register capabilities into the sidecar registry.
6. The sidecar returns a registry snapshot and diagnostics.
7. Aura adapts registrations into native Aura surfaces.

Tool execution path:

1. The model calls an Aura tool backed by an OpenClaw plugin.
2. Aura applies normal governance: risk tier, capability grants, loop budget,
   loop dedup, `ask_user` gates, and result preview/spillover.
3. Aura calls sidecar `tool.execute`.
4. The sidecar executes plugin code.
5. Aura wraps the response as an Aura `ToolResult`.

Provider/channel/hook/service paths follow the same rule: plugin code may
request behavior, but Aura remains the final enforcement and audit point.

## Capability Bridges

OpenClaw capabilities map into Aura concepts:

- `contracts.tools` and runtime tool registrations become Aura tools.
- Provider registrations and provider hooks become Aura provider resolver hooks.
- Channel registrations become Aura channel adapters once Aura's channel
  framework is stable.
- Hooks become Aura event-bus handlers with explicit high-risk allowlists.
- Background services become supervised Aura plugin services.
- HTTP routes become Aura gateway routes only after Aura has a stable gateway
  auth model.

Aura should not expose raw OpenClaw SDK objects to plugin code. The sidecar
provides an Aura compatibility facade that implements only the supported subset
of OpenClaw registration APIs.

## Security And Policy

Full compatibility is default-deny.

1. No runtime load during install, list, inspect, config validation, or policy
   checks.
2. Install, update, uninstall, enable, and disable are risky mutations and must
   be audited in Postgres.
3. Plugin roots and entrypoints are blocked if they escape the plugin root, are
   world-writable, have suspicious ownership, or point outside managed install
   roots without explicit linked-install policy.
4. Pinned sources are preferred: exact git commits, exact npm versions, or
   artifact digests. Unpinned installs require explicit policy.
5. Npm dependencies are installed in managed per-plugin roots. Use `--ignore-scripts`
   by default; native build scripts require explicit approval.
6. Capability allowlists exist at both plugin id and surface level:
   `plugins.allow`, `tools.allow`, `providers.allow`, `channels.allow`,
   `hooks.allow`, and `services.allow`.
7. Conversation, prompt, and tool-result hooks are high-risk and require explicit
   conversation-access config.
8. The Node sidecar runs with a restricted environment. It receives only explicit
   plugin config and approved secrets.
9. Aura performs final checks for approvals, risk scoring, budgets, channel send
   policy, provider selection policy, and audit.
10. Broken, blocked, disabled, and policy-denied plugins remain visible in
    `aura plugins doctor` and `aura plugins inspect`.

## Phased Delivery

The design targets full compatibility, but it should ship in phases.

### Phase A: Cold Plugin Registry

Deliver install records, manifest parsing, package metadata parsing, config
schema validation, path safety, policy, `aura plugins list`, `inspect`,
`doctor`, and Postgres audit. No runtime loading yet.

Acceptance: `aura plugins inspect <id>` shows declared tools, providers,
channels, hooks, and services from manifests without executing plugin code.

### Phase B: Node Sidecar And Runtime Registry

Add `plugin-host/`, sidecar supervision, typed RPC, runtime activation, registry
snapshot, health check, and `inspect --runtime`.

Acceptance: a fixture OpenClaw plugin loads in the sidecar and reports
registered capabilities back to Aura.

### Phase C: Tool Bridge

Adapt plugin tools into Aura's `tools.Registry`. Preserve Aura `ToolResult`,
preview spillover, loop budget, loop dedup, and approval governance.

Acceptance: a fixture OpenClaw tool plugin appears in `aura tools`, is
discoverable via `tool_search`, executes through the sidecar, and returns an
Aura-wrapped result.

### Phase D: Provider Bridge

Map OpenClaw provider registrations and provider hooks into Aura's LLM provider
resolver. Start with text inference providers, then add media/search provider
helpers only when Aura has matching native surfaces.

Acceptance: Aura can use a plugin-provided provider/model ref without changing
the core LLM client API.

### Phase E: Hooks And Services

Bridge lifecycle hooks, prompt hooks, tool-call hooks, tool-result hooks, and
supervised background services. Require explicit high-risk allowlists.

Acceptance: a fixture hook can observe or modify an allowed lifecycle point, and
blocked hooks produce clear diagnostics.

### Phase F: Channels

Bridge OpenClaw channel plugins into Aura's channel framework after Aura's own
AG-UI and channel phases stabilize inbound/outbound semantics.

Acceptance: a fixture channel can receive an inbound message and send an
outbound response through Aura's agent loop.

### Phase G: Distribution Sources

Add npm, git, and ClawHub install flows, update/uninstall, integrity checks,
lockfile drift detection, and source-specific policy.

Acceptance: pinned git/npm installs work reproducibly; unpinned installs fail
unless explicitly allowed.

## Testing Strategy

- Unit tests for manifest parsing, schema validation, path safety, source
  policy, and registry normalization.
- Fixture plugins for tool, provider, hook, service, and channel registration.
- Sidecar integration tests for load, reload, crash, timeout, and diagnostics.
- Audit integration tests for install, enable, disable, runtime load, and blocked
  plugin cases.
- Security tests for path traversal, world-writable roots, escaping entrypoints,
  unpinned source rejection, dependency script gating, and high-risk hook
  deny-by-default behavior.
- End-to-end tests for plugin-backed tool execution through Aura `ToolResult`
  preview/spillover.

## PRD Impact

This is larger than Aura's existing Slice 7 skills plan. It should be tracked as
a new compatibility capability or a PRD amendment before implementation.

The closest existing Aura concepts are:

- deferred tool registry and `tool_search`
- skill install governance
- Postgres audit discipline
- future provider/channel/hook surfaces
- future AG-UI gateway and channel phases

The implementation plan must decide whether this becomes a new phase after
Skills or a replacement/expansion of the existing Skills phase. The approved
design direction is full OpenClaw compatibility through an Aura-owned sidecar,
not a skills-only importer and not an OpenClaw Gateway wrapper.

## Self-review

- No placeholder requirements remain.
- The design keeps the approved full-compatibility target while phasing delivery.
- The control/data-plane split is consistent across architecture, lifecycle, and
  security.
- Aura remains the final enforcement point in every runtime path.
- The design intentionally avoids implementation details that should be resolved
  during planning, such as the exact RPC encoding and Node package layout.
