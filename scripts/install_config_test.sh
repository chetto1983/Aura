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
[ "$CFG_LLM_MODEL" = "any/model-the-operator-pulled:v1" ] || { echo "FAIL: model=$CFG_LLM_MODEL" >&2; exit 1; }
[ "$CFG_EMBED_NGL" = "0" ] || { echo "FAIL: ngl=$CFG_EMBED_NGL" >&2; exit 1; }
[ "$CFG_GVISOR" = "false" ] || { echo "FAIL: gvisor=$CFG_GVISOR" >&2; exit 1; }
[ "$CFG_EMBED_IMAGE" = "ghcr.io/ggml-org/llama.cpp:server" ] || { echo "FAIL: embed_image=$CFG_EMBED_IMAGE" >&2; exit 1; }
# An empty secret must stay empty, not become the literal "base64 of nothing".
[ -z "$CFG_OPENROUTER_API_KEY" ] || { echo "FAIL: key should be empty, got $CFG_OPENROUTER_API_KEY" >&2; exit 1; }

# The claim above the parser -- that base64 keeps a hostile value from breaking the
# key=value format -- is only worth making if something proves it. Encoding neutralises a
# space, a shell metacharacter and an embedded '=' before the parser ever sees them, AND
# (length 2 mod 3, so the encoded form ends in exactly one '=') the encoded form itself ends
# in the padding byte that the old `IFS='=' read` split silently ate. A newline is no longer
# part of this fixture: parse_install_config now refuses one (see the rejection test below),
# so this round-trip case is scoped to the hostile properties that still must survive intact.
hostile="$(printf 'weird model:v1.2;key=value')"
{
  echo "format=1"
  echo "llm_model_base64=$(printf '%s' "$hostile" | base64 | tr -d '\n')"
} > "$fixture_root/hostile.conf"
CFG_LLM_MODEL=""
parse_install_config "$fixture_root/hostile.conf"
[ "$CFG_LLM_MODEL" = "$hostile" ] || { echo "FAIL: base64 did not protect a hostile value: $CFG_LLM_MODEL" >&2; exit 1; }

# A relative path must be refused: the config carries secrets and is resolved as root, so
# resolving it against an unknown cwd is how the wrong file gets read. Exit status alone
# would also be satisfied by a missing-file error or any other exit-2 path, so check the
# diagnostic too.
err="$fixture_root/relative.err"
if ( parse_install_config "relative/install.conf" ) 2>"$err"; then
  echo "FAIL: a relative config path was accepted" >&2
  exit 1
fi
grep -q 'requires an absolute path' "$err" \
  || { echo "FAIL: a relative path was refused for the wrong reason: $(cat "$err")" >&2; exit 1; }

# An unknown format must be refused rather than silently half-parsed.
future_conf="$fixture_root/future.conf"
printf 'format=99\n' > "$future_conf"
future_err="$fixture_root/future.err"
if ( parse_install_config "$future_conf" ) 2>"$future_err"; then
  echo "FAIL: an unknown config format was accepted" >&2
  exit 1
fi
grep -q 'unsupported config format' "$future_err" \
  || { echo "FAIL: an unsupported format was refused for the wrong reason: $(cat "$future_err")" >&2; exit 1; }

# A missing path must fail with its own diagnostic, not be mistaken for the relative-path
# or unsupported-format branch above.
missing_err="$fixture_root/missing.err"
if ( parse_install_config "$fixture_root/definitely-absent.conf" ) 2>"$missing_err"; then
  echo "FAIL: a missing config file was accepted" >&2
  exit 1
fi
grep -q 'config not found' "$missing_err" \
  || { echo "FAIL: a missing config failed for the wrong reason: $(cat "$missing_err")" >&2; exit 1; }

# format=1 is the forward-compatibility hook: a misspelled or corrupted key inside a KNOWN
# format can only be a typo, so the parser must refuse it rather than drop it silently.
printf 'format=1\nnot_a_real_key=x\n' > "$fixture_root/unknown.conf"
unknown_err="$fixture_root/unknown.err"
if ( parse_install_config "$fixture_root/unknown.conf" ) 2>"$unknown_err"; then
  echo "FAIL: an unknown config key was accepted" >&2
  exit 1
