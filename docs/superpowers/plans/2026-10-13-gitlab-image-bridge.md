# GitLab image bridge

**Closed (2026-11-26):** done, as the Status below records; its boxes had never been ticked, and are now,
so the plan no longer reads as open.

**Status (2026-10-15):** DONE. Recorded in docs/superpowers/verification/2026-10-14-gitlab-image-bridge.md and 2026-10-15-docker-restart-and-install-dir.md. The milestone closed on 2026-10-15.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** While GitHub blocks `ghcr.io/chetto1983`:
- every appliance updates from `registry.gitlab.com/chetto1983/aura`;
- a fresh install starts there;
- Andrea's frozen appliance gets there with one command.

The same mechanism brings every host back to ghcr when GitHub returns.

**Architecture:** four parts.
- **(A) Publishing.** `scripts/publish_images.sh` builds the nine repo and fork images on the
  operator's machine and pushes them to the home. Moving tags move last, with `aura:edge` the
  last of all. A test keeps its image list equal to the GitHub workflows'.
- **(B) Moving hosts.** `deploy/aura-image-update.sh` gains `IMAGE_HOMES` and `IMAGE_HOME`.
  `move_image_registry` moves each `.env` image key once its image pulls from the home. The
  superseded-image cleanup folds both homes into one name, and it now also sees the box images.
  Andrea's one-time patch `deploy/aura-registry-bridge.sh` moves `AURA_IMAGE` only.
- **(C) Fresh installs.** Every shipped default names the home: the installer, compose,
  `.env.example`, create-aura and the docs. A Go test reads `IMAGE_HOME` from the updater and
  fails on any other home in a shipped file.
- **(D) Closing.** The images are published and whisper is mirrored. Then the lab VM runs
  Andrea's patch, a clean container proves anonymous reachability, and create-aura goes to npm.

**Tech Stack:** bash (updater, patch, publisher, shell tests); Go 1.27 (artifact tests);
TypeScript + vitest (create-aura); Docker with the `default` buildx builder; glab v1.122.0;
skopeo.

**Spec:** `docs/superpowers/specs/2026-10-13-gitlab-image-bridge-design.md`. It was agreed on
2026-10-13 and revised the same day twice: once for fresh installs, once for authentication
through glab.

## Global Constraints

- **The homes are exactly two.** `ghcr.io/chetto1983` and
  `registry.gitlab.com/chetto1983/aura`.
  - `IMAGE_HOME` is the GitLab one while the bridge lasts.
  - Image names and tags never change: a move is a prefix swap.
  - The whisper digest stays `sha256:c6c6311af66c2fda59645745628edbda1737c619dcd30ce7ea2c00e5e5a1d9d6`.
- **Logs never carry `.env` values.** They carry key names only, as `retire_env_keys` does.
  GNU `sed -i` keeps `.env` at 0600, with its owner.
- **A key moves only after its image pulls from the home.** A failed pull changes nothing and
  is retried next tick.
- **Go runs only in WSL.** Never `go build ./...` or `go test ./...` locally. After every Go
  edit, run `go vet` and `go test -race` on the touched packages.
- **Shell tests run in WSL with `bash scripts/<name>_test.sh`.**
- **Commits.** One commit per task, with the why. Commit from WSL with an explicit pathspec:
  `LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -F <file> -- <paths>`.
  Never `--no-verify`.
- **Payload files.** `compose.yaml`, `deploy/*`, `scripts/install_env.sh` and
  `scripts/appliance_posture.sh` are payload files. A task that changes one commits it first.
  Then `make payload-manifest` writes `scripts/payload_manifest.txt` in a second commit.
- **The operator does every login.** That covers glab, npm and Cloudflare. Claude never handles
  their credentials.
- **No CI while the GitHub account is flagged.** The local gates are the only gate, and the
  ledger says so.
- **The whisper pin.** It is not bumped while the bridge lasts, unless the new digest is
  mirrored to the home first.

## Review Focus

1. **A key moves while its image is missing from the home.** The host would then fail
   `compose up` on the next boot. Pinned by `move_image_registry`'s failed-pull case in Task B1,
   where `.env` stays byte-identical.
2. **Andrea's updater is older than the payload sync.** The patch would rewrite `AURA_IMAGE`
   for an updater that can never install its successor. Pinned by the patch's "updater too
   old" refusal in Task B3.
3. **`aura:edge` moves before the images it needs.** That is the 2026-09-27 failure. Pinned by
   the publisher's ordering case in Task A2: every push precedes the first `imagetools create`,
   and `aura:edge` is the last of them.
4. **Old ghcr images fill the disk after the move.** The VM has 11 GB free. Pinned by the
   folding and box-image cases in Task B2, and measured live in Task D2.
5. **A shipped file still names ghcr.** A fresh install would then pull from a 403. Pinned by
   `TestShippedDefaultsNameTheUpdatersImageHome` in Task C1, which is also proved able to fail.

## Decisions taken while writing this plan

- **Release installs are out of the bridge.** `AURA_INSTALL_REF=vX.Y.Z` and the Windows
  manual `AURA_IMAGE=…:vX.Y.Z` name release tags. `release.yml` publishes those to ghcr only,
  and the publisher pushes `master-<sha>` and the moving tags only. `docs/INSTALL.md` says so.
  The spec's "what this does not establish" gains the line in Task C1.
- **Some files keep their ghcr mentions.** None of them reaches a host:
  - `.goreleaser.yaml` and the GitHub workflows, which are CI;
  - `docker/aura/PROVENANCE.md`, `docs/runbooks/two-identity-live-run.md` and
    `scripts/musr_live_run_preconditions.sh`, which are dev and history docs;
  - the description in the root `package.json`, which names the dead `npx github:` method.
- **The anti-drift test leaves two files out, and reads the updater without its
  `IMAGE_HOMES=` line.** The updater declares both homes by design, and so does the patch.
- **`isThirdPartyImage` takes the homes.** `image_pin_artifacts_test.go:90` treats
  `ghcr.io/chetto1983/` as ours. It now takes the homes read from the updater, so GitLab
  moving tags count as ours too.
- **create-aura goes to 0.2.12.** npm has 0.2.10, and the repo's 0.2.11 was never published.
  The bump marks the registry change.
- **The forks are mirrored with `main` only.** `main` is what both forks publish from (spec,
  measured 2026-10-13). The pim fork's `main` is at `32d0c43` since the aura-connect plan.

## File structure

| File | Part | Responsibility |
|---|---|---|
| `scripts/publish_images.sh` | A | build the nine images, push per-commit tags, then move tags |
| `scripts/publish_images_test.sh` | A | drift against the workflows, push-before-move, fail-closed |
| `deploy/aura-image-update.sh` | B, C | `IMAGE_HOMES`, `IMAGE_HOME`, `move_image_registry`, folding, box images, header |
| `deploy/aura-image-update.service` | C | prerequisites comment, `Description` |
| `scripts/aura_image_update_test.sh` | B | move, return trip, folding, box-image cases |
| `deploy/aura-registry-bridge.sh` | B | Andrea's one-time patch |
| `scripts/aura_registry_bridge_test.sh` | B | the patch's refusals, its one-line rewrite, idempotence |
| `cmd/aura/image_home_artifacts_test.go` | C | `imageHomes`, `foreignHomeRefs`, the shipped-defaults test |
| `cmd/aura/image_pin_artifacts_test.go` | C | `isThirdPartyImage(ref, homes)` |
| `cmd/aura/distribution_artifacts_test.go:297` | C | `.env.example` sandbox line |
| `scripts/install.sh`, `scripts/install_env.sh`, `compose.yaml`, `.env.example` | C | defaults name the home |
| `scripts/install_lib_test.sh`, `scripts/cloudflare_update_test.sh`, `scripts/install_remote_access_test.sh` | C | fixtures name the home |
| `packages/create-aura/src/local.ts`, `src/remote.ts`, their two tests, `README.md`, `package.json` | C | required host, version |
| `docs/INSTALL.md`, `docs/cloudflare-remote-access.md` | C | install commands, channels |
| `docs/superpowers/verification/2026-10-1x-gitlab-image-bridge.md` | D | ledger |

---

## Part A — Publishing

### Task A1: The forks get GitLab mirrors

**Files:** none in Aura.

**Interfaces:**
- Produces two public projects:
  - `https://gitlab.com/Chetto1983/aura-pim-mcp.git` (`main` = `32d0c43`);
  - `https://gitlab.com/Chetto1983/whatsapp-mcp.git` (`main` = `1ec0233`).

  `publish_images.sh` clones both by default.

- [x] **Step 1: Clone the whatsapp fork beside the pim one**

```bash
cd "<scratchpad>/forks" && gh repo clone chetto1983/whatsapp-mcp whatsapp-mcp -- --quiet
git -C whatsapp-mcp log -1 --format='%h %s' main
```

Expected: `1ec0233 fix(media): name a Markdown document's type`.

- [x] **Step 2: Create both projects from a neutral directory, then push `main`**

