# create-aura Artifact Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn Aura's installer into one self-extracting artifact that carries its own
payload, so an install stops fetching 25 files from a moving git ref at root.

**Architecture:** `scripts/install.sh` stays the single hand-written source of install
logic. It gains three things: a sourcing guard so its functions can be unit-tested, a
`download_file` that prefers a local payload directory over the network, and a `--config`
intake so a wizard can drive it without re-implementing `.env` handling. `makeself` then
packs the 25 payload files plus that script into one executable archive; because makeself
runs its startup script with the working directory set to the extracted files, the payload
is simply *there* and no code generation is needed.

**Tech Stack:** Bash 5, `makeself`, GNU coreutils, GNU Make, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-09-02-create-aura-npx-design.md`

This plan implements **workstream (b)** of that spec only. Workstreams (a) the npm package,
(c) the payload auto-update, and (d) the `config.Load()` fix are separate plans. (b) is
first because the spec's v1 mechanism looked obviously correct and was not; nothing should
be built on this until it is proven.

## Global Constraints

- `scripts/install.sh` runs under `set -euo pipefail` (line 17). Every bracket test whose
  false branch must not abort the script has to be an `if`, never `[ ... ] && ...` — a false
  `&&` list exits the shell with the test's status. This footgun already shipped once
  (b8330b822) and silently ended every non-gvisor install.
- `install.sh` must keep working **standalone from a repo checkout** and under
  `curl | bash`. Neither has `AURA_PAYLOAD_DIR` set; both must take the network branch.
- No file may exceed 600 LOC (project rule). `install.sh` is 690 today and is *not* being
  restructured by this plan; do not add bulk to it beyond what the tasks specify.
- Shell tests follow the existing convention: `#!/usr/bin/env bash`, `set -euo pipefail`,
  `repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"`, `mktemp -d` fixtures with
  a `trap ... EXIT`, and a final `echo "ok: ..."` line. See
  `scripts/fetch_embedding_model_test.sh`.
- Payload is exactly the 25 files named by `download_file` calls at `install.sh:624-651`,
  measured 2026-09-02 at 160,603 bytes total, `compose.yaml` 82,877 of them.

## File Structure

| File | Responsibility |
|---|---|
| `scripts/install.sh` (modify) | unchanged responsibility; gains a sourcing guard, a payload-aware `download_file`, and a `--config` intake |
| `scripts/install_lib_test.sh` (create) | sources `install.sh` and drives its functions one at a time |
| `scripts/build_installer.sh` (create) | wraps `makeself` to produce the artifact; the only place packaging flags live |
| `scripts/payload_manifest.txt` (create) | committed sha256 of each of the 25 payload files — the staleness gate's reference |
| `scripts/payload_manifest_gate.sh` (create) | recomputes the manifest and fails on drift |
| `Makefile` (modify) | `installer-artifact` and `payload-manifest` targets |
| `.github/workflows/ci.yml` (modify) | runs the two new shell tests and the manifest gate |

---

### Task 1: Make `install.sh` sourceable

Nothing in this plan can be tested until the script's functions can be reached without
running an install. `install.sh` currently executes top-to-bottom: the last function
definition closes at line 604 and the imperative section starts at line 606 with
`preflight_hw`.

**Files:**
- Modify: `scripts/install.sh:605` (insert before `preflight_hw`)
- Test: `scripts/install_lib_test.sh` (create)

**Interfaces:**
- Consumes: nothing.
- Produces: `install.sh` can be `source`d, defining every function and exiting before any
  side effect. Later tasks rely on this to test `download_file` and the config parser.

- [ ] **Step 1: Write the failing test**

Create `scripts/install_lib_test.sh`:

```bash
#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Sourcing must define the functions and do nothing else. If the guard is missing the
# source runs the whole installer, which needs root and Docker and would hang CI.
# shellcheck source=/dev/null
source "$repo_root/scripts/install.sh"

declare -F download_file >/dev/null || { echo "FAIL: download_file undefined after source" >&2; exit 1; }
declare -F ensure_env_default >/dev/null || { echo "FAIL: ensure_env_default undefined after source" >&2; exit 1; }
declare -F set_env_value >/dev/null || { echo "FAIL: set_env_value undefined after source" >&2; exit 1; }

echo "ok: install.sh sources cleanly and defines its functions"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bash scripts/install_lib_test.sh`
Expected: FAIL — without the guard the source proceeds into `preflight_hw` and beyond,
either erroring on a missing dependency or attempting a real install.

