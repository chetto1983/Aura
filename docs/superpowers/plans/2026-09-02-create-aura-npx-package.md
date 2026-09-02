# `packages/create-aura` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps
> use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `npx create-aura` interviews an operator, writes a config the installer already
knows how to read, and runs the artifact — locally or over SSH onto a mini-PC.

**Architecture:** A TypeScript CLI that produces a **config file**, never `.env`.
`scripts/install.sh` owns `.env` through `write_env_if_missing` / `ensure_env_default` /
`set_env_value`, which are idempotent; duplicating that in TypeScript would be two
implementations diverging at the first edge case. The npm package **carries** the makeself
artifact rather than downloading it, so raw.githubusercontent leaves the install trust path
entirely. Every command that touches the outside world goes through an injected runner, so
local and remote installs are tested without a box.

**Tech Stack:** Node >=22.13, TypeScript (ESM), vitest, Stryker, `@clack/prompts`.

**Spec:** `docs/superpowers/specs/2026-09-02-create-aura-npx-design.md`

**Reference implementation:** `D:/Wpt/wpt-iot/packages/create-wpt-iot` — the same shape,
working. Copy its structure; do **not** copy `artifact.ts`, `installer-manifest.ts`,
`scripts/stamp-installer.mjs`, `scripts/verify-installer-manifest.mjs`. Those four exist
only to download and verify a payload the veneer cannot carry. Ours carries it (spec
decision 5), so they are dead weight here.

## Global Constraints

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

  Read `parse_install_config` in `scripts/install.sh` before writing the emitter. If this
  plan and that function ever disagree, **the function wins and this plan is wrong.**
- **A decoded value may not contain a line break.** `install.sh` rejects it (`exit 2`),
  because `set_env_value` would emit two `.env` lines and the installer (first match) and
  docker compose (last wins) would then read different secrets. The wizard rejects it first,
  while the operator is still typing.
- **The artifact takes its flags after `--`.** makeself's header parses argv first, so
  `./install-appliance.run --config X` dies with `Unrecognized flag`. Every command this
  package generates uses `-- --config <absolute path>`.
- **Mode 0600 in a 0700 directory, absolute path only.** Secrets never travel through argv:
  they reach neither the process table nor shell history.
- **No pinning, no `channel` field.** The npm publish rides `publish-aura-edge.yml`, so the
  payload an operator installs and the images it pulls come from one commit (decision 6).
- **Gates:** vitest coverage >=85% and Stryker mutation >=70%, matching what `web/` meets.
- **Every external command goes through the injected runner.** No `execa` / `child_process`
  call outside `process.ts`. That is what makes `local` and `remote` testable without a box.
- **i18n en + it, both complete.** A key in one catalogue and not the other is a defect.
- Node >=22.13.0, ESM, npm (this repo pins `npm@12.0.0`; it does not use pnpm).

---

## File Structure

```
packages/create-aura/
  package.json          bin -> dist/bin.js; files includes dist + the artifact
  tsconfig.json
  vitest.config.ts      coverage thresholds 85
  stryker.conf.json     mutation threshold 70
  README.md
  src/
    bin.ts              entry; nothing but a call into cli
    cli.ts              flag parsing, flow orchestration, exit codes
    prompts.ts          the interview (@clack), en/it
    validation.ts       install dir, booleans, URLs, model ids, secret shape
    config-file.ts      emit the format=1 contract; 0600 in 0700
    modelroute.ts       GPU probe, container-network endpoint probe, /api/tags listing
    local.ts            run the artifact here
    remote.ts           ship + run the artifact over SSH
    process.ts          the injected runner; the ONLY place a subprocess is spawned
    i18n.ts             loader
    types.ts            shared shapes
    messages/en.ts
    messages/it.ts
    __tests__/*.test.ts one per module
```

