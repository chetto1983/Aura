# Identity package caches — 2026-09-14

The Phase 3 cache finding was reproduced on appliance 192.168.1.225, running
`dff47b0950298ba89440d269cb1933e0fc32ba7e`. The runtime profile was `dev`, with
identity isolation disabled; both keys were absent from its existing `.env`.

Two disposable containers using the deployed sandbox image shared scratch cache
volumes. A installed a harmless package by name/version from a local sdist. B
changed its cached wheel and RECORD. A's repeat install reported `Using cached`
and executed B's replacement. Installing the sdist by direct path instead rebuilt
the wheel and retained the original behavior. No user cache was poisoned.

## Correction and measured checks

- uv/npm/pip volume names now include the trusted identity ID. An existing box
  with foreign or shared cache mounts is recreated, retaining its workspace and
  starting with empty private caches. Suspend retains them; Stop removes only
  the identity's workspace and caches.
- Native Docker integration and race tests passed. The migration test retains
  a workspace marker, rejects the foreign cache marker, and reuses the corrected
  container on a second Resolve. Two identities cannot read or overwrite each
  other's cache markers; deleting A leaves B's markers intact.
- `TestPipCacheIsolation`, using the deployed image and the real backend, passed:
  B cannot see A's wheel, B builds its own same-name/version wheel, and A's next
  cached install still executes A's original code. No network or fake pip is used.
- Native Docker coverage: **3,407/3,935 = 86.58%**, above the 85% gate. The whole
  `usersandbox`, `agent/tools` and `cmd/aura` test runs contribute to this report.
- Cache mount validation mutation spot-check: **7/7 killed**, four non-compiling
  mutations excluded. This is a scoped check, not the whole Phase 3 mutation gate.
- Go build, vet, package race and lint passed. Package tests: **104/104**;
  TypeScript build passed. Installer configuration, preflight, update, posture and
  makeself round-trip suites passed, including secret preservation and idempotence.

## Why the package left an appliance on dev

The public npm 0.2.0 artifact already carried hardened defaults for fresh installs.
Its existing-file branch only filled secrets and never added runtime posture.
The image updater likewise omitted that migration, so Compose's `dev`/`false`
fallbacks remained active indefinitely. Both appliance paths now call the same
posture migration, selecting `single_user_hardened` plus isolation, retaining an
existing `server_production`, and refusing unknown profiles. The package version
is 0.2.1. The native appliance has runc and one Garage replica; the stricter
server-production hardware prerequisites are not claimed.

This closes the measured cache defect in code. Phase 3 remains open: the HTTP
authorization attack suite, audit-trail assertions, host-escape battery and
uv/npm package-tampering scenarios have not been certified by these tests.
Scratch logs and before/after pip evidence are under
`.planning/tmp/identity-cache-check` and `.planning/tmp/identity-cache-fix`.