```bash
export PATH="$HOME/go/bin:$PATH"
cd "$(mktemp -d)"
glab repo create Chetto1983/aura-pim-mcp --public --defaultBranch main -d "Aura's fork of calendar-mcp: the PIM sidecar (mirror)."
glab repo create Chetto1983/whatsapp-mcp --public --defaultBranch main -d "Aura's fork of whatsapp-mcp: the WhatsApp sidecar (mirror)."
for f in aura-pim-mcp whatsapp-mcp; do
  git -C "<scratchpad>/forks/$f" remote add gitlab "git@gitlab.com:Chetto1983/$f.git"
  git -C "<scratchpad>/forks/$f" push gitlab main
  git ls-remote "https://gitlab.com/Chetto1983/$f.git" main
done
```

Expected: `32d0c43…` and `1ec0233…`, read anonymously over https.

- [x] **Step 3: No Aura commit.** Ledger the two heads.

---

### Task A2: `scripts/publish_images.sh` builds, gates and publishes in a safe order

**Files:**
- Create: `scripts/publish_images.sh`
- Create: `scripts/publish_images_test.sh`

**Interfaces:**
- Consumes: Task A1's fork URLs; `IMAGE_HOME` from `deploy/aura-image-update.sh` (Task B1),
  read with `sed -n 's/^IMAGE_HOME=//p'`. Until B1 lands, the line is absent, so the script
  requires `AURA_PUBLISH_REGISTRY` and stops without it.
- Produces: `scripts/publish_images.sh [<aura commit>]`. On success it prints, last, one line
  per published reference:
  - `published <registry>/<name>:<tag>`;
  - then `moved <registry>/<name>:<moving tag>`.

- [x] **Step 1: Write the failing test.** Create `scripts/publish_images_test.sh`:

```bash
#!/usr/bin/env bash
#
# Drives scripts/publish_images.sh against a stubbed docker and git. The images it builds must
# be the ones the GitHub workflows build, nothing may be pushed when a build or an image
# contract fails, and no moving tag may move before every per-commit tag is in the registry --
# with aura:edge last, as publish-aura-edge.yml does since 2026-09-27.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# shellcheck source=/dev/null
source "$repo_root/scripts/publish_images.sh"
declare -F main >/dev/null || fail "main undefined after sourcing the publisher"

# The workflows are the reference while GitHub runs them: same Dockerfiles, same edge order.
edge_workflow="$repo_root/.github/workflows/publish-aura-edge.yml"
workflow_files="$(sed -n 's/^ *file: *//p' "$edge_workflow" | sort -u | tr '\n' ' ')"
script_files="$(for e in "${EDGE_IMAGES[@]}"; do echo "${e#*:}"; done | sort -u | tr '\n' ' ')"
[[ "$workflow_files" == "$script_files" ]] || fail "edge Dockerfiles differ: workflow [$workflow_files] script [$script_files]"
workflow_order="$(sed -n 's/^ *for image in \(.*\); do$/\1/p' "$edge_workflow")"
script_order="$(for e in "${EDGE_IMAGES[@]}"; do printf '%s ' "${e%%:*}"; done)"
[[ "$workflow_order " == "$script_order" ]] || fail "edge tag order differs: workflow [$workflow_order] script [$script_order]"
[[ "$(sed -n 's/^ *file: *//p' "$repo_root/.github/workflows/publish-arcadedb-mcp.yml")" == "${ARCADEDB_MCP#*:}" ]] ||
  fail "arcadedb-mcp Dockerfile differs from its workflow"
echo "ok: the publisher builds what the GitHub workflows build, in the edge workflow's order"

calls="$fixture/calls.log"
src="$fixture/src"
mkdir -p "$src/scripts" "$src/fork"
for s in cloudflared_image_contract_test.sh ingest_image_contract_test.sh sandbox_image_contract_test.sh; do
  printf '#!/usr/bin/env bash\n' >"$src/scripts/$s"
done
touch "$src/fork/Dockerfile"
failing_build=""
failing_contract=""
git() {
  echo "git $*" >>"$calls"
  case "$*" in
    *' rev-parse '*) echo 0123456789abcdef0123456789abcdef01234567 ;;
    *' archive '*) tar -c -C "$src" . ;;
    'clone --quiet '*) mkdir -p "${*: -1}" && cp -R "$src/fork/." "${*: -1}" ;;
    *' checkout --quiet '*) ;;
    *) fail "unexpected git $*" ;;
  esac
}
docker() {
  echo "docker $*" >>"$calls"
  if [[ "$1" == build && -n "$failing_build" && "$*" == *"/$failing_build:"* ]]; then return 1; fi
  case "$1 ${2:-}" in
    'build '* | 'push '* | 'buildx imagetools') ;;
    *) fail "unexpected docker $*" ;;
  esac
}
bash() {
  echo "contract $*" >>"$calls"
  [[ -z "$failing_contract" || "$1" != *"$failing_contract"* ]]
}
export AURA_PUBLISH_REGISTRY=registry.example/aura

: >"$calls"
main HEAD >"$fixture/out" || fail "a clean publish failed: $(tail -5 "$calls")"
pushes="$(grep -c '^docker push ' "$calls")"
[[ "$pushes" == 9 ]] || fail "expected 9 per-commit pushes, saw $pushes"
last_push="$(grep -n '^docker push ' "$calls" | tail -1 | cut -d: -f1)"
first_move="$(grep -n '^docker buildx imagetools create' "$calls" | head -1 | cut -d: -f1)"
((last_push < first_move)) || fail "a moving tag moved before every per-commit tag was pushed"
grep '^docker buildx imagetools create' "$calls" | tail -1 | grep -q -- '--tag registry.example/aura/aura:edge ' ||
  fail "aura:edge did not move last: $(grep '^docker buildx' "$calls" | tail -1)"
grep -q '^docker build .*--build-arg VCS_REF=0123456789abcdef0123456789abcdef01234567' "$calls" ||
  fail "aura was not stamped with its commit"
grep -q -- '--tag registry.example/aura/aura-pim-mcp:sidecar ' "$calls" || fail "the pim fork's sidecar tag did not move"
grep -q -- '--tag registry.example/aura/whatsapp-mcp:latest ' "$calls" || fail "the whatsapp fork's latest tag did not move"
echo "ok: every per-commit tag is pushed before any moving tag moves, and aura:edge moves last"

for case in build contract; do
  : >"$calls"
  if [[ "$case" == build ]]; then failing_build=aura-ingest; else failing_contract=sandbox_image_contract_test.sh; fi
  if main HEAD >"$fixture/out" 2>&1; then fail "a failed $case still published"; fi
  ! grep -q '^docker push ' "$calls" || fail "a failed $case pushed: $(grep '^docker push ' "$calls")"
  failing_build="" failing_contract=""
done
echo "ok: a failed build or image contract pushes nothing"
```

- [x] **Step 2: Run it and watch it fail**

Run in WSL: `bash scripts/publish_images_test.sh`
Expected: FAIL. `source` cannot find `scripts/publish_images.sh`.

- [x] **Step 3: Write `scripts/publish_images.sh`**

