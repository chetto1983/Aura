# Phase 52: Mid-turn steering - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-08-25
**Phase:** 52-mid-turn-steering
**Areas discussed:** Channel reach, Channel semantics, Drain points, Marker trust, HTTP route,
Blocking preconditions, Undrained steer, Cockpit affordance, Lookalike scrub, Budget proof

---

## Framing — what was NOT re-litigated

PRD amendment #132 (2026-08-24) is a design-gate ratification committed before any steering code
existed. Its twelve ratified items were treated as locked and were not put to the user: the delivery
mechanism (tool-result append behind an attribution marker, not a `user` insertion), the
`POST /agent/runs/{runID}/steer` route with its 400/429/410 refusal ladder, the `aura.steer` CUSTOM
echo, drain-time persistence through `AppendTurn`, the tail-only cache invariant, `ask_user`-paused
runs being non-steerable, the steer→cancel ladder with no hard-interrupt rung, `internal/steer.Inbox`
`Push/Drain/Close`, and the child-steering non-goal.

Four measurements taken during the codebase scout, before any question was asked, defined what
remained open: the `RunRegistry` is AG-UI-only, hermes has two drain points rather than one, hermes'
marker is a static string, and `AURA_AGUI_RUN_DETACH` has two contradictory defaults.

---

## Todo cross-reference

| Option | Description | Selected |
|--------|-------------|----------|
| No, resta separato | `approval-resume-defects.md` are authorization gaps, not steering; #132 already rules `ask_user`-paused runs non-steerable, so the surfaces do not cross | |
| Sì, piegalo dentro | Both live in the runner's mid-turn input path; doing them once avoids opening the same seam twice | ✓ |

**User's choice:** Fold it in.
**Notes:** All three defects were then individually confirmed in scope (see below).

---

## Channel reach (STEER-05 / SC#5)

| Option | Description | Selected |
|--------|-------------|----------|
| Inbox su conversation-id | Steer addressed by conversation, so Telegram's busy gate can push; also hermes' shape (`_pending_steer` is agent-scoped, not run-scoped) | ✓ |
| Solo cockpit, STEER-05 differita | Follows #132's resolved OQ "cockpit first, Telegram after real usage"; costs an amendment correcting roadmap SC#5 and orphans STEER-05 | |
| Telegram registra una RunSession | Run identity leaves `internal/agui`; cleanest downstream, most invasive now | |

**User's choice:** Inbox keyed on conversation-id.
**Notes:** The measurement that framed this: `RunRegistry` lives only in `internal/agui`, wired by
`cmd/aura/serve_agui.go:81`; a Telegram turn calls `runner.Turn` directly and has no runID, so a
runID-keyed inbox would put channel steering out of reach by construction.

---

## Channel semantics — what a Telegram message means mid-turn

| Option | Description | Selected |
|--------|-------------|----------|
| Ogni messaggio = steer | Replaces the busy copy; matches what an operator already expects from a chat | ✓ |
| Solo `/steer <testo>` | Explicit, no ambiguity; costs remembering a command in a hurry | |
| Ogni messaggio = steer, con echo | Same as the first plus an acknowledgement | |

**User's choice:** Every message is a steer.
**Notes:** Confirmed against hermes **after** the choice, not before: `acp_adapter/server.py:1727-1745`
routes a plain text prompt arriving mid-run through `state.agent.redirect(...)`, falling back to
`queued_prompts` only when redirect is unavailable. The reading also surfaced that hermes **always**
replies `"Redirected the active turn with your correction."` — so the echo of option 3 was folded
into the chosen option rather than dropped (D-04).

---

## Drain points

| Option | Description | Selected |
|--------|-------------|----------|
| Due, parità hermes | End of tool batch AND before every API call; the second exists because a steer arriving during generation would never land if the model closes the turn | ✓ |
| Uno solo, a fine batch | Literal reading of #132; loses the steer that arrives during the final generation | |
| Due, il secondo solo a fine turno | Pre-API drain only when the round is about to close without tools | |

**User's choice:** Two, hermes parity.
**Notes:** hermes states the reason in code (`agent/conversation_loop.py:1490-1497`): *"steers sent
during an API call only land after the NEXT tool batch, which may never come if the model returns a
final response."* This requires an amendment line correcting #132, which ratifies "the drain point"
in the singular.

---

## Marker trust

