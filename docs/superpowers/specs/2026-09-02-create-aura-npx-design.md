# create-aura — one-command install, verified payload, self-updating appliance

**Date:** 2026-09-02
**Status:** design approved, implementation not started
**Reference implementation:** `D:/Wpt/wpt-iot/packages/create-wpt-iot` (working, in production)

## The problem

Aura ships two install entry points that share one implementation — `curl | bash` and
`npx github:chetto1983/Aura`, both landing in `scripts/install.sh`. Sharing the
implementation is right and stays. What is wrong is everything around it.

**The payload is unverified.** `download_file` is:

```bash
curl -fsSL "${RAW_BASE}/${src}" -o "$dst"
```

`RAW_REF` defaults to `master` — a *moving* ref. Twenty-five files are fetched this way,
four are `chmod +x`'d and executed, and the whole flow runs as root. The single occurrence
of `sha256` in `install.sh` is a comment. An operator running `sudo npx github:...`
executes, as root, whatever raw.githubusercontent serves at that moment.

**The verifier is fetched the same way.** `npx github:owner/repo` clones the repo at a
moving ref, so even a manifest embedded in the veneer would be checked by code obtained
without a check.

**The model route is never offered.** Measured 2026-09-02: nothing forces an OpenRouter
key — `compose.yaml` interpolates `${OPENROUTER_API_KEY:-}`, `install.sh` writes it empty
when unset, `LoadServe` uses `llm.LoadAllowEmptyKey`, and `openai_compat/client.go:53`
applies the key *only* when `ReasoningTarget` resolves to OpenRouter. Aura already
supports a keyless local route end to end; `ReasoningTarget` already knows `ollama` and
`llamacpp`, and `internal/llm/{ollama,llamacpp}_caps.go` already probe them. The gap is
that the installer never *offers* or *wires* that route: the default provider is
`openrouter`, so an Ollama user must know to set three env vars by hand. The only
remaining hard gate on an empty key is `config.Load()`, used in exactly one place —
`cmd/aura/pack_install.go:132`.

