# create-aura — one-command install, self-extracting payload, self-updating appliance

**Date:** 2026-09-02
**Status:** design approved after adversarial review; implementation not started
**Reference implementation:** `D:/Wpt/wpt-iot/packages/create-wpt-iot` (working, in production)
**Revision:** v2. v1 was reviewed adversarially and had a fatal flaw in its central
mechanism plus four factual errors. Both are recorded below rather than quietly fixed.

## The problem

Aura ships two install entry points sharing one implementation — `curl | bash` and
`npx github:chetto1983/Aura`, both landing in `scripts/install.sh`. Sharing the
implementation is right and stays. What is around it is not.

**The payload is unpinned.** `install.sh` fetches 25 files from
`https://raw.githubusercontent.com/chetto1983/Aura/${AURA_INSTALL_REF:-master}` while
pulling images tagged `:edge`. Those are two channels that move independently, so the
`compose.yaml` an operator installs need not match the image it pulls. This is exactly what
Immich warns about: use the compose of the *release*, not of `main`, because main's may be
incompatible with the release. It bites without an attacker, and it bites first.

**The payload is unverified.** `download_file` (`install.sh:205`) is a bare
`curl -fsSL "${RAW_BASE}/${src}" -o "$dst"`. Twenty-five files arrive that way and the flow
runs as root. There is no integrity check anywhere in the script — `grep -ci sha256
scripts/install.sh` returns **0**.

**The verifier would arrive the same way.** `npx github:owner/repo` clones at a moving ref,
so a manifest embedded in the veneer would be checked by code fetched without a check.

**The model route is never offered.** Measured 2026-09-02: nothing forces an OpenRouter key
— `compose.yaml:125,843` interpolate `${OPENROUTER_API_KEY:-}`, `install.sh` writes it
empty when unset, `LoadServe` (`internal/config/config.go:342`) uses `llm.LoadAllowEmptyKey`,
and `openai_compat/client.go:53` applies the key *only* when `ReasoningTarget` resolves to
OpenRouter. `ReasoningTarget` (`internal/llm/reasoning_target.go:54`) already matches
`ollama` and `llamacpp` explicitly, and `internal/llm/{ollama,llamacpp}_caps.go` already
probe them. The gap is that the installer never offers or wires the route: the default
provider is `openrouter`, so an Ollama operator must set three env vars by hand.

**An installed appliance never gets a new payload.** `deploy/aura-image-update.sh` pulls
images (aura, aura-migrate, garage-bootstrap, then every *running* MCP sidecar) on a
5-minute timer and is enabled automatically on `:edge` installs. It requires `compose.yaml`
to exist (line 33) and never writes to it. wpt-iot's equivalent is narrower still —
`docker compose pull backend frontend`. A box installed in September never sees a service
added in October.

## Decisions

| # | Decision | Rejected alternative |
|---|---|---|
| 1 | The artifact is a **`makeself` self-extracting archive**, not a hand-rolled generator | hand-written `build-installer.mjs`; a manifest of 25 hashes; a separately downloaded tarball |
| 2 | Published to **npm with provenance** | staying on `npx github:` (verifier itself unverified) |
| 3 | Wizard offers **two model routes**: OpenRouter and Ollama. llama.cpp stays **manual** | a third `llamacpp` route (see "Why llama.cpp is not in the wizard") |
| 4 | **One logic, one artifact**: `install.sh` stays the hand-written source and learns `--config` and a payload-aware `download_file`; `curl \| bash` moves to the artifact | two paths with only npx verified |
| 5 | The **npm package carries the payload**; nothing is downloaded to install | wpt's download-and-verify from raw.githubusercontent |
| 6 | **No pinning anywhere — always latest.** The npm publish rides `publish-aura-edge.yml`, so payload and images ship from the same commit | a `channel` choice (edge vs vX.Y.Z) in the wizard; Immich's release-asset pinning |

Decision 5 is the one deliberate departure from the reference. wpt downloads and verifies
because its veneer cannot carry its payload; ours can. Carrying it removes
raw.githubusercontent from the install trust path and with it four files that exist only to
compensate for the download: `artifact.ts`, `installer-manifest.ts`, `stamp-installer.mjs`,
`verify-installer-manifest.mjs`.

