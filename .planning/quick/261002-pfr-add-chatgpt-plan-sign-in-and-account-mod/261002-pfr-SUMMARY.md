---
phase: quick-chatgpt-plan
plan: 01
status: in_progress
completed: false
commits: [6cbfc24c6, 92a9aef62]
---

# ChatGPT plan integration checkpoint

Implemented protected identity-scoped credentials, the account model catalog, official SDK Responses routing and login/model selection. Final remote-browser sources and embedded bundle passed full WSL vet/build/race, touched-package lint and repository contracts. Disposable combined coverage is 48,017/54,095 statements (88.8%), with the package-local policy passing. Review of 61 production/call-chain files has no open findings.

Remote sign-in reuses Aura's existing owner sandbox Chromium and authenticated browser page. A private loopback listener next to that browser receives the callback on the VM and transports it to the OAuth service. The pushed `92a9aef62` image was automatically installed, its exact VCS label verified, readiness passed, and deployed login/frame/cancellation E2E passed. The user subsequently completed official OpenAI consent; real status confirms connected and plan enabled. The account returned five visible models and saved `gpt-5.6-sol`.

Actual full-stack E2E passed with real Authula, the new daemon/UI, disposable Postgres, sandbox Chromium, official OpenAI sign-in/frame transport and popup-close cleanup. Frontend checks passed 46/46 focused cases and 3/3 bundle E2E fixtures, with 95.76% new UI/hook/API statement coverage and zero frontend duplicates. Fixtures and sign-in-boundary probes do not certify an account grant or inference.

The connected account exposed a missing reasoning projection: the composer showed only Auto/Off despite advertised efforts. The correction carries the strict advertised set into save/boot profile resolution, composer capabilities and fixed/adaptive policy, while reusing the SDK's effort and summary support. The multi-user restart review finding is closed: catalog metadata uses the persisted profile author; inference remains request-identity scoped. Targeted race and four reasoning mutations pass. The full frontend CI command passes all 3,607 tests, coverage thresholds, lint, typecheck and formatting.

Original CI passed 27/29 jobs; five other workflows passed. The two failed jobs were the ambiguous spinner assertions and browser authentication on HTTP 127.0.0.1. Action-scoped assertions and the existing HTTPS proxy correct those measured causes; independent real desktop/mobile TLS cockpit E2E passes 2/2. Fresh complete remote CI remains required.

`compose.yaml` remains unchanged. The user's active grant is retained. Preserve unrelated files and the other session's committed `web/e2e/auth.ts` change. Final follow-up disposable unit/integration coverage is 48,029/54,107 (88.8%), with package policy passing. Full Go vet/build/race also passed. Push, fresh CI, exact-revision deployment and a completed real inference/tool/summary turn remain in progress.

Evidence and exact limits: [261002-pfr-VALIDATION.md](261002-pfr-VALIDATION.md). Remote account login is proven; this checkpoint does not yet close completed-inference E2E-05.