**An installed appliance never gets a new payload.** `deploy/aura-image-update.sh` pulls
images (aura, migrate, garage-bootstrap, plus every running MCP sidecar) every five
minutes and is enabled automatically on `:edge` installs. It requires `compose.yaml` to
exist and never touches it. wpt-iot's equivalent is narrower still — `docker compose pull
backend frontend`. So a box installed in September never sees a service added in October.

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| 1 | One **self-contained generated** installer artifact | 25 hashes in a manifest; a separately downloaded tarball |
| 2 | Published to **npm with provenance** | staying on `npx github:` (verifier itself unverified) |
| 3 | Wizard offers **three model routes**: OpenRouter / Ollama / llama.cpp | cloud-only with an optional key; two-way auto-detect |
| 4 | **One logic, one artifact**: `install.sh` stays the hand-written source and gains `--config`; `curl \| bash` moves to the generated artifact | two paths with only npx verified |
| 5 | The **npm package carries the payload**; nothing is downloaded to install locally | wpt's download-and-verify from raw.githubusercontent |
| 6 | npm publish is **automated** in the workflow that publishes the edge images | manual publish per payload change |

Decision 5 is the one place this design deliberately departs from the reference. wpt
downloads and verifies because its veneer cannot carry its payload; ours can. Carrying it
removes `raw.githubusercontent.com` from the trust path entirely, and with it four files
that exist only to compensate for the download: `artifact.ts`, `installer-manifest.ts`,
`stamp-installer.mjs`, `verify-installer-manifest.mjs`. Decision 6 removes the cost that
would otherwise come with 5 — a package version pinning a payload version.

## Package layout

```
packages/create-aura/
  package.json          name: create-aura   bin: dist/bin.js
                        files: ["dist", "scripts/install-appliance.sh"]
                        publishConfig: { access: public, provenance: true }
  src/
    bin.ts              argv entry, delegates to cli.ts
    cli.ts              orchestration, dependency-injected (wpt's shape)
    prompts.ts          @inquirer/prompts — target, route, options
    modelroute.ts       NEW: OpenRouter / Ollama / llama.cpp, probe + model listing
    local.ts            preflight + install on this machine
    remote.ts           SSH probe, staging, install on the appliance
    config-file.ts      the 0600 config, base64 values
    validation.ts       host / port / username / absolute path
    process.ts          injectable command runner
    i18n.ts, messages/{en,it}.ts
  scripts/
    build-installer.mjs   generates the artifact
    install-appliance.sh  GENERATED, committed, CI-gated against staleness
  src/__tests__/          vitest
```

The root `package.json` (today `create-aura-appliance`, `bin` → `scripts/create-aura.mjs`)
loses its installer role; the veneer is superseded by this package.

`web/` already brings node, vitest and stryker, so this is a second package, not a second
ecosystem. The project's frontend quality gates (coverage ≥85%, mutation ≥70%) apply here
too.

**npm name:** `create-aura` must be verified free. Fallback: `create-aura-appliance`, the
name the root package already holds.

## The generated artifact

The generator does not rewrite `install.sh`. It **redefines one function underneath it**:

```
install-appliance.sh = #!/usr/bin/env bash
                       AURA_PAYLOAD_B64='<tar.gz of the 25 files, base64>'
                       <extract the blob into a tmpdir>
                       download_file() { cp "$PAYLOAD_DIR/$1" "$2"; }
                       <install.sh verbatim>
```

The 25 `download_file src dst` call sites are untouched; only what the function does
changes. `install.sh` stays readable, single-sourced, and still works standalone from a
repo checkout. The generator is ~20 lines.

Payload measured 2026-09-02: **160,603 bytes across 25 files**, of which `compose.yaml` is
82,877. Gzipped and base64'd it is roughly 40 KB inside the script.

**Determinism is load-bearing.** Without `--sort=name`, a fixed `--mtime`, `--owner=0
--group=0 --numeric-owner` and `gzip -n`, two generations of identical input differ
byte-for-byte, the freshness gate flaps, and within a fortnight someone disables it. With
them the artifact is a pure function of its inputs, and the CI gate is one honest line:
regenerate, then `git diff --exit-code` on the artifact. That gate is what closes the
hazard a committed generated file introduces — someone edits `compose.yaml`, forgets to
regenerate, and the installer ships a stale compose.

`curl | bash` points at the generated artifact on raw. That path remains unverifiable by
construction, exactly as today; the difference is that it is now **one** file instead of
26, so its hash can be checked by hand.

## Config contract and model route

The wizard does **not** write `.env`. `install.sh` already owns it through
`write_env_if_missing`, `ensure_env_default` and `set_env_value`, which are idempotent and
preserve explicit operator choices. Duplicating that in TypeScript would be two
implementations diverging at the first edge case. The wizard produces a config;
`install.sh` applies it.

```
format=1
install_dir_base64=...
channel_base64=...              edge | vX.Y.Z
appliance=true|false
gvisor=true|false
llm_provider_base64=...         openrouter | ollama | llamacpp
llm_base_url_base64=...
llm_model_base64=...
openrouter_api_key_base64=...   empty on a local route
embed_image_base64=...          CUDA or CPU, from the GPU probe
embed_ngl_base64=...            99 or 0
```

Mode `0600` in a `0700` directory, base64 values, `--config` taking a validated **absolute
path**, accepted once. Secrets never travel through argv, so they reach neither the
process table nor shell history.

The config is **persisted to `$INSTALL_DIR/install.conf` (0600)**. Today it would not
survive the install; the auto-update and any manual re-run both need it.

**The endpoint is probed from the network that will use it.** Aura runs in a container; an
Ollama on the host is not at `127.0.0.1` seen from inside. The wizard therefore does not
merely ask for a URL — it runs a throwaway `docker run --rm` on the compose network,
probes from there, and writes the URL **as the container sees it**. This is the trap the
project has already paid for once ("Hyper-V port forwarding lies — probe via docker
network, not 127.0.0.1"); without this step the wizard produces installs that look
successful and cannot answer.

Once the endpoint answers, the wizard **lists the models actually installed** (`/api/tags`
on Ollama, `/v1/models` on llama.cpp) instead of asking the operator to type an id from
memory — LibreChat expresses the same idea as `models.fetch: true`. A typo'd
`AURA_LLM_MODEL` is indistinguishable from an absent model until the first turn fails.

Choosing `llamacpp` selects the existing `localllm` compose profile: no URL to ask, Aura
starts the sidecar itself; the wizard writes provider, the internal base URL, and the
model.

## Remote install

Staging is taken from wpt unchanged, because it is already right: UUID-named files in
`/tmp`, installer `700` and config `600`, `trap` on EXIT/HUP/INT/TERM, a runner that
deletes itself, and a sweep of leftovers older than a day. The installer **travels over
SSH from the copy inside the npm package** — the appliance downloads nothing to obtain it.

The probe is rewritten on Aura's numbers. Targets are clean Ubuntu Server, so wpt's
assumptions (`apt-get`, `systemctl`, `sudo`, `curl`) hold and disk is a sanity check, not a
computed floor.

Two additions wpt does not need:

**GPU detection.** `.env` ships `llama.cpp:server-cuda` with `AURA_EMBED_NGL=99`. Measured
2026-09-02: that image is **6.99 GB**. On a mini-PC with no NVIDIA it is 7 GB pulled for
nothing and the sidecar cannot offload anyway. The probe looks for a GPU on the target and
the config carries `AURA_EMBED_IMAGE` and `AURA_EMBED_NGL` accordingly.

**Different egress.** wpt probes ghcr.io, raw.githubusercontent.com and get.docker.com.
`raw.githubusercontent.com` is no longer needed — the installer arrives over SSH.
`huggingface.co` is, because `ensure_embed_model` fetches 333,590,944 bytes of GGUF from
there. On a WiFi-only appliance that is the fetch that fails first, and it must surface in
the probe rather than half way through an install.

The rest of the probe stays wpt's: Linux, required commands present, architecture in the
allowlist, existing install detected.

## Payload auto-update

No second updater. `install.sh` is already idempotent and says so: *"Re-running preserves
secrets and explicit settings; known-safe deployment defaults may be migrated."* Updating
the payload is re-running the newer artifact with the stored config.

```
timer (5 min, unchanged)
  └─ compare published artifact sha256 against the installed one
     ├─ same    → exit (the normal case, one HEAD)
     └─ differs ↓
        back up compose.yaml + .env
        run the new artifact with --config $INSTALL_DIR/install.conf
        docker compose config -q          ← the real gate
        up -d --wait
        health does not return → restore the backup, up -d, and say so loudly
```

`docker compose config -q` resolves the tension this feature carries: it fails **before
anything is applied** when a new compose requires a variable the installed `.env` lacks —
which is precisely how that failure would otherwise appear at three in the morning on a
machine in someone else's house. Migrating new env defaults is already
`ensure_env_default`'s job, preserving explicit operator choices.

The artifact the timer compares against is the one on raw at the installed channel's ref,
the same file `curl | bash` fetches.

**Be precise about what that hash buys.** Comparing the published artifact against the
installed one detects that the payload *changed*; it does not establish where it came
from. The appliance's update path trusts raw.githubusercontent exactly as `curl | bash`
does, and this design does not fix that — npm's provenance covers the operator running
`npx`, not a box pulling its own updates. What the comparison does earn is real but
narrower: the timer does nothing at all in the normal case, so an unchanged upstream costs
one HEAD and never re-runs a root install. Closing the authenticity half needs a signature
the appliance can verify offline, and that is deliberately left for later rather than
implied by a hash check.

## Testing

- vitest per unit, mirroring wpt's nine suites: cli, prompts, local, remote, config-file,
  validation, process, i18n, plus `modelroute` which is ours.
- The command runner is injected, so local and remote installs are tested without a box.
- A generator test asserts determinism: generate twice, compare bytes.
- A round-trip test asserts the generated artifact's payload extracts to files identical to
  the repo's 25.
- CI: the staleness gate (regenerate + `git diff --exit-code`), the package's own tests,
  and the coverage/mutation thresholds the frontend already meets.

## What this design does NOT do

- It does not change any Go code. The measurement above showed the keyless local route
  already works end to end; the one remaining fail-fast (`config.Load()` in
  `pack_install.go`) is out of scope and untouched.
- It does not make `curl | bash` verifiable, and it does not make the appliance's own
  auto-update verifiable either. Both fetch the artifact from raw and can detect change
  but not origin. They only get smaller — one file instead of 26. npm provenance covers
  the operator who runs `npx`, nobody else.
- It does not prove the payload auto-update is safe on a live appliance. `docker compose
  config -q` catches a missing variable; it does not catch a compose change that is valid
  and still wrong for that box. The rollback exists because that residue is real.
- It does not size the appliance disk requirement. Targets are clean Ubuntu Server, so the
  question was set aside rather than answered; the 27.43 GB of images and 16.4 GB of
  volumes measured on this dev host are an upper bound on a machine that has pulled
  everything, not a floor for an appliance.
- It does not address the first-run MCP remount defect fixed separately in `0be6d243e`;
  an appliance installed before that fix reaches an edge image still needs one restart
  after the wizard.

## Open items

- Verify `create-aura` is free on npm; fall back to `create-aura-appliance`.
- Choose where the npm publish credential lives (OIDC trusted publishing vs a token).