The split worth stating, because it is what keeps this honest: `config-file.ts` knows the
contract, `validation.ts` knows what is acceptable, and neither knows how to run anything.
`local.ts` and `remote.ts` know how to run things and nothing about the contract.

---

### Task 1: Package skeleton, i18n, and the first green test

**Files:**
- Create: `packages/create-aura/{package.json,tsconfig.json,vitest.config.ts}`,
  `src/{bin.ts,types.ts,i18n.ts}`, `src/messages/{en.ts,it.ts}`
- Test: `src/__tests__/i18n.test.ts`

**Interfaces:**
- Produces: `t(key: string, vars?: Record<string, string>): string`,
  `setLocale(l: Locale): void`, `LOCALES`. Every later task calls `t`.

- [ ] **Step 1: Mirror the reference, then strip it**

Read `D:/Wpt/wpt-iot/packages/create-wpt-iot/{package.json,tsconfig.json}` and `src/i18n.ts`.
Mirror them. Remove from `scripts`: `stamp-installer`, `verify-installer-manifest`,
`verify:manifest`, `verify:manifest:remote`. `build` becomes
`node scripts/clean-dist.mjs && tsc`.

- [ ] **Step 2: Write the failing test**

```ts
import { describe, expect, it } from 'vitest'
import { LOCALES, setLocale, t } from '../i18n.js'

describe('i18n', () => {
  it('resolves a key in both locales', () => {
    setLocale('en'); expect(t('route.title')).toBeTruthy()
    setLocale('it'); expect(t('route.title')).toBeTruthy()
  })

  // A key present in one catalogue and missing from the other ships a wizard that goes
  // silent halfway through the interview, in one language only, and nothing else here
  // would notice.
  it('has identical key sets across locales', () => {
    const keys = (l: 'en' | 'it') => Object.keys(LOCALES[l]).sort()
    expect(keys('en')).toEqual(keys('it'))
  })
})
```

- [ ] **Step 3: Run it, watch it fail**

Run: `cd packages/create-aura && npm install && npx vitest run`
Expected: FAIL — `Cannot find module '../i18n.js'`.

- [ ] **Step 4: Implement `i18n.ts` and both catalogues** with one key, `route.title`.

- [ ] **Step 5: Run it, watch it pass.**

- [ ] **Step 6: Commit**

```bash
git add packages/create-aura
git commit -m "feat(create-aura): package skeleton with a locale-parity test"
```

---

### Task 2: `validation.ts`

**Files:**
- Create: `src/validation.ts`
- Test: `src/__tests__/validation.test.ts`

**Interfaces:**
- Produces: `validateInstallDir`, `validateBaseUrl`, `validateModelId`, `validateApiKey`,
  `noLineBreak`, `boolLiteral`. Validators return a message **key** or `null`.

This task exists because the installer's own parser fails open on exactly these values.
Measured: a relative `install_dir` and `../../etc` are both accepted, and
`appliance=True|TRUE|1|yes` all silently mean **false**, because `install.sh:735` compares
`= "true"` literally. Under the artifact a relative install dir is worse than wrong —
makeself `cd`s into its extraction directory and `rm -rf`s it on exit, so the appliance
installs into a directory that disappears seconds later.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from 'vitest'
import { boolLiteral, validateInstallDir, validateModelId } from '../validation.js'

describe('validateInstallDir', () => {
  it('accepts an absolute path', () => expect(validateInstallDir('/opt/aura')).toBeNull())
  it.each(['aura', './aura', ''])('rejects %j', (v) =>
    expect(validateInstallDir(v)).toBe('validation.installDir.absolute'))
  it('rejects a traversal segment even when absolute', () =>
    expect(validateInstallDir('/opt/../etc')).toBe('validation.installDir.traversal'))
})

describe('boolLiteral', () => {
  // install.sh compares = "true" literally, so the wizard must emit exactly that and
  // never pass an operator's spelling through.
  it('emits only the two literals', () => {
    expect(boolLiteral(true)).toBe('true')
    expect(boolLiteral(false)).toBe('false')
  })
})

