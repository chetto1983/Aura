# `packages/create-aura` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps
> use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `npx create-aura` interviews an operator, writes a config the installer already
knows how to read, and runs the artifact — locally or over SSH onto a mini-PC.

**Architecture: this is a PORT, not a new program.**
`D:/Wpt/wpt-iot/packages/create-wpt-iot` is a working installer wizard of exactly this
shape — same two install modes, same base64 `format=1` config handed to a shell installer,
same injected command runner, same en/it i18n. **Copy it. Change only what Aura's contract
actually differs on.** The inventory below names, file by file, what is copied verbatim and
what changes; anything not named as a change is a change you should not be making.

Measured: the reference is ~2,030 lines including tests. Roughly 1,400 of those port with
no edit or a rename. The genuinely new work is one module (`modelroute.ts`), one function
body (`serializeInstallConfig`), one prompt flow (`collectSettings`), and the message
catalogues.

**Tech Stack:** Node >=22.13, TypeScript (ESM), vitest, Stryker, the reference's prompt
library. Do not substitute a different prompt or process library "while we are here".

**Spec:** `docs/superpowers/specs/2026-09-02-create-aura-npx-design.md`

---

## Global Constraints

- **Port first, invent last.** For every file, read the reference's version before writing
  anything. If you find yourself designing an abstraction the reference does not have, stop:
  it either already solved it differently, or you are adding something nobody asked for.
- **Four reference files are deliberately NOT ported:** `artifact.ts`,
  `installer-manifest.ts`, `scripts/stamp-installer.mjs`,
  `scripts/verify-installer-manifest.mjs`. They exist to download and verify a payload the
  wpt veneer cannot carry. Ours carries it (spec decision 5), so they have nothing to do
  here. Their tests go with them.
- **The config contract is fixed by `scripts/install.sh` and is not negotiable.** Nine keys,
  first line exactly `format=1`, and an unknown key makes the installer `exit 2`:

  ```
  format=1
  install_dir_base64=<base64>
  appliance=true|false                 # NOT base64 — raw, compared with = "true" literally
  gvisor=true|false                    # NOT base64 — same
  llm_provider_base64=<base64>         # openrouter | ollama
  llm_base_url_base64=<base64>
  llm_model_base64=<base64>
  openrouter_api_key_base64=<base64>   # empty on the Ollama route
  embed_image_base64=<base64>
  embed_ngl_base64=<base64>
  ```

  Read `parse_install_config` in `scripts/install.sh` before writing the emitter. **If this
  plan and that function disagree, the function wins and this plan is wrong.**
- **The artifact takes its flags after `--`.** makeself's own header parses argv first, so
  `artifact --config X` dies with `Unrecognized flag` and `install.sh` never runs. This is
  the single behavioural difference from the reference's `installLocal` / `installRemote`.
- **A decoded value may not contain a line break.** `install.sh` rejects it (`exit 2`)
  because `set_env_value` would emit two `.env` lines, and the installer (first match) and
  docker compose (last wins) would then read different secrets. Reject it in the wizard too,
  while the operator can still fix it.
- **No pinning, no `channel` field.** The npm publish rides `publish-aura-edge.yml`, so
  payload and images come from one commit (decision 6).
- **Gates:** vitest coverage >=85% and Stryker mutation >=70%, matching `web/`.
- **i18n en + it, both complete.** A key in one catalogue and not the other is a defect.
- Node >=22.13.0, ESM, npm (this repo pins `npm@12.0.0`; it does not use pnpm).

---

## Port inventory

`R` = `D:/Wpt/wpt-iot/packages/create-wpt-iot`. Read each file before porting it.

