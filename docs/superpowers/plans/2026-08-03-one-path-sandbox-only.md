# One path: every tool runs in the box

**Date:** 2026-08-03 · **Status:** PROPOSED. Operator direction, blast radius measured,
three consequences that need a decision before code. PRD amendment required.

> *"io toglierei tutta la parte non di sandbox, il codice resta pulito, tanto singolo
> utente o multi utente sono la stessa cosa"*

---

## What started this — a real defect, found live

Multi-user was switched on (`AURA_MUSR_ISOLATION=true`, `AURA_PROFILE=single_user_hardened`),
the daemon booted healthy, and `shell_exec` worked in the per-identity box. Then the
route → open → compute path was tried end to end, and Aura reported:

> *"il file non esiste davvero su disco. Ho verificato più volte: `/workspace/documents/`
> è vuota, `find / -name "*.xlsx"` non trova nulla, e `fs_read` conferma 'No such file or
> directory'. L'effetto collaterale di `document_open` non è avvenuto sul filesystem,
> nonostante la risposta dichiarasse il contrario. **Nessun risultato inventato.**"*

Nine tool calls, three independent checks, no hallucinated number. The agent behaved
correctly; the architecture lied to it.

**Cause, exactly.** `DocumentOpen` holds only `Documents` and `WorkspaceRoot` and calls
`os.Create` — always the aura container's filesystem. It has no `Router`. Under the strict
profile `shell_exec` and every `fs_*` run in the box, whose `/workspace` is a **different
volume**:

```
aura container   volume aura-workspace                    -> /workspace
per-identity box volume aura-box-e343c45d-81b8-…-cf9e34b5 -> /workspace
```

So `document_open` returns a path that is real where nobody looks and absent everywhere
the agent can reach. A tool that reports success and produces nothing.

**This is the argument for the operator's instruction.** The bug exists only because there
are two paths. With one, it cannot be written.

---

## Blast radius, measured

**Eight tools branch on the route decision** — `fs_edit`, `fs_glob`, `fs_grep`, `fs_read`,
`fs_write`, `send_file`, `shell_exec` (on `routed`), and `skill_read` (on `Strict()`).
Each carries a host implementation beside the box one, plus the `routed && err ⇒ deny`
fail-closed guard.

**`RuntimeProfile.Strict()` does NOT go away.** It governs five configuration gates
(object-store creds, Garage RPC secret, reasoning-trace redaction, web auth, the
multi-user gate) and the approval gateway (`internal/gateway/decide.go:48`,
`gateway.go:168`). Collapsing the tool path leaves all of those untouched. This change is
"the tool path has one branch instead of two", not "delete profiles".

**Work:**

- `SandboxRouter.Route` loses its `routed bool` — it always routes or fails.
- The 8 tools lose their host arm; the fail-closed guard becomes unconditional.
- `buildSandboxRouter` loses the nil-router branch for non-strict.
- `DocumentOpen` gains the `Router` and writes through `Router.WriteFile` — the defect
  above, fixed by construction rather than by a new branch. **Do the collapse first**, so
  this is written once and not twice.
- `compose.yaml`: the warm `npm`/`pip`/`uv` cache volumes on the **aura** service exist
  explicitly "for the non-strict host-direct shell_exec path" and become dead.
- The `aura` image drops `soffice`, `node`, `npm`, `uv`, `python3` — the agent can no
  longer reach them there. Today `aura:local` is 2.5 GB beside `aura-sandbox:latest` at
  2.35 GB, largely duplicated.

---

## Three consequences that need a decision, not a discovery

**1. `pdftotext` must STAY in the aura image.** It is not an agent tool. `filecard.Build`
shells out to it IN-PROCESS from `internal/documents/filecard/pdf_text.go` while writing
the card at ingest. Removing poppler silently breaks the work that took inside-PDF recall
from 0/8 to 8/8. `soffice` by contrast appears only in `document_open`'s tool DESCRIPTION —
no Go caller — so it goes.

**2. Docker becomes a hard runtime dependency of the daemon.** Today a box failure denies
one tool; after this, a machine with no reachable daemon has no working tools at all. That
is arguably the honest posture — but the `aura` CLI verbs that are not `serve`
(`docs ingest`, `db migrate`, `memory …`) must keep working without a box, and that
boundary has to be drawn deliberately rather than discovered in production.

**3. Coverage moves behind a tier that counts for nothing.** CLAUDE.md records that
`docker_integration` — the tier exercising `usersandbox` DockerBackend and the routed
branches in `internal/agent/tools` — runs in CI and **feeds zero coverage**, and names that
split as why the CAP_NET_ADMIN cap-assertion bug stayed latent (WR-01). Deleting the host
arms moves the surviving logic INTO that tier. Either the daemon-free unit tests for the
pure logic are written alongside, or the 85% floor drops and CI fails ~20 minutes after
push.

---

## Order

1. Collapse `Route` to a single outcome; strip the 8 host arms.
2. Wire `Router` into `DocumentOpen` and write through the box. Re-run the live
   route → open → compute test on `Clienti.xlsx`; the acceptance is **699**.
3. Slim the `aura` image, keeping poppler. Report the before/after size.
4. Draw the CLI boundary explicitly and test it.
5. Backfill daemon-free unit tests for whatever left the counted tier, and prove the
   coverage number rather than assuming it.

**Acceptance:** the live tabular question answers 699 through the box; `go test` full
matrix green; coverage ≥85% measured, not asserted; and `docker ps` shows the answer was
computed in `aura-box-<identity>`, not in the aura container.
