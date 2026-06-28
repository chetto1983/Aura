---
spike: 039
name: telegram-onboarding-loopagent-prototype
type: standard
validates: First-run Telegram onboarding can run as LoopAgent[InterviewStepAgent], with confirm/skip/edit terminal paths writing or bypassing Agent.md and resuming chat.
verdict: VALIDATED
related:
  - internal/agent/workflow/loop.go
  - internal/channels/telegram/hitl.go
  - internal/channels/telegram/bot_dispatch_test.go
tags:
  - phase-14
  - telegram
  - onboarding
  - loopagent
  - agent-md
---

# Telegram Onboarding LoopAgent Prototype

## What Validates

Given the existing `workflow.NewLoop` contract, when a first-run Telegram identity needs onboarding, then a `LoopAgent[InterviewStepAgent]` can:

- Ask step-by-step profile questions.
- Emit a draft `Agent.md`.
- Stop on `Actions.Escalate=true` after confirm.
- Support skip and edit paths.
- Return state deltas that a Telegram adapter can map to profile writes and normal chat resume.

## Research

This spike follows the Phase 14 roadmap requirement that onboarding be a `LoopAgent[InterviewStepAgent]` and terminate through `Event{Actions.Escalate=true}`. It uses Aura's real `agent.Agent`, `agent.Event`, `agent.Actions`, and `workflow.NewLoop` types.

## How to Run

```powershell
go run ./.planning/spikes/039-telegram-onboarding-loopagent-prototype
```

## What to Expect

The harness runs three deterministic scenarios:

- `confirm`: name/preference questions, draft, confirm escalation with `agent_md`.
- `skip`: immediate terminal event with `skipped=true` and `resume_chat=true`.
- `edit`: draft, revised draft, confirm escalation with edited `agent_md`.

## Observability

Key output from the run on 2026-06-08:

```text
[CHECK] confirm path escalates with completed Agent.md
[CHECK] skip path escalates and resumes normal chat
[CHECK] edit path revises draft before profile write
[SUMMARY] VALIDATED phase14 Telegram onboarding LoopAgent prototype
```

The trace logs show the emitted `StateDelta` payloads, including `onboarding_completed`, `agent_md`, `preferences_json`, `skipped`, and `resume_chat`.

## Investigation Trail

The important production insight is that the loop does not need tool-call steps for the interview itself. Natural language onboarding events are yielded as non-tool events, and the terminal signal is the escalate event. The Telegram adapter can keep UI/HITL specifics outside the workflow and map the final `StateDelta` into the profile store.

## Results

VERDICT: VALIDATED.

Phase 14 can implement onboarding as a workflow agent plus a Telegram adapter. The adapter should own first-message detection, `/onboard`, buttons/commands for confirm/edit/skip, and post-escalation profile writes.
