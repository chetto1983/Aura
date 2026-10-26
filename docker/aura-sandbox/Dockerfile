# docker/aura-sandbox/Dockerfile — the shared FAT full-capability per-identity box
# image (Phase 37, D-12/D-13). SandboxRouter.Resolve boots one container-per-identity
# from this image under the strict runtime profiles. The box runs the MODEL's shell
# (shell_exec / shell_bg / fs_read / fs_write / skill-snippet exec), NOT the aura
# daemon — so, unlike docker/aura/Dockerfile, it deliberately does NOT COPY the aura
# binary. Isolation is the BOUNDARY (per-identity named volume + cgroup caps + egress
# netns + host-exposure flags made unrepresentable in SandboxSpec), never a crippled
# inside.
#
# Posture (D-12, REVERT ec7fe2f6 — packaging-box.md): FAT, writable rootfs, container
# ROOT (NO `USER` directive), full shell + full runtime. MUST NOT be distroless /
# non-root / read-only-rootfs — each individually breaks shell_exec parity (distroless
# has no shell → exit 127; read-only blocks self-extension; USER 65532 forces non-root).
#
# Digest-pinned (D-13) so the shared box image is reproducible. Both the debian base and
# the astral uv image are @sha256-pinned. Dependabot (.github/dependabot.yml) refreshes the
# debian digest when bookworm-slim moves: Hub can collect a superseded, untagged manifest, as
# it did four times to aura-stt's whisper-server pin. The uv pin is a COPY --from, which
# Dependabot does not parse; resolved 2026-07-27:
#   ghcr.io/astral-sh/uv:0.11.32 -> sha256:df4cae8f3a96d175e2e5f992e597550000edbe78fdc2594d5cd8de1a217f504c
#
# landrun (MIT, github.com/zouuup/landrun) fences the Craft MCP servers' writes to /workspace with
# Landlock (internal/mcp/manager/catalog_craft.go). It is built from its module, which the Go checksum
# database verifies, and statically: the release binary needs glibc 2.38 and bookworm has 2.36. The
# fetch is retried for the reason docker/aura/Dockerfile gives at its `go mod download`.
FROM --platform=$BUILDPLATFORM golang:1.27.3-bookworm@sha256:972b4110ba0f47539a975c31e52c64765e1ff19f2d74469ae4a595c6676e80a5 AS landrun
ARG TARGETARCH
RUN set -e; \
    for attempt in 1 2 3; do \
      if CGO_ENABLED=0 GOOS=linux GOARCH="$TARGETARCH" GOBIN=/out \
           go install -trimpath -ldflags="-s -w" github.com/zouuup/landrun/cmd/landrun@v0.1.17; then break; fi; \
      if [ "$attempt" = 3 ]; then echo "landrun install failed after 3 attempts" >&2; exit 1; fi; \
      sleep $((attempt * 5)); \
    done; \
    cp "$(go env GOMODCACHE)/github.com/zouuup/landrun@v0.1.17/LICENSE" /out/LICENSE

FROM debian:bookworm-slim@sha256:7ff6ba31527b9e336162fa061746c366c4b2032f4d2cff2078bea43f78efe3f8

# A cached RUN keeps the packages it installed: on 2026-10-26 aura-sandbox:edge still had the
# libssl3 of 2026-10-24, deb12u1, while bookworm had deb12u2 (CVE-2026-84782, issue #189). The
# builders pass scripts/apt_week.sh, so every layer below runs again once a week, and apt-get
# upgrade also takes the fixes the base itself lacks.
ARG APT_WEEK=""

