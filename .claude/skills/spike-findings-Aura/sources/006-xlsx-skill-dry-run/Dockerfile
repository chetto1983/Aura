# Spike 006 image — proves the dependency-bake fix for the North-Star:
# the production aura-sandbox-agent:py3 image lacks openpyxl/pandas (the
# Phase-5 curated bake never carried over to the sandbox-agent pivot) and the
# runtime bridge is egressless, so deps MUST be baked at build time.
# Phase-11 7e should fold a hash-pinned curated set into
# docker/sandbox-agent/Dockerfile (Phase-5 D-20 discipline); this spike image
# bakes the minimum to prove the mechanism.
FROM aura-sandbox-agent:py3

USER root
# PEP 668: Debian 12's python3.11 is externally-managed; the sandbox image has
# no venv tooling and IS the isolation boundary — break-system-packages is fine.
RUN pip3 install --no-cache-dir --break-system-packages \
    openpyxl==3.1.5 defusedxml==0.7.1 lxml==5.3.0 validators==0.34.0
USER sandbox