| Target | From | Treatment |
|---|---|---|
| `src/i18n.ts` | `R/src/i18n.ts` (22) | **verbatim** |
| `src/process.ts` | `R/src/process.ts` (192) | **verbatim** — `CommandRunner`, `ProcessRunner`, `redactText`, `StreamingRedactor`, `ProcessExecutionError` are all Aura-agnostic |
| `src/bin.ts` | `R/src/bin.ts` (5) | verbatim but for the imported name |
| `src/preflight.ts` | `R/src/preflight.ts` (26) | copy; change `MINIMUM_DISK_KB` and the architecture set for Aura |
| `src/types.ts` | `R/src/types.ts` (27) | copy `InstallMode`, `RemoteTarget`, `InstallRequest`; replace `InstallSettings` and `PreflightResult` fields with Aura's |
| `src/validation.ts` | `R/src/validation.ts` (71) | copy `ValidationError`, `validateHost`, `validatePort`, `validateUsername`, `validateInstallDir` **unchanged**; drop `validateDeviceSerial` / `validateAdminPassword` / `confirmAdminPassword`; add `validateBaseUrl`, `validateModelId`, `assertNoLineBreak` |
| `src/config-file.ts` | `R/src/config-file.ts` (53) | copy the temp-file machinery **unchanged** (`mkdtemp` + `chmod 0700` + write `0600` + cleanup-on-throw); replace only the body of `serializeInstallConfig` |
| `src/prompts.ts` | `R/src/prompts.ts` (131) | copy `SelectOptions`/`InputOptions`/`PasswordOptions`/`ConfirmOptions`/`PromptPort`/`inquirerPrompt`/`collectTarget` **unchanged**; rewrite `collectSettings` for Aura's questions |
| `src/local.ts` | `R/src/local.ts` (63) | copy the shape; change `REQUIRED_COMMANDS` / `REQUIRED_HOSTS`, the existing-install probe, drop the serial probe, and **add `--` before `--config`** |
| `src/remote.ts` | `R/src/remote.ts` (277) | copy `sshDestination`, `preflightRemote`, the scp plumbing and `WarningSink` **unchanged**; in `installRemote` add `--` before `--config` |
| `src/cli.ts` | `R/src/cli.ts` (262) | copy `CliDependencies` and `runCli`'s structure; change the flow to Aura's questions and add the model-route step |
| `src/modelroute.ts` | — | **new**, the only genuinely new module |
| `src/messages/{en,it}.ts` | `R/src/messages/*` | copy the file shape and the keys that still apply (target, ssh, errors); write Aura's own |
| `src/__tests__/*` | `R/src/__tests__/*` | port each alongside its module; drop `artifact.test.ts` |
| `package.json`, `tsconfig.json`, `vitest.config.ts` | `R/*` | copy; remove the `stamp-installer` / `verify-installer-manifest` scripts |

Two reference details worth calling out because they are already right and easy to lose:

**`validateInstallDir` already does what Aura needs.** It rejects control characters,
normalises with `posix.normalize` (which collapses `..`), and requires a leading `/` that is
not bare `/`. That is exactly the hole measured in `install.sh` — a relative `install_dir`
and `../../etc` are both accepted there, and under makeself a relative dir installs the
appliance into the extraction directory that gets deleted on exit. Port it as-is; do not
re-derive it.

**`ProcessRunner` already redacts secrets from streamed output.** `installLocal` passes
`{ redactions: [settings.adminPassword] }`. Ours passes the OpenRouter key the same way.

---

### Task 1: Port the scaffolding, `i18n`, `process`, `bin`

**Files:** `package.json`, `tsconfig.json`, `vitest.config.ts`, `src/{bin,i18n,process}.ts`,
`src/messages/{en,it}.ts`, `src/__tests__/{i18n,process}.test.ts`

- [ ] **Step 1:** Copy the four config files and the three sources from `R`, renaming
  `create-wpt-iot` to `create-aura`. Remove the `stamp-installer` / `verify-installer-manifest`
  / `verify:manifest*` scripts; `build` becomes `node scripts/clean-dist.mjs && tsc`.
- [ ] **Step 2:** Port `R/src/__tests__/{i18n,process}.test.ts` unchanged but for names.
- [ ] **Step 3:** Add ONE assertion the reference does not have, because it is the failure
  this port is most likely to introduce — a key present in `en` and missing in `it`:

```ts
it('has identical key sets across locales', () => {
  expect(Object.keys(en).sort()).toEqual(Object.keys(it).sort())
})
```

- [ ] **Step 4:** `npm install && npx vitest run` — green.
- [ ] **Step 5:** Commit.

---

### Task 2: Port `validation.ts` and `preflight.ts`

**Files:** `src/validation.ts`, `src/preflight.ts`, `src/__tests__/validation.test.ts`

**Interfaces:** Produces `ValidationError`, `validateHost`, `validatePort`,
`validateUsername`, `validateInstallDir`, plus new `validateBaseUrl`, `validateModelId`,
`assertNoLineBreak`.

- [ ] **Step 1:** Copy `validation.ts`. Delete the three wpt-specific validators. Keep the
  other five **byte-identical** — `validateInstallDir` in particular.
- [ ] **Step 2:** Port `R/src/__tests__/validation.test.ts`, dropping the cases for the
  deleted validators.
- [ ] **Step 3:** Add the new validators and their tests:

```ts
// A newline reaches set_env_value, which writes two .env lines; install.sh's reader takes
// the first and docker compose takes the last, so the installer and the running appliance
// would trust different secrets. install.sh rejects it too — this layer can say so while
// the operator is still typing.
it('rejects a line break in a model id', () =>
  expect(() => validateModelId('a\nOPENROUTER_API_KEY=x')).toThrow(ValidationError))
```

- [ ] **Step 4:** Copy `preflight.ts`, adjusting `MINIMUM_DISK_KB` and the architecture set.
- [ ] **Step 5:** Green, commit.

---

### Task 3: `config-file.ts` — the one function body that changes

