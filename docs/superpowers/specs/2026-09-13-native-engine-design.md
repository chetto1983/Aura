# Aura on a native Docker Engine

Date: 2026-09-13. Status: approved section by section in brainstorming. Awaiting review of this
text. This is sub-project A of the sandbox boundary work; sub-project B (every box under gVisor)
depends on it and gets its own spec.

## Why

Measured on this PC (Windows 11, WSL 2.7.12, kernel 6.18.33.2, Ubuntu 26.04 LTS with systemd) on
2026-09-13:

- **Boxes must run under gVisor, and Docker Desktop cannot run gVisor.** The per-identity box is
  to keep full capability inside (root, `apt install`, default capabilities) and get its boundary
  from a separate kernel, the model of Claude Code cloud and Docker Sandboxes. A throwaway spike
  ran the aura-sandbox rootfs (`ghcr.io/chetto1983/aura-sandbox:edge`) under runc 1.4.3 and runsc
  release-20260817.0, same workloads, Docker default capabilities:

  | Workload | runc | runsc |
  |---|---|---|
  | `python3 -m pip install` | 1.3 s | 3.2 s |
  | `npm install` express + lodash | 2.9 s | 5.9 s |
  | Playwright Chromium screenshot | 0.7 s | 1.7 s |
  | LibreOffice docx→pdf | 1.3 s | 3.3 s |
  | pandas 2M×4 describe | 1.3 s | 2.5 s |
  | pnpm build of the artifact template | 3.7 s | 7.7 s |
  | tar czf / xzf of /usr/lib/python3 | 0.6 s | 0.9 s |
  | `apt-get install cowsay` | 3.4 s | 6.6 s |
  | total | 15.6 s | 32.7 s |

  Every workload passed under runsc (`uname` → `4.19.0-gvisor`). Docker Desktop has no supported
  way to register an extra runtime ([docker/desktop-feedback#364](https://github.com/docker/desktop-feedback/issues/364),
  open); gVisor documents Docker Engine only ([install guide](https://gvisor.dev/docs/user_guide/install/)).
  The spike ran with `--network=host` and no Docker, so it does not measure gVisor's netstack,
  the egress sidecar, memory per box or several boxes at once.
- **The stack runs on Docker Desktop's engine.** Docker Desktop 29.7.2 (runtimes `runc`,
  `nvidia`). `/opt/aura` lives on the Ubuntu distro disk, `aura.service` is enabled there, and
  `docker` in the distro is Docker Desktop's (`/usr/bin/docker → /mnt/wsl/docker-desktop/cli-tools/usr/bin/docker`).
- **Embeddings run on the CPU while the PC has an RTX 3060.** The stack uses
  `compose.yaml:compose.cpu.yaml`; the sidecar logs `no usable GPU found`. Same model file
  (`embeddinggemma-300M-Q8_0.gguf`), same arguments, the live CPU sidecar against a throwaway
  `llama.cpp:server-cuda` one:

  | Request | CPU (today) | RTX 3060 |
  |---|---|---|
  | query, 15 tokens | 22 ms | 7 ms |
  | chunk, 1,927 tokens | 2,364 ms | 94 ms |
  | 8 such chunks in one request | 19.3 s | 0.71 s |

  Cosine between the CPU and GPU vectors: 0.999574 (query), 0.999804 (chunk); 768 dimensions;
  about 1.2 GB of VRAM. Cause: `detect_embed_backend` picks CUDA only when an NVIDIA container
  hook is on the distro's PATH (`scripts/install.sh:546-560`), and none is; the `nvidia` runtime
  lives inside Docker Desktop. Once written, `AURA_EMBED_BACKEND` is never detected again
  (`scripts/install.sh:568`).
- **A WSL distro does not stay up on systemd alone.** A throwaway distro with `systemd=true` and a
  service appending a timestamp every 5 s: the last `wsl.exe` session closed at +21 s, the service
  wrote its last line at +36 s, and the distro stayed down through +335 s. `vmIdleTimeout`
  defaults to 60 s ([wsl-config](https://learn.microsoft.com/en-us/windows/wsl/wsl-config)).
  Docker Desktop keeps WSL up today; without it something must hold a session.
- **The installer already covers part of this.** `install_docker` installs Docker Engine through
  `get.docker.com` when `docker` is absent (`scripts/install.sh:201-236`), and Docker Engine
  supports Ubuntu 26.04 LTS ([install on Ubuntu](https://docs.docker.com/engine/install/ubuntu/)).
  runsc is provisioned only under `--gvisor` (`scripts/install.sh:778-800`), with
  `runsc install || true`, and `--gvisor` puts the `aura` container under runsc through
  `AURA_RUNTIME` (`compose.yaml:34`), not the boxes (`internal/sandbox/usersandbox/router.go:106`).
- **The data is small and bound to `.env`.** Named volumes with data: `aura_aura-postgres`
  72 MB, `aura_aura-arcadedb` 22 MB, `aura_aura-arcadedb-backups` 1.6 MB, `aura_garage-data`
  4.9 MB, `aura_aura-whatsapp-session` 205 kB, `aura_aura-pim-data` 22 kB, `aura_caddy-data`
  5 kB, `aura_aura-home` 2.4 MB, `aura_aura-workspace`, `aura_aura-ingest-state` 447 kB,
  `aura_aura-grafana` 1.3 MB, `aura_aura-prometheus` 21 MB, `aura_aura-tempo` 258 MB,
  `aura_aura-llama-embed` 334 MB, `aura_aura-ocr-vl` 514 MB, and one box workspace
  `aura-box-214f9d28-a020-4627-8611-4ee85ab4e1dc` 295 MB. 130 of 136 anonymous volumes are
  unlinked. `/opt/aura/.env` holds `POSTGRES_PASSWORD`, `AURA_AUTHULA_SECRET` (the key the
  settings secrets are sealed with) and `AURA_ARCADEDB_TENANT_SECRET` (every tenant's ArcadeDB
  credential): restored volumes are readable only with that same file.

## Decisions

1. The engine is Docker Engine inside the `Ubuntu` WSL distro, installed by the installer.
   Docker Desktop is uninstalled.
2. The installer provisions runsc on every native Linux Docker install and proves it with the
   sandbox image. `--gvisor` keeps its one meaning: the `aura` container under runsc.
3. The installer installs the NVIDIA Container Toolkit when it finds a usable NVIDIA GPU and no
   hook, so detection answers `cuda` wherever the GPU is real.
4. The stack's data is carried across by volume, with a per-file checksum proof.
5. A Windows scheduled task started at boot, with no one logged on, holds the distro up.

## Installer

In `scripts/install.sh`:

- `provision_gvisor` becomes `provision_runsc` and runs on every install where
  `native_linux_docker` holds, not only under `--gvisor`. The apt repository stays as today.
  `runsc install` loses `|| true`. After the sandbox image is available (the D-04 block),
  `docker run --rm --runtime=runsc --entrypoint true "$sandbox_image"` must succeed or the install
  fails. On Docker Desktop and macOS it prints a WARN naming docker/desktop-feedback#364 and
  continues; refusing those hosts belongs to sub-project B, when boxes start requiring runsc. A
  native Linux host without dpkg fails, as `--gvisor` does today.
- A new `provision_nvidia_toolkit` runs before `ensure_embed_backend_env` when `nvidia-smi`
  succeeds and neither `nvidia-container-runtime-hook` nor `nvidia-cdi-hook` is on PATH, on native
  Linux Docker with dpkg. It follows NVIDIA's apt procedure
  ([install guide](https://github.com/NVIDIA/cloud-native-docs/blob/main/container-toolkit/install-guide.md)):
  the `libnvidia-container` keyring and list, `nvidia-container-toolkit`,
  `nvidia-ctk runtime configure --runtime=docker`, `systemctl restart docker`. Without dpkg it
  prints a WARN and the detection falls to Vulkan or CPU as today. `runsc install` and
  `nvidia-ctk runtime configure` both edit `/etc/docker/daemon.json`; after both,
  `docker info` must list `runsc` and `nvidia`.
- `detect_embed_backend` is unchanged.
- `--gvisor` help text and the `create-aura` question (`packages/create-aura/src/messages/{it,en}.ts`)
  say that it puts the Aura daemon container under gVisor.

Tests: `scripts/install_lib_test.sh` gains stubbed-command cases for `provision_runsc` (skipped
without native Linux Docker, runs otherwise, fails when the proof run fails) and for
`provision_nvidia_toolkit` (runs only with a working `nvidia-smi` and no hook, never without
dpkg). `cmd/aura/container_artifacts_test.go` follows the renamed function. Release:
`create-aura-appliance` 0.3.0, tag `installer-v0.3.0`, CI publishes to npm.

## Migration on this PC

Every step stops the migration if its check fails; nothing is uninstalled before step 2 passes.

1. **Back up.** `docker compose stop` in `/opt/aura`. Into `D:\Backups\aura-2026-09-13\`: one
   `tar -czpf` per carried volume, a sha256 manifest of every file in each volume, `pg_dump -Fc`
   of `aura` taken before the stop, and a copy of `/opt/aura/.env`. Carried: every named volume
   listed under Why. Not carried: `aura-uv-cache`, `aura-npm-cache`, `aura-pip-cache` (sub-project
   B removes shared caches), the `*-cache-host` build caches, `aura_aura-llm` (empty), and the
   anonymous volumes.
2. **Verify.** Every archive reads end to end and its file count matches its manifest;
   `pg_restore -l` reads the dump.
3. **Remove Docker Desktop.** `winget uninstall Docker.DockerDesktop` (the operator accepts the
   UAC prompt), then `wsl --shutdown`. `wsl -l -v` no longer lists `docker-desktop`, and `docker`
   no longer resolves in the distro.
4. **Install.** Remove `AURA_EMBED_BACKEND`, `COMPOSE_FILE`, `AURA_EMBED_IMAGE` and `AURA_EMBED_NGL`
   from `.env` so detection runs again. Claude runs `npx create-aura-appliance` in local mode.
   It installs Docker Engine, runsc and the NVIDIA toolkit, keeps every other `.env` value,
   detects `cuda`, pulls the images and brings the stack up on empty volumes.
5. **Restore.** `docker compose down` (volumes kept). `docker volume create` the box volume.
   Empty each carried volume and extract its archive into it. `docker compose up -d --wait`.
6. **Prove.** Recompute every manifest on the restored volumes; each must equal its backup
   manifest file for file.

## Start at boot

- `deploy/windows/aura-wsl.xml`, versioned next to `deploy/aura.service`: a boot trigger, run
  whether or not anyone is logged on, as the operator's Windows account (WSL distros are per
  user), action `wsl.exe -d Ubuntu -u root --exec /bin/sleep infinity`, restart every minute on
  failure, no execution time limit, one instance. systemd in the distro then starts
  `docker.service` and `aura.service`.
- Registered with `schtasks /Create /XML` in S4U mode (no stored password). If WSL does not start
  in an S4U session, the task is registered with the operator's password instead; the operator
  types it.

## Done when

On a real reboot of the PC:

1. The stack's containers started before the operator's login (their `StartedAt` precedes the
   logon event), all healthy, none exited 127.
2. `https://localhost` answers from Windows.
3. `docker run --rm --runtime=runsc --entrypoint true <sandbox image>` succeeds.
4. The embedding sidecar runs `llama.cpp:server-cuda`, logs its layers offloaded to CUDA, and a
   1,927-token chunk embeds in under 200 ms.
5. `web/e2e/two-role-live.spec.ts` and `web/e2e/management-key-onboarding-live.spec.ts` pass
   against the migrated data, and the WhatsApp session is still paired.

## Docs and memory

- `CLAUDE.md` "WSL reaches the Windows Docker stack via `127.0.0.1`" becomes false; it says the
  engine runs in the WSL distro.
- Memory: retire `riavvio-wsl-bind-mount-docker-desktop.md`; record that `docker` is used inside
  WSL (`wsl.exe -e docker …`), not from Windows.

## What this does not demonstrate

- Boxes under runsc, gVisor's netstack and the egress sidecar under runsc: sub-project B.
- Installs on macOS, on Linux without dpkg, or on a host where the GPU is AMD or Intel.
- Whether vectors embedded on the CPU and on the GPU rank identically over a whole corpus: the
  cosine was measured on two inputs.
- Behaviour after a Windows or WSL update, beyond the one reboot above.