# --- heavy runtime layer (cache-stable: ordered FIRST so a later tweak re-runs only the
# small trailing layers, never this ~apt/node/uv layer). D-13 bakes the common runtimes;
# uv installs the long tail on-demand into the identity's warm cache at /root/.cache/uv
# (mounted by the usersandbox translator), which makes skill `deps:` frontmatter load-bearing.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && sed -i 's|http://deb.debian.org|https://deb.debian.org|g' /etc/apt/sources.list.d/debian.sources \
    && apt-get update \
    && apt-get upgrade -y \
    && apt-get install -y --no-install-recommends \
        curl \
        git \
        gnupg \
        jq \
        python3 \
        python3-pip \
        python3-venv \
    && rm -rf /var/lib/apt/lists/*

# GitHub CLI (`gh`) from the official cli.github.com apt repo (D-13 bakes gh so the box's
# shell has first-class GitHub parity). signed-by keyring, mirroring the pgdg repo posture
# in docker/aura/Dockerfile.
RUN install -d -m 0755 /etc/apt/keyrings \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        -o /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/*

# Node 24 LTS (Debian bookworm ships Node 18, too old). NodeSource bundles npm/npx — the
# box's JS skill snippets (internal/skills/snippet.go LangJS -> node) and the
# `npx skills find/add` self-extension loop both need it. Mirrors docker/aura/Dockerfile,
# npm 11.21.1 included.
RUN curl -fsSL https://deb.nodesource.com/setup_24.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && npm install -g npm@11.21.1 \
    && npm cache clean --force \
    && rm -rf /var/lib/apt/lists/*

# pnpm 10+ refuses to run a dependency's install/postinstall scripts unless the operator
# approves them, and `pnpm approve-builds` is INTERACTIVE — which no box shell can answer.
# That gate hard-blocks the `web-artifacts-builder` skill: its bundle-artifact.sh runs
# `pnpm add -D parcel @parcel/config-default parcel-resolver-tspaths html-inline`, and all
# four native deps (@parcel/watcher, @swc/core, lmdb, msgpackr-extract) need their build
# scripts. Parcel then re-enters the same wall from its own runDepsStatusCheck → runPnpmCli.
#
# MEASURED 2026-08-29 in this image against pnpm 11.24.0 (what `npm install -g pnpm` resolves
# to today): the documented precise escape hatch — `onlyBuiltDependencies` in
# pnpm-workspace.yaml — did NOT clear ERR_PNPM_IGNORED_BUILDS, neither inside `pnpm add` nor
# on a follow-up `pnpm install`. This env var was the only non-interactive setting that let
# all four compile. Measured on pnpm 11.24.0 only: it does NOT establish behaviour for other
# pnpm majors, and it is NOT a claim that the precise list is wrong in general.
#
# Allowing arbitrary postinstall scripts is acceptable HERE and only here: the box already
# runs the model's own shell (a plain `npm install` executes the same code), so the container
# boundary — per-identity volume + cgroup caps + egress netns — is the control, never pnpm's
# prompt. Do NOT copy this into docker/aura/Dockerfile: the daemon image is not the box.
ENV PNPM_CONFIG_DANGEROUSLY_ALLOW_ALL_BUILDS=true

# uv/uvx as static binaries from the DIGEST-PINNED astral image (sandbox-runtime finding:
# COPY the binaries, never pip-install uv). uv drives the on-demand long-tail dep installs
# into the identity's warm-cache volume (D-13).
COPY --from=ghcr.io/astral-sh/uv@sha256:df4cae8f3a96d175e2e5f992e597550000edbe78fdc2594d5cd8de1a217f504c /uv /uvx /usr/local/bin/

# Documents, photos and video are the Craft MCP servers' work (installed below,
# docs/superpowers/specs/2026-10-18-craft-mcp-design.md), not a shell toolchain's: LibreOffice,
# pandoc, pdftk (and its Java), tesseract, qpdf and poppler left this image with the Python and Node
# Office libraries. file/xxd sniff workspace artifacts; fonts-liberation is the Arial/Times-metric
# family Chromium renders with.
RUN apt-get update \
    && apt-get install -y --no-install-recommends file xxd fonts-liberation \
    && rm -rf /var/lib/apt/lists/*

# The agent installs into this Python at RUNTIME, and shell_exec's own description tells it
# exactly how: "Pick ONE interpreter per task and install into it: `python3 -m pip install
# ...`". On Debian bookworm that command EXITS 1 with `error: externally-managed-environment`
# (PEP 668) -- so the tool prompt was teaching a command the box could not run, and the model
# had no way to learn otherwise except by burning a call on the failure. Measured 2026-09-07
# on aura-sandbox:latest: rc=1, no network reached.
#
# The build below already passes --break-system-packages for exactly this reason; this makes
# the same allowance true for the agent, which is the only user that matters at runtime. The
# usual objection to it -- do not let pip fight the system package manager on a real machine
# -- does not apply here: this container IS the isolation boundary, it is disposable, and
# `python3 -m venv` remains available for anyone who wants one.
ENV PIP_BREAK_SYSTEM_PACKAGES=1

# Match the Python driver and browser revision; preinstall both so validation needs no downloads.
# https://playwright.dev/python/docs/docker#build-your-own-image
ENV PLAYWRIGHT_BROWSERS_PATH=/opt/playwright-browsers
RUN python3 -m pip install --no-cache-dir playwright==1.62.0 \
    && python3 -m playwright install --with-deps chromium \
    && rm -rf /var/lib/apt/lists/*

# The data stack: pandas computes over a table, openpyxl is how it reads an .xlsx (a spreadsheet is
# EDITED by the gridcraft server), and defusedxml is what openpyxl's documentation asks for against
# quadratic-blowup and billion-laughs XML. Pillow and lxml left with the Office libraries that needed them.
RUN pip install --no-cache-dir --break-system-packages \
        openpyxl pandas defusedxml \
    && find /usr/lib/python3 /usr/local/lib -depth -type d -name __pycache__ -exec rm -rf {} +

# Mount points (documented — the ACTUAL mounts are set by the usersandbox translator, not
# baked here; D-10/D-13):
#   /workspace          -> the per-identity named volume aura-box-<id> (persistent, RETAINED on suspend)
#   /workspace/.scratch -> a tmpfs RW scratch (ephemeral)
#   /root/.cache/uv     -> aura-box-<id>-uv-cache
#   /root/.npm          -> aura-box-<id>-npm-cache
#   /root/.cache/pip    -> aura-box-<id>-pip-cache
# Pre-create the dirs so a first exec before any mount still has a writable tree.
RUN mkdir -p /workspace /root/.cache/uv /root/.npm /root/.cache/pip

COPY internal/skills/embed/web-artifacts-builder/scripts/init-artifact.sh internal/skills/embed/web-artifacts-builder/scripts/shadcn-components.tar.gz /opt/aura-artifact-bootstrap/
# pnpm 12.15.0 bundles undici 6.29.1; 12.3.4's 6.28.0 carried two fixable HIGHs. html-inline 1.2.0
# pulls minimist 1.1.3 and 0.0.10, both CVE-2021-44906 (CRITICAL). The override in the template's
# pnpm-workspace.yaml resolves both to 1.2.8, and every project init-artifact.sh copies from the
# template keeps it. Measured on 2026-10-24 in node:24-bookworm-slim: html-inline still runs.
RUN npm install -g pnpm@12.15.0 \
    && mkdir -p /opt/aura-artifacts \
    && cd /opt/aura-artifacts \
    && bash /opt/aura-artifact-bootstrap/init-artifact.sh template \
    && cd template \
    && printf '\noverrides:\n  minimist: "^1.2.8"\n' >> pnpm-workspace.yaml \
    && pnpm add -D parcel@2.16.4 @parcel/config-default@2.16.4 parcel-resolver-tspaths@0.0.9 html-inline@1.2.0 \
    && pnpm run build
RUN ln -s /opt/aura-artifacts/template/node_modules/typescript/bin/tsc /usr/local/bin/tsc \
    && ln -s /opt/aura-artifacts/template/node_modules/vite/bin/vite.js /usr/local/bin/vite
COPY internal/skills/embed/web-artifacts-builder /opt/aura-skills/web-artifacts-builder

# agent-browser — the measured authenticated browser (prd.md §12). The native daemon needs no
# Node; it drives the Playwright Chromium above. Both the npm tarball and the extracted binary are
# sha256-pinned (0.38.2, resolved 2026-10-06 against registry dist.integrity sha512-b/7hvo2R…).
# 0.38.2 drains Chrome's stderr for the browser's lifetime: 0.38.1 stopped reading it after launch,
# and Chrome freezes once its log fills the 64 KiB pipe (upstream #2003; prd.md §12).
# /usr/local/bin/agent-browser is Aura's entry point, which supplies the per-identity state key,
# the workspace-volume HOME and the short autosave the box lifecycle needs.
ARG TARGETARCH
RUN set -eu; \
    case "${TARGETARCH:-amd64}" in \
        amd64) bin=agent-browser-linux-x64;   bin_sha=a54b765192db774666f0513fa8b545a298753b6f29e73bcdf4a1e78f18e7c0e1 ;; \
        arm64) bin=agent-browser-linux-arm64; bin_sha=690c02d952de8497bba4f8cc58b59acbf27dc27b346755869b518f4b411c7f40 ;; \
        *) echo "agent-browser: unsupported arch ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /tmp/agent-browser.tgz https://registry.npmjs.org/agent-browser/-/agent-browser-0.38.2.tgz; \
    echo "2bb1d6e4660b2a109c912c8bb552f727125dbcd681c6d1eefc1af573b4546c49  /tmp/agent-browser.tgz" | sha256sum -c -; \
    tar -xzf /tmp/agent-browser.tgz -C /tmp "package/bin/${bin}" package/LICENSE; \
    echo "${bin_sha}  /tmp/package/bin/${bin}" | sha256sum -c -; \
    install -D -m 0755 "/tmp/package/bin/${bin}" /usr/local/lib/agent-browser/agent-browser; \
    install -D -m 0644 /tmp/package/LICENSE /usr/local/share/licenses/agent-browser/LICENSE; \
    rm -rf /tmp/agent-browser.tgz /tmp/package
# Google refuses to sign anyone in to a browser that announces automation. Measured 2026-09-27 in
# a box: the stock headless Chromium (user agent `HeadlessChrome/151`, navigator.webdriver true)
# was sent to accounts.google.com/v3/signin/rejected at the email step, while with
# AutomationControlled off and a plain Chrome user agent it went on to the account, and the operator
# then signed in through the live view, password and second factor included. The entry point sets
# both. The user agent names the Chromium this image ships, read from the binary (`Google Chrome
# for Testing 151.0.7922.34`), so an upgrade cannot leave a stale version behind.
RUN set -eu; \
    set -- /opt/playwright-browsers/chromium-*/chrome-linux64/chrome; \
    [ "$#" -eq 1 ] && [ -x "$1" ] || { echo "agent-browser: want one Playwright Chromium, found: $*" >&2; exit 1; }; \
    major="$("$1" --version | grep -oE '[0-9]+(\.[0-9]+){3}' | cut -d. -f1)"; \
    [ -n "$major" ] || { echo "agent-browser: cannot read the version of $1" >&2; exit 1; }; \
    mkdir -p /usr/local/share/aura; \
    echo "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/${major}.0.0.0 Safari/537.36" \
        > /usr/local/share/aura/browser-user-agent
