# Native Docker Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Aura's stack runs on Docker Engine inside the WSL distro, with runsc registered, embeddings on the RTX 3060, its data carried over, reachable on the LAN and up from boot with no one logged on.

**Architecture:** The installer (`scripts/install.sh`, shipped as `create-aura-appliance` 0.3.0) learns to register runsc on every native Linux Docker host and to install the NVIDIA Container Toolkit when it finds a usable GPU. This PC then leaves Docker Desktop: volumes are archived with per-file checksums, Docker Desktop is uninstalled, WSL switches to mirrored networking, the published installer runs again over the existing `/opt/aura`, the archives are restored and proven, and a boot task holds the distro up.

**Tech Stack:** bash (install.sh, stub-based `install_lib_test.sh`), Go test (`cmd/aura`), TypeScript (`packages/create-aura`), Docker Engine 29 on Ubuntu 26.04 LTS in WSL 2.7.12, gVisor runsc, NVIDIA Container Toolkit, Windows Task Scheduler, Playwright.

**Spec:** `docs/superpowers/specs/2026-09-13-native-engine-design.md`

## Global Constraints

- Commit on `master`; no branches. One commit per task, imperative subject, body explains why, trailer `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`. Tick this plan's boxes in the same commit.
- Never read, print or name `.env` values in a command's output. Secrets come from `docker exec <container> printenv <NAME>` captured into a variable, or from `/opt/aura/.env` only through `cp`/`sed -i` that print nothing. Mask `sk-or-v1-*` in any output.
- Credentials are asked of the operator in one line and passed only as inline environment variables; never written to a file in the repository.
- From Git Bash, every `docker` or `wsl.exe` command carrying a Linux path runs with `MSYS_NO_PATHCONV=1`, or Git Bash rewrites `/root/...` into `C:/Program Files/Git/root/...` (measured twice on 2026-09-13).
- A script run inside WSL is written to a file and run as `MSYS_NO_PATHCONV=1 wsl.exe -u root -e bash /mnt/c/.../script.sh`, never piped through `bash -s`.
- From Task 5 on, `docker` exists only inside WSL: `MSYS_NO_PATHCONV=1 wsl.exe -u root -e docker …`.
- After editing `scripts/install.sh`'s `download_file` list or anything under `deploy/`, run `bash scripts/payload_manifest_gate.sh` inside WSL; if it fails, `bash scripts/payload_manifest_gate.sh --write`, and check that each hash equals `git show HEAD:<path> | sha256sum` once committed (CRLF on this checkout changes the hash).
- `scripts/install.sh` is 965 lines and stays one file: `curl -fsSL …/install.sh | bash` downloads it alone, so it cannot source siblings. New functions go in it; no split.
- `<scratchpad>` is `C:/Users/chett/AppData/Local/Temp/claude/d--Repo-Aura/e6c2285f-80ee-4497-b064-65f1f1dea7a9/scratchpad` (`/mnt/c/Users/chett/AppData/Local/Temp/claude/d--Repo-Aura/e6c2285f-80ee-4497-b064-65f1f1dea7a9/scratchpad` inside WSL). Throwaway scripts live there, never in the repository.
- Backups live in `D:\Backups\aura-2026-09-13\` (`/d/Backups/aura-2026-09-13` in Git Bash, `/mnt/d/Backups/aura-2026-09-13` in WSL).
- Carried volumes (spec, Why): `aura_aura-postgres aura_aura-arcadedb aura_aura-arcadedb-backups aura_garage-data aura_aura-whatsapp-session aura_aura-pim-data aura_caddy-data aura_aura-home aura_aura-workspace aura_aura-ingest-state aura_aura-grafana aura_aura-prometheus aura_aura-tempo aura_aura-llama-embed aura_aura-ocr-vl aura-box-214f9d28-a020-4627-8611-4ee85ab4e1dc`.

---

### Task 1: runsc on every native Linux Docker install

**Files:**
- Modify: `scripts/install.sh:43` (usage), `scripts/install.sh:778-800` (`provision_gvisor` → `provision_runsc`, new `prove_runsc`), `scripts/install.sh:862` and `:943` (main flow)
- Modify: `scripts/install_lib_test.sh` (append)
- Modify: `cmd/aura/container_artifacts_test.go:266-270`
- Modify: `deploy/aura.service:12-24`
- Modify: `packages/create-aura/src/messages/it.ts:48`, `packages/create-aura/src/messages/en.ts:46`
- Modify: `scripts/payload_manifest.txt` (regenerated)

**Interfaces:**
- Consumes: `native_linux_docker`, `as_root`, `env_value`, globals `OS`, `GVISOR` (all in `scripts/install.sh`).
- Produces: `provision_runsc` (no args; exits 1 on a native host it cannot provision; returns 0 with a WARN elsewhere unless `GVISOR=1`), `prove_runsc` (no args; reads `AURA_SANDBOX_IMAGE` from `./.env`). Test helpers `make_logging_stubs <dir> <tool>...` and `make_docker_stub <dir>` in `scripts/install_lib_test.sh`, reused by Task 2.

- [ ] **Step 1: Write the failing tests**

Append to `scripts/install_lib_test.sh`:

```bash
# Provisioning talks to apt, systemd and Docker. The stubs record what it would have run, and the
# PATH holds only them plus the few real tools the functions need, so a real apt-get, tee or
# systemctl can never be reached from a test.
make_logging_stubs() {
  dir="$1"
  shift
  mkdir -p "$dir"
  for tool in "$@"; do
    printf '#!/bin/sh\necho "%s $*" >> "$STUB_LOG"\n' "$tool" > "$dir/$tool"
    chmod +x "$dir/$tool"
  done
  for real in id awk sed cat dirname chmod grep; do
    ln -sf "$(command -v "$real")" "$dir/$real"
  done
  printf '#!/bin/sh\nexec "$@"\n' > "$dir/sudo"
  chmod +x "$dir/sudo"
}

make_docker_stub() {
  cat > "$1/docker" <<'STUB'
#!/bin/sh
echo "docker $*" >> "$STUB_LOG"
case "$*" in
  *OperatingSystem*) echo "${STUB_DOCKER_OS:-Ubuntu 26.04 LTS}" ;;
  *Runtimes*) if [ -n "${STUB_DOCKER_RUNTIMES:-}" ]; then echo "$STUB_DOCKER_RUNTIMES"; else echo '{"runc":{}}'; fi ;;
  run*) exit "${STUB_DOCKER_RUN_RC:-0}" ;;
esac
STUB
  chmod +x "$1/docker"
}

declare -F provision_runsc >/dev/null || { echo "FAIL: provision_runsc undefined after source" >&2; exit 1; }
declare -F prove_runsc >/dev/null || { echo "FAIL: prove_runsc undefined after source" >&2; exit 1; }
if declare -F provision_gvisor >/dev/null; then echo "FAIL: provision_gvisor survived: runsc is no longer --gvisor-only" >&2; exit 1; fi

