# Wave one — the cockpit and the agent with a model in the loop

Date: 2026-10-09. Tree: `7dd3b3978` plus the fixes this run produced (`d0d1dd303`, `a61d1aaba`,
`a75374c54`). Scope: the three wave-one items, tool policy, browser hand-back and work board
(`docs/superpowers/plans/2026-10-09-{tool-approval-policy,browser-takeover,work-board}.md`).

Not the lab VM. This ran in a cloud container: `aura serve` on a disposable Postgres
(`aura_e2e`), no Docker, no Garage, no ArcadeDB, no embedder. The model was not a model: a
llama.cpp-shaped OpenAI-compatible endpoint (`AURA_LLM_PROVIDER=llamacpp`) parked every
completion request, and the reviewer answered each one by hand as the model would, reading the
real system prompt, manifest and tool results Aura sent. The operator was a Playwright driver
on the real cockpit, desktop and Pixel 5.

## What this run does not prove

- **No real model.** Every answer was written by the reviewer. It shows what Aura sends and
  how it handles each reply; it says nothing about whether a given model makes those replies.
  The spec's acceptance item 2 ("three runs out of three on two models of the release's
  matrix") is still open.
- **No phone.** The Pixel 5 profile is Chromium's emulation. Acceptance item 1 (a real touch
  device) is still open.
- **No sandbox.** With no Docker, every box-routed tool was denied; nothing here exercises the
  browser takeover against a live agent-browser. The live view was driven with a fake stream.
- **No week of use**, so nothing about whether the board stays tidy.

## Measured

| What | Profile | Result |
|---|---|---|
| "Add a card for tomorrow and move the invoices to Done": `tool_search` loads `board`, list and add in parallel, move | dev | done, no approval prompt (add and move are Normal) |
| delete a card | dev | **ran with no prompt**: the gateway is a declared no-op under `dev` and `local_trusted` (spec: "Strict profiles only"), so are policies |
| delete a card | `single_user_hardened` | withheld before running; the prompt offers *once* and *this conversation*, no *always*, because the subject has an `ask` policy |
| a second delete in the same conversation after *this conversation* | strict | ran with no prompt: the session grant holds under `ask` |
| `web_search` with a `deny` policy | strict | refused before running: "disabled for this identity by policy: do not retry it, tell the operator" |
| a card id from before a daemon restart | strict | the approval was spent, then the tool refused the id ("run board list or board search first"); after a search, a second prompt |
| "remind me to call the supplier" with the card already on the board | strict, after `a61d1aaba` | the model searched first, found the overdue card and moved its due date; no twin |
| the board page, the editor, the views and the column editor | both | screenshots, desktop and Pixel 5, English and Italian, light and dark |

## Found, and fixed in this run

1. **Duplicate card.** Before `a61d1aaba` the description did not say to look before adding;
   the model (the reviewer, playing it plainly) sent list and add together and made a twin of
   an overdue card.
2. **`&#34;` in tool results.** `%q` in the board tool's move and search lines reached the model
   HTML-escaped by the `<tool_output>` envelope.
3. **Roboto from cdn.svar.dev on the Studio**, which CI caught (`d0d1dd303`): sharing
   `svar.css` between the two SVAR widgets let the bundler load it before the package sheets,
   and the later CDN `@font-face` won. Reproduced locally (the out-of-order build fetched
   `cdn.svar.dev/fonts/roboto/regular.woff2`), fixed, and guarded by `e2e/svar-origin.spec.ts`.
4. **Overflows** (`a75374c54`): the chevron over Italian filter labels, the column editor on a
   phone, and "Bacheca" running into "Studio" in the mobile tab bar (operator screenshot).

## Found, not fixed — decisions for the operator

- **The approval prompt for a delete names no card.** It reads "Approve board
  (risk=destructive)? … args: action, id": the operator approves without seeing which card.
  The gateway lists argument names only, on purpose (arguments may carry secrets). A per-tool
  safe summary for the prompt would fix it; that is a gateway design change.
- **A restart costs a second prompt.** The seen-ids rule lives in memory, so after a restart a
  legitimate id from the conversation is refused only after the operator approved it.
  Persisting the seen set, or checking it before the gateway prompts, would avoid it.
- **Policies are inert under `dev` and `local_trusted`**, by the spec's decision, and the
  cockpit does not say so: a policy set there looks active and does nothing.
- **A denied tool call shows as completed** in the chat (green dot), the same as one that ran.
- **The gateway's approval request reaches the model HTML-escaped** (`&#34;` throughout the
  JSON it must copy verbatim). A model that copies it literally would send escaped strings.
- **`aura serve` did not exit within 8 seconds of SIGTERM** once, with an idle stream open; not
  investigated.