- [ ] **Step 3: Insert the guard**

In `scripts/install.sh`, immediately after the closing `}` of the last function (line 604)
and before `preflight_hw` (line 606), insert:

```bash
# Sourcing this script defines its functions and stops here; executing it falls through and
# installs. The tests drive one function at a time, which is impossible while the file is
# imperative top-to-bottom. Under `curl | bash` BASH_SOURCE[0] is unset, so it defaults to
# $0 and the comparison is equal -- that path still installs, which is the whole point of
# it. The :- is required because line 17 sets `-u`.
if [ "${BASH_SOURCE[0]:-$0}" != "$0" ]; then
  return 0
fi
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bash scripts/install_lib_test.sh`
Expected: `ok: install.sh sources cleanly and defines its functions`

- [ ] **Step 5: Verify executing still works**

Run: `bash scripts/install.sh --help`
Expected: the usage block, exit 0. This proves the guard does not break execution.

- [ ] **Step 6: Commit**

```bash
git add scripts/install.sh scripts/install_lib_test.sh
git commit -m "test(install): make install.sh sourceable so its functions can be tested

The script is imperative top-to-bottom, so no function in it has ever been reachable
from a test without running a real install as root. The guard returns on source and
falls through on execute; under curl | bash BASH_SOURCE[0] is unset and defaults to \$0,
so that path is unchanged."
```

---

### Task 2: `download_file` prefers a local payload

**Files:**
- Modify: `scripts/install.sh:205-213`
- Test: `scripts/install_lib_test.sh` (extend)

**Interfaces:**
- Consumes: Task 1's sourcing guard.
- Produces: `download_file <src> <dst>` copies from `$AURA_PAYLOAD_DIR/<src>` when that
  variable is non-empty, and falls back to `curl "${RAW_BASE}/<src>"` otherwise. Task 5's
  artifact sets the variable; nothing else does.

- [ ] **Step 1: Write the failing test**

Append to `scripts/install_lib_test.sh`, before its final `echo`:

```bash
fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT

mkdir -p "$fixture_root/payload/observability/tempo" "$fixture_root/out"
printf 'compose from payload' > "$fixture_root/payload/compose.yaml"
printf 'tempo from payload' > "$fixture_root/payload/observability/tempo/tempo.yml"

# With the payload dir set, nothing may touch the network. RAW_BASE points at a port
# nothing listens on, so a curl fallback would fail the test loudly instead of silently
# passing against a real fetch.
AURA_PAYLOAD_DIR="$fixture_root/payload" \
RAW_BASE="http://127.0.0.1:1/unreachable" \
  download_file compose.yaml "$fixture_root/out/compose.yaml"
grep -q 'compose from payload' "$fixture_root/out/compose.yaml" \
  || { echo "FAIL: download_file did not copy from AURA_PAYLOAD_DIR" >&2; exit 1; }

# Nested paths must survive, and the destination directory must be created.
AURA_PAYLOAD_DIR="$fixture_root/payload" \
RAW_BASE="http://127.0.0.1:1/unreachable" \
  download_file observability/tempo/tempo.yml "$fixture_root/out/observability/tempo/tempo.yml"
grep -q 'tempo from payload' "$fixture_root/out/observability/tempo/tempo.yml" \
  || { echo "FAIL: download_file lost a nested payload path" >&2; exit 1; }

# Unset, it must still take the network branch -- this is the standalone checkout and the
# curl | bash path, and an unreachable RAW_BASE must therefore FAIL.
if ( AURA_PAYLOAD_DIR="" RAW_BASE="http://127.0.0.1:1/unreachable" \
     download_file compose.yaml "$fixture_root/out/net.yaml" ) 2>/dev/null; then
  echo "FAIL: download_file did not fall back to the network when AURA_PAYLOAD_DIR is empty" >&2
  exit 1
fi

echo "ok: download_file prefers the payload and still falls back to the network"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bash scripts/install_lib_test.sh`
Expected: FAIL — `download_file did not copy from AURA_PAYLOAD_DIR`, because the current
body only curls.

