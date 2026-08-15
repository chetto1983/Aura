---
phase: 45-harness-correctness
plan: 09
subsystem: agent-harness
tags: [completion-critic, reply-hygiene, deliberation-leak, fabricated-identifiers, tdd, gap-closure]

# Dependency graph
requires: ["45-08"]
provides:
  - "internal/agent/llm_agent_completion_leak.go: leakedDeliberation, unsourcedIdentifiers, and gateReplyHygiene — deterministic reply-hygiene checks at the completion-critic seam"
  - "internal/agent/llm_agent_completion.go: gateCompletion runs reply hygiene before the critic call"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Enforce what a prompt rule only requests. 45-05 closed D-21 with system-prompt text; the text was verified present in the shipped binary and the model violated it anyway, so the harness now checks the part it can check itself."
    - "Read the operator-facing channel only. The transport separates TEXT_MESSAGE_CONTENT from REASONING_MESSAGE_CONTENT cleanly, so a detector spanning both would fire on every healthy turn."
    - "Key on self-ADDRESS, not on the first person: 'Ho cercato in memoria' is a result, 'let me be careful to copy them exactly' is the model talking to itself with the operator watching."
    - "Provenance as the fabrication test: a fact_key-shaped token that appears in no tool result of the same turn cannot have been read, so it was invented."

key-files:
  created:
    - internal/agent/llm_agent_completion_leak.go
    - internal/agent/llm_agent_completion_leak_test.go
  modified:
    - internal/agent/llm_agent_completion.go

key-decisions:
  - "The checks run BEFORE the LLM critic, not after. They cost no tokens, and they catch what the critic structurally cannot: the critic judges whether the WORK is done, not whether the prose about it is fit to send. The measured failure was a substantively COMPLETE turn — a correct DONE verdict on an unsendable reply."
  - "Neither check rewrites the reply. Laundering the text would hide the failure from the operator instead of surfacing it; the model is told what tripped and asked to send the result again without it."
  - "leakedDeliberation strips fenced and blockquoted spans first, so a transcript the operator pasted back stays answerable. unsourcedIdentifiers deliberately does NOT strip them: the measured failure presented its invented key inside a fence, and a fence is not evidence of provenance."
  - "A blank/absent tool-result set must not become a blanket veto on any hex-looking string — pinned by its own test."

requirements-completed: [ACC-02, HARN-06, HARN-07]

# Metrics
duration: ~45min
completed: 2026-08-15
---

# Phase 45 Plan 09: Stop a reply leaving with drafting notes or invented keys Summary

**The live run caught the harness doing the thing this phase exists to stop — showing the operator a value that was never recorded — and this closes it with a check rather than a stronger request.**

## What happened

45-08 drove the real stack and measured a reply that: leaked self-directed drafting notes
into user-facing text; did so in English to an operator writing Italian; and presented
`fact_key` values that no tool returned, stating in the same visible text *"I'm making this
up. I shouldn't fake hashes."*

The D-21 system-prompt rule 45-05 shipped for exactly this was verified **present in the
running binary** (`grep -a` inside `aura:local` at build `ed252f6b6`) and did not hold. So
the diagnosis was not "the rule is missing" but "a rule the model is asked to follow is
necessary and, measured, not sufficient."

## Task Commits

| Task | Commit | What |
|---|---|---|
| 1 | `fde87a2cd` | RED — failing tests reproducing the leak by shape, plus both probe edges |
| 2 | `a85627198` | GREEN — `gateReplyHygiene` at the completion-critic seam |
| 3 | `8ecf66182` | Live re-drive of the measured trigger, recorded |

## Verification

Unit, 8 subtests, all passing — including the three legitimate first-person replies and the
quoted/fenced spans that must NOT be flagged.

`go build`/`go vet` clean. `go test -race ./internal/agent/` **green in WSL** (29.8s); the
two Windows-only baseline failures unaffected. Pre-commit lint: 0 issues.

**Live, same shape that failed, build `a85627198`:**

| Measure | Before | After |
|---|---|---|
| deliberation markers in `TEXT_MESSAGE_CONTENT` | 6 | **0** |
| deliberation markers in `REASONING_MESSAGE_CONTENT` | 0 | 0 |
| language | degraded to English mid-reply | Italian throughout |
| fact_keys reported | truncated inventions | 9, all full-length |
| **unsourced (fabricated)** | the invented ones | **0** |

The fabrication check is mechanical: every 64-hex token in the reply was tested for presence
in that turn's `TOOL_CALL_RESULT` payloads (4621 bytes). All nine present.

## Not Claimed

This proves the measured failure does not reproduce on its own trigger. It does **not**
re-score SC#5 across the whole scenario — SC#3 and SC#4 remain open in 45-08 — so no
sign-off box is ticked on the strength of it. The detector is also a marker list, not a
semantic judge: a leak phrased in words outside `deliberationMarkers` would pass, and the
honest scope of this fix is the measured failure mode plus its close neighbours, not
"deliberation can no longer leak."

## Self-Check: PASSED

- [x] RED before GREEN, verified failing on behaviour rather than on build
- [x] Both probe edges pinned before the fix, not after
- [x] Reads the operator-facing channel only
- [x] Never rewrites the reply
- [x] `-race` green in WSL; lint 0 issues; file-size within cap
- [x] Re-driven live on the exact failing shape; 6 markers → 0, 0 unsourced keys
- [x] Scope limits stated rather than implied
