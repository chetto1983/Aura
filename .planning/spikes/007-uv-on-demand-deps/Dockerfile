# Spike 007 image — pip + uv baked as TOOLING (no curated dep set): proves the
# on-demand dependency-install path the user proposed as alternative/complement
# to spike-006's build-time bake. uv from the official static binary image
# (no curl|sh installer — supply-chain discipline).
FROM aura-sandbox-agent:py3

COPY --from=ghcr.io/astral-sh/uv:0.7.13 /uv /uvx /usr/local/bin/
USER root
# Writable uv cache + a default venv dir for the sandbox user.
RUN install -d -o sandbox -g sandbox /home/sandbox/.cache /opt/venvs && chown sandbox:sandbox /opt/venvs
USER sandbox