describe('validateModelId', () => {
  it('accepts a plain id', () => expect(validateModelId('qwen3:8b')).toBeNull())
  // A newline reaches set_env_value, which writes two .env lines; install.sh's reader
  // takes the first and docker compose takes the last, so the installer and the running
  // appliance would trust different values.
  it('rejects an embedded line break', () =>
    expect(validateModelId('a\nOPENROUTER_API_KEY=x')).toBe('validation.lineBreak'))
})
```

- [ ] **Step 2: Run it, watch it fail.**
- [ ] **Step 3: Implement**, adding every message key to BOTH catalogues.
- [ ] **Step 4: Run it, watch it pass.**
- [ ] **Step 5: Commit** — the message says what install.sh fails open on and why it matters.

---

### Task 3: `config-file.ts`

**Files:**
- Create: `src/config-file.ts`
- Test: `src/__tests__/config-file.test.ts`

**Interfaces:**
- Consumes: `validation.ts`.
- Produces: `writeInstallConfig(answers: Answers, dir: string): Promise<string>` returning
  the absolute path it wrote, and `renderInstallConfig(answers: Answers): string`.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from 'vitest'
import { renderInstallConfig } from '../config-file.js'

const answers = {
  installDir: '/opt/aura', appliance: true, gvisor: false,
  llmProvider: 'ollama' as const,
  llmBaseUrl: 'http://host.docker.internal:11434/v1',
  llmModel: 'any/model-the-operator-pulled:v1',
  openrouterApiKey: '', embedImage: 'ghcr.io/ggml-org/llama.cpp:server', embedNgl: '0',
}
const d = (s: string) => Buffer.from(s, 'base64').toString('utf8')
const lines = () => renderInstallConfig(answers).split('\n').filter(Boolean)
const value = (k: string) => lines().find((l) => l.startsWith(k + '='))!.slice(k.length + 1)

describe('renderInstallConfig', () => {
  it('puts format=1 on the first line', () => expect(lines()[0]).toBe('format=1'))

  // install.sh's parser exits 2 on any key it does not name. This asserts the exact set,
  // not merely that the ones we care about are present.
  it('emits exactly the nine keys install.sh accepts', () => {
    expect(lines().slice(1).map((l) => l.split('=')[0]).sort()).toEqual([
      'appliance', 'embed_image_base64', 'embed_ngl_base64', 'gvisor',
      'install_dir_base64', 'llm_base_url_base64', 'llm_model_base64',
      'llm_provider_base64', 'openrouter_api_key_base64',
    ])
  })

  // appliance and gvisor are the two the installer reads RAW. Base64-ing them would make
  // `= "true"` false and silently produce a non-appliance install.
  it('leaves appliance and gvisor unencoded', () => {
    expect(value('appliance')).toBe('true')
    expect(value('gvisor')).toBe('false')
  })

  it('round-trips every base64 value', () => {
    expect(d(value('install_dir_base64'))).toBe('/opt/aura')
    expect(d(value('llm_model_base64'))).toBe('any/model-the-operator-pulled:v1')
  })

  // An empty secret must be an empty value, not the base64 of an empty string plus padding.
  it('emits an empty key as an empty value', () =>
    expect(value('openrouter_api_key_base64')).toBe(''))

  // GNU base64 wraps at 76 columns; a wrapped value would break the key=value format.
  it('never wraps a long value', () => {
    const long = renderInstallConfig({ ...answers, openrouterApiKey: 'sk-' + 'a'.repeat(400) })
    expect(long.split('\n').every((l) => !l.startsWith(' '))).toBe(true)
    expect(long.split('\n').filter((l) => l.startsWith('openrouter_api_key_base64=')).length).toBe(1)
  })
})
```