```bash
#!/usr/bin/env bash
#
# Publishes every appliance image to the image home from the operator's machine, while GitHub
# runs no workflow (docs/superpowers/specs/2026-10-13-gitlab-image-bridge-design.md). It builds
# what .github/workflows/publish-aura-edge.yml and publish-arcadedb-mcp.yml build, the same way,
# plus the two forks; nothing is pushed until every build and image contract passed, and the
# moving tags move only once every per-commit tag is in the registry, aura:edge last.
#
#   scripts/publish_images.sh [<aura commit>]        (default HEAD)
#
# Docker must already reach the registry (`glab auth configure-docker`).
#   AURA_PUBLISH_REGISTRY     default: IMAGE_HOME in deploy/aura-image-update.sh
#   AURA_PUBLISH_PIM_GIT      default: https://gitlab.com/Chetto1983/aura-pim-mcp.git
#   AURA_PUBLISH_WHATSAPP_GIT default: https://gitlab.com/Chetto1983/whatsapp-mcp.git
#   AURA_PUBLISH_FORK_REF     default: main

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")/.." && pwd)"
AURA_SOURCE=https://gitlab.com/Chetto1983/aura

# name:dockerfile, in the order publish-aura-edge.yml moves the edge tags: aura last.
EDGE_IMAGES=(
  aura-caddy:docker/caddy/Dockerfile
  aura-cloudflared:docker/cloudflared/Dockerfile
  aura-ingest:docker/aura-ingest/Dockerfile
  aura-sandbox:docker/aura-sandbox/Dockerfile
  aura-egress:docker/aura-egress/Dockerfile
  aura:docker/aura/Dockerfile
)
ARCADEDB_MCP=aura-arcadedb-mcp:docker/arcadedb-mcp/Dockerfile
# Each fork builds its root Dockerfile and carries both moving tags appliances follow.
FORK_NAMES=(aura-pim-mcp whatsapp-mcp)
FORK_MOVING_TAGS=(sidecar latest)
# image, the variable its contract test reads the image from, the test.
CONTRACTS=(
  "aura-cloudflared AURA_CLOUDFLARED_TEST_IMAGE scripts/cloudflared_image_contract_test.sh"
  "aura-ingest AURA_INGEST_IMAGE scripts/ingest_image_contract_test.sh"
  "aura-sandbox AURA_SANDBOX_IMAGE scripts/sandbox_image_contract_test.sh"
)

fork_git() {
  case "$1" in
    aura-pim-mcp) echo "${AURA_PUBLISH_PIM_GIT:-https://gitlab.com/Chetto1983/aura-pim-mcp.git}" ;;
    whatsapp-mcp) echo "${AURA_PUBLISH_WHATSAPP_GIT:-https://gitlab.com/Chetto1983/whatsapp-mcp.git}" ;;
  esac
}

# <tag> <context dir> <dockerfile relative to it> <revision> <source url> [docker build args]
build_image() {
  local tag="$1" ctx="$2" file="$3" rev="$4" src="$5"
  shift 5
  docker build --platform linux/amd64 -f "${ctx}/${file}" \
    --label "org.opencontainers.image.source=${src}" \
    --label "org.opencontainers.image.revision=${rev}" "$@" -t "${tag}" "${ctx}"
}

main() {
  local registry sha work entry name file fork_sha tag image var test
  local -a pushes=() moves=()
  registry="${AURA_PUBLISH_REGISTRY:-$(sed -n 's/^IMAGE_HOME=//p' "${repo_root}/deploy/aura-image-update.sh")}"
  [[ -n "${registry}" ]] || { echo "publish: no registry (set AURA_PUBLISH_REGISTRY)" >&2; return 1; }
  sha="$(git -C "${repo_root}" rev-parse "${1:-HEAD}^{commit}")"
  work="$(mktemp -d)"
  trap 'rm -rf "${work}"' RETURN
  mkdir -p "${work}/aura"
  git -C "${repo_root}" archive "${sha}" | tar -x -C "${work}/aura"

  for entry in "${EDGE_IMAGES[@]}"; do
    name="${entry%%:*}" file="${entry#*:}" tag="${registry}/${name}:master-${sha}"
    if [[ "${name}" == aura ]]; then
      build_image "${tag}" "${work}/aura" "${file}" "${sha}" "${AURA_SOURCE}" --build-arg "VCS_REF=${sha}"
    else
      build_image "${tag}" "${work}/aura" "${file}" "${sha}" "${AURA_SOURCE}"
    fi
    pushes+=("${tag}")
  done
  name="${ARCADEDB_MCP%%:*}" file="${ARCADEDB_MCP#*:}" tag="${registry}/${name}:${sha}"
  build_image "${tag}" "${work}/aura" "${file}" "${sha}" "${AURA_SOURCE}"
  pushes+=("${tag}")
  moves+=("${registry}/${name}:edge ${tag}")

  for name in "${FORK_NAMES[@]}"; do
    git clone --quiet "$(fork_git "${name}")" "${work}/${name}"
    git -C "${work}/${name}" checkout --quiet "${AURA_PUBLISH_FORK_REF:-main}"
    fork_sha="$(git -C "${work}/${name}" rev-parse HEAD)"
    tag="${registry}/${name}:${fork_sha}"
    build_image "${tag}" "${work}/${name}" Dockerfile "${fork_sha}" "$(fork_git "${name}" | sed 's/\.git$//')"
    pushes+=("${tag}")
    for image in "${FORK_MOVING_TAGS[@]}"; do moves+=("${registry}/${name}:${image} ${tag}"); done
  done

  for entry in "${CONTRACTS[@]}"; do
    read -r name var test <<<"${entry}"
    env "${var}=${registry}/${name}:master-${sha}" bash "${work}/aura/${test}"
  done

  for tag in "${pushes[@]}"; do
    docker push "${tag}"
    echo "published ${tag}"
  done
  # The edge set moves last and together, aura:edge the very last, as publish-aura-edge.yml does.
  for entry in "${EDGE_IMAGES[@]}"; do
    name="${entry%%:*}"
    moves+=("${registry}/${name}:edge ${registry}/${name}:master-${sha}")
  done
  for entry in "${moves[@]}"; do
    read -r tag image <<<"${entry}"
    docker buildx imagetools create --prefer-index=false --tag "${tag}" "${image}"
    echo "moved ${tag}"
  done
}

[[ "${BASH_SOURCE[0]:-$0}" != "$0" ]] || main "$@"
```

- [x] **Step 4: Run the test**

Run in WSL: `bash scripts/publish_images_test.sh`
Expected: three `ok:` lines and exit 0.

- [x] **Step 5: Lint and commit**

```bash
shellcheck scripts/publish_images.sh scripts/publish_images_test.sh
LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -F <file> -- scripts/publish_images.sh scripts/publish_images_test.sh
```

Message: `feat(release): publish appliance images from the operator's machine`. The body says why:
- GitHub runs no workflow since 2026-10-11;
- the script builds what the workflows build, plus the forks;
- nothing is pushed after a failed build or contract;
- `aura:edge` moves last;
- the test keeps the image list equal to the workflows'.

Expected: shellcheck prints nothing, and the hooks are green.

---

## Part B — Moving hosts

### Task B1: The updater moves each image key to its home once the image pulls

**Files:**
- Modify: `deploy/aura-image-update.sh`:
  - after line 50 (`SIDECARS=`), add `IMAGE_HOMES` and `IMAGE_HOME`;
  - at line 213, the cloudflared default;
  - after `ensure_cloudflared_edge_env`, add `move_image_registry`;
  - in `apply_update`, after `ensure_cloudflared_edge_env`, call it.
- Modify: `scripts/aura_image_update_test.sh`, with a new block before the image-cleanup block
  (line 261).
- Modify (second commit): `scripts/payload_manifest.txt`

**Interfaces:**
- Produces:
  - `IMAGE_HOMES=(ghcr.io/chetto1983 registry.gitlab.com/chetto1983/aura)`;
  - `IMAGE_HOME=registry.gitlab.com/chetto1983/aura`;
  - `move_image_registry`, which takes no arguments and reads `INSTALL_DIR/.env`.

  Task A2 reads `IMAGE_HOME` with sed, and Task C1's Go test reads both lines with a regex. So
  both stay on one line each, at column 0.

- [x] **Step 1: Write the failing test.** Insert into `scripts/aura_image_update_test.sh`,
  right before the comment block that starts `# A pin change leaves the previous image behind`:

```bash
# The GitLab bridge (2026-10-13): a key of .env that names a repo-built image under another home
# moves to IMAGE_HOME, keeping name and tag, but only once that image pulls from there. A key
# whose image is not published yet stays where it is, an image under any other registry is never
# touched, the log carries key names only, and flipping IMAGE_HOME brings every key back.
(
  INSTALL_DIR="$fixture/move"
  mkdir -p "$INSTALL_DIR"
  cat >"$INSTALL_DIR/.env" <<'ENV'
POSTGRES_PASSWORD=do-not-print-me
AURA_IMAGE=ghcr.io/chetto1983/aura:edge
AURA_CADDY_IMAGE="ghcr.io/chetto1983/aura-caddy:edge"
AURA_INGEST_IMAGE=ghcr.io/chetto1983/aura-ingest:edge
AURA_SANDBOX_IMAGE=registry.gitlab.com/chetto1983/aura/aura-sandbox:edge
AURA_STT_IMAGE=hwdsl2/whisper-server:latest
ENV
  chmod 600 "$INSTALL_DIR/.env"
  # pull_retrying waits 10 s between tries; the test does not.
  sleep() { :; }

  cp "$INSTALL_DIR/.env" "$fixture/env.before"
  IMAGE_HOME=registry.gitlab.com/chetto1983/aura
  docker() {
    echo "docker $*" >>"$calls"
    return 1
  }
  move_image_registry >/dev/null
  cmp -s "$INSTALL_DIR/.env" "$fixture/env.before" || fail "pulls that all failed still changed .env"

  docker() {
    echo "docker $*" >>"$calls"
    [[ "$1 $2" == 'pull -q' ]] || fail "unexpected docker $*"
    [[ "$3" != *"/aura-ingest:"* ]]
  }
  : >"$calls"
  move_image_registry >"$fixture/move.out"
  [[ "$(cat "$INSTALL_DIR/.env")" == "POSTGRES_PASSWORD=do-not-print-me
AURA_IMAGE=registry.gitlab.com/chetto1983/aura/aura:edge
AURA_CADDY_IMAGE=registry.gitlab.com/chetto1983/aura/aura-caddy:edge
AURA_INGEST_IMAGE=ghcr.io/chetto1983/aura-ingest:edge
AURA_SANDBOX_IMAGE=registry.gitlab.com/chetto1983/aura/aura-sandbox:edge
AURA_STT_IMAGE=hwdsl2/whisper-server:latest" ]] || fail "the move left .env as: $(cat "$INSTALL_DIR/.env")"
  [[ "$(stat -c %a "$INSTALL_DIR/.env")" == 600 ]] || fail "the move widened .env"
  pulled="$(sed -n 's/^docker pull -q //p' "$calls" | sort -u | tr '\n' ' ')"
  [[ "$pulled" == "registry.gitlab.com/chetto1983/aura/aura-caddy:edge registry.gitlab.com/chetto1983/aura/aura-ingest:edge registry.gitlab.com/chetto1983/aura/aura:edge " ]] ||
    fail "pulled: $pulled"
  grep -qx 'env: AURA_IMAGE moved to registry.gitlab.com/chetto1983/aura.' "$fixture/move.out" || fail "a move was not logged"
  grep -q '^env: AURA_INGEST_IMAGE stays put' "$fixture/move.out" || fail "a key left behind was not logged"
  ! grep -q 'do-not-print-me\|ghcr.io/chetto1983/aura-ingest:edge\|hwdsl2' "$fixture/move.out" ||
    fail "the move logged .env values: $(cat "$fixture/move.out")"

  IMAGE_HOME=ghcr.io/chetto1983
  docker() {
    echo "docker $*" >>"$calls"
    [[ "$1 $2" == 'pull -q' ]] || fail "unexpected docker $*"
  }
  move_image_registry >/dev/null
  [[ "$(grep -c '=ghcr.io/chetto1983/' "$INSTALL_DIR/.env")" == 4 ]] ||
    fail "flipping IMAGE_HOME did not bring every key back: $(cat "$INSTALL_DIR/.env")"
  grep -qx 'AURA_STT_IMAGE=hwdsl2/whisper-server:latest' "$INSTALL_DIR/.env" || fail "the return trip touched another registry"
)
echo "ok: an image key moves to its home once its image pulls there, and back when the home flips"

(
  INSTALL_DIR="$fixture/cloudflared"
  mkdir -p "$INSTALL_DIR"
  printf 'AURA_IMAGE=registry.gitlab.com/chetto1983/aura/aura:edge\n' >"$INSTALL_DIR/.env"
  ensure_cloudflared_edge_env >/dev/null
  grep -qx "AURA_CLOUDFLARED_IMAGE=${IMAGE_HOME}/aura-cloudflared:edge" "$INSTALL_DIR/.env" ||
    fail "the cloudflared default does not follow IMAGE_HOME: $(cat "$INSTALL_DIR/.env")"
)
echo "ok: the cloudflared default names IMAGE_HOME"
```