# The workspace-volume HOME both the entry point and the live-view relay read (state under /root
# died with a box recreate).
ENV AURA_AGENT_BROWSER_HOME=/workspace/.agent-browser-home
COPY --chmod=0755 docker/aura-sandbox/agent-browser.sh /usr/local/bin/agent-browser

# The Craft apps' MCP servers, run in this box by the gridcraft, pdfcraft, wordcraft, deckcraft,
# photocraft and filmcraft recipes (internal/mcp/manager/catalog_craft.go). Official, unmodified
# release builds (the ArtCraft brand terms allow those as they are), each tarball pinned by the SHA-256
# its GitHub release publishes; only the CLI leaves it, with its licenses and font notices. PdfCraft's
# is its CLI-only package: no OCR models, which the agent does not need (it reads a rendered page).
RUN set -eu; \
    case "${TARGETARCH:-amd64}" in \
        amd64) arch=x86_64 ;; \
        arm64) arch=aarch64 ;; \
        *) echo "craft: unsupported arch ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    craft() { \
        app=$1; ver=$2; asset="$3-linux-$arch.tar.gz"; \
        if [ "$arch" = x86_64 ]; then sha=$4; else sha=$5; fi; \
        curl -fsSL -o /tmp/craft.tgz "https://github.com/storytold/$app/releases/download/v$ver/$asset"; \
        echo "$sha  /tmp/craft.tgz" | sha256sum -c -; \
        mkdir /tmp/craft && tar -xzf /tmp/craft.tgz -C /tmp/craft --strip-components 1; \
        install -D -m 0755 "$(find /tmp/craft -type f -name "$app-cli" | head -1)" "/usr/local/bin/$app-cli"; \
        mkdir -p "/usr/local/share/licenses/$app"; \
        find /tmp/craft -type f \( -name 'LICENSE*' -o -name 'NOTICE*' -o -name 'OFL*' -o -name 'ATTRIBUTION*' \) \
            -exec cp {} "/usr/local/share/licenses/$app/" \; ; \
        rm -rf /tmp/craft /tmp/craft.tgz; \
    }; \
    craft gridcraft 0.11.1 gridcraft-0.11.1 \
        f2c83e6c26449a6c45983e903bf32c1d4c302b2042f263ead995271129c70e87 \
        16e4da4873867a9080ec41d76e38430a8d0e62fa22b43edb6d7b1dbde0925435; \
    craft pdfcraft 0.13.0 pdfcraft-cli-0.13.0 \
        fd40f80157856cadd4348a464ef0acde8f19894d831d78e9823708ecf70a3352 \
        1d1181757f281b2291ca218a0446ade4cfd54c5c258f3d4f22953fa1037dd29d; \
    craft wordcraft 0.10.0 wordcraft-0.10.0 \
        ca99ea350ba0aea4103fc8020cad98f051c58d9e9fcf670c119d810108cc45ff \
        77e838120d9da38752ab355066a0987253d9bc95dfddad1c47bac1f180975bbb; \
    craft deckcraft 0.10.0 deckcraft-0.10.0 \
        546616eb2300ce8efb20b44a53e60abfe6b96d71ac606b452a36fd21c6b1ff28 \
        7be0594d90c4c0be15b812a597b85f6005271716be778b2999cb492f52c8676b; \
    craft photocraft 0.15.1 photocraft-0.15.1 \
        b622be40355513056f8152afe895d0a05c1bc3c344a027f772a9a12fa3d1c223 \
        ff2ea1945df3b413b17c64ec7c0131c093b09c717994a47b4c46c0dabf1c822f; \
    craft filmcraft 0.12.0 filmcraft-0.12.0 \
        ddff0a2ec0d89e5e9e65644ed724b5b79959600b1602fd4ea6653b5a54bf88fd \
        83714c4a73774a3f54a3d2bf52ae0065d3fb8ca8c781deb7cb3450f9b4e74568
