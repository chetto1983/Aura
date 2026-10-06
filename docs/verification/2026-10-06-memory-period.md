# Complete period recall: appliance verification, 2026-10-06

Code revision: `9bc8eb4570ad3825343873b3cd1ccebda8a1f968`.
The existing appliance updater installed this revision on VM `192.168.101.158`.
Both `aura` and `aura-arcadedb-mcp` reported this revision and healthy containers;
the updater finished with `state=current` and no error at 17:16:10 UTC.

## Before and after

The exported answer sampled six conversation windows with `recent`. A native,
identity-scoped ArcadeDB query found 48 projected user/assistant turns across nine
conversations within the 5 October 2026 Europe/Berlin day. The recent sample missed
morning conversations about drinking water and testing scheduler destinations.

The authenticated Playwright test submitted the unchanged question
`cosa abbiamo parlato ieri?` to the real agent over the appliance's HTTPS cockpit.
Conversation: `01a11238-b619-787b-b350-0ab187feec99`; stored model: `gpt-5.6-sol`.
The agent read the memory skill and made one `memory__memory_recall` call:

```json
{"mode":"period","from":"2026-10-05T00:00:00+02:00","to":"2026-10-06T00:00:00+02:00","limit":100}
```

The real tool response contained nine conversations and 48 turns, with no
`next_cursor`. All 48 unique `source_ref` values matched the pre-change native
query's set exactly. The run finished without an error. Playwright passed in
31.1 seconds, including a 25.3-second test.

The answer recovered the interrupted water reminders, the four-destination test,
the five-destination test, the rejected email attempt, the explicit WhatsApp
failure and retry, the incorrect immediate-send approach to the ten-minute
reminder, and the separate two-minute tests including an unanswered request.
It cited source turn ranges and explicitly distinguished recorded messages from
proof of external delivery. The operator independently reported a complete
nine-conversation read and confirmed that the updated agent discipline worked.

## Acceptance score

This is a binary acceptance rubric for this measured scenario, not a general
model-quality score. Each item contributes one point; all ten passed: **10/10**.

| Check | Evidence |
|---|---|
| Correct deployed revision and healthy stack | Container labels and completed updater |
| Authenticated, unchanged question, completed real run | HTTP 200, `RUN_FINISHED`, no `RUN_ERROR` |
| Updated skill used by the agent | `skill` call followed by recall |
| Correct local day and half-open boundaries | Actual call arguments above |
| Complete projected source set | Exact equality of 48 unique source references |
| All nine chats and exhausted interval | Nine evidence windows; no next cursor |
| Morning water reminders recovered | Answer includes interruptions and later recorded reminder |
| Five destinations, failures and retry preserved | Answer names none/email/stdout/Telegram/WhatsApp and distinguishes outcomes |
| Separate reminder attempts retained in synthesis | Ten-minute case, multiple two-minute cases and unanswered request |
| Evidence retained without claiming external delivery | Source turn ranges and explicit delivery caveat |

The tagged disposable-database integration test on the same appliance passed
against ArcadeDB 26.10.1. It separately exercised offset boundaries, exclusive end,
equal timestamps spanning pages, sparse turn sequences, deletion, active-chat
exclusion and identity isolation. Unit/race tests, vet/build and package lint also
passed locally. Mutations and the full release gates are run by CI.

## Reproduction and evidence

Local verification artifacts are intentionally untracked: they include private
conversation content. Under `.planning/tmp/`, the Playwright config is
`memory-period-vm.config.ts`, the spec is `memory-period-vm.spec.ts`, and the captured
stream is `memory-period-vm-sse.txt`. The expected-source oracle is
`memory-period-original-sources.json`.

```sh
cd web
AURA_E2E_ORIGIN=https://192.168.101.158 \
AURA_E2E_HTTPS_ORIGIN=https://192.168.101.158 \
AURA_E2E_USE_BUNDLED_CHROMIUM=true \
npx playwright test --config=../.planning/tmp/memory-period-vm.config.ts --project=chrome --workers=1
```

SHA-256 of the expected-source oracle:
`2578ed8486072b69ec97b75bb1a21857e110d8535613a8d568a7f9131d22ed56`.
SHA-256 of the completed stream:
`8e4d7b9440910cdfc8041d821b81633ba4324f3e8d42e886b5a2d2f0c6d4c047`.

CI completion is recorded below after the final jobs finish.

## Limits

This proves complete recall of the measured projected interval, not completeness
of projection against PostgreSQL or receipt of an external WhatsApp/email message.
Related conversations are grouped by the agent in its answer; the store preserves
separate attempts and every source turn. Pagination edge cases are covered by the
live fixture; this real historical interval fits in a single page of 100.
