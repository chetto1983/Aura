#!/usr/bin/env bash
# The per-identity box image's runtime contract for `pip`.
#
# shell_exec's description tells the agent, in the schema it reads at the moment it decides,
# to "Pick ONE interpreter per task and install into it: `python3 -m pip install ...`". On
# Debian bookworm that command exits 1 with `error: externally-managed-environment` (PEP 668)
# before a single byte leaves the box, so the prompt was teaching a command the image could
# not run. docker/aura-sandbox/Dockerfile now sets PIP_BREAK_SYSTEM_PACKAGES=1; this proves
# it, on the shipped image, at runtime.
#
# Every assertion here is OFFLINE. A network install would test the runner's egress rather
# than the image, and would go red on any host behind a TLS-intercepting proxy -- which is
# how this defect was found in the first place (pypi reachable, certificate untrusted).
set -euo pipefail

img="${AURA_SANDBOX_IMAGE:-aura-sandbox:latest}"

# 1. The variable is in the image CONFIG, not merely honoured by a build-time flag. A future
#    rewrite that moves the pip layer without carrying the ENV would still pass assertion 2
#    if the caller happened to export it, so this pins the image itself.
env_line="$(docker image inspect "$img" --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | grep '^PIP_BREAK_SYSTEM_PACKAGES=' || true)"
[ "$env_line" = "PIP_BREAK_SYSTEM_PACKAGES=1" ] || {
  echo "FAIL: image config has ${env_line:-no PIP_BREAK_SYSTEM_PACKAGES}, want PIP_BREAK_SYSTEM_PACKAGES=1" >&2
  exit 1
}

# 2. A REAL install, start to finish, with no index and no network: pip builds a throwaway
#    package with the setuptools already in the image and writes it into site-packages, and
#    python then imports it. Asserting only "no externally-managed-environment in the output"
#    would pass on an image whose pip is broken for some other reason; this cannot.
#    Verified to FAIL on the pre-fix image (rc=1, externally-managed-environment).
docker run --rm --entrypoint bash "$img" -c '
set -e
mkdir -p /tmp/probe/aura_pip_probe
printf "[build-system]\nrequires = [\"setuptools\"]\nbuild-backend = \"setuptools.build_meta\"\n[project]\nname = \"aura-pip-probe\"\nversion = \"0.0.1\"\n" > /tmp/probe/pyproject.toml
echo "VALUE = 42" > /tmp/probe/aura_pip_probe/__init__.py

if ! python3 -m pip install --no-input --no-index --no-build-isolation /tmp/probe > /tmp/pip.log 2>&1; then
  echo "FAIL: python3 -m pip install exited $?, which is the command shell_exec teaches" >&2
  tail -20 /tmp/pip.log >&2
  exit 1
fi
if grep -q externally-managed-environment /tmp/pip.log; then
  echo "FAIL: PEP 668 still blocks the agent installing into the box Python" >&2
  exit 1
fi
python3 -c "import aura_pip_probe; assert aura_pip_probe.VALUE == 42"
echo "pip ok: python3 -m pip install landed in site-packages and imported"
'

# 3. The documented alternative keeps working. It is what a caller should reach for when a
#    task genuinely wants an isolated interpreter, and it must not become the only option
#    again without this test noticing.
docker run --rm --entrypoint bash "$img" -c '
python3 -m venv /tmp/v >/dev/null 2>&1 || { echo "FAIL: python3 -m venv is unavailable" >&2; exit 1; }
[ -x /tmp/v/bin/python ] || { echo "FAIL: the venv has no interpreter" >&2; exit 1; }
echo "venv ok: python3 -m venv still builds a usable interpreter"
'

echo "ok: sandbox image pip contract"