Decision 6 is what makes 5 safe without pinning. `publish-aura-edge.yml` runs on **every
master push** and tags both `:edge` and an immutable `master-<sha>`. Publishing the npm
package from that same workflow means the payload an operator installs and the images it
pulls come from one commit. Drift is closed by making the two travel together, not by
freezing either.

## The artifact: makeself, not a bespoke generator

**v1 of this spec was wrong here, fatally.** It proposed emitting

```
AURA_PAYLOAD_B64='...'
download_file() { cp "$PAYLOAD_DIR/$1" "$2"; }
<install.sh verbatim>
```

and claimed the generator "does not rewrite install.sh". Those two are incompatible:
`install.sh:205` defines `download_file` itself, the first call site is `install.sh:624`,
and in bash the last definition executed wins. The verbatim include would silently clobber
the override and all 25 calls would run the original curl against the moving ref — the
precise failure the design exists to remove. Found by adversarial review, verified by hand.

`makeself` is the standard tool for this and removes the problem structurally:

- it produces a shell stub + compressed tar, one file, executable;
- it **embeds checksums by default** (CRC + MD5; `--sha256` adds SHA256) and validates them
  on extraction, so integrity self-validation is the tool's job, not ours;
- `--base64` is built in;
- the startup script runs **with its working directory set to the extracted files**.

That last property is the fix. `install.sh` starts life already sitting next to its 25
files. The only change it needs is one function that prefers them:

```bash
download_file() {
  if [ -n "${AURA_PAYLOAD_DIR:-}" ]; then cp "$AURA_PAYLOAD_DIR/$1" "$2"; return; fi
  curl -fsSL "${RAW_BASE}/$1" -o "$2" || { ... }
}
```

`install.sh` captures its startup directory into `AURA_PAYLOAD_DIR` before its own
`cd "$INSTALL_DIR"`, and stays fully usable standalone from a repo checkout, where the
variable is unset and the curl branch runs. Single source, no code generation, no clobber.

Payload measured 2026-09-02: **160,603 bytes across 25 files**, `compose.yaml` alone
**82,877**.

**The staleness gate moves to the inputs.** v1 proposed regenerating the artifact in CI and
running `git diff --exit-code` on it. makeself documents no reproducible-output guarantee,
so that gate would flap. The gate instead hashes the 25 **input files** and compares against
a committed manifest; the artifact is a build output, not a committed blob. This also
sidesteps mode-bit and tar-implementation drift between a WSL-generated and a CI-generated
archive.

`curl | bash` points at the published artifact. That path still cannot verify its own
origin — see "What this design does NOT do".

## Config contract and model route

The wizard does **not** write `.env`. `install.sh` owns it through `write_env_if_missing`
(`:456`), `ensure_env_default` (`:322`) and `set_env_value` (`:308`), which are idempotent
and preserve explicit operator choices. Duplicating that in TypeScript would be two
implementations diverging at the first edge case. The wizard produces a config;
`install.sh` applies it.

```
format=1
install_dir_base64=...
appliance=true|false
gvisor=true|false
llm_provider_base64=...         openrouter | ollama
llm_base_url_base64=...
llm_model_base64=...
openrouter_api_key_base64=...   empty on the Ollama route
embed_image_base64=...          CUDA or CPU, from the GPU probe
embed_ngl_base64=...            99 or 0
```

Mode `0600` in a `0700` directory, base64 values, `--config` taking a validated **absolute
path**, accepted once. Secrets never travel through argv, so they reach neither the process
table nor shell history. There is no `channel` field: decision 6 removed it.

The config is **persisted to `$INSTALL_DIR/install.conf` (0600)**. It does not survive the
install today, and both the auto-update and any manual re-run need it.