export STUB_LOG="$fixture_root/stub.log"
runsc_ready="$fixture_root/runsc-ready"
make_logging_stubs "$runsc_ready" dpkg runsc systemctl apt-get curl gpg tee
make_docker_stub "$runsc_ready"
runsc_fresh="$fixture_root/runsc-fresh"
make_logging_stubs "$runsc_fresh" dpkg systemctl curl gpg tee
make_docker_stub "$runsc_fresh"
# apt-get installing runsc is what makes the runsc binary exist for the next line.
cat > "$runsc_fresh/apt-get" <<'STUB'
#!/bin/sh
echo "apt-get $*" >> "$STUB_LOG"
case " $* " in
  *" runsc "*) printf '#!/bin/sh\necho "runsc $*" >> "$STUB_LOG"\n' > "$(dirname "$0")/runsc"; chmod +x "$(dirname "$0")/runsc" ;;
esac
STUB
chmod +x "$runsc_fresh/apt-get"
runsc_nodpkg="$fixture_root/runsc-nodpkg"
make_logging_stubs "$runsc_nodpkg" runsc systemctl apt-get
make_docker_stub "$runsc_nodpkg"
runsc_broken="$fixture_root/runsc-broken"
make_logging_stubs "$runsc_broken" dpkg systemctl apt-get
make_docker_stub "$runsc_broken"
printf '#!/bin/sh\necho "runsc $*" >> "$STUB_LOG"\nexit 1\n' > "$runsc_broken/runsc"
chmod +x "$runsc_broken/runsc"

logged() { grep -qx "$1" "$STUB_LOG"; }

: > "$STUB_LOG"
( PATH="$runsc_ready" OS=Linux GVISOR=0 provision_runsc )
logged "runsc install" && logged "systemctl reload docker" \
  || { echo "FAIL: a native host with runsc on PATH was not registered: $(cat "$STUB_LOG")" >&2; exit 1; }
if grep -q '^apt-get' "$STUB_LOG"; then echo "FAIL: runsc already installed, apt-get still ran" >&2; exit 1; fi

: > "$STUB_LOG"
( PATH="$runsc_ready" OS=Linux GVISOR=0 STUB_DOCKER_RUNTIMES='{"runc":{},"runsc":{}}' provision_runsc )
if grep -q '^runsc install' "$STUB_LOG"; then echo "FAIL: a registered runsc was installed again" >&2; exit 1; fi

: > "$STUB_LOG"
( PATH="$runsc_fresh" OS=Linux GVISOR=0 provision_runsc )
logged "apt-get install -y runsc" && logged "runsc install" \
  || { echo "FAIL: a native host without runsc did not install and register it: $(cat "$STUB_LOG")" >&2; exit 1; }

: > "$STUB_LOG"
( PATH="$runsc_ready" OS=Linux GVISOR=0 STUB_DOCKER_OS="Docker Desktop" provision_runsc ) 2>"$fixture_root/runsc-desktop.err"
grep -q 'desktop-feedback#364' "$fixture_root/runsc-desktop.err" \
  || { echo "FAIL: Docker Desktop was not warned about: $(cat "$fixture_root/runsc-desktop.err")" >&2; exit 1; }
if grep -q -E '^(runsc|apt-get|systemctl)' "$STUB_LOG"; then echo "FAIL: provisioning ran on Docker Desktop" >&2; exit 1; fi

if ( PATH="$runsc_ready" OS=Linux GVISOR=1 STUB_DOCKER_OS="Docker Desktop" provision_runsc ) 2>/dev/null; then
  echo "FAIL: --gvisor on Docker Desktop was accepted" >&2
  exit 1
fi
if ( PATH="$runsc_nodpkg" OS=Linux GVISOR=0 provision_runsc ) 2>"$fixture_root/runsc-nodpkg.err"; then
  echo "FAIL: a native host without dpkg was accepted" >&2
  exit 1
fi
grep -q dpkg "$fixture_root/runsc-nodpkg.err" || { echo "FAIL: no-dpkg refused for the wrong reason" >&2; exit 1; }
if ( PATH="$runsc_broken" OS=Linux GVISOR=0 provision_runsc ) 2>"$fixture_root/runsc-broken.err"; then
  echo "FAIL: a failing runsc install was swallowed" >&2
  exit 1
fi
grep -q 'runsc install' "$fixture_root/runsc-broken.err" || { echo "FAIL: runsc install failure not named" >&2; exit 1; }

echo "ok: provision_runsc registers runsc on native Linux Docker, warns on Docker Desktop, fails loudly otherwise"

mkdir -p "$fixture_root/prove"
(
  cd "$fixture_root/prove"
  printf 'AURA_SANDBOX_IMAGE=ghcr.io/example/aura-sandbox:edge\n' > .env
  : > "$STUB_LOG"
  ( PATH="$runsc_ready" OS=Linux prove_runsc )
  logged "docker run --rm --runtime=runsc --entrypoint true ghcr.io/example/aura-sandbox:edge" \
    || { echo "FAIL: prove_runsc did not run the sandbox image under runsc: $(cat "$STUB_LOG")" >&2; exit 1; }
  if ( PATH="$runsc_ready" OS=Linux STUB_DOCKER_RUN_RC=1 prove_runsc ) 2>/dev/null; then
    echo "FAIL: a sandbox image that cannot run under runsc was accepted" >&2
    exit 1
  fi
  : > "$STUB_LOG"
  ( PATH="$runsc_ready" OS=Linux STUB_DOCKER_OS="Docker Desktop" prove_runsc )
  if grep -q '^docker run' "$STUB_LOG"; then echo "FAIL: prove_runsc ran on Docker Desktop" >&2; exit 1; fi
)

echo "ok: prove_runsc runs the box image under runsc where runsc can exist"
```

In `cmd/aura/container_artifacts_test.go`, after the existing `set_env_value AURA_RUNTIME runsc` check (line 270), add:

```go
	// runsc is registered on every native Linux install, not only under --gvisor, and proven
	// with the box image after it is available and before compose starts anything.
	if strings.Contains(installer, "provision_gvisor") {
		t.Fatal("scripts/install.sh still carries provision_gvisor: runsc is no longer --gvisor-only")
	}
	proveAt := strings.LastIndex(installer, "\nprove_runsc")
	if proveAt < 0 || proveAt < strings.LastIndex(installer, "\nensure_sandbox_image") || proveAt > strings.LastIndex(installer, "docker compose up -d --wait") {
		t.Fatal("scripts/install.sh must call prove_runsc after ensure_sandbox_image and before docker compose up")
	}
	if !strings.Contains(installer, "\nprovision_runsc") {
		t.Fatal("scripts/install.sh must call provision_runsc")
	}