- [ ] **Step 3: Replace the function body**

In `scripts/install.sh`, replace lines 205-213 with:

```bash
# The artifact (scripts/build_installer.sh) unpacks the payload beside this script and
# points AURA_PAYLOAD_DIR at it, so an appliance install fetches nothing: the 25 files it
# used to pull from a MOVING git ref, as root, travel with the installer instead. Unset --
# a repo checkout, or curl | bash -- keeps the network branch, so both entry points still
# work from one implementation.
download_file() {
  src="$1"
  dst="$2"
  mkdir -p "$(dirname "$dst")"
  if [ -n "${AURA_PAYLOAD_DIR:-}" ]; then
    cp "$AURA_PAYLOAD_DIR/$src" "$dst" || {
      echo "FAIL: payload is missing ${src}" >&2
      exit 1
    }
    return 0
  fi
  curl -fsSL "${RAW_BASE}/${src}" -o "$dst" || {
    echo "FAIL: could not fetch ${RAW_BASE}/${src}" >&2
    exit 1
  }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bash scripts/install_lib_test.sh`
Expected: both `ok:` lines.

- [ ] **Step 5: Commit**

```bash
git add scripts/install.sh scripts/install_lib_test.sh
git commit -m "feat(install): let download_file take its payload from disk

Twenty-five files were fetched from raw.githubusercontent at \${AURA_INSTALL_REF:-master}
-- a moving ref -- and the flow runs as root. The artifact now unpacks them beside the
script and sets AURA_PAYLOAD_DIR. Unset, the curl branch is unchanged, so a repo checkout
and curl | bash keep working from the same implementation."
```

---

### Task 3: `--config` intake

**Files:**
- Modify: `scripts/install.sh:37-56` (argument loop) and the variable block at `:22-24`
- Test: `scripts/install_config_test.sh` (create)

**Interfaces:**
- Consumes: Task 1's sourcing guard.
- Produces: `parse_install_config <path>` sets `CFG_INSTALL_DIR`, `CFG_APPLIANCE`,
  `CFG_GVISOR`, `CFG_LLM_PROVIDER`, `CFG_LLM_BASE_URL`, `CFG_LLM_MODEL`,
  `CFG_OPENROUTER_API_KEY`, `CFG_EMBED_IMAGE`, `CFG_EMBED_NGL`. Absent keys become the
  empty string. Task 4 consumes these.

- [ ] **Step 1: Write the failing test**

Create `scripts/install_config_test.sh`:

```bash
#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT

# shellcheck source=/dev/null
source "$repo_root/scripts/install.sh"

conf="$fixture_root/install.conf"
{
  echo "format=1"
  echo "install_dir_base64=$(printf '/opt/aura' | base64 | tr -d '\n')"
  echo "appliance=true"
  echo "gvisor=false"
  echo "llm_provider_base64=$(printf 'ollama' | base64 | tr -d '\n')"
  echo "llm_base_url_base64=$(printf 'http://host.docker.internal:11434/v1' | base64 | tr -d '\n')"
  echo "llm_model_base64=$(printf 'any/model-the-operator-pulled:v1' | base64 | tr -d '\n')"
  echo "openrouter_api_key_base64="
  echo "embed_image_base64=$(printf 'ghcr.io/ggml-org/llama.cpp:server' | base64 | tr -d '\n')"
  echo "embed_ngl_base64=$(printf '0' | base64 | tr -d '\n')"
} > "$conf"

parse_install_config "$conf"

[ "$CFG_INSTALL_DIR" = "/opt/aura" ] || { echo "FAIL: install_dir=$CFG_INSTALL_DIR" >&2; exit 1; }
[ "$CFG_APPLIANCE" = "true" ] || { echo "FAIL: appliance=$CFG_APPLIANCE" >&2; exit 1; }
[ "$CFG_LLM_PROVIDER" = "ollama" ] || { echo "FAIL: provider=$CFG_LLM_PROVIDER" >&2; exit 1; }
[ "$CFG_LLM_BASE_URL" = "http://host.docker.internal:11434/v1" ] || { echo "FAIL: base_url=$CFG_LLM_BASE_URL" >&2; exit 1; }
[ "$CFG_EMBED_NGL" = "0" ] || { echo "FAIL: ngl=$CFG_EMBED_NGL" >&2; exit 1; }
# An empty secret must stay empty, not become the literal "base64 of nothing".
[ -z "$CFG_OPENROUTER_API_KEY" ] || { echo "FAIL: key should be empty, got $CFG_OPENROUTER_API_KEY" >&2; exit 1; }

# A relative path must be refused: the config carries secrets and is resolved as root, so
# resolving it against an unknown cwd is how the wrong file gets read.
if ( parse_install_config "relative/install.conf" ) 2>/dev/null; then
  echo "FAIL: a relative config path was accepted" >&2
  exit 1
fi

# An unknown format must be refused rather than silently half-parsed.
printf 'format=99\n' > "$fixture_root/future.conf"
if ( parse_install_config "$fixture_root/future.conf" ) 2>/dev/null; then
  echo "FAIL: an unknown config format was accepted" >&2
  exit 1
fi

echo "ok: parse_install_config reads the v1 format and fails closed"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bash scripts/install_config_test.sh`
Expected: FAIL with `parse_install_config: command not found`.

