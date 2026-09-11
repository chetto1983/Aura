#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT

# shellcheck source=/dev/null
source "$repo_root/scripts/install.sh"

b64() { printf '%s' "$1" | base64 | tr -d '\n'; }

# expect_refused CONFIG DIAGNOSTIC WHAT: parse_install_config must exit non-zero on CONFIG and
# say DIAGNOSTIC. Exit status alone would also be satisfied by any other exit-2 path, so the
# diagnostic is checked too. Run in a subshell: the refusal's exit would end this script.
expect_refused() {
  local err="$fixture_root/refused.err"
  if ( parse_install_config "$1" ) 2>"$err"; then
    echo "FAIL: $3 was accepted" >&2
    exit 1
  fi
  grep -q "$2" "$err" || { echo "FAIL: $3 was refused for the wrong reason: $(cat "$err")" >&2; exit 1; }
}

conf="$fixture_root/install.conf"
{
  echo "format=2"
  echo "install_dir_base64=$(b64 /opt/aura)"
  echo "appliance=true"
  echo "gvisor=false"
} > "$conf"

parse_install_config "$conf"

[ "$CFG_INSTALL_DIR" = "/opt/aura" ] || { echo "FAIL: install_dir=$CFG_INSTALL_DIR" >&2; exit 1; }
[ "$CFG_APPLIANCE" = "true" ] || { echo "FAIL: appliance=$CFG_APPLIANCE" >&2; exit 1; }
[ "$CFG_GVISOR" = "false" ] || { echo "FAIL: gvisor=$CFG_GVISOR" >&2; exit 1; }

# The claim above the parser -- that base64 keeps a hostile value from breaking the key=value
# format -- is only worth making if something proves it: a space, a shell metacharacter and an
# embedded '=' must survive, and the encoded form must end in exactly the one '=' padding byte
# that the old `IFS='=' read` split silently ate.
hostile='/opt/weird dir;key=value12'
case "$(b64 "$hostile")" in
  *[!=]=) ;;
  *) echo "FAIL: the hostile fixture must encode to exactly one padding byte" >&2; exit 1 ;;
esac
printf 'format=2\ninstall_dir_base64=%s\n' "$(b64 "$hostile")" > "$fixture_root/hostile.conf"
CFG_INSTALL_DIR=""
parse_install_config "$fixture_root/hostile.conf"
[ "$CFG_INSTALL_DIR" = "$hostile" ] || { echo "FAIL: base64 did not protect a hostile value: $CFG_INSTALL_DIR" >&2; exit 1; }

# The config carries the operator's answers and is resolved as root, so a relative path
# resolved against an unknown cwd is how the wrong file gets read.
expect_refused "relative/install.conf" 'requires an absolute path' 'a relative config path'
printf 'format=99\n' > "$fixture_root/future.conf"
expect_refused "$fixture_root/future.conf" 'unsupported config format' 'an unknown config format'
# Format 1 carried the model route and the OpenRouter key. The web setup owns those answers
# now, so an old config is refused rather than half-applied.
{ echo "format=1"; echo "install_dir_base64=$(b64 /opt/aura)"; } > "$fixture_root/v1.conf"
expect_refused "$fixture_root/v1.conf" 'unsupported config format' 'a format 1 config'
expect_refused "$fixture_root/definitely-absent.conf" 'config not found' 'a missing config file'

# Inside a known format a key naming nothing can only be a typo or corruption. The embed keys
# the 0.1.x wizard sent would be a second authority over the backend install.sh detects, and
# the llm_ and openrouter_ keys a second authority over the web setup: all are refused.
for key in not_a_real_key embed_image_base64 embed_ngl_base64 llm_provider_base64 \
           llm_base_url_base64 llm_model_base64 openrouter_api_key_base64; do
  printf 'format=2\n%s=%s\n' "$key" "$(b64 x)" > "$fixture_root/$key.conf"
  expect_refused "$fixture_root/$key.conf" "unknown key '$key'" "a config carrying $key"
done

# A decoded value with a line break would make set_env_value write a second .env line, and
# env_value (first match) and docker compose (last wins) would then disagree.
printf 'format=2\ninstall_dir_base64=%s\n' "$(b64 "$(printf '/opt/aura\nAURA_IMAGE=attacker')")" \
  > "$fixture_root/newline.conf"
expect_refused "$fixture_root/newline.conf" 'line break' 'a config value containing a line break'

echo "ok: parse_install_config reads format 2 and fails closed"

# The model route and the OpenRouter key are the web setup's: nothing in the installer writes
# them, and the fresh .env template carries no key for the operator to fill in.
if declare -F apply_install_config >/dev/null; then
  echo "FAIL: apply_install_config still exists" >&2
  exit 1
fi
if grep -q 'OPENROUTER_API_KEY' "$repo_root/scripts/install.sh"; then
  echo "FAIL: install.sh still names OPENROUTER_API_KEY" >&2
  exit 1
fi

echo "ok: the installer writes no model route and no OpenRouter key"

# set_env_value is not only reached through the parser; a direct caller handing it a
# multi-line value must get the same refusal, with its own diagnostic so the two guards are
# distinguishable in a log, and .env must come out with exactly one line for the key.
set_env_dir="$fixture_root/setenvtest"
mkdir -p "$set_env_dir"
cd "$set_env_dir"
printf 'AURA_EXAMPLE_KEY=original-value\n' > .env
multiline_value="$(printf 'line-one\nline-two')"
set_env_err="$fixture_root/set_env_value.err"
if ( set_env_value AURA_EXAMPLE_KEY "$multiline_value" ) 2>"$set_env_err"; then
  echo "FAIL: set_env_value accepted a multi-line value" >&2
  exit 1
fi
grep -q 'refusing to write' "$set_env_err" \
  || { echo "FAIL: set_env_value refused for the wrong reason: $(cat "$set_env_err")" >&2; exit 1; }
[ "$(grep -c '^AURA_EXAMPLE_KEY=' .env)" = 1 ] \
  || { echo "FAIL: a rejected multi-line value left .env without exactly one line for the key" >&2; exit 1; }
cd "$repo_root"

echo "ok: set_env_value refuses a multi-line value and leaves .env untouched"
