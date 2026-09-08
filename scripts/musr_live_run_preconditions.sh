#!/usr/bin/env bash
# scripts/musr_live_run_preconditions.sh — sourced by scripts/musr_live_run.sh (`. scripts/
# musr_live_run_preconditions.sh`, not executed standalone: it exits 2 on failure and exports
# variables the caller needs). Split out to keep the orchestrator under CLAUDE.md's 600-LOC
# ceiling; T-06 threat register: name the first missing precondition and refuse, never guess.
#
# MEASURED (2026-09-08): a raw shell-env check is the WRONG authority for some of these keys.
# internal/agui/settings_api.go:430 effectiveSettingValue, and boot-time
# cmd/aura/chat_boot.go's resolveConfigAndPoolWithSettings -> settings.OverlayEnv
# (internal/settings/settings.go:213), resolve STORE (Postgres aura.settings) OVER env: at
# boot, every settings.AllowedKeys row present in the store is os.Setenv'd into the process
# BEFORE config reloads — a value only in the store, never in `.env`, still boots the daemon
# correctly. Confirmed empirically this session (key names only, via `docker exec
# aura-postgres psql`, never a value) that TELEGRAM_BOT_TOKEN and OPENROUTER_API_KEY are both
# present in aura.settings on this host even though `.env` does not carry them. A gate that
# only inspects the shell environment reports a FALSE missing-precondition on a correctly
# configured host — worse than no gate, because it teaches the operator to ignore it. This
# gate therefore checks BOTH: shell env first (cheap, no Docker round trip), then the store
# for any key settings.AllowedKeys actually covers.
#
# AURA_MUSR_ISOLATION and AURA_SANDBOX_IMAGE are NOT in settings.AllowedKeys (verified: `select
# key from aura.settings where key like 'AURA_%'` on this host lists LLM/embedding/loop
# knobs only — no isolation or sandbox-image row exists or could exist via this mechanism).
# They are genuine deployment-topology config, only ever env-resolved. If truly absent from
# BOTH `.env` and the store, this self-supplies the documented default from `.env.example` for
# THIS harness's own separately-started `aura serve` process only — never touching the real
# `.env` or the compose container.

STORE_KEY_PRESENT_CACHE=""
store_has_key() {
  # $1=key -> 0 if aura.settings has a row for it (key existence only, value never read/
  # printed). Best-effort: a psql failure (no docker, container down) is treated as "not
  # present", which is the conservative direction for a precondition gate.
  local key="$1"
  if [[ -z "${STORE_KEY_PRESENT_CACHE}" ]]; then
    STORE_KEY_PRESENT_CACHE="$(docker exec aura-postgres psql -U aura -d aura -tAc \
      "select key from aura.settings;" 2>/dev/null || true)"
    STORE_KEY_PRESENT_CACHE="${STORE_KEY_PRESENT_CACHE}
__sentinel__"
  fi
  grep -qxF "${key}" <<<"${STORE_KEY_PRESENT_CACHE}"
}

missing=()
for v in AURA_ARCADEDB_TENANT_SECRET AURA_AUTHULA_SECRET AURA_E2E_AUTHULA_EMAIL \
  AURA_E2E_AUTHULA_PASSWORD ARCADEDB_PASSWORD; do
  if [[ -z "${!v:-}" ]]; then
    missing+=("$v")
  fi
done
for v in TELEGRAM_BOT_TOKEN OPENROUTER_API_KEY; do
  if [[ -z "${!v:-}" ]] && ! store_has_key "$v"; then
    missing+=("$v (checked shell env AND the aura.settings store — present in neither)")
  fi
done
if [[ -z "${AURA_PROFILE:-}" ]]; then
  missing+=("AURA_PROFILE")
fi
if [[ "${#missing[@]}" -gt 0 ]]; then
  echo "FAIL: missing required precondition variable(s): ${missing[*]}" >&2
  echo "      source .env before running this harness (see the invoke comment above)" >&2
  exit 2
fi
if [[ "${AURA_PROFILE}" != *hardened* && "${AURA_PROFILE}" != *production* ]]; then
  echo "FAIL: AURA_PROFILE=${AURA_PROFILE} is not a strict profile — this run needs strict + isolation-on (D-01)" >&2
  exit 2