- [ ] **Step 3: Add the parser and the flag**

In `scripts/install.sh`, add near the other variable defaults (after line 24):

```bash
CONFIG_FILE=""
CFG_INSTALL_DIR=""; CFG_APPLIANCE=""; CFG_GVISOR=""
CFG_LLM_PROVIDER=""; CFG_LLM_BASE_URL=""; CFG_LLM_MODEL=""
CFG_OPENROUTER_API_KEY=""; CFG_EMBED_IMAGE=""; CFG_EMBED_NGL=""
```

Add the function beside the other helpers (after `download_file`):

```bash
# The wizard hands its answers over in a file, never in argv: an API key on a command line
# is readable in the process table and lands in shell history. Values are base64 so a
# newline or a shell metacharacter in a model name cannot break the format.
parse_install_config() {
  path="$1"
  case "$path" in
    /*) ;;
    *) echo "FAIL: --config requires an absolute path" >&2; exit 2 ;;
  esac
  [ -f "$path" ] || { echo "FAIL: config not found: $path" >&2; exit 2; }
  head -n 1 "$path" | grep -qx 'format=1' || {
    echo "FAIL: unsupported config format in $path" >&2
    exit 2
  }
  while IFS='=' read -r key value; do
    case "$key" in
      install_dir_base64) CFG_INSTALL_DIR="$(config_decode "$value")" ;;
      appliance) CFG_APPLIANCE="$value" ;;
      gvisor) CFG_GVISOR="$value" ;;
      llm_provider_base64) CFG_LLM_PROVIDER="$(config_decode "$value")" ;;
      llm_base_url_base64) CFG_LLM_BASE_URL="$(config_decode "$value")" ;;
      llm_model_base64) CFG_LLM_MODEL="$(config_decode "$value")" ;;
      openrouter_api_key_base64) CFG_OPENROUTER_API_KEY="$(config_decode "$value")" ;;
      embed_image_base64) CFG_EMBED_IMAGE="$(config_decode "$value")" ;;
      embed_ngl_base64) CFG_EMBED_NGL="$(config_decode "$value")" ;;
    esac
  done < "$path"
}

config_decode() {
  [ -n "$1" ] || { printf ''; return 0; }
  printf '%s' "$1" | base64 -d
}
```

In the argument loop (line 37), add before the `-h|--help` case:

```bash
    --config)
      [ "$#" -ge 2 ] || { echo "FAIL: --config requires a file" >&2; exit 2; }
      [ -z "$CONFIG_FILE" ] || { echo "FAIL: --config may only be supplied once" >&2; exit 2; }
      CONFIG_FILE="$2"
      shift
      ;;
```

And extend `usage()` (line 27) with:

```
  --config PATH  read answers from an absolute-path config file (see the spec)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bash scripts/install_config_test.sh`