- [x] **Step 2: Run it and watch it fail**

Run: `bash scripts/aura_image_update_test.sh`
Expected: FAIL with `move_image_registry: command not found` (bash exits 127 inside the
subshell) after the earlier `ok:` lines.

- [x] **Step 3: Implement in `deploy/aura-image-update.sh`.** First, after the `SIDECARS=` line,
  add:

```bash
# Every registry path the repo-built images have been published under, and the one they live
# under now. move_image_registry moves a host's .env from any other home to IMAGE_HOME, one key
# at a time, so the constant that sends hosts to the GitLab bridge also brings them back
# (docs/superpowers/specs/2026-10-13-gitlab-image-bridge-design.md).
IMAGE_HOMES=(ghcr.io/chetto1983 registry.gitlab.com/chetto1983/aura)
IMAGE_HOME=registry.gitlab.com/chetto1983/aura
```

  In `ensure_cloudflared_edge_env`, the case line becomes:

```bash
      AURA_CLOUDFLARED_IMAGE) value="${IMAGE_HOME}/aura-cloudflared:edge" ;;
```

  Right after `ensure_cloudflared_edge_env`'s closing brace, add:

```bash
# A key of .env that names a repo-built image under another home moves to IMAGE_HOME, keeping
# its name and tag, but only once that image pulls from there: a host never points at an image
# the home does not serve yet, and a key whose pull fails is tried again next tick. Like
# retire_env_keys it logs key names only; GNU sed keeps .env's mode and owner.
move_image_registry() {
  local env_file="${INSTALL_DIR}/.env" key value home target
  [[ -f "${env_file}" ]] || return 0
  while IFS='=' read -r key value; do
    value="$(tr -d "\"'" <<<"${value}")"
    for home in "${IMAGE_HOMES[@]}"; do
      [[ "${home}" != "${IMAGE_HOME}" && "${value}" == "${home}/"* ]] || continue
      target="${IMAGE_HOME}/${value#"${home}"/}"
      if pull_retrying docker pull -q "${target}" >/dev/null; then
        sed -i "s|^${key}=.*|${key}=${target}|" "${env_file}"
        echo "env: ${key} moved to ${IMAGE_HOME}."
      else
        echo "env: ${key} stays put until its image pulls from ${IMAGE_HOME}."
      fi
    done
  done < <(grep -E '^AURA_([A-Z0-9_]+_)?IMAGE=' "${env_file}" || true)
}
```

  In `apply_update`, the two lines become three:

```bash
  retire_env_keys
  ensure_cloudflared_edge_env
  move_image_registry
```

- [x] **Step 4: Run the test**

Run: `bash scripts/aura_image_update_test.sh`
Expected: every `ok:` line, the two new ones included, and exit 0. The dispatch test at the top
still passes: its fixture has no `.env`, so `move_image_registry` returns at once.

- [x] **Step 5: Run the other updater suites**

Run: `bash scripts/aura_update_consent_test.sh && bash scripts/cloudflare_update_test.sh`
Expected: `aura_update_consent_test.sh` passes. `cloudflare_update_test.sh` FAILS: its `.env`
names `ghcr.io/chetto1983/aura:edge`, so the real updater now pulls the GitLab aura, and its
strict docker stub refuses `pull -q`. Task C1 moves that fixture to the home. Ledger the
expected red here, and go on to Step 6.

- [x] **Step 6: Commit, then the manifest**

```bash
LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -F <file> -- deploy/aura-image-update.sh scripts/aura_image_update_test.sh
make payload-manifest
LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -m "chore(payload): record the updater that moves image keys to their home" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>" -- scripts/payload_manifest.txt
```

Message for the first commit: `feat(updater): move each image key to the image home once it
pulls`. The body names:
- the flag and the 403;
- the one-key-at-a-time rule, and why: a host never points at an unpublished image;
- the return trip through `IMAGE_HOME`.

---

### Task B2: Old images leave the disk after a move, box images included

**Files:**
- Modify: `deploy/aura-image-update.sh`:
  - `image_repository` (line 227);
  - `remove_superseded_images` (line 247) and its comment.
- Modify: `scripts/aura_image_update_test.sh`, with a new block right after the existing
  image-cleanup block's `ok:` line.
- Modify (second commit): `scripts/payload_manifest.txt`

**Interfaces:**
- Consumes: `IMAGE_HOMES` (Task B1).
- Produces: `image_repository` returns the same name for a reference under either home.

- [x] **Step 1: Write the failing test.** Insert after the line
  `echo "ok: images no pin names any more are removed, untagged ones by ID, and a refusal is reported"`:

```bash
# After the bridge moved a host, the ghcr copy of every image is superseded by its GitLab pin:
# the two homes are one repository to the cleanup. The box images live in .env, not compose,
# and are pinned the same way. A ghcr name on the same image ID as its pin (whisper keeps its
# digest) is skipped like any pinned image.
[[ "$(image_repository registry.gitlab.com/chetto1983/aura/aura-caddy:edge)" == "$(image_repository ghcr.io/chetto1983/aura-caddy:edge)" ]] ||
  fail "the two homes are two repositories: $(image_repository registry.gitlab.com/chetto1983/aura/aura-caddy:edge)"
[[ "$(image_repository registry.gitlab.com/chetto1983/aura/aura:edge)" == chetto1983/aura ]] ||
  fail "the GitLab aura is $(image_repository registry.gitlab.com/chetto1983/aura/aura:edge)"
(
  INSTALL_DIR="$fixture/bridge-images"
  mkdir -p "$INSTALL_DIR"
  printf '%s\n' AURA_SANDBOX_IMAGE=registry.gitlab.com/chetto1983/aura/aura-sandbox:edge \
    AURA_SANDBOX_EGRESS_IMAGE=registry.gitlab.com/chetto1983/aura/aura-egress:edge >"$INSTALL_DIR/.env"
  local_images=(
    "registry.gitlab.com/chetto1983/aura/aura-caddy edge sha256:caddy-new"
    "ghcr.io/chetto1983/aura-caddy edge sha256:caddy-old"
    "registry.gitlab.com/chetto1983/aura/aura-sandbox edge sha256:box-new"
    "ghcr.io/chetto1983/aura-sandbox edge sha256:box-old"
    "registry.gitlab.com/chetto1983/aura/aura-egress edge sha256:egress-new"
    "ghcr.io/chetto1983/aura-egress edge sha256:egress-old"
    "registry.gitlab.com/chetto1983/aura/aura-whisper-server <none> sha256:c6c6"
    "ghcr.io/chetto1983/aura-whisper-server <none> sha256:c6c6"
    "aura-sandbox lab-browser-sidecar-20261011-03 sha256:lab-box"
  )
  compose_images=(
    registry.gitlab.com/chetto1983/aura/aura-caddy:edge
    registry.gitlab.com/chetto1983/aura/aura-whisper-server:2026.10.08.1056@sha256:c6c6
  )
  : >"$calls"
  remove_superseded_images >"$fixture/bridge-images.out"
  removed="$(sed -n 's/^docker rmi //p' "$calls" | LC_ALL=C sort | tr '\n' ' ')"
  [[ "$removed" == "ghcr.io/chetto1983/aura-caddy:edge ghcr.io/chetto1983/aura-egress:edge ghcr.io/chetto1983/aura-sandbox:edge " ]] ||
    fail "after the bridge, removal attempts were: $removed"
)
echo "ok: after a move the ghcr copies are superseded by their GitLab pins, the box images too"
```

  The block reuses the `docker()`, `local_image_id` and arrays of the block above it. It
  redefines `local_images` and `compose_images` inside its subshell, so the outer test is
  untouched.

- [x] **Step 2: Run it and watch it fail**

Run: `bash scripts/aura_image_update_test.sh`
Expected: FAIL at `the two homes are two repositories: chetto1983/aura/aura-caddy`.

- [x] **Step 3: Implement.** `image_repository` becomes:

```bash
# [registry/]path[:tag][@digest] -> path. The registry host (a first component with a
# dot, a port or "localhost") and Docker Hub's implicit library/ are dropped, so a pin
# that moved registry -- searxng/searxng to ghcr.io/searxng/searxng on 2026-10-07 --
# still supersedes the image it replaced. The image homes are folded into the first one
# first, for the same reason: a key move_image_registry moved still supersedes the image
# it replaced, although the GitLab home has one path component more than ghcr.
image_repository() {
  local ref="${1%%@*}" first home
  [[ "${ref##*/}" != *:* ]] || ref="${ref%:*}"
  for home in "${IMAGE_HOMES[@]}"; do
    [[ "${ref}" != "${home}/"* ]] || ref="${IMAGE_HOMES[0]}/${ref#"${home}"/}"
  done
  first="${ref%%/*}"
  if [[ "${ref}" == */* && ( "${first}" == *.* || "${first}" == *:* || "${first}" == localhost ) ]]; then
    ref="${ref#*/}"
  fi
  printf '%s' "${ref#library/}"
}
```

  In `remove_superseded_images`, the pinned set also reads the box images. Its loop's input
  becomes:

```bash
  done < <(docker compose config --images; env_value AURA_SANDBOX_IMAGE; env_value AURA_SANDBOX_EGRESS_IMAGE)
```

  and its comment's sentence
  `Images outside compose (the per-user sandbox boxes) are never considered, and docker itself`
  becomes:

```bash
# The box images are not compose services; their pins come from .env, and refresh_sandbox_images
# has already removed every box on a superseded image when this runs. Docker itself
```

  The rest of that sentence stays: `refuses to remove one a container still uses; the refusal
  is logged with docker's reason.`

- [x] **Step 4: Run the test**

Run: `bash scripts/aura_image_update_test.sh`
Expected: every `ok:` line and exit 0. The existing cleanup expectations do not change: that
fixture's `.env` has no box keys, and its ghcr `aura:edge` has its pin's ID.

- [x] **Step 5: Commit, then the manifest**

```bash
LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -F <file> -- deploy/aura-image-update.sh scripts/aura_image_update_test.sh
make payload-manifest
LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -m "chore(payload): record the updater that reclaims the images a move superseded" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>" -- scripts/payload_manifest.txt
```

Message: `fix(updater): reclaim the ghcr images a move to the home superseded`. The body says:
- the VM holds about 6 GB of them;
- the two homes now fold to one repository;
- the box images were never considered before.

---

### Task B3: Andrea's one-time patch

**Files:**
- Create: `deploy/aura-registry-bridge.sh` (not in the payload: `install.sh` does not download it)
- Create: `scripts/aura_registry_bridge_test.sh`

**Interfaces:**
- Consumes: the updater installed at `AURA_IMAGE_UPDATE_BIN`
  (default `/usr/local/sbin/aura-image-update.sh`), and `INSTALL_DIR` from
  `/etc/default/aura`, as the updater reads both.
- Produces: an appliance whose `AURA_IMAGE` names the GitLab home, with
  `aura-image-update.service` started.

- [x] **Step 1: Write the failing test.** Create `scripts/aura_registry_bridge_test.sh`:

```bash
#!/usr/bin/env bash
#
# Drives deploy/aura-registry-bridge.sh against a stubbed id, docker, df and systemctl. Every
# refusal must leave .env byte-identical; a run that proceeds changes AURA_IMAGE and nothing
# else, and starts the updater; a second run changes nothing and only starts the updater.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# shellcheck source=/dev/null
source "$repo_root/deploy/aura-registry-bridge.sh"
declare -F main >/dev/null || fail "main undefined after sourcing the patch"

calls="$fixture/calls.log"
export INSTALL_DIR="$fixture/aura" AURA_IMAGE_UPDATE_BIN="$fixture/updater.sh"
uid=0 free_gb=40 pull_ok=1
id() { echo "$uid"; }
df() { printf 'Avail\n%sG\n' "$free_gb"; }
docker() {
  echo "docker $*" >>"$calls"
  case "$1" in
    info) echo /var/lib/docker ;;
    pull) ((pull_ok)) ;;
    *) fail "unexpected docker $*" ;;
  esac
}
systemctl() { echo "systemctl $*" >>"$calls"; }

reset() {
  rm -rf "$INSTALL_DIR"
  mkdir -p "$INSTALL_DIR"
  printf 'POSTGRES_PASSWORD=do-not-print-me\nAURA_IMAGE=ghcr.io/chetto1983/aura:edge\nAURA_CADDY_IMAGE=ghcr.io/chetto1983/aura-caddy:edge\n' >"$INSTALL_DIR/.env"
  chmod 600 "$INSTALL_DIR/.env"
  printf 'sync_payload() {\n  :\n}\n' >"$AURA_IMAGE_UPDATE_BIN"
  cp "$INSTALL_DIR/.env" "$fixture/env.before"
  : >"$calls"
  uid=0 free_gb=40 pull_ok=1
}

refuses() { # <why> : main must fail and leave .env byte-identical
  if main >"$fixture/out" 2>&1; then fail "$1: the patch went on"; fi
  cmp -s "$INSTALL_DIR/.env" "$fixture/env.before" || fail "$1: .env changed"
  ! grep -q '^systemctl' "$calls" || fail "$1: the updater was started"
}

reset; uid=1000; refuses "not root"
reset; printf 'AURA_IMAGE=example.invalid/aura:edge\n' >"$INSTALL_DIR/.env"; cp "$INSTALL_DIR/.env" "$fixture/env.before"; refuses "AURA_IMAGE under neither home"
reset; printf 'echo too old\n' >"$AURA_IMAGE_UPDATE_BIN"; refuses "an updater with no payload sync"
reset; free_gb=3; refuses "too little free space"
reset; pull_ok=0; refuses "the image does not pull"
echo "ok: every refusal leaves .env byte-identical and starts nothing"

reset
main >"$fixture/out"
[[ "$(cat "$INSTALL_DIR/.env")" == "POSTGRES_PASSWORD=do-not-print-me
AURA_IMAGE=registry.gitlab.com/chetto1983/aura/aura:edge
AURA_CADDY_IMAGE=ghcr.io/chetto1983/aura-caddy:edge" ]] || fail "the patch left .env as: $(cat "$INSTALL_DIR/.env")"
[[ "$(stat -c %a "$INSTALL_DIR/.env")" == 600 ]] || fail "the patch widened .env"
grep -qx 'docker pull -q registry.gitlab.com/chetto1983/aura/aura:edge' "$calls" || fail "the patch did not pull first"
grep -qx 'systemctl start --no-block aura-image-update.service' "$calls" || fail "the updater was not started"
! grep -q 'do-not-print-me' "$fixture/out" || fail "the patch printed .env values"
echo "ok: the patch moves AURA_IMAGE alone and starts the updater"

cp "$INSTALL_DIR/.env" "$fixture/env.before"
: >"$calls"
main >"$fixture/out"
cmp -s "$INSTALL_DIR/.env" "$fixture/env.before" || fail "a second run changed .env"
! grep -q '^docker pull' "$calls" || fail "a second run pulled again"
grep -qx 'systemctl start --no-block aura-image-update.service' "$calls" || fail "a second run did not start the updater"
echo "ok: a second run only starts the updater"
```

- [x] **Step 2: Run it and watch it fail**

Run: `bash scripts/aura_registry_bridge_test.sh`
Expected: FAIL. `source` cannot find `deploy/aura-registry-bridge.sh`.

- [x] **Step 3: Write `deploy/aura-registry-bridge.sh`**

```bash
#!/usr/bin/env bash
#
# One-time bootstrap for an Aura appliance whose updater cannot pull its successor: GitHub
# blocks ghcr.io/chetto1983 since 2026-10-11. It points AURA_IMAGE at the GitLab bridge and
# starts the updater; the aura image pulled from there carries the updater that moves every
# other image key on its own (docs/superpowers/specs/2026-10-13-gitlab-image-bridge-design.md).
#
#   curl -fsSL https://gitlab.com/Chetto1983/aura/-/raw/master/deploy/aura-registry-bridge.sh | sudo bash
#
# It refuses, and changes nothing, unless it runs as root on an appliance whose updater syncs
# its payload, whose AURA_IMAGE is under a known home, with room for the new images, and once
# the new aura image has pulled. Removed from the repo on the return trip to ghcr.

set -euo pipefail

GHCR_HOME=ghcr.io/chetto1983
BRIDGE_HOME=registry.gitlab.com/chetto1983/aura
# The published set measured about 7.5 GB on the lab VM (2026-10-13); the old images stay on
# disk until the first apply finishes.
MIN_FREE_GB=10

say() { echo "aura-registry-bridge: $*"; }
die() {
  say "$* -- nothing was changed." >&2
  exit 1
}

start_updater() {
  systemctl start --no-block aura-image-update.service
  say "the updater is running: journalctl -fu aura-image-update.service"
  say "the cockpit will offer the update; 'Update now' applies it, or Aura applies it after 15 idle minutes."
}

main() {
  local env_file updater current target free
  [[ "$(id -u)" == 0 ]] || die "run it as root (sudo)"
  # shellcheck source=/dev/null
  [[ ! -f /etc/default/aura ]] || source /etc/default/aura
  env_file="${INSTALL_DIR:-/opt/aura}/.env"
  updater="${AURA_IMAGE_UPDATE_BIN:-/usr/local/sbin/aura-image-update.sh}"
  [[ -f "${env_file}" ]] || die "no ${env_file}"
  current="$(sed -n 's/^AURA_IMAGE=//p' "${env_file}" | tail -n 1 | tr -d "\"'")"
  case "${current}" in
    "${BRIDGE_HOME}/"*)
      say "AURA_IMAGE already names the GitLab bridge."
      start_updater
      return 0
      ;;
    "${GHCR_HOME}/"*) ;;
    *) die "AURA_IMAGE names neither ${GHCR_HOME} nor ${BRIDGE_HOME}" ;;
  esac
  grep -q '^sync_payload()' "${updater}" 2>/dev/null ||
    die "${updater} predates the payload sync and could never install its successor"
  free="$(df -P -BG "$(docker info --format '{{.DockerRootDir}}')" | awk 'NR == 2 { print $4 }' | tr -dc 0-9)"
  ((${free:-0} >= MIN_FREE_GB)) || die "Docker has ${free:-0} GB free, ${MIN_FREE_GB} GB are needed"
  target="${BRIDGE_HOME}/${current#"${GHCR_HOME}"/}"
  say "pulling ${target}"
  docker pull -q "${target}" >/dev/null || die "${target} does not pull"
  sed -i "s|^AURA_IMAGE=.*|AURA_IMAGE=${target}|" "${env_file}"
  say "AURA_IMAGE now names the GitLab bridge."
  start_updater
}

[[ "${BASH_SOURCE[0]:-$0}" != "$0" ]] || main "$@"
```

  `df -P -BG` prints `Avail` in the 4th column as `NNG`. The test's `df` stub prints `NNG` on
  line 2, so the same `awk`/`tr` reads both.