- [ ] **Step 2: Run it, watch it fail.**
- [ ] **Step 3: Implement.** `writeInstallConfig` creates the directory `0700` and the file
  `0600`, and returns an absolute path. Refuse a relative `dir` rather than resolving it.
- [ ] **Step 4: Add a test that `writeInstallConfig` produces mode 0600 in a 0700 dir**
  (skip the mode assertion on win32 with an explicit reason in the test name — do not skip
  the rest).
- [ ] **Step 5: Run, watch pass, commit.**

---

### Task 4: `process.ts` — the injected runner

**Files:**
- Create: `src/process.ts`
- Test: `src/__tests__/process.test.ts`

**Interfaces:**
- Produces:
  ```ts
  export type RunResult = { code: number; stdout: string; stderr: string }
  export type Runner = (cmd: string, args: string[], opts?: RunOpts) => Promise<RunResult>
  export const realRunner: Runner
  export function fakeRunner(script: Array<{ match: RegExp; result: Partial<RunResult> }>): Runner & { calls: Array<[string, string[]]> }
  ```
- Every later task takes a `Runner` as a parameter and defaults to `realRunner`.

Read `D:/Wpt/wpt-iot/packages/create-wpt-iot/src/process.ts` (192 lines) and mirror it. The
one thing to preserve carefully: it never interpolates user input into a shell string.
Arguments go through the argv array, so a model id containing a space or a `;` cannot become
a second command.

- [ ] **Step 1: Write the failing test** — assert `fakeRunner` records calls in order, that
  an unmatched command throws rather than silently returning success, and that `realRunner`
  passes arguments as argv (spawn a `node -e` that prints `process.argv.slice(2)` and check a
  value containing a space and a semicolon arrives as ONE argument).
- [ ] **Step 2: Run, watch fail.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run, watch pass, commit.**

---

### Task 5: `modelroute.ts`

**Files:**
- Create: `src/modelroute.ts`
- Test: `src/__tests__/modelroute.test.ts`

**Interfaces:**
- Consumes: `Runner` from `process.ts`.
- Produces: `probeGpu(run: Runner): Promise<{ cuda: boolean; embedImage: string; embedNgl: string }>`
  and `probeOllama(run: Runner, url: string): Promise<{ reachable: boolean; models: string[] }>`.

Two measured constraints drive this and neither is optional:

**The endpoint must be probed from the network that will use it.** Aura runs in a container;
an Ollama on the host is not at `127.0.0.1` as seen from inside. Probe with a throwaway
`docker run --rm` on the compose network and record the URL **as the container sees it**.
This project has already paid for this once — "Hyper-V port forwarding lies: probe via docker
network, not 127.0.0.1". Without it the wizard produces installs that look successful and
cannot answer a single turn.

**List the models actually installed rather than asking for an id from memory.** `/api/tags`
returns them. A typo'd `AURA_LLM_MODEL` is indistinguishable from an absent model until the
first turn fails, which is long after the operator has walked away.

- [ ] **Step 1: Write the failing test** using `fakeRunner`:
  - a GPU present (`nvidia-smi` exits 0) yields the CUDA image and `embedNgl: '99'`;
    absent yields the CPU image and `'0'`
  - `probeOllama` returns the model list from a `/api/tags` body
  - **a host-only Ollama is reported unreachable**: the fake answers `127.0.0.1` from the
    host and refuses from inside the container, and the probe must report what the
    CONTAINER saw. Assert on the recorded call that `docker run` was used — a probe that
    passes by querying the host is passing for the wrong reason.
  - an unreachable endpoint returns `reachable: false` and does NOT throw
- [ ] **Step 2: Run, watch fail.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run, watch pass, commit.**

---

### Task 6: `prompts.ts` and `cli.ts`

**Files:**
- Create: `src/prompts.ts`, `src/cli.ts`
- Modify: `src/bin.ts` (call into `cli`)
- Test: `src/__tests__/prompts.test.ts`, `src/__tests__/cli.test.ts`