```

- [ ] **Step 2: Run the tests and watch them fail**

Run: `MSYS_NO_PATHCONV=1 wsl.exe -u root -e bash -c 'cd /mnt/d/Repo/Aura && bash scripts/install_lib_test.sh'`
Expected: `FAIL: provision_runsc undefined after source`.

Run: `go test ./cmd/aura -run TestProductionContainerArtifactsMatchFatImageContract -count=1`
Expected: FAIL with `still carries provision_gvisor`.

- [ ] **Step 3: Implement**

In `scripts/install.sh`, replace the whole `provision_gvisor` function (lines 778-800) with:

```bash
# gVisor is the boxes' kernel boundary, so every host that can register runsc gets it, not only
# --gvisor, which keeps one meaning: the aura container itself under runsc (AURA_RUNTIME).
# Docker Desktop has no supported way to register an extra runtime
# (docker/desktop-feedback#364): there the install warns and goes on, unless --gvisor asked for a
# runtime that engine cannot have.
provision_runsc() {
  if ! native_linux_docker; then
    if [ "$GVISOR" -eq 1 ]; then
      echo "FAIL: --gvisor is only supported on native Linux Docker, not Docker Desktop or ${OS}." >&2
      exit 1
    fi
    echo "WARN: runsc needs native Linux Docker; Docker Desktop and ${OS} cannot register it (docker/desktop-feedback#364)." >&2
    return 0
  fi
  command -v dpkg >/dev/null 2>&1 || {
    echo "FAIL: runsc provisioning currently requires a Debian/Ubuntu-style host with dpkg." >&2
    exit 1
  }
  if ! command -v runsc >/dev/null 2>&1; then
    as_root apt-get update
    as_root apt-get install -y apt-transport-https ca-certificates curl gnupg
    curl -fsSL https://gvisor.dev/archive.key \
      | as_root gpg --dearmor --yes -o /usr/share/keyrings/gvisor-archive-keyring.gpg
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/gvisor-archive-keyring.gpg] https://storage.googleapis.com/gvisor/releases release main" \
      | as_root tee /etc/apt/sources.list.d/gvisor.list >/dev/null
    as_root apt-get update
    as_root apt-get install -y runsc
  fi
  if docker info --format '{{json .Runtimes}}' | grep -q '"runsc"'; then
    return 0
  fi
  as_root runsc install || {
    echo "FAIL: runsc install could not register the runsc runtime with Docker." >&2
    exit 1
  }
  as_root systemctl reload docker
}

# Registered is not working: the proof runs the box image itself under runsc.
prove_runsc() {
  native_linux_docker || return 0
  sandbox_image="$(env_value AURA_SANDBOX_IMAGE)"
  docker run --rm --runtime=runsc --entrypoint true "$sandbox_image" || {
    echo "FAIL: Docker cannot run the sandbox image ($sandbox_image) under runsc." >&2
    exit 1
  }
}
```

In the main flow, line 862: `provision_gvisor` → `provision_runsc`. After line 943 `ensure_sandbox_image`, add a line `prove_runsc`.

Line 43 of the usage text becomes:

```
  --gvisor       also run the aura container itself under runsc (AURA_RUNTIME=runsc in .env)
```

`deploy/aura.service` lines 12-24 become:

```
# gVisor: scripts/install.sh registers runsc on every native Linux Docker host (never Docker
# Desktop, which cannot: docker/desktop-feedback#364). To also run the aura container itself
# under it, set AURA_RUNTIME=runsc in /opt/aura/.env — nessun drop-in e nessun -f: questa unit
# resta identica nei due casi. gVisor is a transparent isolation boundary, not capability
# stripping.
```

`packages/create-aura/src/messages/it.ts:48`:

```ts
  gvisorQuestion: 'Eseguire anche il container del demone Aura dentro gVisor (solo Linux con Docker nativo e gestore pacchetti Debian/Ubuntu)?',
```

`packages/create-aura/src/messages/en.ts:46`:

```ts
  gvisorQuestion: 'Also run the Aura daemon container itself under gVisor (native Linux Docker with a Debian/Ubuntu package manager only)?',
```

- [ ] **Step 4: Run the tests and watch them pass**

Run: `MSYS_NO_PATHCONV=1 wsl.exe -u root -e bash -c 'cd /mnt/d/Repo/Aura && bash scripts/install_lib_test.sh'`
Expected: every `ok:` line, including the two new ones, and exit 0.

Run: `go test ./cmd/aura -run TestProductionContainerArtifactsMatchFatImageContract -count=1`
Expected: PASS.

Run: `cd packages/create-aura && npx vitest run`
Expected: all tests pass.

Run: `MSYS_NO_PATHCONV=1 wsl.exe -u root -e bash -c 'cd /mnt/d/Repo/Aura && bash scripts/payload_manifest_gate.sh'`
Expected: pass; if it reports the payload changed, run it with `--write` and re-run to pass.

- [ ] **Step 5: Commit**

```bash
git add scripts/install.sh scripts/install_lib_test.sh cmd/aura/container_artifacts_test.go deploy/aura.service packages/create-aura/src/messages/it.ts packages/create-aura/src/messages/en.ts scripts/payload_manifest.txt docs/superpowers/plans/2026-09-13-native-engine.md
git commit -m "feat(installer): register runsc on every native Linux Docker host"
```

---

### Task 2: CUDA wherever the GPU is real

**Files:**
- Modify: `scripts/install.sh:539-560` (`detect_embed_backend`, new `nvidia_smi_bin`, `nvidia_gpu_usable`, `nvidia_hook_present`, `provision_nvidia_toolkit`), main flow after `provision_runsc`
- Modify: `scripts/install_lib_test.sh:131-154` and append
- Modify: `scripts/payload_manifest.txt` if the gate asks

**Interfaces:**
- Consumes: `make_logging_stubs`, `make_docker_stub` (Task 1), `native_linux_docker`, `as_root`.
- Produces: `nvidia_smi_bin [wsl_lib_dir]` prints a path or nothing; `nvidia_gpu_usable [wsl_lib_dir]`; `nvidia_hook_present`; `detect_embed_backend [dri_dir] [wsl_lib_dir]`; `ensure_embed_backend_env [dri_dir] [wsl_lib_dir]` (unchanged body, passes `"$@"`); `provision_nvidia_toolkit [wsl_lib_dir]`.

- [ ] **Step 1: Read the WSL placement of `nvidia-smi`**

Read https://docs.nvidia.com/cuda/wsl-user-guide/index.html and confirm it states the driver's user-mode libraries and `nvidia-smi` are mapped into `/usr/lib/wsl/lib`. Measured on this PC 2026-09-13: `/usr/lib/wsl/lib/nvidia-smi` exists, a root login shell has it on PATH, `sudo sh -c 'command -v nvidia-smi'` finds nothing (`secure_path=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/snap/bin`). Put the documentation URL in the comment of Step 4.

- [ ] **Step 2: Write the failing tests**

In `scripts/install_lib_test.sh`, replace `expect_backend` (lines 137-144) and its calls (147-152) so every case passes an explicit WSL lib dir (the dev WSL itself has a real `/usr/lib/wsl/lib/nvidia-smi`):

```bash
mkdir -p "$fixture_root/wsl-none" "$fixture_root/wsl-smi"
printf '#!/bin/sh\nexit 0\n' > "$fixture_root/wsl-smi/nvidia-smi"
chmod +x "$fixture_root/wsl-smi/nvidia-smi"
make_stubs "$fixture_root/bin-hook-only" nvidia-container-runtime-hook

