#!/usr/bin/env bash
# Prints the content hash of the Craft web image's inputs: the tag publish-craft-web.yml pushes
# and docker/aura/Dockerfile pins (TestCraftWebImagePinMatchesItsInputs).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
find docker/craft-web docker/aura-craft-web -type f -print0 | LC_ALL=C sort -z |
  xargs -0 sha256sum | sha256sum | cut -c1-16