**The endpoint is probed from the network that will use it.** Aura runs in a container; an
Ollama on the host is not at `127.0.0.1` seen from inside. The wizard runs a throwaway
`docker run --rm` on the compose network, probes from there, and writes the URL **as the
container sees it**. The project has already paid for this once ("Hyper-V port forwarding
lies — probe via docker network, not 127.0.0.1"); without it the wizard produces installs
that look successful and cannot answer.

Once the endpoint answers, the wizard **lists the models actually installed** (`/api/tags`)
rather than asking for an id from memory — LibreChat expresses the same idea as
`models.fetch: true`. A typo'd `AURA_LLM_MODEL` is indistinguishable from an absent model
until the first turn fails.

### Why llama.cpp is not in the wizard

`compose.yaml:1149-1258` defines `aura-llm` under `profiles: [localllm]`. It needs two GGUF
files in the named volume `aura-llm` — a ~6.7 GB model plus an MTP drafter — and has a
healthcheck. `scripts/fetch_llm_model.sh` exists but `install.sh` references it **zero**
times (`grep -cE 'fetch_llm_model|aura-llm|localllm' scripts/install.sh` → 0); it is called
only from the `Makefile` and its own test. A wizard that enabled the profile would leave the
volume empty, the healthcheck would never pass, `docker compose up -d --wait
--wait-timeout 300` (`install.sh:670`) would time out, and under `set -euo pipefail` the
**whole install would abort**.

Wiring it means an `ensure_llm_model` twin of `ensure_embed_model` (`install.sh:265`) that
fetches ~6.7 GB to the appliance. That is real work with its own bandwidth story on a
WiFi-only box, and it is deliberately deferred: llama.cpp stays a manual route the operator
wires by editing `.env` and enabling the profile.

## Remote install

Staging is taken from wpt unchanged, because it is already right: UUID-named files in
`/tmp`, installer `700` and config `600`, `trap` on EXIT/HUP/INT/TERM, a runner that deletes
itself, a sweep of leftovers older than a day. The installer **travels over SSH from the
copy inside the npm package** — the appliance downloads nothing to obtain it.

Targets are clean Ubuntu Server, so wpt's probe assumptions (`apt-get`, `systemctl`, `sudo`,
`curl`) hold and disk is a sanity check, not a computed floor.

Two additions wpt does not need:

**GPU detection.** `.env` ships `llama.cpp:server-cuda` with `AURA_EMBED_NGL=99`. Measured
2026-09-02: that image is **6.99 GB**. On a mini-PC with no NVIDIA it is 7 GB pulled for
nothing and the sidecar cannot offload anyway. The probe looks for a GPU and the config
carries `AURA_EMBED_IMAGE` and `AURA_EMBED_NGL` accordingly.

**Different egress.** `raw.githubusercontent.com` is no longer needed for the payload.
`huggingface.co` is, because `ensure_embed_model` fetches 333,590,944 bytes of GGUF. On a
WiFi-only appliance that is the fetch that fails first and it must surface in the probe.
`ghcr.io` and `get.docker.com` remain, the latter because `install.sh:174-181` pipes it to
`sh` as root when Docker is absent.

**Concurrency.** `deploy/aura-image-update.sh:26-30` uses `flock`; `install.sh` has no
equivalent. Nothing today stops two installs, or an operator re-run racing the auto-update's
re-run, from writing `.env`/`compose.yaml` and calling `docker compose up` concurrently.
The installer takes a lock on `$INSTALL_DIR`.

## Payload auto-update — a `deploy/` deliverable

This is **not** part of the npm package. It lands in `deploy/aura-image-update.sh` (or a
sibling unit) and is Bash/ops work with its own review. v1 described the mechanism but
listed only TypeScript files, which is how a scope gap hides.

No second updater: `install.sh` is already idempotent and says so — *"Re-running preserves
secrets and explicit settings; known-safe deployment defaults may be migrated."* Updating
the payload is re-running the newer artifact with the stored config.

```
timer (5 min, unchanged)
  └─ compare the published artifact against the installed one
     ├─ same    → exit (the normal case, one HEAD)
     └─ differs ↓
        take the flock
        back up compose.yaml + .env
        run the new artifact with --config $INSTALL_DIR/install.conf
        docker compose config -q          ← the gate
        up -d --wait
        health does not return → restore the backup, up -d, and say so loudly
```

`docker compose config -q` fails **before anything is applied** when a new compose needs a
variable the installed `.env` lacks — which is how that failure would otherwise appear at
three in the morning on a machine in someone else's house. Migrating new env defaults is
already `ensure_env_default`'s job.

## Deliverables

Four separable workstreams. They are listed in dependency order and should not be bundled
into one plan; (b) gates the rest.