expect_backend() {
  want="$1"
  bin="$2"
  dri="$3"
  wsl="${4:-$fixture_root/wsl-none}"
  got="$(PATH="$bin:/usr/bin:/bin" detect_embed_backend "$dri" "$wsl")"
  [ "$got" = "$want" ] \
    || { echo "FAIL: detect_embed_backend with $(basename "$bin"), $(basename "$dri") and $(basename "$wsl") gave '$got', want '$want'" >&2; exit 1; }
}
expect_backend cuda "$fixture_root/bin-hook" "$fixture_root/dri-none"
expect_backend cuda "$fixture_root/bin-cdi" "$fixture_root/dri-render"
expect_backend vulkan "$fixture_root/bin-smi" "$fixture_root/dri-render"
expect_backend vulkan "$fixture_root/bin-none" "$fixture_root/dri-render"
expect_backend cpu "$fixture_root/bin-smi" "$fixture_root/dri-none"
expect_backend cpu "$fixture_root/bin-none" "$fixture_root/dri-none"
# WSL: nvidia-smi lives in /usr/lib/wsl/lib, off sudo's secure_path.
expect_backend cuda "$fixture_root/bin-hook-only" "$fixture_root/dri-none" "$fixture_root/wsl-smi"
expect_backend cpu "$fixture_root/bin-none" "$fixture_root/dri-none" "$fixture_root/wsl-smi"
```

The `/usr/bin:/bin` tail of `expect_backend`'s PATH could hold a real `nvidia-smi` on a GPU developer machine; the CI runner has none, and the existing cases already carry that assumption.

The `ensure_embed_backend_env` calls at lines 162 and 185 pass `"$fixture_root/dri-render"` only; add the second argument `"$fixture_root/wsl-none"` to both.

Append:

```bash
declare -F provision_nvidia_toolkit >/dev/null || { echo "FAIL: provision_nvidia_toolkit undefined after source" >&2; exit 1; }

nv_native="$fixture_root/nv-native"
make_logging_stubs "$nv_native" dpkg apt-get curl gpg tee nvidia-ctk systemctl
make_docker_stub "$nv_native"
printf '#!/bin/sh\nexit 0\n' > "$nv_native/nvidia-smi"
chmod +x "$nv_native/nvidia-smi"
nv_nodpkg="$fixture_root/nv-nodpkg"
make_logging_stubs "$nv_nodpkg" apt-get curl gpg tee nvidia-ctk systemctl
make_docker_stub "$nv_nodpkg"
printf '#!/bin/sh\nexit 0\n' > "$nv_nodpkg/nvidia-smi"
chmod +x "$nv_nodpkg/nvidia-smi"
nv_nogpu="$fixture_root/nv-nogpu"
make_logging_stubs "$nv_nogpu" dpkg apt-get curl gpg tee nvidia-ctk systemctl
make_docker_stub "$nv_nogpu"
nv_hooked="$fixture_root/nv-hooked"
make_logging_stubs "$nv_hooked" dpkg apt-get curl gpg tee nvidia-ctk systemctl nvidia-container-runtime-hook
make_docker_stub "$nv_hooked"
printf '#!/bin/sh\nexit 0\n' > "$nv_hooked/nvidia-smi"
chmod +x "$nv_hooked/nvidia-smi"

toolkit_ran() { grep -q '^apt-get install -y nvidia-container-toolkit' "$STUB_LOG"; }

: > "$STUB_LOG"
( PATH="$nv_native" OS=Linux provision_nvidia_toolkit "$fixture_root/wsl-none" )
toolkit_ran && logged "nvidia-ctk runtime configure --runtime=docker" && logged "systemctl restart docker" \
  || { echo "FAIL: a usable GPU without a hook did not get the toolkit: $(cat "$STUB_LOG")" >&2; exit 1; }

: > "$STUB_LOG"
( PATH="$nv_nogpu" OS=Linux provision_nvidia_toolkit "$fixture_root/wsl-smi" )
toolkit_ran || { echo "FAIL: a WSL GPU (nvidia-smi only in the WSL lib dir) did not get the toolkit" >&2; exit 1; }

for case in "$nv_nogpu:$fixture_root/wsl-none:no GPU" "$nv_hooked:$fixture_root/wsl-none:hook present"; do
  bin="${case%%:*}"; rest="${case#*:}"; wsl="${rest%%:*}"; why="${rest#*:}"
  : > "$STUB_LOG"
  ( PATH="$bin" OS=Linux provision_nvidia_toolkit "$wsl" )
  if toolkit_ran; then echo "FAIL: the toolkit was installed with $why" >&2; exit 1; fi
done

: > "$STUB_LOG"
( PATH="$nv_native" OS=Linux STUB_DOCKER_OS="Docker Desktop" provision_nvidia_toolkit "$fixture_root/wsl-none" )
if toolkit_ran; then echo "FAIL: the toolkit was installed on Docker Desktop" >&2; exit 1; fi

: > "$STUB_LOG"
( PATH="$nv_nodpkg" OS=Linux provision_nvidia_toolkit "$fixture_root/wsl-none" ) 2>"$fixture_root/nv-nodpkg.err"
if toolkit_ran; then echo "FAIL: the toolkit was installed without dpkg" >&2; exit 1; fi
grep -q 'NVIDIA Container Toolkit' "$fixture_root/nv-nodpkg.err" || { echo "FAIL: no-dpkg GPU host was not warned" >&2; exit 1; }

echo "ok: provision_nvidia_toolkit installs the toolkit only for a usable GPU Docker cannot drive yet"
```

- [ ] **Step 3: Run the tests and watch them fail**

Run: `MSYS_NO_PATHCONV=1 wsl.exe -u root -e bash -c 'cd /mnt/d/Repo/Aura && bash scripts/install_lib_test.sh'`
Expected: FAIL on `detect_embed_backend with bin-hook-only, dri-none and wsl-smi gave 'cpu', want 'cuda'`.

- [ ] **Step 4: Implement**

In `scripts/install.sh`, replace lines 539-560 (the comment above `detect_embed_backend` and the function) with:

```bash
# On WSL the NVIDIA driver maps nvidia-smi into /usr/lib/wsl/lib
# (https://docs.nvidia.com/cuda/wsl-user-guide/index.html), a directory a login shell has on
# PATH and sudo's secure_path drops. The installer runs under sudo, so looking on PATH alone
# missed a working RTX 3060 (measured 2026-09-13).
nvidia_smi_bin() {
  wsl_lib="${1:-/usr/lib/wsl/lib}"
  if command -v nvidia-smi >/dev/null 2>&1; then
    command -v nvidia-smi
  elif [ -x "$wsl_lib/nvidia-smi" ]; then
    echo "$wsl_lib/nvidia-smi"
  fi
}