Expected: `ok: parse_install_config reads the v1 format and fails closed`

- [ ] **Step 5: Commit**

```bash
git add scripts/install.sh scripts/install_config_test.sh
git commit -m "feat(install): read answers from a --config file instead of flags

A wizard cannot pass an API key as an argument -- argv is readable in the process
table and lands in shell history -- so the answers arrive in a 0600 file with base64
values. Absolute path only, accepted once, unknown format refused: this file is read
as root and resolving it against an unknown cwd is how the wrong one gets read."
```

---

### Task 4: Apply the config through the existing `.env` helpers

**Files:**
- Modify: `scripts/install.sh` (call site after argument parsing, and after
  `write_env_if_missing` runs at `:656`)
- Test: `scripts/install_config_test.sh` (extend)

**Interfaces:**
- Consumes: Task 3's `CFG_*` variables; the existing `set_env_value` (`:308`) and
  `ensure_env_default` (`:322`).
- Produces: `apply_install_config` writes the non-empty `CFG_*` values into `.env`. Empty
  values are left alone, so an operator's existing choice is never cleared.

- [ ] **Step 1: Write the failing test**

Append to `scripts/install_config_test.sh` before its final `echo`:

```bash
env_dir="$fixture_root/envtest"
mkdir -p "$env_dir"
cd "$env_dir"
printf 'AURA_LLM_PROVIDER=openrouter\nOPENROUTER_API_KEY=sk-operator-choice\n' > .env

CFG_LLM_PROVIDER="ollama"
CFG_LLM_BASE_URL="http://host.docker.internal:11434/v1"
CFG_LLM_MODEL="any/model-the-operator-pulled:v1"
CFG_OPENROUTER_API_KEY=""
CFG_EMBED_IMAGE=""
CFG_EMBED_NGL=""
apply_install_config

grep -qx 'AURA_LLM_PROVIDER=ollama' .env || { echo "FAIL: provider not applied" >&2; exit 1; }
grep -qx 'AURA_LLM_BASE_URL=http://host.docker.internal:11434/v1' .env || { echo "FAIL: base url not applied" >&2; exit 1; }
grep -qx 'AURA_LLM_MODEL=any/model-the-operator-pulled:v1' .env || { echo "FAIL: model not applied" >&2; exit 1; }
# An empty config value must NOT clear what the operator already had.
grep -qx 'OPENROUTER_API_KEY=sk-operator-choice' .env || { echo "FAIL: an empty config value cleared an operator's key" >&2; exit 1; }
cd "$repo_root"

echo "ok: apply_install_config writes only what the config actually carries"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bash scripts/install_config_test.sh`
Expected: FAIL with `apply_install_config: command not found`.

- [ ] **Step 3: Add the applier and wire it**

Add beside `parse_install_config`:

```bash
# Only non-empty values are written. An absent key means "the wizard did not ask", not
# "clear it": clearing is how a re-run would wipe a key the operator set by hand, and
# install.sh's whole contract is that re-running preserves explicit settings.
apply_install_config() {
  if [ -n "$CFG_LLM_PROVIDER" ]; then set_env_value AURA_LLM_PROVIDER "$CFG_LLM_PROVIDER"; fi
  if [ -n "$CFG_LLM_BASE_URL" ]; then set_env_value AURA_LLM_BASE_URL "$CFG_LLM_BASE_URL"; fi
  if [ -n "$CFG_LLM_MODEL" ]; then set_env_value AURA_LLM_MODEL "$CFG_LLM_MODEL"; fi
  if [ -n "$CFG_OPENROUTER_API_KEY" ]; then set_env_value OPENROUTER_API_KEY "$CFG_OPENROUTER_API_KEY"; fi
  if [ -n "$CFG_EMBED_IMAGE" ]; then set_env_value AURA_EMBED_IMAGE "$CFG_EMBED_IMAGE"; fi
  if [ -n "$CFG_EMBED_NGL" ]; then set_env_value AURA_EMBED_NGL "$CFG_EMBED_NGL"; fi
}
```

Immediately after the argument-parsing loop ends (line 56), add:

