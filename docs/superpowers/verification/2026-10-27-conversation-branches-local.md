# Conversation branches — the live run through the cockpit

Date: 2026-10-27. Acceptance 9 of `docs/superpowers/specs/2026-10-27-conversation-branches-design.md`,
Task 8 of `docs/superpowers/plans/2026-10-27-conversation-branches.md`. Tree: `1aa073a`. The
binary reported `v0.0.0-20261027133830-1aa073ad8e05`, with no `+dirty`.

**Verdict:** passes. Chromium drove the real cockpit against `aura serve`:
- every request the model received held only the branch on screen;
- Previous switched without a read;
- the reload opened on the branch written last;
- the snapshot held three leaves and two roots.

The run also found one defect in the spec and one in the cockpit. Both are fixed: `4efbe60` and
`7514707`.

## Setup

Not the lab VM. This ran in a cloud container, without Garage, ArcadeDB or the embedder:
- `aura serve --only=cli`, `AURA_PROFILE=dev`;
- Postgres 18.6 on a disposable database (`aura_it`), migrated;
- `web/e2e/chat-branches-live.spec.ts`, `AURA_E2E_REAL_AGENT=1`, project `chrome`.

**The model was not a model.** This follows the precedent of
`2026-10-09-wave1-model-in-the-loop.md`, by operator choice ("Puoi montarti te come modello c'è
già un harness"):
- a llama.cpp-shaped endpoint (`AURA_LLM_PROVIDER=llamacpp`) parked every completion request;
- the reviewer read each request and answered as the model, with one `text_response` call;
- the endpoint streamed that answer back in llama.cpp's SSE form, usage included.

The spec named gemma-4-12B through compose's `aura-llm`. It was downloaded and started, then
dropped before any turn was timed:
- each request is 57 KB: 32 KB of schemas for 16 tools, 25 KB of messages (measured on the
  parked requests);
- the container has 4 CPU cores, and no GPU;
- the estimate was about 15 tokens/s of prompt reading, so tens of minutes before the first byte
  of every turn not already in the cache. It is an estimate: no llama-server timing was
  recorded.

**The browser is not the pinned build.** Playwright 1.64.1 wants Chromium revision 1248. The
container ships 1243 (`/opt/pw-browsers`), and the run used it through symlinks.

## What the final run measured

Conversation `01a17d97-f879-7c08-9a62-d4e1cfae2340`, 42.5 s.

| Step | The cockpit posted | The model received | Answer |
|---|---|---|---|
| send France | `/agent/run`, no `parent_seq` | France | Paris. |
| edit it into Italy | `/edit`, `diverge_seq` 1, role user | Italy, and nothing of France | Rome. |
| Previous | nothing | — | France on screen |
| send Spain | `/agent/run`, `aura.parent_seq` 2 | France, Paris., Spain | Madrid. |
| regenerate Madrid. | `/edit`, `diverge_seq` 6, role assistant | France, Paris., Spain | Madrid |
| reload | — | — | Spain on screen, Italy absent |

**History reads** (`/threads/{id}/messages`), in ms from the Previous click:
- −22,141: the load;
- −13,542 and −3,875: the read after each run (below);
- none in the 3 s after the click.

**Turns written**, read from `aura.conversation_turns`:

| seq | parent | branch | role | content |
|---|---|---|---|---|
| 1 | — | canonical | user | France |
| 2 | 1 | canonical | assistant | Paris. |
| 3 | — | `67395537…` | user | Italy |
| 4 | 3 | `67395537…` | assistant | Rome. |
| 5 | 2 | canonical | user | Spain |
| 6 | 5 | canonical | assistant | Madrid. |
| 7 | 5 | `8837b490…` | assistant | (empty: the regenerate's placeholder) |
| 8 | 7 | `8837b490…` | assistant | Madrid |

**The snapshot** drops `msg-7`, hangs `msg-8` from `msg-5`, and so has the leaves `msg-4`,
`msg-6` and `msg-8`. Its roots are `msg-1` and `msg-3`.

An earlier pass at `55c425d`, built before the fix, wrote the same tree
(`01a17d8a-7e70-70d8-9cca-a5fe213aca49`).

## Found, and fixed in this run

1. **The spec did not wait for its runs** (`4efbe60`).
   - `runOnce` took "Run complete" in the footer as the end of the run it started. The footer
     keeps the previous run's text while the next one runs, so from the second run on
     `runOnce` returned as soon as the POST left.
   - In the second pass, Regenerate was then clicked while Madrid. was still streaming. That
     message had no action bar yet, so the last Regenerate on the page was Paris.'s, which was
     disabled.
   - Playwright keeps the element it resolved. It waited for the run to end, then regenerated
     Paris. (`diverge_seq` 2). The server did what it was told: seq 7 with parent 1, cancelled
     when the failed test closed the page (conversation `01a17d84-…`).
   - The first pass failed on a third history read. This fits the same cause, a click that came
     before the read after the edit run, but that pass recorded no timings to prove it.
   - `runOnce` now waits for the stream to close and the Stop button to leave, the idiom of
     `mcp-cockpit-live.spec.ts` and `media-generation-live.helpers.ts`.
2. **A branch switch could be undone by the read after a run** (`7514707`).
   - `useWorkerReportRefresh` re-reads the history whenever the coordinator status changes, and
     puts the branch written last on screen. The watch stream reports that status for every run
     on the thread (`RunRegistry.coordinatorStatus`), not only for swarms. Those are the two
     reads at −13.5 s and −3.9 s.
   - The branch picker re-enables when the run ends, but the read waits for the next one-second
     poll (`swarmTranscriptTailInterval`).
   - A Previous clicked inside that window was undone when the read landed: its guard
     (`historyRequestRef`) moved on a send and on a thread switch, not on a branch switch.
   - The runtime's `setMessages` now advances that guard. assistant-ui calls it for a switch, a
     delete, a reset and the resync after a cancel. The new Vitest case holds the read, switches,
     then releases it; before the fix it saw the other branch return.
   - The live run did not reproduce the race. The spec waits 3 s after the last read before it
     clicks, so the Vitest case is the evidence.

## Corrections to the spec

- **§5, acceptance 9.** "Send, edit, Previous, send, reload" leaves two leaves, not three. The
  third leaf needs the regenerate, which the spec run includes.
- **§4.4.** "An edit … on such a message is refused (the composer keeps the text)" cannot hold.
  - In `@assistant-ui/core` 0.3.31, `DefaultEditComposerRuntimeCore.handleSend`
    (`default-edit-composer-runtime-core.js:77-88`) calls `runtime.append`, then `handleCancel()`
    straight away. That resets the text and closes the editor, whatever `onEdit` does.
  - So an edit on a message whose seq is unknown posts nothing and loses the edited text. Only
    the send composer gets a draft back, and only on a `MessageNotSentError`.

## What this does not show

- **A real model.** Every answer was written by the reviewer. The run shows what Aura sends and
  how it handles each reply. It says nothing about gemma-4-12B or any other model.
- **The race above in a browser.** Only Vitest exercises it.
- **Which of the two Madrid answers the reload opens on.** The spec checks the branch (Spain on
  screen, Italy absent), not the leaf. The snapshot's head is `msg-8`.
- **Tool rounds.** Every answer is a single `text_response`, so no tool result is folded into a
  card on this tree. The Vitest and agui unit cases cover that.
- **Compaction.** `/compact` condenses the path written last, not the branch on screen
  (`conversations.Store.Compact`), and the marker follows it. That stays an open item of the
  spec.
- **The lab VM, WebKit or iOS, and the pinned Chromium build.**
