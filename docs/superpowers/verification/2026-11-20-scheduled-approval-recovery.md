# Scheduled approvals in the lab

The lab appliance at `192.168.101.158` reported `4ed06afd97064a78027dd99e73edb34426ffe5e1` in `/healthz`. The three code and bundle commits end at `46f5d488c`. Only documentation changed between those revisions. Tests used the operator's cockpit, Telegram and connected Google account. Every email test used the verified sending address as its only distinct recipient.

| Scenario | Observed result |
|---|---|
| Simple email review on Telegram | The draft store reported the requested subject, body, To and Cc fields. Playwright found the matching Cc and subject on the same card as Approva and Rifiuta. The test chose Rifiuta. |
| Multipart email review | The pending draft contained both MIME alternatives. The Telegram card had one Rifiuta button, zero Approva buttons and directions to the cockpit. Playwright found the test marker in both body editors in the cockpit. The test declined the Telegram card. |
| Ordinary scheduled approval | An agent job started with one pending approval. The decision left zero pending approvals, one active task and an ACTIVE acknowledgement in the thread. The test cancelled the future job. |
| Expired one-shot | The thread recorded the expiry tool answer and exactly one assistant notice. The approval queue and the list of manageable tasks contained zero matching entries. The task-specific run-history endpoint returned 404 after cleanup. |
| Scheduled email declined in the cockpit | A card prepared before the update remained usable after the restart. The cockpit returned HTTP 200, draft status `declined` and zero remaining approvals. |
| Scheduled email approved in the cockpit | The cockpit returned HTTP 200 and `approved`. The transcript then recorded `task list`, `task pause` and a successful `task cancel` for the approved ID before it was due. Google search for the exact subject in the sending account's inbox returned zero messages and zero warnings. This result led to the [follow-up](../specs/2026-11-20-preserve-approved-scheduled-send-design.md). |

The Telegram cases counted above used the draft created by the actual calendar tool. An earlier model-authored approval prompt was discarded. The timed approval and expiry cases used ordinary client operations on the lab. Interrupted writes, row-lock timing, restart recovery and fan-out were tested against disposable PostgreSQL databases.

The original code candidate passed WSL vet, build, the full unit and race suites, and the database coverage gate with 54,011 of 61,388 owned statements covered (88.0%). The local pre-push checks included generated SQL, tagged-package compilation and web lint, types and formatting.