nvidia_gpu_usable() {
  smi="$(nvidia_smi_bin "$@")"
  [ -n "$smi" ] && "$smi" >/dev/null 2>&1
}

nvidia_hook_present() {
  command -v nvidia-container-runtime-hook >/dev/null 2>&1 || command -v nvidia-cdi-hook >/dev/null 2>&1
}

# Decided here, on the target, the only machine whose answer is true. Docker registers its
# `nvidia` device driver only when nvidia-container-runtime-hook or nvidia-cdi-hook is on
# its PATH (moby daemon/devices_nvidia_linux.go), and without it compose.yaml's
# reservation kills `up` ("could not select device driver nvidia", measured 2026-09-10 on
# a mini PC) -- so a GPU nvidia-smi sees is CUDA only if a hook is there too. A render
# node is Intel or AMD, integrated included, which the Vulkan image drives. Metal cannot
# reach a Docker container on macOS, so a Mac lands on the CPU.
detect_embed_backend() {
  dri_dir="${1:-/dev/dri}"
  if nvidia_gpu_usable "${2:-}" && nvidia_hook_present; then
    echo cuda
    return
  fi
  for node in "$dri_dir"/renderD*; do
    if [ -e "$node" ]; then
      echo vulkan
      return
    fi
  done
  echo cpu
}

# A usable NVIDIA GPU Docker cannot drive is missing only the toolkit that gives Docker its
# nvidia driver. NVIDIA's apt procedure:
# https://github.com/NVIDIA/cloud-native-docs/blob/main/container-toolkit/install-guide.md
provision_nvidia_toolkit() {
  if ! nvidia_gpu_usable "$@" || nvidia_hook_present || ! native_linux_docker; then
    return 0
  fi
  if ! command -v dpkg >/dev/null 2>&1; then
    echo "WARN: an NVIDIA GPU is present, but the NVIDIA Container Toolkit is installed here only through apt; embeddings fall back to Vulkan or CPU." >&2
    return 0
  fi
  as_root apt-get update
  as_root apt-get install -y --no-install-recommends ca-certificates curl gnupg2
  curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey \
    | as_root gpg --dearmor --yes -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg
  curl -fsSL https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list \
    | sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' \
    | as_root tee /etc/apt/sources.list.d/nvidia-container-toolkit.list >/dev/null
  as_root apt-get update
  as_root apt-get install -y nvidia-container-toolkit
  as_root nvidia-ctk runtime configure --runtime=docker
  as_root systemctl restart docker
}
```

Main flow: after the `provision_runsc` line, add `provision_nvidia_toolkit`.

- [ ] **Step 5: Run the tests and watch them pass**

Run: `MSYS_NO_PATHCONV=1 wsl.exe -u root -e bash -c 'cd /mnt/d/Repo/Aura && bash scripts/install_lib_test.sh'`
Expected: every `ok:` line, exit 0.

Run: `MSYS_NO_PATHCONV=1 wsl.exe -u root -e bash -c 'cd /mnt/d/Repo/Aura && bash scripts/payload_manifest_gate.sh'`
Expected: pass (regenerate with `--write` if asked).

- [ ] **Step 6: Commit**

```bash
git add scripts/install.sh scripts/install_lib_test.sh scripts/payload_manifest.txt docs/superpowers/specs/2026-09-13-native-engine-design.md docs/superpowers/plans/2026-09-13-native-engine.md
git commit -m "feat(installer): put embeddings on the NVIDIA GPU wherever Docker can reach it"
```

---

### Task 3: Release create-aura-appliance 0.3.0

**Files:**
- Modify: `packages/create-aura/package.json` (`"version": "0.3.0"`)
- Modify: `packages/create-aura/package-lock.json` (the root `"version"` and `packages[""].version` to `0.3.0`)

**Interfaces:**
- Consumes: Tasks 1 and 2 on `master`.
- Produces: `npm view create-aura-appliance version` → `0.3.0`, used by Task 5.

- [ ] **Step 1: Bump the version**

Set `"version": "0.3.0"` in `packages/create-aura/package.json`, then in `packages/create-aura`: `npm install --package-lock-only` and check `git diff packages/create-aura/package-lock.json` changes only the two version fields.

- [ ] **Step 2: Verify locally**

Run: `cd packages/create-aura && npx vitest run && npm run build`
Expected: tests pass, build succeeds.

- [ ] **Step 3: Commit and push**

```bash
git add packages/create-aura/package.json packages/create-aura/package-lock.json docs/superpowers/plans/2026-09-13-native-engine.md
git commit -m "chore(installer): release create-aura-appliance 0.3.0"
git push origin master
```

- [ ] **Step 4: CI green on the pushed commit**

Run: `gh run list --branch master --limit 10 --json name,status,conclusion,headSha` until every run for the pushed SHA is `completed`; all must be `success`. On a failure, read `gh run view <id> --log-failed`, fix, commit, push, repeat.

- [ ] **Step 5: Tag and publish**

```bash
git tag installer-v0.3.0
git push origin installer-v0.3.0
```

Wait for the `Verify and publish create-aura-appliance` run on the tag to succeed, then:
Run: `npm view create-aura-appliance version`
Expected: `0.3.0`.

---

### Task 4: Back up and verify the stack's data

**Files:**
- Create (scratchpad, not the repo): `backup_native_engine.sh`, `verify_native_engine.sh`
- Modify: this plan (evidence under the task)

**Interfaces:**
- Produces: in `D:\Backups\aura-2026-09-13\`, one `<volume>.tgz` and one `<volume>.sha256` per carried volume, `postgres-aura.dump`, `dotenv`. Task 5 consumes all of them.

- [ ] **Step 1: Record the WhatsApp session before anything changes**

Run `MSYS_NO_PATHCONV=1 docker logs aura-whatsapp 2>&1 | grep -i -E 'connect|logged|pair|qr' | tail -5` and record the lines in this task; Task 7 Step 5 compares against them.

Write `<scratchpad>/backup_native_engine.sh` (runs in Git Bash against Docker Desktop):

```bash
#!/usr/bin/env bash
set -euo pipefail
export MSYS_NO_PATHCONV=1
B=/d/Backups/aura-2026-09-13
BW=D:/Backups/aura-2026-09-13
VOLS="aura_aura-postgres aura_aura-arcadedb aura_aura-arcadedb-backups aura_garage-data aura_aura-whatsapp-session aura_aura-pim-data aura_caddy-data aura_aura-home aura_aura-workspace aura_aura-ingest-state aura_aura-grafana aura_aura-prometheus aura_aura-tempo aura_aura-llama-embed aura_aura-ocr-vl aura-box-214f9d28-a020-4627-8611-4ee85ab4e1dc"
mkdir -p "$B"
docker exec aura-postgres sh -c 'pg_dump -U "$POSTGRES_USER" -Fc "$POSTGRES_DB"' > "$B/postgres-aura.dump"
wsl.exe -u root -e sh -c 'cd /opt/aura && docker compose stop'
docker ps --format '{{.Names}}' | grep -E '^aura-box-' | xargs -r docker stop
for v in $VOLS; do
  docker run --rm -v "$v:/v:ro" -v "$BW:/b" ubuntu:26.04 \
    sh -c "cd /v && tar -czpf /b/$v.tgz . && find . -type f -exec sha256sum {} + | sort -k2 > /b/$v.sha256"
  echo "archived $v $(du -h "$B/$v.tgz" | cut -f1) files=$(wc -l < "$B/$v.sha256")"
