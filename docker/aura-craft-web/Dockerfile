# The five Craft apps Aura builds from source, published as their own image so the aura image
# copies them by digest instead of compiling them (docker/aura/Dockerfile). Unlike GridCraft,
# whose release the aura Dockerfile takes as it is, these need a change: WordCraft, DeckCraft,
# PhotoCraft and FilmCraft have no bridge upstream, PhotoCraft draws its web interface in English
# whatever the browser's language, and PdfCraft picks languages its web build has no face for.
# docker/craft-web builds each from its release tag, checked against the tag's commit, with the
# patch beside the script (that change, and the ArtCraft marks a modified build may not carry).
# WebAssembly is the same on every platform, so it builds once, on the builder's. The tools are
# release binaries pinned by SHA-256; trunk runs offline, so it fetches none of its own.
#
# The inputs are this file and docker/craft-web/; scripts/craft_web_inputs.sh hashes them into
# the tag publish-craft-web.yml pushes, the aura Dockerfile names that tag beside its digest, and
# TestCraftWebImagePinMatchesItsInputs fails when the two drift. This file lives outside
# docker/craft-web/ on purpose: the stage below copies that directory whole, and one more file in
# it would change the cache key of every layer after the copy.
FROM --platform=$BUILDPLATFORM rust:1.97.0-bookworm@sha256:8fa55b2f3ddf97471ab6a767bfa3f37e6bad0986ba823e75fea57e2a2a5c3073 AS craft-web
ARG BUILDARCH
RUN set -eu; \
    case "$BUILDARCH" in \
        amd64) arch=x86_64; trunk=f2b4680cd239693a646a2795e4633c625328d7b2a044fbe749fa3a2fe9e7036b; \
               bindgen=82d12bb940e2d4e72e0d5605387fc1b8ca179044e012b620f0ce4e7440e8320e; \
               binaryen=e959f2170af4c20c552e9de3a0253704d6a9d2766e8fdb88e4d6ac4bae9388fe ;; \
        arm64) arch=aarch64; trunk=b1d8e60e454f7fc182d9a4d95d1506ffbae947d8ba90f8f6f02da93b60f980f9; \
               bindgen=2ed4351c35dd9440308bbb02767d47ea278efe851a52465300f3c94f5b6c2a87; \
               binaryen=4b6bd61ba6cd3b18c993b4657d93426c782f9b91b74be0d38018cd8be1319376 ;; \
        *) echo "craft-web: unsupported build arch ${BUILDARCH}" >&2; exit 1 ;; \
    esac; \
    tool() { \
        curl -fsSL -o /tmp/tool.tgz "$1"; echo "$2  /tmp/tool.tgz" | sha256sum -c -; \
        mkdir /tmp/tool && tar -xzf /tmp/tool.tgz -C /tmp/tool; \
        install -m 0755 "/tmp/tool/$3" /usr/local/bin/; rm -rf /tmp/tool /tmp/tool.tgz; \
    }; \
    tool "https://github.com/trunk-rs/trunk/releases/download/v0.21.14/trunk-$arch-unknown-linux-gnu.tar.gz" "$trunk" trunk; \
    tool "https://github.com/wasm-bindgen/wasm-bindgen/releases/download/0.2.129/wasm-bindgen-0.2.129-$arch-unknown-linux-musl.tar.gz" "$bindgen" "wasm-bindgen-0.2.129-$arch-unknown-linux-musl/wasm-bindgen"; \
    tool "https://github.com/WebAssembly/binaryen/releases/download/version_123/binaryen-version_123-$arch-linux.tar.gz" "$binaryen" binaryen-version_123/bin/wasm-opt; \
    rustup target add wasm32-unknown-unknown
COPY docker/craft-web/ /craft-web/
RUN /craft-web/build.sh pdfcraft v0.13.0 53199fb4fa36f3c95b539add6218c57aeb2f823c /apps
RUN /craft-web/build.sh wordcraft v0.10.0 9130181a5fdaee2293e67756fd5458c601489f3c /apps
RUN /craft-web/build.sh deckcraft v0.10.0 b47195feec0f0fe4220d8943ad1c8add5dad8072 /apps
RUN /craft-web/build.sh photocraft v0.15.1 60c87b9884fdae0e464614d482a3bc435cbbc578 /apps
RUN /craft-web/build.sh filmcraft v0.12.0 274c4cf85ba638d4b6f867d97ca3f6478a993c8d /apps

FROM scratch
COPY --from=craft-web /apps /apps
