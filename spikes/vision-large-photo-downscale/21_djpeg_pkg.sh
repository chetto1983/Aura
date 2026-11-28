#!/bin/bash
# What libjpeg-turbo-progs adds to a bookworm image, and whether aura:edge already carries its library.
docker run --rm vision-spike-djpeg:local sh -c 'dpkg -s libjpeg-turbo-progs | grep -E "^(Version|Installed-Size|Depends)"; dpkg -L libjpeg-turbo-progs | grep bin/'
docker run --rm --entrypoint sh ghcr.io/chetto1983/aura:edge -c 'dpkg -l libjpeg62-turbo | tail -1; apt-cache rdepends --installed libjpeg62-turbo 2>/dev/null | head -8; command -v djpeg || echo "djpeg: not in aura:edge"'