| Option | Description | Selected |
|--------|-------------|----------|
| Nonce per-run + nota di sistema | Reuses `toolOutputNonce()` from `internal/agent/trust.go`, plus the system-prompt note teaching the model to trust only that marker | ✓ |
| Statico + nota, parità hermes | `STEER_MARKER_OPEN`/`CLOSE` + `STEER_CHANNEL_NOTE`; in production at hermes and cache-stable, but the defence rests entirely on the model obeying | |
| Nonce, senza nota | Non-forgeable without spending prompt tokens, but the model has never seen the wrapper and may read it as tool noise | |

**User's choice:** Per-run nonce plus the system-prompt note.
**Notes:** A static marker is forgeable by anything that can put text in a tool result, a web page or
a file. Aura already owns the non-forgeable version — inventory before invention.

---

## HTTP route shape

| Option | Description | Selected |
|--------|-------------|----------|
| Rotta runID + inbox conv | `POST /agent/runs/{runID}/steer` stays as #132 ratifies and resolves the conversation internally; Telegram pushes from its own path | ✓ |
| Rotta per conversazione | One addressing form, consistent with the inbox key; costs correcting #132 item 2 and loses alignment with `agent_run_cancel` in `httpMutationRoutes` | |
| Entrambe le rotte | Two surfaces to authorize, version and test instead of one | |

**User's choice:** Keep the runID route; the inbox is conversation-keyed behind it.

---

## `AURA_AGUI_RUN_DETACH` — two contradictory defaults

| Option | Description | Selected |
|--------|-------------|----------|
| `true` — vince il codice | `config_agui_run.go:36` is already `true` and `config_agui_test.go:27` records "campaign passed 10/10" with `=false` as the explicit rollback; steering requires detach | ✓ |
| `false` — vince il catalogo | Conservative; changes today's effective default under anyone already running detached, and discards the 10/10 campaign | |

**User's choice:** `true`. `config_knobs.go:116` is the side that gets corrected.
**Notes:** #132 explicitly refused to decide this and made it a precondition of STEER-01. Now decided.

---

## How the phase closes — `AURA_AGUI_RUN_STEER`

| Option | Description | Selected |
|--------|-------------|----------|
| Off di default, E2E acceso a mano | Literal #132 items 10 and 12; the default flip becomes a later amendment | |
| Flip a true dentro la fase | The phase carries the amendment line #132 item 12 requires, right after the >9.8 E2E | |

**User's choice:** *Neither, as framed.* Free-text response, verbatim: **"un flag a off è = a dark
code la smetti?"**

**Notes:** The framing was withdrawn. CLAUDE.md already forbids dark code, so offering a
shipped-but-off feature as an option was the error. Resolved as: `AURA_AGUI_RUN_STEER` ships default
`true` and the knob exists as the explicit rollback — the same shape the user had just chosen for
`AURA_AGUI_RUN_DETACH` one question earlier. Consequence carried forward without a further question,
because it follows: #132 item 12's composer contract lands **inside** this phase, since a cockpit
that still returns 409 `ErrThreadBusy` on send contradicts steering being on.

---

## The three folded approval-resume defects (amendment #133)

| Option | Description | Selected |
|--------|-------------|----------|
| Policy di decisione per-tool | `resumeAnswers` cannot express "this pause may only be declined"; LibreChat 403s here | ✓ |
| Risposta vuota che riprende | `payloadString(nil)` → `""` resumes the model with an empty answer; LibreChat 400s | ✓ |
| TTL sulle approvazioni pending | Nothing in `internal/gateway` expires a pending approval; LibreChat has `APPROVAL_EXPIRED_ERROR` | ✓ |

**User's choice:** All three.

---

## Undrained steer — asked twice

**First pass.** Three options were offered (dedicated `aura.steer_missed` frame / field on
`RUN_FINISHED` / frame plus persisted row).

**User's response:** free text — **"guarda hermes e librechat"**.

The options were withdrawn and the references were read. Findings:

- `agent/turn_finalizer.py:683-685` hands the leftover back on `result["pending_steer"]` *"so it can
  be delivered as the next user turn instead of being silently lost"*.
- `cli.py:14515-14528`, `gateway/run.py:25518-25532` and the ACP path are three independent consumers
  that all deliver it automatically, printing a visible line.
- `acp_adapter/server.py:2407-2425` (`_cmd_steer`) never refuses: running → queued on the active
  turn; idle → queued for the next turn, with the depth reported.