done
wsl.exe -u root -e cp /opt/aura/.env /mnt/d/Backups/aura-2026-09-13/dotenv
ls -la "$B"
```

- [ ] **Step 2: Run it**

Run: `bash <scratchpad>/backup_native_engine.sh`
Expected: 16 `archived` lines, a non-empty `postgres-aura.dump`, `dotenv` present.

- [ ] **Step 3: Write and run the verification**

`<scratchpad>/verify_native_engine.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
export MSYS_NO_PATHCONV=1
BW=D:/Backups/aura-2026-09-13
VOLS="aura_aura-postgres aura_aura-arcadedb aura_aura-arcadedb-backups aura_garage-data aura_aura-whatsapp-session aura_aura-pim-data aura_caddy-data aura_aura-home aura_aura-workspace aura_aura-ingest-state aura_aura-grafana aura_aura-prometheus aura_aura-tempo aura_aura-llama-embed aura_aura-ocr-vl aura-box-214f9d28-a020-4627-8611-4ee85ab4e1dc"
for v in $VOLS; do
  docker run --rm -v "$BW:/b:ro" ubuntu:26.04 \
    sh -c "mkdir /t && tar -xzpf /b/$v.tgz -C /t && cd /t && find . -type f -exec sha256sum {} + | sort -k2 | diff -q - /b/$v.sha256 >/dev/null" \
    && echo "ok $v" || { echo "FAIL $v"; exit 1; }
done
docker run --rm -v "$BW:/b:ro" postgres:18.4-alpine3.24 pg_restore -l /b/postgres-aura.dump | grep -c 'TABLE DATA'
```

Run: `bash <scratchpad>/verify_native_engine.sh`
Expected: 16 `ok` lines and a `TABLE DATA` count above zero. Any `FAIL` stops the plan here: nothing is uninstalled.

- [ ] **Step 4: Record and restart**

Record in this task the archive sizes, file counts and the `TABLE DATA` count. Restart the stack so Aura stays usable until Task 5: `MSYS_NO_PATHCONV=1 wsl.exe -u root -e sh -c 'cd /opt/aura && docker compose up -d --wait'`. Commit the plan: `git commit -am "docs(plans): native engine backup verified"`.

---

### Task 5: Leave Docker Desktop, install, restore

**Files:**
- Modify: `%UserProfile%\.wslconfig`, `/opt/aura/.env` (four keys removed, no read)
- Create (scratchpad): `restore_native_engine.sh`
- Modify: this plan (evidence)

**Interfaces:**
- Consumes: the Task 4 backups; `create-aura-appliance@0.3.0` from Task 3.
- Produces: the stack running on Docker Engine in WSL with the carried data, used by Tasks 6 and 7.

- [ ] **Step 1: Stop and snapshot the latest state**

Repeat Task 4 Steps 2-3 (the stack ran between tasks, so the archives must be retaken now) and confirm 16 `ok`.

- [ ] **Step 2: Uninstall Docker Desktop (operator)**

Ask the operator to run, accepting UAC: `winget uninstall Docker.DockerDesktop`.
Then: `wsl.exe --shutdown`, and check `wsl.exe -l -v` lists only `Ubuntu`, and `MSYS_NO_PATHCONV=1 wsl.exe -u root -e sh -c 'command -v docker || echo absent'` prints `absent`.

- [ ] **Step 3: Mirror the network (operator for the firewall rule)**

Append under `[wsl2]` in `C:\Users\chett\.wslconfig`:

```
networkingMode=mirrored
```

Ask the operator to run in an elevated PowerShell:

```powershell
New-NetFirewallHyperVRule -Name AuraHttps -DisplayName "Aura HTTPS" -Direction Inbound -VMCreatorId '{40E0AC32-46A5-438A-A0B2-2B479E8F2E90}' -Protocol TCP -LocalPorts 443
```

Then `wsl.exe --shutdown`, and check `MSYS_NO_PATHCONV=1 wsl.exe -u root -e wslinfo --networking-mode` prints `mirrored`.

- [ ] **Step 4: Let detection run again**

```bash
MSYS_NO_PATHCONV=1 wsl.exe -u root -e sed -i -e '/^AURA_EMBED_BACKEND=/d' -e '/^COMPOSE_FILE=/d' -e '/^AURA_EMBED_IMAGE=/d' -e '/^AURA_EMBED_NGL=/d' /opt/aura/.env
```

- [ ] **Step 5: Run the published installer**

All prompt defaults are the intended answers (install dir `/opt/aura`, appliance yes, gVisor for the aura container no, confirm yes):

```bash
MSYS_NO_PATHCONV=1 wsl.exe -u root -e bash -lc "printf '\n\n\n\n' | script -qec 'npx --yes create-aura-appliance@0.3.0 --mode local' /dev/null" 2>&1 | tee <scratchpad>/install_native_engine.log | tail -40
```

Expected in the log: Docker Engine installed by `get.docker.com`, `runsc` installed, the NVIDIA toolkit installed, no `FAIL`, the stack `--wait` healthy, the install-complete message. If the prompts do not take the piped answers, stop and report the log.

Check:
```bash
MSYS_NO_PATHCONV=1 wsl.exe -u root -e sh -c "docker info --format '{{.OperatingSystem}} {{json .Runtimes}}'; grep -E '^(AURA_EMBED_BACKEND|COMPOSE_FILE|AURA_EMBED_IMAGE)=' /opt/aura/.env"
```
Expected: `Ubuntu 26.04 LTS` with `runsc` and `nvidia` among the runtimes; `AURA_EMBED_BACKEND=cuda`, `COMPOSE_FILE=compose.yaml`, `AURA_EMBED_IMAGE=ghcr.io/ggml-org/llama.cpp:server-cuda` (these three are configuration names and values, not secrets).

- [ ] **Step 6: Write the restore script**

`<scratchpad>/restore_native_engine.sh` (runs inside WSL as root):

```bash
#!/usr/bin/env bash
set -euo pipefail
B=/mnt/d/Backups/aura-2026-09-13
VOLS="aura_aura-postgres aura_aura-arcadedb aura_aura-arcadedb-backups aura_garage-data aura_aura-whatsapp-session aura_aura-pim-data aura_caddy-data aura_aura-home aura_aura-workspace aura_aura-ingest-state aura_aura-grafana aura_aura-prometheus aura_aura-tempo aura_aura-llama-embed aura_aura-ocr-vl aura-box-214f9d28-a020-4627-8611-4ee85ab4e1dc"
cd /opt/aura
docker compose down
for v in $VOLS; do
  docker volume create "$v" >/dev/null
  docker run --rm -v "$v:/v" -v "$B:/b:ro" ubuntu:26.04 \
    sh -c "find /v -mindepth 1 -delete && tar -xzpf /b/$v.tgz -C /v"