**Files:** `src/config-file.ts`, `src/types.ts`, `src/__tests__/config-file.test.ts`

- [ ] **Step 1:** Copy `config-file.ts` whole. Keep `createTemporaryInstallConfig`
  **unchanged** — the `mkdtemp` + `chmod 0700` + write `0600` + cleanup-on-throw sequence is
  already right.
- [ ] **Step 2:** Replace `InstallSettings` in `types.ts` with Aura's fields, and rewrite
  only the array inside `serializeInstallConfig`:

```ts
export function serializeInstallConfig(s: InstallSettings): string {
  return [
    'format=1',
    `install_dir_base64=${encode(s.installDir)}`,
    // appliance and gvisor are the two install.sh reads RAW; base64-ing them would make
    // its literal `= "true"` comparison false and silently produce a non-appliance install.
    `appliance=${s.appliance ? 'true' : 'false'}`,
    `gvisor=${s.gvisor ? 'true' : 'false'}`,
    `llm_provider_base64=${encode(s.llmProvider)}`,
    `llm_base_url_base64=${encode(s.llmBaseUrl)}`,
    `llm_model_base64=${encode(s.llmModel)}`,
    `openrouter_api_key_base64=${encode(s.openrouterApiKey ?? '')}`,
    `embed_image_base64=${encode(s.embedImage)}`,
    `embed_ngl_base64=${encode(s.embedNgl)}`,
    '',
  ].join('\n');
}
```

- [ ] **Step 3:** Port the reference's test, then add the assertion its shorter key list did
  not need — the exact set, because `install.sh` exits 2 on any key it does not name:

```ts
it('emits exactly the nine keys install.sh accepts', () => {
  const keys = serializeInstallConfig(settings).split('\n').filter(Boolean).slice(1)
    .map((l) => l.split('=')[0]).sort()
  expect(keys).toEqual([
    'appliance', 'embed_image_base64', 'embed_ngl_base64', 'gvisor', 'install_dir_base64',
    'llm_base_url_base64', 'llm_model_base64', 'llm_provider_base64',
    'openrouter_api_key_base64',
  ])
})
```

Also assert an empty key stays an empty value (not the base64 of an empty string), and that
`appliance`/`gvisor` are unencoded.

- [ ] **Step 4:** Green, commit.

---

### Task 4: `modelroute.ts` — the only new module

**Files:** `src/modelroute.ts`, `src/__tests__/modelroute.test.ts`

**Interfaces:** Consumes `CommandRunner`. Produces
`probeGpu(runner): Promise<{ cuda: boolean; embedImage: string; embedNgl: string }>` and
`probeOllama(runner, url): Promise<{ reachable: boolean; models: string[] }>`.

Two measured constraints, neither optional:

**Probe the endpoint from the network that will use it.** Aura runs in a container; an
Ollama on the host is not at `127.0.0.1` as seen from inside. Probe with a throwaway
`docker run --rm` on the compose network and record the URL **as the container sees it**.
This project has already paid for this once — "Hyper-V port forwarding lies: probe via docker
network, not 127.0.0.1". Without it the wizard produces installs that look successful and
cannot answer a single turn.

**List the models actually installed** (`/api/tags`) rather than asking for an id from
memory. A typo'd model is indistinguishable from an absent one until the first turn fails,
long after the operator has walked away.

- [ ] **Step 1:** Write the failing test against a fake `CommandRunner`:
  - `nvidia-smi` exiting 0 yields the CUDA image and `embedNgl: '99'`; absent yields the CPU
    image and `'0'`
  - `probeOllama` returns the ids from an `/api/tags` body
  - **a host-only Ollama is reported unreachable** — the fake answers on the host and
    refuses from inside the container, and the probe must report what the CONTAINER saw.
    Assert on the recorded call that `docker run` was used: a probe that passes by querying
    the host is passing for the wrong reason.
  - an unreachable endpoint returns `reachable: false` and does not throw
- [ ] **Step 2:** Watch it fail, implement, watch it pass, commit.

---

### Task 5: Port `prompts.ts` and `cli.ts`

**Files:** `src/prompts.ts`, `src/cli.ts`, `src/__tests__/{prompts,cli}.test.ts`

- [ ] **Step 1:** Copy `prompts.ts`. Keep the option interfaces, `PromptPort`,
  `inquirerPrompt` and `collectTarget` **unchanged** — host/port/username is the same
  question for us.