- [x] **Step 4: Run the test**

Run: `bash scripts/aura_registry_bridge_test.sh`
Expected: three `ok:` lines and exit 0.

- [x] **Step 5: Lint and commit**

```bash
shellcheck deploy/aura-registry-bridge.sh scripts/aura_registry_bridge_test.sh
LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -F <file> -- deploy/aura-registry-bridge.sh scripts/aura_registry_bridge_test.sh
```

Message: `feat(deploy): a one-time patch that points a frozen appliance at the GitLab bridge`.
The body says:
- Andrea's updater cannot pull its successor;
- the patch moves `AURA_IMAGE` only;
- what it refuses;
- it is removed on the return trip.

---

## Part C — Fresh installs

### Task C1: Every shipped default names the home, and a test keeps it so

**Files:**
- Create: `cmd/aura/image_home_artifacts_test.go`
- Modify: `cmd/aura/image_pin_artifacts_test.go:32,88-92`
- Modify: `cmd/aura/distribution_artifacts_test.go:297`
- Modify: `scripts/install.sh:4-5,13,88,91,97`
- Modify: `scripts/install_env.sh:163,171-172,179-185,196-197,407`
- Modify: `compose.yaml`:
  - lines 21-22 (comment);
  - 1144-1152 (whisper comment and fallback);
  - 1019 (whatsapp fallback);
  - 1275 (pim fallback).
- Modify: `.env.example:58`
- Modify: `deploy/aura-image-update.sh:19-27` (header) and `deploy/aura-image-update.service:13-22`
- Modify: `scripts/install_lib_test.sh:113,115`, `scripts/cloudflare_update_test.sh:11,37,48`,
  `scripts/install_remote_access_test.sh:44`
- Modify: `docs/INSTALL.md:23-29,46-60,92`, `docs/cloudflare-remote-access.md:16`
- Modify: `docs/superpowers/specs/2026-10-13-gitlab-image-bridge-design.md` ("what this does
  not establish": release installs)
- Modify (second commit): `scripts/payload_manifest.txt`

**Interfaces:**
- Consumes: `IMAGE_HOME`, `IMAGE_HOMES` (Task B1).
- Produces:
  - `imageHomes(t, root) (home string, others []string)`;
  - `foreignHomeRefs(text string, others []string) []string`;
  - `isThirdPartyImage(ref string, homes []string) bool`.

- [x] **Step 1: Write the failing test.** Create `cmd/aura/image_home_artifacts_test.go`:

```go
package main

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// rawBaseOfHome is where each image home's git mirror serves install.sh from: a fresh install
// that pulls from one home must fetch its files from the same side.
var rawBaseOfHome = map[string]string{
	"ghcr.io/chetto1983":                  "raw.githubusercontent.com/chetto1983/Aura",
	"registry.gitlab.com/chetto1983/aura": "gitlab.com/Chetto1983/aura/-/raw",
}

// imageHomes reads the updater's IMAGE_HOME and IMAGE_HOMES, the one place the image homes are
// declared, and returns the current home and every other one.
func imageHomes(t *testing.T, root string) (string, []string) {
	t.Helper()
	updater := readProjectFile(t, root, "deploy/aura-image-update.sh")
	home := regexp.MustCompile(`(?m)^IMAGE_HOME=(\S+)$`).FindStringSubmatch(updater)
	homes := regexp.MustCompile(`(?m)^IMAGE_HOMES=\(([^)]*)\)$`).FindStringSubmatch(updater)
	if home == nil || homes == nil {
		t.Fatal("deploy/aura-image-update.sh declares no IMAGE_HOME or IMAGE_HOMES")
	}
	var others []string
	declared := false
	for h := range strings.FieldsSeq(homes[1]) {
		if h == home[1] {
			declared = true
			continue
		}
		if rawBaseOfHome[h] == "" {
			t.Fatalf("image home %s has no raw base in rawBaseOfHome", h)
		}
		others = append(others, h)
	}
	if !declared {
		t.Fatalf("IMAGE_HOME %s is not one of IMAGE_HOMES", home[1])
	}
	return home[1], others
}

// foreignHomeRefs lists every reference text makes to another home: its registry path, or the
// raw-file host its git mirror serves install.sh from.
func foreignHomeRefs(text string, others []string) []string {
	var found []string
	for _, h := range others {
		for _, needle := range []string{h + "/", rawBaseOfHome[h]} {
			if strings.Contains(text, needle) {
				found = append(found, needle)
			}
		}
	}
	return found
}

func TestForeignHomeRefsFindsTheOtherHome(t *testing.T) {
	others := []string{"ghcr.io/chetto1983"}
	stale := "AURA_IMAGE=ghcr.io/chetto1983/aura:edge\ncurl -fsSL https://raw.githubusercontent.com/chetto1983/Aura/master/scripts/install.sh"
	if got := foreignHomeRefs(stale, others); len(got) != 2 {
		t.Fatalf("a stale file yielded %v, want its registry path and its raw base", got)
	}
	if got := foreignHomeRefs("AURA_IMAGE=registry.gitlab.com/chetto1983/aura/aura:edge", others); len(got) != 0 {
		t.Fatalf("a file on the home yielded %v", got)
	}
}

// Every default a host receives names the home the updater moves hosts to, so a fresh install
// and an updated host pull the same images (docs/superpowers/specs/2026-10-13-gitlab-image-bridge-design.md).
// The updater declares every home by design, so its IMAGE_HOMES line is not read; the one-time
// bridge patch names both homes by design and is not shipped.
func TestShippedDefaultsNameTheUpdatersImageHome(t *testing.T) {
	root := repoRootForTest(t)
	home, others := imageHomes(t, root)
	shipped := []string{"scripts/install.sh", "scripts/install_env.sh", ".env.example", "packages/create-aura/README.md"}
	for _, pattern := range []string{"compose*.yaml", "deploy/*", "packages/create-aura/src/*.ts"} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil || len(matches) == 0 {
			t.Fatalf("glob %s: %v %v", pattern, matches, err)
		}
		for _, m := range matches {
			rel, _ := filepath.Rel(root, m)
			shipped = append(shipped, filepath.ToSlash(rel))
		}
	}
	declaration := regexp.MustCompile(`(?m)^IMAGE_HOMES=.*$`)
	for _, rel := range shipped {
		if rel == "deploy/aura-registry-bridge.sh" {
			continue
		}
		text := readProjectFile(t, root, rel)
		if rel == "deploy/aura-image-update.sh" {
			text = declaration.ReplaceAllString(text, "")
		}
		if refs := foreignHomeRefs(text, others); len(refs) > 0 {
			t.Errorf("%s still names %v; every shipped default names %s", rel, refs, home)
		}
	}
}
```

- [x] **Step 2: Run it and watch it fail**

Run: `go test -run 'TestForeignHomeRefsFindsTheOtherHome|TestShippedDefaultsNameTheUpdatersImageHome' ./cmd/aura/`
Expected: `TestForeignHomeRefsFindsTheOtherHome` PASSES. That is the proof the check can fail.
`TestShippedDefaultsNameTheUpdatersImageHome` FAILS, listing at least these files:
- `scripts/install.sh`
- `scripts/install_env.sh`
- `.env.example`
- `compose.yaml`
- `deploy/aura-image-update.sh`
- `deploy/aura-image-update.service`
- `packages/create-aura/src/local.ts`
- `packages/create-aura/src/remote.ts`
- `packages/create-aura/README.md`