done
for v in $VOLS; do
  docker run --rm -v "$v:/v:ro" -v "$B:/b:ro" ubuntu:26.04 \
    sh -c "cd /v && find . -type f -exec sha256sum {} + | sort -k2 | diff -q - /b/$v.sha256 >/dev/null" \
    && echo "proven $v" || { echo "FAIL $v differs from its backup manifest"; exit 1; }
done
docker compose up -d --wait --wait-timeout 300
docker compose ps --format '{{.Name}} {{.Status}}'
```

- [ ] **Step 7: Restore and prove**

Run: `MSYS_NO_PATHCONV=1 wsl.exe -u root -e bash /mnt/c/Users/chett/AppData/Local/Temp/claude/d--Repo-Aura/e6c2285f-80ee-4497-b064-65f1f1dea7a9/scratchpad/restore_native_engine.sh`
Expected: 16 `proven` lines before `up`, then every service healthy. A `FAIL` stops here with the stack down; the archives are untouched and the step can be re-run.

- [ ] **Step 8: Record and commit**

Record the installer log excerpts, runtimes, the 16 `proven` lines and the service statuses in this task. `git commit -am "docs(plans): the stack runs on Docker Engine in WSL with its data"`.

---

### Task 6: Start at boot

**Files:**
- Create: `deploy/windows/aura-wsl.xml`
- Modify: this plan (evidence)

**Interfaces:**
- Consumes: the running stack from Task 5.
- Produces: the registered task `\Aura\WSL keep-alive`.

- [ ] **Step 1: Write the task definition**

`deploy/windows/aura-wsl.xml`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!-- Holds the Ubuntu WSL distro up from boot, with no one logged on, so systemd starts Docker
     and aura.service. A WSL distro stops about 15 s after its last wsl.exe session even with
     systemd services running (measured 2026-09-13). Windows-only: not part of the installer
     payload. Register: docs/superpowers/plans/2026-09-13-native-engine.md, Task 6. -->
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>Aura: keep the Ubuntu WSL distro running so systemd starts Docker and aura.service.</Description>
  </RegistrationInfo>
  <Triggers>
    <BootTrigger>
      <Enabled>true</Enabled>
    </BootTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <LogonType>S4U</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <IdleSettings>
      <StopOnIdleEnd>false</StopOnIdleEnd>
      <RestartOnIdle>false</RestartOnIdle>
    </IdleSettings>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>999</Count>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>C:\Windows\System32\wsl.exe</Command>
      <Arguments>-d Ubuntu -u root --exec /bin/sleep infinity</Arguments>
    </Exec>
  </Actions>
</Task>
```

- [ ] **Step 2: Register it (operator, elevated PowerShell)**

```powershell
Register-ScheduledTask -TaskName 'WSL keep-alive' -TaskPath '\Aura\' -Xml (Get-Content -Raw 'D:\Repo\Aura\deploy\windows\aura-wsl.xml') -User "$env:USERDOMAIN\$env:USERNAME"
Start-ScheduledTask -TaskPath '\Aura\' -TaskName 'WSL keep-alive'
```

- [ ] **Step 3: Prove it holds WSL without a session**

Close every WSL session this plan opened; wait 90 s; then from PowerShell:
`(Get-ScheduledTask -TaskPath '\Aura\' -TaskName 'WSL keep-alive').State` → `Running`, and `wsl.exe -l --running` lists `Ubuntu`.
If the task is not `Running` or WSL is down, S4U cannot start WSL: set `<LogonType>Password</LogonType>` in the XML and the operator re-registers in an elevated Windows PowerShell 5.1, typing the password into the prompt (never Claude):

```powershell
$c = Get-Credential "$env:USERDOMAIN\$env:USERNAME"
Register-ScheduledTask -TaskName 'WSL keep-alive' -TaskPath '\Aura\' -Xml (Get-Content -Raw 'D:\Repo\Aura\deploy\windows\aura-wsl.xml') -User $c.UserName -Password $c.GetNetworkCredential().Password -Force
```

Repeat this step. Record which logon type held.

- [ ] **Step 4: Commit**

```bash
git add deploy/windows/aura-wsl.xml docs/superpowers/plans/2026-09-13-native-engine.md
git commit -m "feat(deploy): hold the WSL distro up from boot for Docker and aura.service"
```

- [ ] **Step 5: Reboot (operator; this Claude session ends)**

Ask the operator to reboot and to wait at the lock screen for at least 3 minutes before logging in, then resume this plan at Task 7 in a new session.

---

### Task 7: Done-when on the rebooted PC, docs, memory, push

**Files:**
- Modify: `CLAUDE.md` (the WSL row of "Where to run what")
- Delete: `C:\Users\chett\.claude\projects\d--Repo-Aura\memory\riavvio-wsl-bind-mount-docker-desktop.md`
- Create: `C:\Users\chett\.claude\projects\d--Repo-Aura\memory\docker-dentro-wsl.md`
- Modify: `C:\Users\chett\.claude\projects\d--Repo-Aura\memory\MEMORY.md`
- Modify: this plan (evidence)

**Interfaces:**
- Consumes: everything above.

- [ ] **Step 1: Started before login, healthy, no 127**

```bash
MSYS_NO_PATHCONV=1 wsl.exe -u root -e sh -c "docker ps -a --format '{{.Names}} {{.Status}}'; docker inspect --format '{{.Name}} {{.State.StartedAt}} exit={{.State.ExitCode}}' \$(docker ps -aq)"
powershell.exe -NoProfile -Command "(Get-Process explorer | Sort-Object StartTime | Select-Object -First 1).StartTime.ToUniversalTime().ToString('o')"
```
Expected: every service `Up … (healthy)` (or `Up` where it has no healthcheck), no exit code 127, and every `StartedAt` (UTC) earlier than explorer's start (the login).

- [ ] **Step 2: LAN and localhost**

`curl -sk -o /dev/null -w '%{http_code}\n' https://localhost/` from Git Bash → an HTTP status (not `000`).
Find the LAN address (single quotes, or bash expands `$_`): `powershell.exe -NoProfile -Command '(Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.PrefixOrigin -eq "Dhcp" }).IPAddress'`.
Ask the operator for the NAS password in one line, then from PowerShell:
`& 'C:\Program Files\PuTTY\plink.exe' -batch -ssh casa@192.168.1.15 -pw <password> "curl -sk -o /dev/null -w '%{http_code}' https://<LAN address>/"` → an HTTP status, not `000`. If SSH is off on the NAS, ask the operator to enable it in DSM.

- [ ] **Step 3: runsc and CUDA**