- [ ] **Step 2:** Rewrite `collectSettings`: install dir, appliance, gVisor, model route
  (OpenRouter or Ollama — **not** llama.cpp; see the spec's "Why llama.cpp is not in the
  wizard"), then the route-specific questions, then the GPU probe.
- [ ] **Step 3:** Copy `cli.ts`'s `CliDependencies` and `runCli` structure; change the flow
  and add the model-route step.
- [ ] **Step 4:** Port both tests, then add the two assertions Aura's contract needs:
  - the OpenRouter route asks for a key, the Ollama route does not
  - **the Ollama route still emits an empty `openrouter_api_key_base64`** rather than
    omitting it — `install.sh` accepts an empty value and `exit 2`s on a missing key set
- [ ] **Step 5:** Green, commit.

---

### Task 6: Port `local.ts` and `remote.ts`

**Files:** `src/local.ts`, `src/remote.ts`, `src/__tests__/{local,remote}.test.ts`

- [ ] **Step 1:** Copy both. In `remote.ts` keep `sshDestination`, `preflightRemote`, the
  scp plumbing and `WarningSink` unchanged.
- [ ] **Step 2:** Change `local.ts`'s `REQUIRED_COMMANDS` / `REQUIRED_HOSTS` and its
  existing-install probe for Aura; drop the serial probe.
- [ ] **Step 3:** The one behavioural change, in BOTH files:

```ts
// makeself's header parses argv before it execs the embedded script, so
// `artifact --config X` dies with "Unrecognized flag" and install.sh never runs.
await runner.run('sudo', ['bash', artifact.path, '--', '--config', config.path],
  { redactions: [settings.openrouterApiKey] });
```

- [ ] **Step 4:** Port both tests, then add:
  - `expect(runner.calls[0][1]).toContain('--')` before `--config`, in both
  - the remote config is `0600` and is **removed after the install** — it carries the API
    key and leaving it in `/tmp` on a customer box is what nobody notices
  - a failed `scp` aborts before the install runs, and its exit code propagates
- [ ] **Step 5:** Green, commit.

---

### Task 7: Carry the artifact; publish from the same commit as the images

**Files:** `package.json`, `scripts/build-artifact.mjs`,
`.github/workflows/{publish-aura-edge,ci}.yml`, `src/__tests__/packaging.test.ts`

- [ ] **Step 1:** Write the failing test:

```ts
// Decision 5: the package CARRIES the payload, so nothing is fetched at install time.
// A tarball without the artifact cannot install anything, and every other test here
// would still pass.
it('lists the artifact in package.json files', () => {
  expect(pkg.files).toContain('install-appliance.run')
})
```

- [ ] **Step 2:** `build-artifact.mjs` shells out to `scripts/build_installer.sh` and writes
  `packages/create-aura/install-appliance.run`. It must FAIL when `makeself` is absent — no
  skip-as-green.
- [ ] **Step 3:** Add a `create-aura` job to `ci.yml` mirroring `web-test` and
  `web-mutation` (coverage >=85, mutation >=70), installing `makeself` where the artifact is
  built.
- [ ] **Step 4:** In `publish-aura-edge.yml`, after the images push, build and
  `npm publish --provenance --access public`. Publishing from that workflow is what makes
  decision 6 true; publishing anywhere else silently re-opens the drift this design closes.
- [ ] **Step 5:** `npm pack --dry-run` must list `dist/**` and the artifact and must NOT list
  `src/**` or `__tests__`. Report the file list.
- [ ] **Step 6:** Commit.

---

## Deferred, with reasons

- **`install.conf` persistence** (`$INSTALL_DIR/install.conf`, 0600) is promised by the spec
  and written by nothing. It belongs to `install.sh`, and workstream (c) depends on it.
- **The `*:edge` guard** (`install.sh:472`): `AURA_INSTALL_REF=vX.Y.Z` disarms the image pins
  and a source-less host then tries to `docker build`. The wizard cannot cause it (no
  `channel` field) but an operator's shell can. `install.sh`'s task, not this one.
- **llama.cpp as a third route** — enabling the profile without a ~6.7 GB fetch makes
  `docker compose up -d --wait` time out and, under `set -euo pipefail`, aborts the install.

## Self-Review

**Spec coverage.** (a)'s deliverables — `bin`, `cli`, `prompts`, `modelroute`, `local`,
`remote`, `config-file`, `validation`, `process`, i18n en/it, vitest suites — are covered by
Tasks 1-6; Task 7 covers the npm publish the (b) plan deferred here for lack of a package to
publish. `artifact.ts` and `installer-manifest.ts` are deliberately absent per decision 5,
stated in the inventory so a reader does not think they were forgotten.

**Placeholders.** None. Every task names its source file and its delta.

**Type consistency.** `CommandRunner` comes from Task 1's ported `process.ts` and is consumed
under that name by Tasks 4 and 6. `InstallSettings` is defined in Task 3 and consumed by
Tasks 5 and 6, so Task 3's field names bind — later implementers read Task 3, not the reverse.

**One risk this plan does not remove.** Every test drives a fake runner. Nothing here proves
a real install works on a real box; that needs root, Docker and a clean host. The first real
mini-PC install is the acceptance test, and it belongs before publication, not after.