- [x] **Step 3: Move the defaults.** Exact edits, `H=registry.gitlab.com/chetto1983/aura`:
  - **`scripts/install.sh`:**
    - line 4 → `#   npx create-aura-appliance            (the npm installer, which carries this script)`;
    - line 5 → `#   curl -fsSL https://gitlab.com/Chetto1983/aura/-/raw/<ref>/scripts/install.sh | bash`;
    - line 13 → `#   AURA_IMAGE=registry.gitlab.com/chetto1983/aura/aura:edge`;
    - line 88 → `RAW_BASE="${AURA_INSTALL_BASE_URL:-https://gitlab.com/Chetto1983/aura/-/raw/${RAW_REF}}"`;
    - line 91 → `# publishes registry.gitlab.com/chetto1983/aura/aura:edge (there is no :master image tag), and`;
    - line 97 → `DEFAULT_IMAGE="registry.gitlab.com/chetto1983/aura/aura:${IMAGE_TAG}"`.
  - **`scripts/install_env.sh`:**
    - line 163: `tracks GHCR continuously` → `tracks the image home continuously`;
    - lines 171-172, 179, 181, 183, 185, 196, 197 and 407: replace `ghcr.io/chetto1983/` with
      `registry.gitlab.com/chetto1983/aura/`.
  - **`compose.yaml`:**
    - lines 21-22 become
      `# \`never\` keeps the dev flow source-built; an appliance tracking the edge channel sets`
      and `# AURA_IMAGE=<image home>/aura:edge and AURA_PULL_POLICY=always`;
    - lines 1019, 1152 and 1275: replace `ghcr.io/chetto1983/` with
      `registry.gitlab.com/chetto1983/aura/`, keeping tag and digest;
    - the whisper comment sentence `The image is our GHCR copy of hwdsl2/whisper-server:latest, never Docker Hub.`
      → `The image is our copy of hwdsl2/whisper-server:latest on the image home, never Docker Hub.`;
    - its last sentence → `scripts/mirror_whisper_server.sh copies each new :latest to a
      dated tag with the same digest (on GitHub, mirror-whisper-server.yml runs it and
      Dependabot proposes the bump).`
  - **`.env.example:58`:** `AURA_SANDBOX_IMAGE=registry.gitlab.com/chetto1983/aura/aura-sandbox:edge`.
  - **`deploy/aura-image-update.sh:19-27`:** in the prerequisite comment lines, replace
    `ghcr.io/chetto1983/` with `registry.gitlab.com/chetto1983/aura/`.
  - **`deploy/aura-image-update.service`:**
    - lines 13-19: the same replacement;
    - line 22 → `Description=Aura appliance image update from its image home`.
  - **create-aura:** `packages/create-aura/src/local.ts` and `src/remote.ts` are Task C2's. The
    test stays red on them until C2. Ledger it, and run Step 4's command with
    `-run TestForeignHomeRefsFindsTheOtherHome` plus a check that the failure lists only those
    three create-aura files.

- [x] **Step 4: Move the tests that pinned ghcr**
  - `scripts/install_lib_test.sh:113,115`, `scripts/cloudflare_update_test.sh:11,37,48` and
    `scripts/install_remote_access_test.sh:44`: replace `ghcr.io/chetto1983/` with
    `registry.gitlab.com/chetto1983/aura/`.
  - `cmd/aura/distribution_artifacts_test.go:297` →
    `"AURA_SANDBOX_IMAGE=registry.gitlab.com/chetto1983/aura/aura-sandbox:edge",`.
  - `cmd/aura/image_pin_artifacts_test.go`:
    - `isThirdPartyImage` becomes:

```go
// Repo-built images ride the edge channel's moving tags on purpose, published and pulled by
// the same pipeline, under any image home the updater knows; locally built ones are named aura*.
func isThirdPartyImage(ref string, homes []string) bool {
	if strings.HasPrefix(ref, "aura") {
		return false
	}
	for _, h := range homes {
		if strings.HasPrefix(ref, h+"/") {
			return false
		}
	}
	return true
}
```

    - its caller at line 32: before the loop, add
      `home, others := imageHomes(t, root)` and `homes := append([]string{home}, others...)`.
      The call becomes `if !isThirdPartyImage(ref, homes) {`.

- [x] **Step 5: Docs.**
  - **`docs/INSTALL.md` "Releases and channels" (lines 23-29).** Keep the release sentence. The
    edge sentence becomes: `Independently of releases, the moving edge image every default
    install tracks is published as \`registry.gitlab.com/chetto1983/aura/aura:edge\` (plus an
    immutable \`master-<sha>\` tag) while GitHub blocks the account: release tags stay on ghcr
    and are not mirrored, so a pinned \`vX.Y.Z\` install is not available until GitHub returns.`
  - **`docs/INSTALL.md`, lines 46-52.** Delete the `npx github:` paragraph and its code block.
  - **`docs/INSTALL.md`, line 59.** The URL becomes
    `https://gitlab.com/Chetto1983/aura/-/raw/master/scripts/install.sh`.
  - **`docs/INSTALL.md`, line 92.** Add the comment line
    `# a release image is on ghcr only; while GitHub blocks it, use registry.gitlab.com/chetto1983/aura/aura:edge`
    above `AURA_IMAGE=ghcr.io/chetto1983/aura:vX.Y.Z`.
  - **`docs/cloudflare-remote-access.md:16`.** `ghcr.io/chetto1983/aura-cloudflared:edge` →
    `registry.gitlab.com/chetto1983/aura/aura-cloudflared:edge`.
  - **The spec's "What this does not establish".** Add:
    `- **Release installs.** A pinned \`vX.Y.Z\` install names a release tag that only ghcr carries; the bridge mirrors the edge channel.`

- [x] **Step 6: Run everything this task touched**

```bash
go vet ./cmd/aura/
CGO_ENABLED=1 go test -race -run 'TestForeignHomeRefsFindsTheOtherHome|TestShippedDefaultsNameTheUpdatersImageHome|TestThirdPartyImagesArePinnedWhereTheUpdaterDeliversThem|TestRetiredEnvKeysStayRetired|TestPIMSidecarIsToldTheGoogleRelay|Distribution|EnvExample' ./cmd/aura/
bash scripts/install_lib_test.sh && bash scripts/cloudflare_update_test.sh && bash scripts/install_remote_access_test.sh && bash scripts/aura_image_update_test.sh
```

Expected:
- vet is clean;
- every named Go test passes, except `TestShippedDefaultsNameTheUpdatersImageHome`, which
  lists only the three create-aura files;
- the four shell suites pass. `cloudflare_update_test.sh` is green again.

- [x] **Step 7: Commit, then the manifest**

```bash
LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -F <file> -- \
  cmd/aura/image_home_artifacts_test.go cmd/aura/image_pin_artifacts_test.go cmd/aura/distribution_artifacts_test.go \
  scripts/install.sh scripts/install_env.sh compose.yaml .env.example deploy/aura-image-update.sh deploy/aura-image-update.service \
  scripts/install_lib_test.sh scripts/cloudflare_update_test.sh scripts/install_remote_access_test.sh \
  docs/INSTALL.md docs/cloudflare-remote-access.md docs/superpowers/specs/2026-10-13-gitlab-image-bridge-design.md
make payload-manifest
LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -m "chore(payload): record the shipped defaults that name the image home" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>" -- scripts/payload_manifest.txt
```

Message: `feat(install): start fresh installs on the GitLab image home`. The body says:
- every entry point answered 403 or 404;
- what moved;
- the anti-drift test keyed to `IMAGE_HOME`;
- release installs stay out.

If the pre-commit hook runs the Go tests and the create-aura red blocks the commit, do Task C2
first, then commit C1 and C2 as written, in order. Ledger the ruling.

---

### Task C2: create-aura checks the home and goes to 0.2.12

**Files:**
- Modify: `packages/create-aura/src/local.ts:34-44`, `src/remote.ts:24-26,71`
- Modify: `packages/create-aura/src/__tests__/local.test.ts:39,63`, `src/__tests__/remote.test.ts:84-85`
- Modify: `packages/create-aura/README.md:3,42`, `package.json` and `package-lock.json` (version)

**Interfaces:**
- Produces: `REQUIRED_HOSTS = ['https://registry.gitlab.com', 'https://get.docker.com', 'https://huggingface.co']`.
  `https://registry.gitlab.com` answered 200 to `curl -fsS` on 2026-10-13. Its `/v2/` answers
  401, so it would fail `-f`.

- [x] **Step 1: Write the failing tests.**
  - `local.test.ts:63`: the hosts array becomes
    `['https://registry.gitlab.com', 'https://get.docker.com', 'https://huggingface.co']`.
  - `local.test.ts:39`: the comment becomes
    `// REQUIRED_HOSTS: registry.gitlab.com, get.docker.com, huggingface.co`.
  - `remote.test.ts:84-85` become:

```ts
    // R3: the image home (registry.gitlab.com while GitHub blocks ghcr) and get.docker.com,
    // huggingface.co added, raw.githubusercontent.com removed.
    expect(probeScript).toContain('https://registry.gitlab.com');
    expect(probeScript).not.toContain('https://ghcr.io');
```

- [x] **Step 2: Run them and watch them fail**

Run, from `packages/create-aura` in WSL: `npx vitest run src/__tests__/local.test.ts src/__tests__/remote.test.ts`
Expected: FAIL. The calls still name `https://ghcr.io`.