COPY --from=landrun /out/landrun /usr/local/bin/landrun
COPY --from=landrun /out/LICENSE /usr/local/share/licenses/landrun/LICENSE
# Chrome must not sign itself in to the Google account a user logs into on the web. Measured
# 2026-09-27 on the lab VM: after an operator signed in to YouTube through the live view, Chrome's
# account consistency (DICE) opened chrome://signin-error and a new tab, and the session's daemon
# answered no command again until it was SIGKILLed. BrowserSignin 0 turns DICE off
# (chrome://signin-internals: "Account Consistency: None" instead of "DICE") and leaves Google's
# web sign-in working. Chrome for Testing reads this directory, not /etc/opt/chrome/policies
# (components/policy/core/common/policy_paths.cc).
COPY --chmod=0644 docker/aura-sandbox/chrome-policy.json /etc/opt/chrome_for_testing/policies/managed/aura.json
COPY --chmod=0644 docker/aura-sandbox/browser-relay.mjs /usr/local/lib/aura/browser-relay.mjs

WORKDIR /workspace

# NO `USER` directive (root inside — D-12 parity). NO aura binary and NO privilege-dropping
# entrypoint: the box is driven by `docker exec` (usersandbox.DockerBackend), so the default
# command is just a keep-alive PID the daemon execs into. AutoRemove=false is set by the
# translator (Pitfall 5: --rm would destroy a suspendable box), not here.
CMD ["sleep", "infinity"]