fi
if [[ -z "${AURA_MUSR_ISOLATION:-}" ]]; then
  echo "==> AURA_MUSR_ISOLATION unset in shell env and not store-resolvable (not in settings.AllowedKeys) — defaulting to true for this harness's own aura serve process only (not .env, not the compose container)"
  AURA_MUSR_ISOLATION=true
fi
export AURA_MUSR_ISOLATION
if [[ "${AURA_MUSR_ISOLATION}" != "true" ]]; then
  echo "FAIL: AURA_MUSR_ISOLATION=${AURA_MUSR_ISOLATION}, want true — a second identity needs isolation on" >&2
  exit 2
fi
if [[ -z "${AURA_SANDBOX_IMAGE:-}" ]]; then
  AURA_SANDBOX_IMAGE="ghcr.io/chetto1983/aura-sandbox:edge"
  echo "==> AURA_SANDBOX_IMAGE unset in shell env and not store-resolvable — defaulting to ${AURA_SANDBOX_IMAGE} (.env.example's documented default) for this harness's own aura serve process only"
fi
# MEASURED (2026-09-08): ARCADEDB_ADMIN_USER/ARCADEDB_ADMIN_PASSWORD are what
# cmd/aura/serve_deprovision_purgers.go:161-191 (buildArcadeMemoryLifecycle) reads for the
# memory-provisioning leg identity B's saga needs (WARN observed on a live dry run: "no
# ArcadeDB server credential — memory lifecycle disabled" -> the saga then refuses with
# "onboarding: provisioning backend not configured"). Neither name lives in the root `.env`
# — compose.yaml only sets them INSIDE specific container service blocks
# (ARCADEDB_ADMIN_USER:-root, ARCADEDB_ADMIN_PASSWORD: ${ARCADEDB_PASSWORD}), never at the
# top level a bare `aura serve`/`aura identity create` process inherits. Derive them the same
# way compose.yaml does, from the ARCADEDB_PASSWORD this harness already requires above.
export ARCADEDB_ADMIN_USER="${ARCADEDB_ADMIN_USER:-root}"
export ARCADEDB_ADMIN_PASSWORD="${ARCADEDB_ADMIN_PASSWORD:-${ARCADEDB_PASSWORD}}"

# MEASURED (2026-09-08): AURA_GARAGE_ADMIN_ENDPOINT's Go-level default is `http://garage:3903`
# (internal/config/config.go:558 — the compose-internal service DNS name), and this host's own
# `.env` sets AURA_OBJECTSTORE_ENDPOINT to the same `garage:3900` form for the CONTAINERIZED
# deployment's benefit. Neither resolves from a bare WSL process (compose service names are
# Docker-network-internal DNS, unlike Ollama's host.docker.internal problem above — this one
# has no gateway-IP workaround, it needs the actual published port). Garage publishes both:
# `docker port aura-garage` -> `127.0.0.1:3900` and `127.0.0.1:3903`. Override unconditionally
# for this harness's own processes (never touching the real `.env` or the compose containers).
export AURA_OBJECTSTORE_ENDPOINT="http://127.0.0.1:3900"
export AURA_GARAGE_ADMIN_ENDPOINT="http://127.0.0.1:3903"
if ! command -v docker >/dev/null 2>&1; then
  echo "FAIL: docker CLI not found on PATH" >&2
  exit 2
fi
for svc in postgres arcadedb garage; do
  state="$(docker compose ps --format '{{.Service}} {{.State}}' 2>/dev/null | awk -v s="$svc" '$1==s{print $2}')"
  if [[ -z "${state}" ]]; then
    echo "FAIL: compose service '${svc}' is not up — bring the live stack up before running this harness" >&2
    exit 2
  fi
  if [[ "${state}" != "running" ]]; then
    echo "FAIL: compose service '${svc}' is '${state}', want running" >&2
    exit 2
  fi
done
if ! command -v python3 >/dev/null 2>&1; then
  echo "FAIL: python3 not found on PATH — required for TOTP code computation and the SSE conversation drivers" >&2
  exit 2
fi
echo "==> preconditions OK"