- [x] **Step 3: Implement.**
  - **`local.ts`.** In `REQUIRED_HOSTS`, `'https://ghcr.io'` → `'https://registry.gitlab.com'`.
    Append to the R3 comment: `The image registry is the one install.sh pulls from, the image
    home (deploy/aura-image-update.sh IMAGE_HOME).`
  - **`remote.ts:71`.** `for URL in https://ghcr.io https://get.docker.com https://huggingface.co; do`
    → `for URL in https://registry.gitlab.com https://get.docker.com https://huggingface.co; do`.
  - **`remote.ts:24`.** The comment becomes
    `// R3: the same REQUIRED_HOSTS -- the image home and get.docker.com kept, raw.githubusercontent.com`.
  - **`README.md`.**
    - line 3: `[Aura](https://github.com/chetto1983/Aura)` → `[Aura](https://gitlab.com/Chetto1983/aura)`;
    - line 42: `` `ghcr.io` `` → `` `registry.gitlab.com` ``.
  - **Version.** In `packages/create-aura`, run `npm version 0.2.12 --no-git-tag-version`.

- [x] **Step 4: Run the package's tests and the Go drift test**

```bash
cd packages/create-aura && npx vitest run && npx tsc --noEmit && cd ../..
go test -run 'TestShippedDefaultsNameTheUpdatersImageHome' ./cmd/aura/
```

Expected: every create-aura test passes, `tsc` prints nothing, and the drift test PASSES.

- [x] **Step 5: Commit.** Commit Task C1's changes here if the hook held them back. Then:

```bash
LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -F <file> -- \
  packages/create-aura/src/local.ts packages/create-aura/src/remote.ts \
  packages/create-aura/src/__tests__/local.test.ts packages/create-aura/src/__tests__/remote.test.ts \
  packages/create-aura/README.md packages/create-aura/package.json packages/create-aura/package-lock.json
```

Message: `feat(create-aura): require the image home, not ghcr, and release 0.2.12`.

---

## Part D — Closing

### Task D1: Publish, mirror whisper, read back anonymously

- [x] **Step 1: Docker authenticates through glab.** In WSL, run
  `export PATH="$HOME/go/bin:$PATH"; glab auth configure-docker`. Then
  `grep -c credHelpers ~/.docker/config.json` must be ≥ 1, read without printing the file.
- [x] **Step 2: skopeo.** Run `wsl -u root apt-get install -y skopeo`, then `skopeo --version`.
- [x] **Step 3: Mirror whisper.** Run
  `DEST_REPO=registry.gitlab.com/chetto1983/aura/aura-whisper-server bash scripts/mirror_whisper_server.sh`.
  Expected: `mirror: registry.gitlab.com/chetto1983/aura/aura-whisper-server:2026.10.08.1056@sha256:c6c6311a…`.

  If skopeo cannot use the glab helper, the operator runs `skopeo login registry.gitlab.com`.
  Ledger the ruling.
- [x] **Step 4: Publish the HEAD that carries Parts B and C.** Run
  `bash scripts/publish_images.sh HEAD 2>&1 | tee <workspace>/publish.log | grep -E '^(published|moved) '`.
  Expected: 9 `published` lines and 11 `moved` lines: 6 edge, 1 arcadedb-mcp, and 2 for each
  of the 2 forks. The last line is `moved registry.gitlab.com/chetto1983/aura/aura:edge`.
- [x] **Step 5: Anonymous read-back from the VM.** The VM has no registry login. Through `vm.sh`,
  for each of the ten references:
  - `docker manifest inspect` or `docker buildx imagetools inspect`;
  - then `docker run --rm --entrypoint aura <aura:edge> version`, which must name the published
    commit;
  - whisper's digest must be `sha256:c6c6311a…`.

  Record the sizes from `docker image ls`, and confirm `MIN_FREE_GB=10` covers them. Otherwise
  a ruling changes it, with a test-first edit.

### Task D2: The lab VM takes Andrea's path

- [x] **Step 1: Before.** Read and record:
  - the image keys in `.env`, with key names and prefixes only;
  - the running images;
  - `df -h /`;
  - the timer state.

  Check `auth.log`.
- [x] **Step 2: Hand the VM to the updater.**
  - The VM currently runs the lab overlay `aura:lab-aura-connect-20261013-1`, through
    `compose.lab.yaml` passed with `-f`. The updater uses `.env`'s `COMPOSE_FILE`, without the
    overlay, so its first apply moves `aura` to `edge`. That is intended.
  - Start the timer again: `systemctl start aura-image-update.timer`. Its last stop was a lab
    deploy, and the bridge is the moment to hand the VM back to it. Ledger that.
- [x] **Step 3: Run the patch exactly as Andrea will.** Run
  `curl -fsSL https://gitlab.com/Chetto1983/aura/-/raw/master/deploy/aura-registry-bridge.sh | sudo bash`
  on the VM.
  Expected:
  - `pulling registry.gitlab.com/chetto1983/aura/aura:edge`;
  - `AURA_IMAGE now names the GitLab bridge.`;
  - the updater starts.
- [x] **Step 4: Apply.** With the operator's cockpit session, post "update now" through the
  cockpit's update decision. Otherwise wait for the 15 idle minutes. Follow
  `journalctl -u aura-image-update.service` until `Aura image update completed`.
- [x] **Step 5: Read back:**
  - all nine `.env` image keys name the GitLab prefix, read with
    `grep -c '=registry.gitlab.com/chetto1983/aura/'`;
  - every compose container and every box runs an image whose `RepoTags` or `RepoDigests` name
    the GitLab prefix;
  - `docker exec aura aura version` names the published commit;
  - through the integrations proxy, the PIM `/admin/accounts` and the WhatsApp `/api/status`
    answer;
  - the journal has one `env: … moved` line per key, and exit 0;
  - `docker images | grep -c ghcr.io/chetto1983` counts only the whisper name that shares its
    pin's ID;
  - `df -h /` is back near Step 1's value.
- [x] **Step 6: Second tick.** Run `systemctl start aura-image-update.service`. The journal has
  no `moved` line, and the tick exits 0.
- [x] **Step 7: Smoke.** One chat turn in the cockpit and one on Telegram, both answered.

### Task D3: Fresh installs are reachable, and create-aura is on npm

- [x] **Step 1: The cheap probe in a clean container, with no credentials.** In WSL, list what a
  fresh install fetches and pulls. Then let a clean `ubuntu:24.04` container read each of them
  anonymously:

```bash
cd /mnt/d/Aura
{ echo scripts/install.sh; bash -c 'source scripts/payload_files.sh; payload_files .'; } >/tmp/fresh-files
{ sed -n 's/.*ensure_env_default AURA_[A-Z_]*IMAGE \(registry\.gitlab\.com\/\S*\).*/\1/p' scripts/install_env.sh
  sed -n 's/^DEFAULT_IMAGE="\(.*\)\${IMAGE_TAG}"$/\1edge/p' scripts/install.sh
  grep -oE '\$\{AURA_[A-Z_]+_IMAGE:-registry\.gitlab\.com/[^}]+\}' compose.yaml | sed 's/.*:-//; s/}$//'
} | sort -u >/tmp/fresh-images
docker run --rm -v /tmp/fresh-files:/f:ro -v /tmp/fresh-images:/i:ro ubuntu:24.04 bash -c '
  apt-get -qq update >/dev/null && apt-get -qq install -y curl ca-certificates skopeo >/dev/null
  B=https://gitlab.com/Chetto1983/aura/-/raw/master
  while read -r f; do curl -fsS -o /dev/null "$B/$f" && echo "file ok $f" || echo "FILE MISSING $f"; done </f
  while read -r i; do skopeo inspect --no-creds --raw "docker://$i" >/dev/null && echo "image ok $i" || echo "IMAGE MISSING $i"; done </i'
```

  Expected: one `ok` line per file and per image, and no `MISSING`. The image list holds at
  least `aura:edge`, the 9 sidecar and box defaults, and the 3 compose fallbacks (pim,
  whatsapp, whisper).
- [x] **Step 2: create-aura.** The operator publishes it from `packages/create-aura` in WSL with
  `npm publish`; `makeself` is installed. Then:

```bash
npm view create-aura-appliance version          # 0.2.12
cd "$(mktemp -d)" && npm pack create-aura-appliance@0.2.12 >/dev/null && tar -xzf create-aura-appliance-0.2.12.tgz
grep -a -c 'registry.gitlab.com/chetto1983/aura/aura' package/install-appliance.run
```

  Expected: `0.2.12`, and a count ≥ 1.

### Task D4: Ledger, PRD, push, Andrea

- [x] **Step 1: Write the ledger.**
  `docs/superpowers/verification/<date>-gitlab-image-bridge.md` records:
  - D1-D3, with commands and read-backs;
  - what this does not establish: the spec's list, plus that no CI ran.
- [x] **Step 2: Amend PRD §17.** Add the bridge, citing the ledger and the date: the home, the
  move, the patch, and the return trip.
- [x] **Step 3: Commit; push to `origin` and `gitlab`.** Every pre-push gate must be green.
- [x] **Step 4: Andrea.** Give the operator the one line to send him:
  `curl -fsSL https://gitlab.com/Chetto1983/aura/-/raw/master/deploy/aura-registry-bridge.sh | sudo bash`.
  Add the two things to tell him:
  - after "Update now" in the cockpit, the Google redirect URI to add, as in the aura-connect
    plan;
  - logging in to the CIMD servers again.

  When he runs it, his updater's journal goes into the same ledger.
