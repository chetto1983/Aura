# Install and operate

How to install the Docker Compose appliance, reach the cockpit, keep it updated, and
pair the optional messaging sidecars. The two-command path is in the
[README Quick Start](../README.md#quick-start); this page carries everything else.

## What the appliance runs

The default stack brings up Aura (with its migration one-shot), Postgres, ArcadeDB and
its MCP, Garage, the local embedding sidecar, document ingestion, SearXNG,
speech-to-text and text-to-speech, the PIM and WhatsApp MCP sidecars, the Cloudflare
tunnel supervisor (idle until enabled), and Caddy in front of the Authula sign-in.
Compose profiles add the rest: `localllm` (a llama.cpp server with Gemma 4 12B QAT),
`ocr`, `observability` (Prometheus, Tempo, Grafana) and `sandbox` (the Docker socket
proxy for per-identity boxes).

The default deployment keeps Aura socketless: no Docker socket is mounted into the
Aura container. The opt-in `sandbox` profile reaches Docker only through a socket
proxy that allows the box lifecycle verbs.

## Releases and channels

`ghcr.io/chetto1983/aura:<tag>` and the binary archives are published by the
`Release` workflow on a `v*` tag, and only after the exact-SHA *Production Readiness*
check passed for that commit ([release-readiness.md](release-readiness.md)). Check the
[Releases page](https://github.com/chetto1983/Aura/releases) for the current tag
(`v1.0.2-rc1` is the latest) and use it as `vX.Y.Z` below. Independently of releases,
every master push publishes the moving `ghcr.io/chetto1983/aura:edge` image (plus an
immutable `master-<sha>` tag), the continuous-delivery channel a default install tracks.

## Linux or macOS

The interactive installer supports local installation or a Linux target over SSH:

```bash
npx create-aura-appliance
npx create-aura-appliance --mode remote
```

It requires Node.js **22.13 or newer** on the workstation. The target needs at least
**4 CPU cores, 14 GiB usable RAM, and 20 GiB free disk**; documents, models and backup
retention need additional capacity. The installer detects the embedding backend on the
target: CUDA for an NVIDIA GPU Docker can drive, otherwise Vulkan for an Intel or AMD GPU
exposing `/dev/dri`, otherwise CPU. See the
[installer guide](../packages/create-aura/README.md) for supported targets and
prerequisites. The npm installer carries its own payload.

The source-hosted installer remains available. Install Docker, then run it on a
machine with Node 18+ (`npx` fetches the repo and runs `scripts/install.sh`):

```bash
sudo npx github:chetto1983/Aura -- --appliance
```

or the curl equivalent of the same script. Use `master` to track the edge channel, or
a release tag `vX.Y.Z` to pin:

```bash
curl -fsSL https://raw.githubusercontent.com/chetto1983/Aura/master/scripts/install.sh | sudo bash -s -- --appliance
```

The installer checks hardware, creates `.env` with generated `POSTGRES_PASSWORD`, the
three `ARCADEDB_*` secrets, and `AURA_ACCESS_TOKEN`, downloads the Compose/Caddy
assets, and starts the stack. Re-running it keeps an existing `.env` intact. A
master/edge install points `.env` at the `:edge` moving tags, and `--appliance` also
enables the `aura-image-update` systemd timer: from then on the machine re-pulls aura
and its MCP sidecars from GHCR on its own, migrations and Compose payload included,
with no operator involved. Without `--appliance` (no systemd units, no timer), the
stack still starts; updates stay manual.

The default stack also starts the Cloudflare supervisor healthy-idle. Enable it after
setup in **Settings > Remote access**; the installer requires no Cloudflare credential.
Existing edge appliances receive the sidecar through the payload updater without
replacing database volumes. See [Cloudflare Remote Access](cloudflare-remote-access.md)
for registered-domain prerequisites, Access OTP, organization WARP, token refresh and
safe disable/delete.

Add `--gvisor` on native Linux Docker hosts that should run Aura under `runsc`.
Docker Desktop is intentionally not supported for that isolation tier.

## Windows

Use Docker Desktop and the shipped Compose files. From PowerShell in the Aura
checkout or release directory:

```powershell
function New-Hex { -join ((1..32) | ForEach-Object { '{0:x2}' -f (Get-Random -Maximum 256) }) }
@"
POSTGRES_PASSWORD=$(New-Hex)
POSTGRES_USER=aura
POSTGRES_DB=aura
ARCADEDB_PASSWORD=$(New-Hex)
ARCADEDB_APP_PASSWORD=$(New-Hex)
AURA_ARCADEDB_TENANT_SECRET=$(New-Hex)
AURA_IMAGE=ghcr.io/chetto1983/aura:vX.Y.Z
AURA_ACCESS_TOKEN=$(New-Hex)
AURA_AUTHULA_SECRET=$(New-Hex)
SEARXNG_SECRET=$(New-Hex)
AURA_OBJECTSTORE_ACCESS_KEY=GK$((New-Hex).Substring(0,24))
AURA_OBJECTSTORE_SECRET_KEY=$(New-Hex)
GARAGE_RPC_SECRET=$(New-Hex)
AURA_GARAGE_ADMIN_TOKEN=$(New-Hex)
AURA_BACKUP_DIR=./backups
AURA_EMBED_REVISION=0f741b5a6585bd53aeb15cd1372c56f2a0f65e12
AURA_EMBED_FINGERPRINT=b5ce9d77a3fc4b3b39ccb5643c36777911cc4eb46a66962eadfa3f5f60490d63
AURA_EMBED_NGL=99
"@ | Set-Content -Path .env -Encoding ascii

docker run --rm --gpus all nvidia/cuda:12.8.0-base-ubuntu24.04 nvidia-smi
docker compose up -d
```

This `.env` targets an NVIDIA GPU: the embedding sidecar reserves one, so fix
Docker/NVIDIA before starting Aura if the `nvidia-smi` container check fails. Without
an NVIDIA GPU, run embeddings on the CPU instead: set `AURA_EMBED_NGL=0` and
`COMPOSE_FILE=compose.yaml;compose.cpu.yaml` (`;` is Compose's path separator on
Windows), which drops the GPU reservation and selects the CPU build of the pinned
llama.cpp server.

The model route, the OpenRouter key and the Telegram bot token are not `.env`
settings: choose them in the first-run web setup, and Aura keeps them in
`aura.settings`. For local development images, replace `AURA_IMAGE` with
`aura:local` after building the image (see [CONTRIBUTING.md](../CONTRIBUTING.md)).

Postgres 18 is the default Compose image for new installs. When upgrading an existing
Aura deployment from Postgres 17, migrate the data with `pg_dump` / `pg_restore` or
`pg_upgrade`; a Postgres 18 container cannot reuse a Postgres 17 data volume directly.

## First access

Aura listens on loopback; Caddy serves the cockpit on HTTPS at `https://<host>`,
behind the Authula sign-in. On a fresh install the sign-in page offers **Create first
user**: that account is the operator, and after signing in the cockpit's first-run
setup finishes the configuration. The Telegram bot is connected through the setup
wizard, gated by the access token the installer prints:

```text
https://<host>/setup/?token=<AURA_ACCESS_TOKEN>
```

Caddy uses `tls internal`. Browsers on other LAN machines will warn until they trust
the local CA root from the `caddy-data` volume:

```bash
docker compose exec caddy cat /data/caddy/pki/authorities/local/root.crt > aura-caddy-root.crt
```

Trust on the Ubuntu server does not make remote browsers trust that CA. Cloudflare
named-tunnel hostnames use the public edge certificate; direct port 443 bypasses
Cloudflare Access and retains Authula. Quick Tunnels are temporary testing only and
cannot validate Aura chat because they do not support SSE.

## Updates

An edge appliance installed with `--appliance` updates itself: the
`aura-image-update.timer` (5-minute cadence, flock-guarded) pulls the moving tags and
recreates only what changed, running migrations first. The aura image also carries
the installation payload (the Compose files, the updater and its units, the sidecar
configuration), and each tick installs whatever differs from `/opt/aura` (backing up
what it replaces under `backups/payload-*`) and brings the whole stack up on it. A
version pin changed in `compose.yaml` therefore reaches every appliance on its own.
`sha256sum -c payload_manifest.txt` inside `/opt/aura` shows whether a host matches
its payload. Watch it with `journalctl -u aura-image-update.service -f`.

Manual update (pinned installs, or no systemd). Volumes persist, and the
`aura-migrate` one-shot runs the Postgres migrations before the Aura service starts
(ArcadeDB needs none: the MCP creates each identity's database on first use):

```bash
docker compose pull
docker compose up -d
```

## Backups

Postgres dumps and hourly ArcadeDB archives run on their own; see
[BACKUP-RESTORE.md](BACKUP-RESTORE.md) for schedules, the restore drill, manual
restore commands and the recovery scope.

## WhatsApp MCP

The `whatsapp` service is part of the default stack, mounted through Aura's MCP
catalog. It uses an unofficial whatsmeow-based client, so it carries
WhatsApp Terms of Service and account-ban risk. First pairing is headless:

```bash
docker compose logs -f whatsapp
```

Scan the QR code shown in the logs. Aura boot never depends on this service.

## Migrating from the retired host setup

The host needs no Python MCP runtime at all: memory is served by Aura's own ArcadeDB
MCP, a Go binary in the image. Old host-level Python installs and the earlier WSL
WhatsApp MCP install can be removed after migrating to the Compose appliance.