**Interfaces:**
- Consumes: `validation`, `config-file`, `modelroute`, `process`, `i18n`.
- Produces: `runCli(argv: string[], deps: Deps): Promise<number>` returning the exit code.
  `Deps` carries `{ run: Runner; prompt: PromptFns; cwd: string }` so the whole flow is
  driven in tests without a TTY.

The interview, in order: locale, install target (local or remote), install dir, appliance,
gVisor, model route (OpenRouter or Ollama — **not** llama.cpp, see the spec's "Why llama.cpp
is not in the wizard"), then route-specific questions, then the GPU probe.

- [ ] **Step 1: Write the failing `cli.test.ts`**, driving the whole flow with a scripted
  prompt double and `fakeRunner`. Assert:
  - the OpenRouter route asks for a key and the Ollama route does not
  - **the Ollama route emits an empty `openrouter_api_key_base64`** rather than omitting the
    key — the installer accepts it empty and rejects an unknown key set
  - `--help` exits 0 and `--nonsense` exits 2
  - a validation failure re-prompts rather than writing a config
- [ ] **Step 2: Run, watch fail.**
- [ ] **Step 3: Implement** `prompts.ts` then `cli.ts`, adding every string to both catalogues.
- [ ] **Step 4: Run, watch pass, commit.**

---

### Task 7: `local.ts`

**Files:**
- Create: `src/local.ts`
- Test: `src/__tests__/local.test.ts`

**Interfaces:**
- Consumes: `Runner`, the config path from `config-file`.
- Produces: `installLocally(run: Runner, artifact: string, configPath: string): Promise<number>`.

- [ ] **Step 1: Write the failing test.** The assertion that matters:

```ts
// makeself's own header parses argv before it execs the embedded script, so
// `artifact --config X` dies with "Unrecognized flag" and install.sh never runs.
// The separator is the whole contract of this function.
it('passes installer flags after --', async () => {
  const run = fakeRunner([{ match: /install-appliance/, result: { code: 0 } }])
  await installLocally(run, '/tmp/install-appliance.run', '/tmp/install.conf')
  expect(run.calls[0][1]).toEqual(['--', '--config', '/tmp/install.conf'])
})
```

Also assert a non-zero installer exit is propagated, not swallowed.

- [ ] **Step 2: Run, watch fail.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run, watch pass, commit.**

---

### Task 8: `remote.ts`

**Files:**
- Create: `src/remote.ts`
- Test: `src/__tests__/remote.test.ts`

**Interfaces:**
- Consumes: `Runner`.
- Produces: `installRemotely(run: Runner, target: RemoteTarget, artifact: string, configPath: string): Promise<number>`.

This is the mini-PC path: a clean Ubuntu server, reached over SSH. Read
`D:/Wpt/wpt-iot/packages/create-wpt-iot/src/remote.ts` (277 lines) and mirror its shape.

- [ ] **Step 1: Write the failing test.** Assert, with `fakeRunner`:
  - the artifact and the config are copied with `scp` before anything is executed
  - **the config lands `0600` on the remote and is removed afterwards** — it carries the API
    key, and leaving it in `/tmp` on a customer box is the kind of thing nobody notices
  - the remote command uses `-- --config`, same as local
  - a failed `scp` aborts before the install command runs, and its exit code propagates
  - **no user value is interpolated into a remote shell string** — drive a host or path
    containing a space and a `;` and assert it survives as one argument
- [ ] **Step 2: Run, watch fail.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run, watch pass, commit.**

---

### Task 9: Carry the artifact, and publish from the same commit as the images

**Files:**
- Modify: `packages/create-aura/package.json` (`files`, a `prepack` that builds the artifact)
- Create: `packages/create-aura/scripts/build-artifact.mjs`
- Modify: `.github/workflows/publish-aura-edge.yml`, `.github/workflows/ci.yml`
- Test: `packages/create-aura/src/__tests__/packaging.test.ts`

**Interfaces:**
- Consumes: `scripts/build_installer.sh` (which needs `makeself`).
- Produces: an npm tarball containing `dist/` and the `.run` artifact.

- [ ] **Step 1: Write the failing test**

```ts
// Decision 5: the package CARRIES the payload, so nothing is fetched at install time.
// A tarball without the artifact is a package that cannot install anything, and every
// other test here would still pass.
it('lists the artifact in package.json files', () => {
  const pkg = JSON.parse(readFileSync(resolve(__dirname, '../../package.json'), 'utf8'))
  expect(pkg.files).toContain('install-appliance.run')
})
```

- [ ] **Step 2: Run, watch fail. Implement `build-artifact.mjs`**, which shells out to
  `scripts/build_installer.sh` and writes `packages/create-aura/install-appliance.run`.
  It must FAIL when `makeself` is absent — no skip-as-green.

- [ ] **Step 3: Wire CI.** Add a `create-aura` job to `ci.yml` mirroring `web-test` and
  `web-mutation`: `npm ci && npm run lint && npm run typecheck && npm run test` with
  coverage >=85, and a mutation job at >=70. Install `makeself` in any job that builds the
  artifact.

- [ ] **Step 4: Wire the publish.** In `publish-aura-edge.yml`, after the images are pushed,
  build the package and `npm publish --provenance --access public`. Publishing from that
  workflow is what makes decision 6 true: the payload an operator installs and the images it
  pulls come from one commit. Publishing from anywhere else silently re-opens the drift this
  design exists to close.

- [ ] **Step 5: Verify the tarball.** `npm pack --dry-run` must list `dist/**` and the
  artifact, and must NOT list `src/**` or `__tests__`. Report the file list.

- [ ] **Step 6: Commit.**

---

## Deferred, with reasons

- **`install.conf` persistence** (`$INSTALL_DIR/install.conf`, 0600) is promised by the spec
  and written by nothing. It belongs to `install.sh`, not this package, and workstream (c)
  depends on it. It is its own task in `install.sh`'s next round, not here.
- **The `*:edge` guard** (`install.sh:472`): `AURA_INSTALL_REF=vX.Y.Z` disarms the image pins
  and a source-less host then tries to `docker build`. The wizard cannot cause it (no
  `channel` field, decision 6) but an operator's shell can. `install.sh` should refuse a
  non-`:edge` tag under the artifact — again `install.sh`'s task, not this one.
- **llama.cpp as a third route** — see the spec's "Why llama.cpp is not in the wizard".
  Enabling the profile without a ~6.7 GB fetch makes `docker compose up -d --wait` time out
  and, under `set -euo pipefail`, aborts the whole install.

## Self-Review

**Spec coverage.** (a)'s deliverable list is `bin`, `cli`, `prompts`, `modelroute`, `local`,
`remote`, `config-file`, `validation`, `process`, i18n en/it, and vitest suites — Tasks 1-8
cover each, Task 9 covers the npm publish that the (b) plan deferred here for lack of a
package to publish. `artifact.ts` and `installer-manifest.ts` are deliberately absent per
decision 5, and this plan says so where a reader would otherwise think they were forgotten.

**Placeholders.** None. Every step carries its code or its exact command.

**Type consistency.** `Runner` is defined in Task 4 and consumed by Tasks 5, 7, 8 under that
name. `Answers` is produced by Task 6 and consumed by Task 3's `renderInstallConfig`, so
Task 3 must define the type it accepts and Task 6 must satisfy it — the implementer of Task 6
reads Task 3's signature, not the other way round.

**One risk this plan does not remove.** Every test here drives a fake runner. Nothing in it
proves a real install works on a real box — that needs root, Docker and a clean host, and it
is the same gap the (b) plan closed only partially. The first real mini-PC install is the
acceptance test, and it should happen before this package is published, not after.
