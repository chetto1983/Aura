# Aura sandbox-agent image: rivetdev/sandbox-agent (the "-full" tag ships node
# v22 but not python3) plus a Python toolchain, so sandbox_exec can run Python
# as well as Node. Installed as root, then back to the non-root sandbox user
# (uid 1001) used by the base image.
FROM rivetdev/sandbox-agent:0.4.2-full

USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        python3 \
        python3-pip \
        python-is-python3 \
    && rm -rf /var/lib/apt/lists/* \
    # The base image has no /workspace: the volume mount used to create it
    # root:root, so the sandbox user (uid 1001) could not write and named-volume
    # persistence broke. Bake it sandbox-owned — Docker copies this ownership
    # into the volume at first initialization.
    && install -d -o sandbox -g sandbox /workspace

# Bake the xlsx North-Star common dep set (D-36 hybrid leg, spike 006). These are the
# bundled-script import set of anthropics/skills/xlsx; baking them at IMAGE-BUILD time
# makes the 11-08 E2E offline-deterministic instead of relying on Docker Desktop's
# accidental NAT egress (spike 007: on a native-Linux non-masquerading bridge there is
# NO egress, so an unbaked dep would silently fail the E2E). Pinned exact versions. The
# long-tail deps stay on-demand uv (the `deps:` frontmatter leg), NOT baked here.
RUN python3 -m pip install --no-cache-dir --break-system-packages \
        openpyxl==3.1.5 \
        defusedxml==0.7.1 \
        lxml==5.3.0 \
        validators==0.34.0
USER sandbox