```bash
if [ -n "$CONFIG_FILE" ]; then
  parse_install_config "$CONFIG_FILE"
  if [ -n "$CFG_INSTALL_DIR" ]; then INSTALL_DIR="$CFG_INSTALL_DIR"; fi
  if [ "$CFG_APPLIANCE" = "true" ]; then APPLIANCE=1; fi
  if [ "$CFG_GVISOR" = "true" ]; then APPLIANCE=1; GVISOR=1; fi
fi
```

And immediately after the existing `write_env_if_missing` call (line 656), add:

```bash
apply_install_config
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bash scripts/install_config_test.sh`
Expected: all three `ok:` lines.

- [ ] **Step 5: Commit**

```bash
git add scripts/install.sh scripts/install_config_test.sh
git commit -m "feat(install): apply the config through the existing .env helpers

The wizard does not write .env; install.sh already owns it through set_env_value and
ensure_env_default, which are idempotent and preserve explicit operator choices. Only
non-empty config values are written, so an absent key means the wizard did not ask --
never that the operator's own value should be cleared."
```

---

### Task 5: Build the artifact with makeself

**Files:**
- Create: `scripts/build_installer.sh`
- Modify: `Makefile` (add `installer-artifact`)
- Test: `scripts/build_installer_test.sh` (create)

**Interfaces:**
- Consumes: Tasks 2 and 4 — the artifact is only correct once `download_file` reads the
  payload and `--config` exists.
- Produces: `scripts/build_installer.sh <output.run>` writes a makeself archive whose
  startup script sets `AURA_PAYLOAD_DIR` to the extraction directory and execs
  `install.sh` with the caller's arguments.

- [ ] **Step 1: Write the failing test**

Create `scripts/build_installer_test.sh`:

```bash
#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# NO SKIP-AS-GREEN: a missing makeself in CI must redden the job, not pass it silently.
# Locally it still skips, so a developer without the tool is not blocked.
if ! command -v makeself >/dev/null 2>&1; then
  if [ -n "${CI:-}" ]; then
    echo "FAIL: makeself is required in CI (apt-get install -y makeself)" >&2
    exit 1
  fi
  echo "SKIP: makeself is not installed" >&2
  exit 0
fi

fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT
artifact="$fixture_root/install-appliance.run"

bash "$repo_root/scripts/build_installer.sh" "$artifact"
[ -x "$artifact" ] || { echo "FAIL: artifact is not executable" >&2; exit 1; }

# makeself embeds checksums; --check validates the archive against them.
"$artifact" --check >/dev/null || { echo "FAIL: artifact failed its own integrity check" >&2; exit 1; }

# The payload must round-trip byte-for-byte. A silently truncated or re-encoded file is
# the failure this test exists to catch.
extract="$fixture_root/extracted"
"$artifact" --noexec --keep --target "$extract" >/dev/null
while read -r rel; do
  cmp "$repo_root/$rel" "$extract/$rel" \
    || { echo "FAIL: payload differs for $rel" >&2; exit 1; }
done < <(grep '^download_file ' "$repo_root/scripts/install.sh" | awk '{print $2}')

echo "ok: the artifact self-checks and its payload round-trips"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bash scripts/build_installer_test.sh`
Expected: FAIL — `scripts/build_installer.sh` does not exist. (If makeself is absent the
test SKIPs; install it with `apt-get install -y makeself` before proceeding.)

- [ ] **Step 3: Write the builder**

Create `scripts/build_installer.sh`:

```bash
#!/usr/bin/env bash
#
# Packs install.sh plus the 25 files it installs into one self-extracting archive.
#
# makeself rather than a hand-rolled base64 blob: it embeds checksums and validates them on
# extraction, and — the property that matters — it runs the startup script with the working
# directory set to the extracted files. An earlier design emitted a download_file override
# ahead of install.sh verbatim; install.sh defines download_file itself, bash takes the last
# definition, and every download would have gone back to the network. There is no code
# generation here for exactly that reason.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output="${1:?usage: build_installer.sh OUTPUT.run}"

command -v makeself >/dev/null 2>&1 || {
  echo "FAIL: makeself is required (apt-get install -y makeself)" >&2
  exit 1
}

staging="$(mktemp -d)"
trap 'rm -rf "$staging"' EXIT

# The payload list is derived from install.sh itself, so a file added there cannot be
# forgotten here.
while read -r rel; do
  mkdir -p "$staging/$(dirname "$rel")"
  cp "$repo_root/$rel" "$staging/$rel"
done < <(grep '^download_file ' "$repo_root/scripts/install.sh" | awk '{print $2}')

cp "$repo_root/scripts/install.sh" "$staging/install.sh"

cat > "$staging/startup.sh" <<'STARTUP'
#!/usr/bin/env bash
set -euo pipefail
# makeself runs this from the extraction directory, so the payload is right here.
export AURA_PAYLOAD_DIR="$PWD"
exec bash "$PWD/install.sh" "$@"
STARTUP
chmod +x "$staging/startup.sh"

makeself --sha256 --gzip \
  "$staging" "$output" \
  "Aura appliance installer" \
  ./startup.sh

chmod +x "$output"
echo "ok: wrote $output"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bash scripts/build_installer_test.sh`
Expected: `ok: the artifact self-checks and its payload round-trips`

- [ ] **Step 5: Add the Make target**

In `Makefile`, add to the `.PHONY` line and as a target:

```make
installer-artifact:
	bash scripts/build_installer.sh dist/install-appliance.run
```

- [ ] **Step 6: Commit**

```bash
mkdir -p dist && echo 'install-appliance.run' > dist/.gitignore
git add scripts/build_installer.sh scripts/build_installer_test.sh Makefile dist/.gitignore
git commit -m "feat(install): pack the installer and its payload into one makeself archive

The 25 files travel with the script instead of being fetched from a moving ref at root.
makeself embeds checksums and runs its startup script from the extraction directory, so
install.sh starts life beside its payload and needs no generated override -- the earlier
design's override was clobbered by install.sh's own definition of the same function.

The payload list is read out of install.sh, so a file added there cannot be forgotten
here. The artifact is a build output and is not committed."
```

---

### Task 6: Freeze the payload inventory against drift

The artifact is a build output, so it cannot be diffed in CI, and makeself documents no
reproducible-output guarantee. The gate therefore watches the **inputs**.

**Files:**
- Create: `scripts/payload_manifest.txt`, `scripts/payload_manifest_gate.sh`
- Modify: `Makefile` (add `payload-manifest`)

**Interfaces:**
- Consumes: the payload list derived from `install.sh` in Task 5.
- Produces: `scripts/payload_manifest_gate.sh` exits non-zero when the 25 files' hashes, or
  the set of files itself, differ from `scripts/payload_manifest.txt`.

- [ ] **Step 1: Write the failing test**

Create `scripts/payload_manifest_gate.sh`:

```bash
#!/usr/bin/env bash
#
# The artifact ships a copy of 25 repo files. Nothing else notices when one of them changes
# and the artifact is not rebuilt, so an appliance would install last month's compose.yaml
# against this month's images. This gate makes that impossible to do quietly.
#
# It watches the INPUTS, not the artifact: makeself makes no reproducible-output promise,
# so diffing the built archive would flap and be switched off within a fortnight.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="$repo_root/scripts/payload_manifest.txt"

compute() {
  while read -r rel; do
    printf '%s  %s\n' "$(sha256sum "$repo_root/$rel" | cut -d' ' -f1)" "$rel"
  done < <(grep '^download_file ' "$repo_root/scripts/install.sh" | awk '{print $2}' | sort)
}

if [ "${1:-}" = "--write" ]; then
  compute > "$manifest"
  echo "ok: wrote $(wc -l < "$manifest") payload hashes"
  exit 0
fi

[ -f "$manifest" ] || { echo "FAIL: $manifest is missing; run make payload-manifest" >&2; exit 1; }

if ! diff -u "$manifest" <(compute); then
  echo "FAIL: the payload changed without its manifest. Run 'make payload-manifest' and commit." >&2
  exit 1
fi
echo "ok: payload matches its manifest ($(wc -l < "$manifest") files)"
```

- [ ] **Step 2: Run it to verify it fails**

