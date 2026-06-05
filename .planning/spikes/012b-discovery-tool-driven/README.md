---
spike: 012b
name: discovery-tool-driven
type: comparison
validates: "Same xlsx North-Star scenario on the current action=catalog flow (existing cot_eval) — fresh head-to-head baseline numbers"
verdict: VALIDATED
related: [012a, 011, 013]
tags: [skills, discovery, cot-eval, phase-11]
---

# Spike 012b: discovery-tool-driven (baseline)

## What This Validates

The baseline side of the 012 comparison: the SHIPPED architecture — system-prompt
§Skills routing (amendment #49, 6 iterations) + `skill action=catalog` (Go HTTP client)
+ `action=install` (gate) — measured with the same action-aware capture, same natural
D-35 prompt, stop-at-approval-endpoint.

## How to Run

```bash
set -a; . ./.env; set +a
go run ./.planning/spikes/012a-discovery-skill-driven -variant tool -n 3
```

(The harness lives in 012a — shared code, `-variant` selects the side.)

## What to Expect

2-3 PASS lines with `catalog:` discovery evidence and an approval endpoint; the FAIL
mode (run 1 in our session) is a turn that calls install and then ENDS IN PROSE.

## Investigation Trail

1. **Fresh baseline: 2/3 PASS** (walls 64.9s/37.0s/28.1s). PASS runs: catalog →
   install(anthropics/skills@xlsx) → model-composed ask_user. FAIL run: catalog → a
   15-web_fetch detour → install with CORRECT args → **terminal prose, no pause**.
2. **Root cause of the FAIL (production design gap, not model whim):** the hard pause
   only fires for model-initiated `ask_user` calls — `pauseCalls` is name-gated
   (`llm_agent_pause.go:38`). A non-ask_user tool returning `ErrAwaitingUserInput`
   goes through `runTool` → `fmt.Sprintf("error: %s", err)` (`llm_agent.go:300`) and
   `Error()` is the literal **"awaiting user input"** (`ask_user.go:83`). So after
   `skill action=install`:
   - the model sees `error: awaiting user input` — NO gate question, NO red flags;
   - `renderInstallGate`'s D-13 payload (red flags, always-stripped notice, tier) is
     DEAD CODE on this path — the operator never sees it unless the model happens to
     paraphrase from its own context;
   - iteration 6's teaching ("calling install IS the approval request — it
     automatically pauses") describes behavior the dispatch does not implement.
3. This root-causes today's full-E2E FAIL (`docs/aura-skills-eval-2026-06-05.md`,
   judge 0.47/0.90, install_prudence=1): the model installed and proceeded without an
   approval round-trip because the gate never reached it as a pause.
4. **The shipped eval harness cannot see any of this:** `turnCapture.toolNames`
   records only the tool NAME (`skill`), so the D-35 `requiredSeq`
   catalog→ask_user→install→sandbox_exec is STRUCTURALLY unsatisfiable (seqOK always
   false) and `installApprove` is biased false (any 2 skill calls of ANY action count
   as "install seen") — and the judge is fed those false observations. 3 of 7 hard-
   floor signals are harness artifacts.
5. Discovery quality note: baseline catalog queries arrived as topic-ish multi-word
   ("xlsx Excel spreadsheet") despite the iteration-4 retry-hint; the 012a skill-driven
   runs queried bare format tokens (`xlsx`) as taught — and 011 proved single-token
   queries rank dramatically better on this marketplace.

## Results

**VALIDATED as a baseline measurement** — the shipped flow reaches a correct install
request 2/3, with the miss rooted in the pause-machinery gap above, and the full-E2E
artifact gate FAILING today (0.47 judge, date-less artifact).

Superseded as an architecture mid-spike: the operator directive (2026-06-05, recorded
in MANIFEST Requirements) removes the install-approval ceremony entirely — see 012a
for the self-install full-loop result and 013 for what that deletes. The findings here
(dead gate payload, name-gated pause, blind eval capture) remain actionable for
whatever gate surface survives 013.