- LibreChat has no steering at all — only abort — so it is not a witness here.

**Second pass, with the evidence:**

| Option | Description | Selected |
|--------|-------------|----------|
| Consegna automatica, come hermes | Leftover becomes the next turn on its own, with a visible line | ✓ |
| Nominato, l'operatore rimanda | #132 item (B) and STEER-04 as they stand; no amendment to correct | |
| Consegna automatica solo dal cockpit | Two behaviours to explain instead of one | |

**User's choice:** Automatic delivery.
**Notes:** This is the discussion's substantive finding — **both** #132 item (B) ("drained and named
in the run's terminal event") and STEER-04 ("returned to the operator to re-send as a normal turn")
are weaker than the reference implementation, and roadmap SC#3 validates the superseded behaviour.
All three need correcting before code.

---

## Cockpit affordance — asked twice

**First pass.** Three options were offered (composer sends a steer / composer with visible
affordance / separate control).

**User's response:** free text — **"guarda hermes e librechat"**.

The references were read. The two diverge because they are different designs:

- LibreChat `client/src/components/Chat/Input/ChatForm.tsx:400-415` — the textarea stays live but
  send is `disabled={... || isSubmitting}` and replaced by a `StopButton`. The only intervention is
  abort.
- hermes `acp_adapter/server.py:1727-1760` — the composer is always live, a plain prompt during a run
  is redirected into the active turn, and the client is told so.

**Second pass, with the evidence:**

| Option | Description | Selected |
|--------|-------------|----------|
| hermes — redirect + echo | Composer stays the composer, send becomes a steer, UI echoes the redirect; the 409 on send goes away | ✓ |
| LibreChat — send diventa stop | What users expect from a chat AI today, but removes steering from the cockpit, which is the point of the phase | |
| Redirect + echo + stop | Both rungs of #132 item 8 visible side by side | |

**User's choice:** hermes — redirect plus echo.

---

## Non-text messages on a channel

| Option | Description | Selected |
|--------|-------------|----------|
| In coda per il turno dopo | hermes' explicit fallback: redirect is gated on `text_only_prompt`, everything else goes to `queued_prompts` | ✓ |
| Steer con il testo della caption | Recovers the "use this instead" intent, at the cost of splitting one message across two destinations | |
| Rifiutato con la busy copy | No new behaviour on the media path, already the bot's most fragile | |

**User's choice:** Queued for the next turn.

---

## Lookalike marker scrub

| Option | Description | Selected |
|--------|-------------|----------|
| No, basta il nonce | The nonce closes forgery; a scrub is belt and braces on a threat already closed | |
| Sì, scrub prima del rendering | Tool outputs cleaned of lookalike markers before entering history | ✓ |
| Rimandato, ma scritto | Out of scope here, recorded as an open edge | |

**User's choice:** Scrub, in this phase.
**Notes:** Must be tested against legitimate text that merely resembles the marker — the scrub must
not eat real content.

---

## STEER-02 budget proof

| Option | Description | Selected |
|--------|-------------|----------|
| Invariante + confronto live | Code invariant pinned by a test, plus a steered/unsteered A/B on `aura.conversation_turns` | ✓ |
| Solo confronto live | ACC-01 literal; nothing stops a future change from moving the budget unnoticed | |
| Solo invariante + test | Exactly the shape CLAUDE.md rejects as a closing signal | |

**User's choice:** Both.

---

## Claude's Discretion

- Marker and system-prompt note wording, subject to byte-stability for cache-prefix safety.
- Telegram redirect-echo wording and the auto-delivery line wording.
- The `aura.steer` echo frame's payload shape, beyond being ring-buffered.
- Which sibling of `llm_agent.go` (561/600 LOC) carries the two drain points.

## Deferred Ideas

- Child (delegated-worker) steering — #132 (D), a scope decision with evidence, not an assumption of
  simplicity.
- A hard interrupt rung — deliberate divergence from hermes parity, #132 item 8.
- Steer withdrawal, and a budget-bonus knob — both resolved 2026-07-23 as "Claude Code parity".
- Multi-replica steering — out of scope by construction; the inbox is in-memory on the run.
- Provider-path verification of the plain-`user` fallback on OpenRouter and llama.cpp — flagged for
  the researcher, per #132's own requirement on STEER-01.