Run: `bash scripts/payload_manifest_gate.sh`
Expected: FAIL — `scripts/payload_manifest.txt is missing`.

- [ ] **Step 3: Generate the manifest**

Run: `bash scripts/payload_manifest_gate.sh --write`
Expected: `ok: wrote 25 payload hashes`

- [ ] **Step 4: Verify the gate now passes, and that it catches drift**

```bash
bash scripts/payload_manifest_gate.sh           # expect: ok: payload matches its manifest (25 files)
printf '\n# drift\n' >> observability/tempo/tempo.yml
bash scripts/payload_manifest_gate.sh || echo "gate correctly refused"
git checkout -- observability/tempo/tempo.yml
bash scripts/payload_manifest_gate.sh           # expect: ok again
```

- [ ] **Step 5: Add the Make target**

```make
payload-manifest:
	bash scripts/payload_manifest_gate.sh --write
```

- [ ] **Step 6: Commit**

```bash
git add scripts/payload_manifest.txt scripts/payload_manifest_gate.sh Makefile
git commit -m "feat(install): fail closed when the payload changes without its manifest

The artifact carries copies of 25 repo files, and nothing noticed when one changed and
the artifact was not rebuilt -- an appliance would install last month's compose.yaml
against this month's images, which is the drift Immich warns about.

The gate watches the inputs, not the built archive: makeself promises no reproducible
output, so diffing the artifact would flap and be disabled."
```

---

### Task 7: Run all of it in CI

**Files:**
- Modify: `.github/workflows/ci.yml` (the job that already runs the shell contract tests,
  around line 88)

**Interfaces:**
- Consumes: Tasks 1, 3, 5 and 6's scripts.
- Produces: nothing later depends on.

- [ ] **Step 1: Add the steps**

In `.github/workflows/ci.yml`, in the step block that runs
`bash scripts/coverage_profile_gate_test.sh` and its siblings, append:

```yaml
          bash scripts/install_lib_test.sh
          bash scripts/install_config_test.sh
          bash scripts/payload_manifest_gate.sh
```

And add a separate step before it, so the artifact test does not silently SKIP:

```yaml
      - name: Install makeself (the artifact packer)
        run: sudo apt-get update && sudo apt-get install -y makeself
      - name: Installer artifact self-check and payload round-trip
        run: bash scripts/build_installer_test.sh
```

- [ ] **Step 2: Verify the SKIP cannot hide a failure**

Run locally: `command -v makeself || echo "absent"`. `build_installer_test.sh` exits 0 with
`SKIP:` when makeself is absent, which is right for a developer laptop and wrong for CI —
the explicit install step above is what stops it being a falsely-green job. This mirrors
the project's no-skip-as-green rule.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: run the installer library, config, artifact and payload-manifest gates

build_installer_test.sh SKIPs without makeself, which is right on a laptop and would be
a falsely-green job here, so CI installs makeself explicitly before running it."
```

---

## Self-Review

**Spec coverage for workstream (b).** The spec's (b) is "makeself packaging, install.sh's
payload-aware `download_file` and `--config` intake, the input-hash staleness gate, the npm
publish wired into `publish-aura-edge.yml`". Tasks 1-7 cover all but the last.

**Gap found and resolved:** the npm publish cannot be in this plan — it publishes a package
that workstream (a) has not built yet. It moves to the end of (a)'s plan, where the package
exists to publish. The spec's deliverable list should be corrected when (a) is planned.

**Placeholders:** none. Every step carries the code or the exact command.

**Type consistency:** `AURA_PAYLOAD_DIR` is set in Task 5's startup script and read in Task
2's `download_file`. `CFG_*` are declared in Task 3 and consumed in Task 4.
`parse_install_config` / `apply_install_config` / `config_decode` are used with the names
they are defined under. The payload list is derived by the same
`grep '^download_file ' | awk '{print $2}'` in Tasks 5, 6 and in Task 5's test.

**One risk this plan does not remove:** Task 5's test compares the extracted payload against
the repo, so it proves the artifact carries what the repo has. It does *not* prove a full
install works from the artifact — that needs Docker, root and a clean host, and belongs to
an end-to-end check on a real target, not to CI.
