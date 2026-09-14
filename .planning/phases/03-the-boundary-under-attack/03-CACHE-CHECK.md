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

## CI test correction

CI run 34832104176 reported three excess goroutines in
`TestServer_DisconnectClosesPump`. Forcing the finite fixture to finish before
cancel reproduced the same +3 locally; stack traces showed HTTP connection
read/write/server loops, not an SSE producer. The test now reuses the existing
context-blocked runner, waits until the turn is open, disconnects, closes its own
HTTP client, and checks remaining goroutines with goleak. The corrected test
passed 50 consecutive race runs and the full AG-UI race suite. Production SSE
behavior is unchanged.

## Appliance verification

The shipped updater migrated the existing appliance environment automatically:
`AURA_PROFILE=single_user_hardened`, `AURA_MUSR_ISOLATION=true`. Configuration
validation returned no errors and `/readyz` returned ready with no reasons. The
old unreferenced global uv/npm/pip volumes were removed; the workspace was retained.
The update timer is active. The live image subsequently advanced to `605a9d8fa`.

On 2026-09-14 the real operator agent, through `aura shell`, called `shell_exec`
and successfully wrote a unique marker into all three package caches. Docker
inspection confirmed each mount source includes the operator identity, alongside
its existing workspace. The independent verifier read back all three markers in
exactly one box and removed them. The agent reported exit code 0. This is a real
model/tool/isolated-container check, not a readiness-only claim.

`aura chat new` exposed a separate stdin defect: its idempotency subprocess reached
the prompt and exited on EOF even with supplied input or a TTY. The executor now
inherits `os.Stdin`; a real two-process regression failed before the fix (zero
bytes instead of two lines). `aura shell` did not traverse the defective executor.

The clean disposable database coverage gate passed **39,884/45,579 = 87.5%**, with
the package-local policy satisfied. This is separate from the native Docker
coverage denominator above. All six workflows on `605a9d8fa` passed, including CI,
Skills, CodeQL, package verification and both image publication workflows.

Installer tag `installer-v0.2.1` names that verified revision. Release workflow
34841610637 passed Linux and Windows verification and published npm 0.2.1 with
provenance. The independently downloaded public tarball starts its CLI, passes
makeself integrity checks, contains byte-identical posture/environment helpers,
and migrates an existing dev fixture while preserving its other values.
