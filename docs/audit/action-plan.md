# Action Plan

## Immediate P0/P1 Fixes

## Task: Add production execution capability profiles

- Description: Define `local_trusted`, `remote_read_only`, `workspace_write`, and `full_runtime_break_glass` profiles. Filter or deny tools before dispatch based on identity/channel/session/container workspace.
- Owner role: Platform/security engineer
- Expected outcome: Dangerous tools are unavailable by default in production contexts, even inside the container.
- Acceptance criteria: Remote profile cannot execute shell, arbitrary FS, or skill-write actions; tests cover allow/prompt/deny decisions.

## Task: Disable model-authored skill auto-activation in production

- Description: Change model actor create/update behavior to stage `pending_approval` unless an explicit disposable local sandbox profile is active.
- Owner role: Agent runtime engineer
- Expected outcome: Model self-extension requires approval in production.
- Acceptance criteria: Model create/update returns pending in production tests; approved skill becomes active only after resume/CLI approval.

## Task: Make `FSWrite` atomic

- Description: Replace direct `os.WriteFile` with the existing atomic writer and preserve file mode where possible.
- Owner role: Agent tools engineer
- Expected outcome: Crash-safe file overwrite semantics.
- Acceptance criteria: Failure-injection test proves existing file survives partial write.

## Task: Add background shell ownership and TTL

- Description: Store owner session/identity, enforce poll/kill authorization, and add default max runtime.
- Owner role: Agent tools engineer
- Expected outcome: Background jobs cannot outlive policy or cross session boundaries.
- Acceptance criteria: Expired job is killed; cross-session poll/kill is denied.

## Task: Enforce terminal exclusivity

- Description: Reject batches containing `text_response` plus mutating or unknown sibling calls.
- Owner role: Agent loop engineer
- Expected outcome: Final responses cannot hide side effects.
- Acceptance criteria: Unit test confirms mixed terminal plus `fs_write` produces model feedback and no write.

## Short-Term Improvements

## Task: Validate invocation context at loop entry

- Description: Ensure non-nil `Ctx`, `Budget`, client, registry, and agent. Derive budget deadline if missing.
- Owner role: Agent loop engineer
- Expected outcome: Direct package consumers cannot accidentally run unbounded loops.
- Acceptance criteria: Nil-budget test returns controlled error; blocking tool is canceled by budget.

## Task: Add tool ledger outbox

- Description: Persist failed ledger events to a durable outbox and retry asynchronously.
- Owner role: Infrastructure engineer
- Expected outcome: Forensic tool records survive transient DB failure.
- Acceptance criteria: Injected ledger failure queues event and later persists it.

## Task: Add MCP local policy manifests

- Description: Override server-provided mutability and risk metadata with local policy.
- Owner role: Integrations engineer
- Expected outcome: External MCP tools cannot understate side effects.
- Acceptance criteria: Misleading `ReadOnlyHint=true` is overridden by local mutating policy.

## Task: Harden reasoning trace

- Description: Add production guard for `AURA_REASONING_TRACE=full`, retention, and encrypted destination option.
- Owner role: Observability engineer
- Expected outcome: Debug traces cannot silently persist sensitive prompts in production.
- Acceptance criteria: Production config rejects full trace unless break-glass is set.

## Medium-Term Architecture Work

## Task: Build sandboxed shell/filesystem execution

- Description: Run shell/FS operations inside OS/container sandbox with workspace roots, read-only roots, writable roots, and network policy.
- Owner role: Platform engineer
- Expected outcome: Tool permissions are enforced outside model cooperation.
- Acceptance criteria: Sandbox tests prove denial of outside-workspace writes and blocked network when disabled.

## Task: Add tool transaction/idempotency model

- Description: Require mutating tools to define idempotency key, commit semantics, and recovery handler.
- Owner role: Agent runtime engineer
- Expected outcome: Crash after side effect does not cause blind replay or unknown state.
- Acceptance criteria: Crash-injection tests pass for representative mutating tools.

## Task: Add deterministic memory middleware

- Description: Perform memory recall before model call and memory write classification after turn using runtime policy.
- Owner role: Memory/agent engineer
- Expected outcome: Recall/write behavior is testable and model-independent.
- Acceptance criteria: Memory is injected when relevant even if the model omits search.

## Long-Term Industrialization

## Task: Add full production health model

- Description: Implement liveness/readiness with dependency classes and degradation reasons.
- Owner role: Infrastructure engineer
- Expected outcome: Operators know when the service can safely accept traffic.
- Acceptance criteria: Health endpoint covers DB, LLM, MCP, embedder, scheduler, sidecars, shell supervisor, and tracing.

## Task: Add load, chaos, and security CI lanes

- Description: Add race, integration, live smoke, chaos, and prompt-injection test suites.
- Owner role: QA/platform engineer
- Expected outcome: Production failure classes are continuously exercised.
- Acceptance criteria: CI reports separate unit, integration, live, race, and security lanes.

## Task: Create tool onboarding checklist

- Description: Require risk tier, timeout, policy, idempotency, observability, and tests for every new tool.
- Owner role: Principal engineer
- Expected outcome: Tool surface area remains governable.
- Acceptance criteria: New tools cannot merge without checklist completion.
