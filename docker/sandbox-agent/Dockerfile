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
USER sandbox
