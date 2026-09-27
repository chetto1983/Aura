---
name: browser-aura
description: "Driving a real browser in your sandbox with agent-browser — pages that need JavaScript, clicking through a site, filling forms, downloading files, and above all sites that need the operator to be logged in. Load this BEFORE the first browser step: whenever web_fetch is not enough (a login wall, a single-page app, a download behind a button), and whenever the operator asks you to use a site with their account. The rule it carries is the one that matters: you never ask for a password in chat — the operator logs in themselves through the live view link."
metadata:
  short-description: Use the sandbox browser and hand logins to the operator
---

# The sandbox browser

agent-browser runs a real Chromium inside your sandbox, and its tools are mounted for you as
`browser__agent_browser_*` (open, snapshot, click, fill, type, press, select, scroll,
screenshot, get_url, get_title, eval, close, tabs, waits). They are deferred: find them with
`tool_search` ("browser open", "browser snapshot"). Pages come back as an accessibility
snapshot with `@eN` references you click and fill. `web_fetch` is still the right tool for a
public page you only need to read.

## Always name the session, always restore it

Every call carries the same `session` (a short name of letters, digits, `-` and `_`, such as
`bank` or `docs-portal`) and `restore: true`. The session keeps the site's cookies across your
turns, across a sandbox restart, and across days — that is what makes a login last.

1. `browser__agent_browser_open` with `url`, `session`, `restore: true`.
2. `browser__agent_browser_snapshot` with `interactive: true` to get the `@eN` refs.
3. `browser__agent_browser_click` with `selector: "@e3"`, `browser__agent_browser_fill` with
   `selector` and `text`; take a new snapshot after anything that navigates, because refs are
   renumbered.

Restoring brings back the login, not the open page: after a pause, `open` the page again
before you read it. The first screenshot of a browser takes about ten seconds; later ones are
quick.

Each open session is its own Chromium: on a real site about 200 processes and 400 MB of your
sandbox, which has room for four at most. Keep to one session per site, reuse its name, and
`browser__agent_browser_close` it when the site is done — the restore file keeps the login for
next time, the process does not need to stay alive. A browser left idle for ten minutes closes
by itself; open the page again with the same session and `restore: true`.

If the `browser__` tools are not mounted, the same commands work through `shell_exec`:
`agent-browser --session portal --restore open https://example.com/login`, then `snapshot -i`,
`click @e3`, `fill @e5 "text"`, `get url`, `download @e9 /workspace/downloads/report.pdf`.

## Logins: the operator types, you never see the password

1. Open the login page in a named session.
2. Tell the operator to sign in and stop. In the cockpit chat the live view of the session
   opens under your answer by itself. On another channel, give them the page to open in the
   cockpit: `/browser/portal`, with your session's name.
3. They sign in themselves — password, two-factor code, CAPTCHA — and tell you when they
   are done. Do not poll the page while they type.
4. Take a snapshot to confirm you are past the login, then carry on.

Never ask for a password, a one-time code or a recovery key in chat, and never type one
you were given there: it would sit in the conversation for good. If the operator asks you
to remember a login so they need not repeat it, `agent-browser auth save` exists, but first
tell them plainly that a stored password is readable by anything running in this sandbox,
you included — the live view is the safer default.

## Before you act on a site

Respect the site's own terms. Some sites forbid automated access or reuse of their
content; when the operator's goal would break them, say so instead of doing it.

Clicking and filling are not gated, because the browser is yours. What they do on the site is
not yours to undo: before you submit anything irreversible — a payment, an order, a message,
a deletion — stop and ask the operator.

## Handing results back

Files you download land in `/workspace`; give them to the operator with `send_file`. When
you finish with a site for good, close its session to save it and free the browser.
