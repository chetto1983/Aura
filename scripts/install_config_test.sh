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
# An empty secret must stay empty, not become the literal "base64 of nothing".
[ -z "$CFG_OPENROUTER_API_KEY" ] || { echo "FAIL: key should be empty, got $CFG_OPENROUTER_API_KEY" >&2; exit 1; }

# The claim above the parser -- that base64 keeps a hostile value from breaking the
# key=value format -- is only worth making if something proves it. A newline would end
# the record early and a bare '=' would be eaten by IFS.
hostile="$(printf 'weird/model:v1\nkey=value')"
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

echo "ok: parse_install_config reads the v1 format and fails closed"