```bash
MSYS_NO_PATHCONV=1 wsl.exe -u root -e sh -c "cd /opt/aura && docker run --rm --runtime=runsc --entrypoint true \$(grep '^AURA_SANDBOX_IMAGE=' .env | cut -d= -f2-) && echo runsc-ok; docker inspect --format '{{.Config.Image}}' aura-llama-embed; docker logs aura-llama-embed 2>&1 | grep -i -E 'CUDA0|offloaded' | head -3"
MSYS_NO_PATHCONV=1 wsl.exe -u root -e bash -c 'cd /mnt/c/Users/chett/AppData/Local/Temp/claude/d--Repo-Aura/e6c2285f-80ee-4497-b064-65f1f1dea7a9/scratchpad/embed && for i in $(seq 1 11); do curl -s -o /dev/null -w "%{time_total}\n" http://127.0.0.1:8081/v1/embeddings -d @long.json; done | sort -n | sed -n 6p'
```
`long.json` is the 1,927-token request the 2026-09-13 benchmark left in `<scratchpad>/embed`.
Expected: `runsc-ok`; image `ghcr.io/ggml-org/llama.cpp:server-cuda`; log lines naming CUDA0 and offloaded layers; the printed median under `0.200` seconds.

- [ ] **Step 4: The live E2E on the migrated data**

The admin's credentials: ask the operator in one line; pass only inline. The management key: read the sealed row and open it with a throwaway Go program in the scratchpad, never printing it.

`<scratchpad>/mk/main.go`:

```go
package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

func main() {
	raw, err := hex.DecodeString(strings.TrimSpace(os.Getenv("AUTHULA_SECRET")))
	if err != nil || len(raw) != 32 {
		fmt.Fprintln(os.Stderr, "bad secret")
		os.Exit(1)
	}
	key, err := hkdf.Key(sha256.New, raw, nil, "aura-settings-secret-v1", 32)
	if err != nil {
		panic(err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	rest := strings.TrimPrefix(strings.TrimSpace(os.Getenv("SEALED")), "enc:v1:")
	nonceHex, sealedHex, _ := strings.Cut(rest, ":")
	nonce, _ := hex.DecodeString(nonceHex)
	sealed, _ := hex.DecodeString(sealedHex)
	plain, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "decrypt failed")
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[1], plain, 0o600); err != nil {
		panic(err)
	}
}
```

`<scratchpad>/mk/run.sh`, which prints only the key's byte count:

```bash
#!/usr/bin/env bash
set -euo pipefail
AUTHULA_SECRET="$(docker exec aura printenv AURA_AUTHULA_SECRET)"
SEALED="$(docker exec aura-postgres sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc "select value from aura.settings where key = '\''AURA_OPENROUTER_MANAGEMENT_KEY'\''"')"
export AUTHULA_SECRET SEALED
go run "$(dirname "$0")/main.go" /root/mk.txt
wc -c < /root/mk.txt
```

Run it inside WSL: `MSYS_NO_PATHCONV=1 wsl.exe -u root -e bash -l /mnt/c/Users/chett/AppData/Local/Temp/claude/d--Repo-Aura/e6c2285f-80ee-4497-b064-65f1f1dea7a9/scratchpad/mk/run.sh` → a byte count above zero.

Then from `web/` in Git Bash:

```bash
cd web
AURA_E2E_ORIGIN=https://localhost AURA_E2E_LIVE_TWO_ROLE=1 AURA_E2E_AUTHULA_EMAIL=<operator> AURA_E2E_AUTHULA_PASSWORD=<operator> npx playwright test e2e/two-role-live.spec.ts --reporter=line
AURA_E2E_ORIGIN=https://localhost AURA_E2E_LIVE_MANAGEMENT_KEY=1 AURA_E2E_OPENROUTER_MANAGEMENT_KEY="$(MSYS_NO_PATHCONV=1 wsl.exe -u root -e cat /root/mk.txt)" AURA_E2E_AUTHULA_EMAIL=<operator> AURA_E2E_AUTHULA_PASSWORD=<operator> npx playwright test e2e/management-key-onboarding-live.spec.ts --reporter=line
MSYS_NO_PATHCONV=1 wsl.exe -u root -e rm -f /root/mk.txt
```
Expected: both specs pass. `/root/mk.txt` is removed whatever the outcome.

- [ ] **Step 5: WhatsApp still paired**

Run `MSYS_NO_PATHCONV=1 wsl.exe -u root -e sh -c "docker logs --since 30m aura-whatsapp 2>&1 | grep -i -E 'connect|logged|pair|qr' | tail -5"` and compare with the lines Task 4 Step 1 recorded before the migration: the session reconnects without asking for a new pairing (no QR code requested). Record both.

- [ ] **Step 6: Docs and memory**

In `CLAUDE.md`, the "Where to run what" row for everything currently reads "WSL reaches the Windows Docker stack via `127.0.0.1`". Replace that clause with: "Docker Engine runs inside the WSL distro (since 2026-09-13; no Docker Desktop) — `docker` exists only there".

Delete `riavvio-wsl-bind-mount-docker-desktop.md` and its line in `MEMORY.md`. Create `docker-dentro-wsl.md`:

```markdown
---
name: docker-dentro-wsl
description: Docker gira solo dentro WSL (Docker Engine nativo, niente Docker Desktop): dai comandi Windows si passa da wsl.exe
metadata:
  type: project
---

Dal 2026-09-13 lo stack di Aura gira su Docker Engine dentro la distro `Ubuntu` di WSL, con
runsc e il toolkit NVIDIA registrati; Docker Desktop è disinstallato. Da Git Bash:
`MSYS_NO_PATHCONV=1 wsl.exe -u root -e docker …`.

**Why:** Docker Desktop non può registrare runsc (docker/desktop-feedback#364) e i box per
identità girano sotto gVisor. La distro resta accesa grazie al task `\Aura\WSL keep-alive`
(`deploy/windows/aura-wsl.xml`): senza una sessione aperta WSL si spegne in ~15 s anche con
systemd. La LAN arriva alla 443 perché WSL è in `networkingMode=mirrored` con la regola Hyper-V
`AuraHttps`.

**How to apply:** se lo stack non risponde dopo un riavvio, guarda prima lo stato del task e
`wsl.exe -l --running`, poi `systemctl status docker aura` dentro la distro. Vedi
[[wsl-script-da-file-non-stdin]].
```

Add to `MEMORY.md`: `- [Docker solo dentro WSL](docker-dentro-wsl.md) — niente Docker Desktop: \`wsl.exe -u root -e docker …\`; la distro la tiene viva il task \Aura\WSL keep-alive.`

- [ ] **Step 7: Commit, push, CI**

```bash
git add CLAUDE.md docs/superpowers/plans/2026-09-13-native-engine.md
git commit -m "docs: Aura runs on Docker Engine inside WSL"
git push origin master
```
Wait for every run on the pushed SHA to be `success` (`gh run list --branch master --limit 10 --json name,status,conclusion,headSha`).