- **(a) `packages/create-aura`** — the TypeScript package: `bin`, `cli`, `prompts`,
  `modelroute`, `local`, `remote`, `config-file`, `validation`, `process`, i18n en/it,
  vitest suites mirroring wpt's nine plus `modelroute`.
- **(b) The artifact** — makeself packaging, `install.sh`'s payload-aware `download_file`
  and `--config` intake, the input-hash staleness gate, the npm publish wired into
  `publish-aura-edge.yml`. **Prove this before building (a) on it**: v1's mechanism looked
  obviously correct and was not.
- **(c) Payload auto-update** — `deploy/`, root, unattended, on customer boxes. Deserves its
  own threat model.
- **(d) `config.Load()`** — `cmd/aura/pack_install.go:132` does `cfg, err := config.Load();
  if err != nil { return nil }`. The gate at `internal/llm/config.go:351` is unconditional
  on provider, and the error is **swallowed**. So on any keyless local-route deployment —
  precisely the audience decision 3 exists to serve — `packSkillInstaller` returns nil and
  skill installation is silently and permanently disabled. v1 called this a narrow
  out-of-scope wrinkle; it is a standing feature loss and it is now a work item.

## Testing

- vitest per unit; the command runner is injected, so local and remote installs are tested
  without a box.
- A round-trip test: the artifact's payload extracts to files byte-identical to the repo's 25.
- The input-hash staleness gate in CI.
- Coverage ≥85% and mutation ≥70%, matching the frontend gates `web/` already meets.

## What this design does NOT do

- It does not make `curl | bash` verifiable, and it does not make the appliance's own
  auto-update verifiable either. Both fetch the artifact and can detect **change, not
  origin**. makeself's embedded checksums prove the archive is intact, not that it is ours.
  npm provenance covers the operator who runs `npx` and nobody else. Closing that half needs
  a signature the appliance can verify offline, and is deliberately left for later.
- It does not remove every unverified root-executed fetch. `install.sh:174-181` pipes
  get.docker.com to `sh` as root; `:554-559` pipes a gVisor GPG key to `gpg --dearmor` as
  root under `--gvisor`; `:664-667` pulls the Aura image by moving tag with no digest pin.
  None are in the 25-file payload and none are fixed by carrying it.
- It does not defend against a **downgrade**. The auto-update triggers on "published differs
  from installed", which is symmetric: it cannot tell a forward update from a channel
  pointed at an older artifact, and `docker compose config -q` cannot catch an older `aura`
  meeting a schema its migrations already moved forward.
- It does not wire llama.cpp. See above; that route stays manual.
- It does not size the appliance disk requirement. Targets are clean Ubuntu Server. The
  27.43 GB of images and 16.4 GB of volumes measured on this dev host are an upper bound for
  a machine that has pulled everything, not a floor for an appliance.
- It does not address the first-run MCP remount defect fixed in `0be6d243e`; an appliance
  installed before that fix reaches an edge image still needs one restart after the wizard.

## Corrections to v1

Recorded rather than silently fixed, because v1's two central claims were written as
measured fact without being exercised end to end — the failure this project's PRD-first
principle exists to prevent.

1. **Fatal:** the `download_file` override was clobbered by install.sh's own definition.
   Replaced by makeself plus a payload-aware `download_file` in install.sh itself.
2. **Wrong fact:** "the single occurrence of `sha256` in install.sh is a comment" — there
   are **zero** occurrences; the earlier grep matched `checksum`.
3. **Overstated:** "four are chmod +x'd and executed". Two run during the install
   (`fetch_embedding_model.sh`, `observability_sidecar_check.sh`); `aura-image-update.sh` is
   staged for systemd; `garage_bootstrap.sh` is downloaded and never invoked — the live
   entrypoint is a copy baked into the image.
4. **Imprecise:** `config.Load()` has one *production* call site; there are seven including
   tests.
5. **Scope gap:** the payload auto-update had no deliverable in the package layout.
6. **Understated:** `config.Load()`'s effect is a silent permanent feature loss, not a
   narrow fail-fast.

## Open items

- Verify `create-aura` is free on npm; fall back to `create-aura-appliance`.
- Choose where the npm publish credential lives (OIDC trusted publishing vs a token).
