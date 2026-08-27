#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_root="$(mktemp -d)"
trap 'rm -rf "$test_root"' EXIT

mkdir -p "$test_root/home/.local/bin" "$test_root/bin"
printf '%s\n' 'POSTGRES_PASSWORD=stale-home-password' >"$test_root/home/aura.env"

cat >"$test_root/bin/docker" <<'FAKE_DOCKER'
#!/usr/bin/env bash
printf 'docker|%s\n' "$*" >>"$AURA_TEST_CALLS"
FAKE_DOCKER
chmod +x "$test_root/bin/docker"

cat >"$test_root/home/.local/bin/go" <<'FAKE_GO'
#!/usr/bin/env bash
printf 'go|%s|%s|%s\n' "$AURA_DB_URL" "$AURA_DB_MIGRATE_URL" "$*" >>"$AURA_TEST_CALLS"
FAKE_GO
chmod +x "$test_root/home/.local/bin/go"

calls="$test_root/calls.log"
HOME="$test_root/home" PATH="$test_root/bin:$PATH" AURA_TEST_CALLS="$calls" \
  POSTGRES_PASSWORD=stack-password \
  AURA_RUNNER_TEST_DB=runner_gate_test \
  bash "$repo_root/scripts/run_runner_integration.sh" -run '^TestVerifyOnStopFiresOnARealTurn$'

grep -F 'DROP DATABASE IF EXISTS "runner_gate_test" WITH (FORCE)' "$calls" >/dev/null
grep -F 'CREATE DATABASE "runner_gate_test" OWNER aura_migrate' "$calls" >/dev/null
grep -F 'postgres://aura_app:stack-password@127.0.0.1:5432/runner_gate_test?sslmode=disable' "$calls" >/dev/null

drop_count="$(grep -Fc 'DROP DATABASE IF EXISTS "runner_gate_test" WITH (FORCE)' "$calls")"
if [ "$drop_count" -ne 2 ]; then
  printf 'expected setup and cleanup drops, got %s\n' "$drop_count" >&2
  exit 1
fi

printf 'ok: runner integration uses and removes a disposable database\n'

for unsafe_name in aura 'runner;DROP'; do
  if HOME="$test_root/home" PATH="$test_root/bin:$PATH" AURA_TEST_CALLS="$calls" \
    POSTGRES_PASSWORD=stack-password \
    AURA_RUNNER_TEST_DB="$unsafe_name" bash "$repo_root/scripts/run_runner_integration.sh" >/dev/null 2>&1; then
    printf 'runner integration accepted unsafe database name %s\n' "$unsafe_name" >&2
    exit 1
  fi
done

printf 'ok: runner integration rejects live and malformed database names\n'