fi
grep -q "unknown key 'not_a_real_key'" "$unknown_err" \
  || { echo "FAIL: unknown key refused for the wrong reason: $(cat "$unknown_err")" >&2; exit 1; }

echo "ok: parse_install_config reads the v1 format and fails closed"

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

# The line above only proves the empty-value guard when read together with the three
# assertions before it -- alone it would pass against a no-op, because the fixture seeds
# that key. Applying a NON-empty value to the same field is the other half: together the
# pair pins the guard to this field rather than to the order of the block.
CFG_OPENROUTER_API_KEY="sk-from-config"
apply_install_config
grep -qx 'OPENROUTER_API_KEY=sk-from-config' .env \
  || { echo "FAIL: a non-empty config value did not overwrite the operator's key" >&2; exit 1; }
[ "$(grep -c '^OPENROUTER_API_KEY=' .env)" = 1 ] \
  || { echo "FAIL: overwriting the key duplicated it instead of replacing it" >&2; exit 1; }
CFG_OPENROUTER_API_KEY=""

# apply_install_config must be idempotent -- install.sh's contract is that re-running it
# preserves explicit settings, so a second apply replaces the key rather than duplicating it.
apply_install_config
[ "$(grep -c '^AURA_LLM_PROVIDER=' .env)" = 1 ] \
  || { echo "FAIL: a second apply duplicated the key instead of replacing it" >&2; exit 1; }

cd "$repo_root"

echo "ok: apply_install_config writes only what the config actually carries"

# The newline moved out of the hostile round-trip fixture above belongs here instead: a
# decoded value containing one must make parse_install_config exit non-zero, because
# set_env_value writes one line per value and a newline in it becomes a second .env line --
# env_value (first match) and docker compose (last wins) would then disagree about which
# secret is live. Wrapped in a subshell: the validation exit would otherwise end this whole
# test script.
newline_hostile="$(printf 'weird/model:v1.2\nOPENROUTER_API_KEY=sk-attacker-injected')"
{
  echo "format=1"
  echo "llm_model_base64=$(printf '%s' "$newline_hostile" | base64 | tr -d '\n')"
} > "$fixture_root/newline.conf"
newline_err="$fixture_root/newline.err"
if ( CFG_LLM_MODEL=""; parse_install_config "$fixture_root/newline.conf" ) 2>"$newline_err"; then
  echo "FAIL: a config value containing a line break was accepted" >&2
  exit 1
fi
grep -q 'line break' "$newline_err" \
  || { echo "FAIL: a line-break value was refused for the wrong reason: $(cat "$newline_err")" >&2; exit 1; }

echo "ok: parse_install_config refuses a decoded value containing a line break"

# set_env_value is not only reached through the parser above; a direct caller handing it a
# multi-line value must get the same refusal, with its own diagnostic so the two guards are
# distinguishable in a log, and .env must come out of the attempt with exactly one line for
# the key -- not silently split into two.
set_env_dir="$fixture_root/setenvtest"
mkdir -p "$set_env_dir"
cd "$set_env_dir"
printf 'AURA_LLM_MODEL=original-value\n' > .env
multiline_value="$(printf 'line-one\nline-two')"
set_env_err="$fixture_root/set_env_value.err"
if ( set_env_value AURA_LLM_MODEL "$multiline_value" ) 2>"$set_env_err"; then
  echo "FAIL: set_env_value accepted a multi-line value" >&2
  exit 1
fi
grep -q 'refusing to write' "$set_env_err" \
  || { echo "FAIL: set_env_value refused for the wrong reason: $(cat "$set_env_err")" >&2; exit 1; }
[ "$(grep -c '^AURA_LLM_MODEL=' .env)" = 1 ] \
  || { echo "FAIL: a rejected multi-line value left .env without exactly one line for the key" >&2; exit 1; }
cd "$repo_root"

echo "ok: set_env_value refuses a multi-line value and leaves .env untouched"
