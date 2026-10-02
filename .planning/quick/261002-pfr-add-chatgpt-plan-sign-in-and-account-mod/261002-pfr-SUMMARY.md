---
phase: quick-chatgpt-plan
plan: 01
status: in_progress
completed: false
commits: [6cbfc24c6]
---

# ChatGPT plan integration checkpoint

Implemented protected identity-scoped credentials, the account model catalog, official SDK Responses routing and login/model selection. Final remote-browser sources and embedded bundle passed full WSL vet/build/race, touched-package lint and repository contracts. Disposable combined coverage is 48,017/54,095 statements (88.8%), with the package-local policy passing. Review of 61 production/call-chain files has no open findings.

Remote sign-in now reuses Aura's existing owner sandbox Chromium and authenticated browser page. A private loopback listener next to that browser receives the callback on the VM and transports it to the OAuth service. Actual VM probes loaded the official OpenAI sign-in page, streamed frames, rejected wrong state and verified callback/cancellation cleanup. A real Chromium HTTP-redirect fixture confirmed that a callback arriving before navigation-ready does not deadlock. These measurements stop before human authentication, grant issuance and inference. The obsolete public callback and port derivation were removed.

Actual full-stack E2E passed with real Authula, the new daemon/UI, disposable Postgres, sandbox Chromium, official OpenAI sign-in/frame transport and popup-close cleanup. Frontend checks passed 46/46 focused cases and 3/3 bundle E2E fixtures, with 95.76% new UI/hook/API statement coverage and zero frontend duplicates. Fixtures and sign-in-boundary probes do not certify an account grant or inference.

`compose.yaml` remains unchanged. PRD measurement record `6cbfc24c6` is committed; implementation commit/push, CI/image publication and deployed E2E are in progress. No VM deployment setting or account credential was changed. Preserve unrelated workspace files and the concurrently landed OpenRouter commits.

Evidence and exact limits: [261002-pfr-VALIDATION.md](261002-pfr-VALIDATION.md). This checkpoint does not close E2E-05 or the remote login criterion.
