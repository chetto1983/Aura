---
name: browser-aura
description: "Driving a real browser in your sandbox with agent-browser — pages that need JavaScript, clicking through a site, filling forms, downloading files, and above all sites that need the operator to be logged in. Load this BEFORE the first browser step: whenever web_fetch is not enough (a login wall, a single-page app, a download behind a button), and whenever the operator asks you to use a site with their account. The rule it carries is the one that matters: you never ask for a password in chat — the operator logs in themselves through the live view link."
metadata:
  short-description: Use the sandbox browser and hand logins to the operator
---

# The sandbox browser

`agent-browser` runs a real Chromium inside your sandbox. You drive it through `shell_exec`.
Pages come back as an accessibility snapshot with `@eN` references you click and fill.
`web_fetch` is still the right tool for a public page you only need to read.

## Always name the session, always restore it

Every command carries the same `--session <name> --restore`. The name is short, made of
letters, digits, `-` and `_` (for example `bank`, `docs-portal`). The session keeps the
site's cookies across your turns, across a sandbox restart, and across days — that is what
makes a login last.

```bash
agent-browser --session portal --restore open https://example.com/login
agent-browser --session portal --restore snapshot -i      # interactive elements with @refs
agent-browser --session portal --restore click @e3
agent-browser --session portal --restore fill @e5 "text"
agent-browser --session portal --restore get url
agent-browser --session portal --restore download @e9 /workspace/downloads/report.pdf
```

Run `agent-browser skills get core` once when you need the full command set.

Each open session is its own Chromium: about 140 processes and 180 MB of your sandbox, which
has room for three at most. Keep to one session per site, reuse its name, and `close` it
when the site is done — the restore file keeps the login for next time, the process does not
need to stay alive.

## Logins: the operator types, you never see the password

1. Open the login page in a named session.
2. Give the operator the live view link and stop: `[Log in here](/browser/portal)` in the
   cockpit. On another channel, tell them to open `/browser/portal` in the cockpit.
3. They sign in themselves — password, two-factor code, CAPTCHA — and tell you when they
   are done. Do not poll the page while they type.
4. Take a `snapshot` to confirm you are past the login, then carry on.

Never ask for a password, a one-time code or a recovery key in chat, and never type one
you were given there: it would sit in the conversation for good. If the operator asks you
to remember a login so they need not repeat it, `agent-browser auth save` exists, but first
tell them plainly that a stored password is readable by anything running in this sandbox,
you included — the live view is the safer default.

## Before you use a site

Respect the site's own terms. Some sites forbid automated access or reuse of their
content; when the operator's goal would break them, say so instead of doing it.

## Handing results back

Files you download land in `/workspace`; give them to the operator with `send_file`. When
you finish with a site for good, `agent-browser --session <name> --restore close` saves the
session and frees the browser.
