#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
source "$repo_root/scripts/install.sh"
export AURA_PAYLOAD_DIR="$repo_root"

# No network is needed to exercise the real .env upgrade path. Generated secrets
# are fixture-only; embedding provenance is outside this runtime-posture contract.
ensure_embed_provenance() { :; }

for profile in missing dev local_trusted single_user_hardened server_production; do
  mkdir "$fixture/$profile"
  (
    cd "$fixture/$profile"
    printf 'POSTGRES_PASSWORD=fixture-do-not-change\n' > .env
    if [[ "$profile" != missing ]]; then
      printf 'AURA_PROFILE=%s\nAURA_MUSR_ISOLATION=false\n' "$profile" >> .env
    fi
    APPLIANCE=1
    write_env_if_missing
    expected=single_user_hardened
    [[ "$profile" != server_production ]] || expected=server_production
    [[ "$(env_value AURA_PROFILE)" == "$expected" ]] || {
      echo "FAIL: appliance upgrade from $profile did not choose $expected" >&2; exit 1;
    }
    [[ "$(env_value AURA_MUSR_ISOLATION)" == true ]] || {
      echo 'FAIL: appliance upgrade left identity isolation disabled' >&2; exit 1;
    }
    [[ "$(env_value POSTGRES_PASSWORD)" == fixture-do-not-change ]] || exit 1
    cp .env before.env
    write_env_if_missing
    cmp -s .env before.env || { echo 'FAIL: second upgrade changed .env' >&2; exit 1; }
  )
done
echo 'ok: appliance installs and upgrades enforce a strict identity-isolated posture'

mkdir "$fixture/dev-checkout"
(
  cd "$fixture/dev-checkout"
  printf 'AURA_PROFILE=dev\nAURA_MUSR_ISOLATION=false\n' > .env
  APPLIANCE=0
  write_env_if_missing
  [[ "$(env_value AURA_PROFILE)" == dev && "$(env_value AURA_MUSR_ISOLATION)" == false ]]
)
echo 'ok: a development checkout does not become an appliance'

mkdir "$fixture/duplicates"
(
  cd "$fixture/duplicates"
  printf 'AURA_PROFILE=server_production\nAURA_PROFILE="dev"\nAURA_MUSR_ISOLATION=false\nAURA_MUSR_ISOLATION=false\nKEEP=value=with=equals\n' > .env
  bash "$repo_root/scripts/appliance_posture.sh" .env
  [[ "$(grep -c '^AURA_PROFILE=' .env)" == 1 ]]
  [[ "$(grep -c '^AURA_MUSR_ISOLATION=' .env)" == 1 ]]
  grep -qx 'AURA_PROFILE=single_user_hardened' .env
  grep -qx 'KEEP=value=with=equals' .env
  [[ "$(stat -c %a .env)" == 600 ]]
  printf 'AURA_PROFILE=invalid-profile\nKEEP=unchanged\n' > .env
  cp .env before.env
  if bash "$repo_root/scripts/appliance_posture.sh" .env 2>error; then
    echo 'FAIL: an unknown profile was silently accepted' >&2; exit 1
  fi
  grep -q 'unknown AURA_PROFILE' error
  cmp -s .env before.env
)
echo 'ok: migration resolves duplicates, preserves other values, and refuses unknown profiles'
