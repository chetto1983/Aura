#!/bin/sh
# Builds Aura's web build of one Craft app into <out>/<app>
# (docs/superpowers/specs/2026-10-19-craft-web-apps-design.md):
#
#   build.sh <app> <tag> <commit> <out>
#
# The release tag's source, checked against its commit, plus <app>.patch beside this script: what
# the app lacks upstream (the `?host=parent` bridge the cockpit opens and saves files through, the
# browser's language), and the ArtCraft marks the brand terms (docs/brand/LICENSE-brand.txt
# upstream) keep out of a modified build. The tools come from PATH (trunk, wasm-bindgen, wasm-opt,
# pinned by the Dockerfile), never downloaded here.
set -eu

app=$1 tag=$2 commit=$3 out=$4
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
src=$work/$app

git clone --quiet --depth 1 --branch "$tag" "https://github.com/storytold/$app" "$src"
if [ "$(git -C "$src" rev-parse HEAD)" != "$commit" ]; then
    echo "craft-web: $app $tag is $(git -C "$src" rev-parse HEAD), not $commit" >&2
    exit 1
fi
git -C "$src" apply "$here/$app.patch"
# An app whose translations live in storytold/craft-i18n names the commit in craft-i18n.rev, part
# of the tag checked above; its build embeds them from CRAFT_I18N_DIR and is English only without
# it (PhotoCraft from v0.23.0). REQUIRED makes a missing catalog fail the build instead.
catalogs=$src/crates/ui-egui/src/i18n
if [ -f "$src/craft-i18n.rev" ]; then
    rev=$(tr -d '[:space:]' <"$src/craft-i18n.rev")
    git init --quiet "$work/craft-i18n"
    git -C "$work/craft-i18n" fetch --quiet --depth 1 https://github.com/storytold/craft-i18n "$rev"
    git -C "$work/craft-i18n" checkout --quiet FETCH_HEAD
    export CRAFT_I18N_DIR="$work/craft-i18n" CRAFT_I18N_REQUIRED=1
    catalogs=$CRAFT_I18N_DIR/locales
fi
# A translated row naming ArtCraft goes: the patch rewords or drops the English it translated.
if [ -d "$catalogs" ]; then
    find "$catalogs" -name '*.tsv' -exec sed -i '/artcraft/Id' {} +
fi

export CARGO_TARGET_DIR="$work/target"
if [ "$app" = filmcraft ]; then
    (cd "$src" && cargo xtask web)
    dist=$CARGO_TARGET_DIR/web/dist
else
    (cd "$src/apps/$app-web" && trunk build --release --offline)
    dist=$src/dist/web
fi

mkdir -p "$out/$app"
cp -R "$dist"/. "$out/$app/"
cp "$src/LICENSE-MIT" "$src/LICENSE-APACHE" "$out/$app/"
# Nothing in front of aura compresses: aura sends the .gz to a browser that accepts it
# (internal/staticapp).
gzip -9 -k "$out/$app"/*.wasm "$out/$app"/*.js
rm -rf "$work"
