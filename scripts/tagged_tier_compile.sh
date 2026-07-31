#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source_root="${AURA_TAGGED_SOURCE_ROOT:-$repo_root}"
mode="${1:---compile}"

case "$mode" in
  --compile | --list | --plan) ;;
  *)
    echo "usage: $0 [--compile|--list|--plan]" >&2
    exit 2
    ;;
esac

is_structural_tag() {
  case "$1" in
    aix | android | darwin | dragonfly | freebsd | hurd | illumos | ios | js | linux | \
      netbsd | openbsd | plan9 | solaris | wasip1 | windows | \
      386 | amd64 | amd64p32 | arm | arm64 | loong64 | mips | mips64 | mips64le | \
      mipsle | ppc64 | ppc64le | riscv64 | s390x | sparc64 | wasm | \
      cgo | gc | gccgo | goexperiment.* | ignore | race | unix)
      return 0
      ;;
  esac
  return 1
}

is_tier_tag() {
  [[ "$1" =~ (_integration|_live|_eval|_e2e)$ ]] ||
    [[ "$1" == "smoke" ]] ||
    [[ "$1" == "serve_smoke" ]] ||
    [[ "$1" == live_* ]]
}

declare -A tiers=()
declare -A unknown=()
declare -A package_tiers=()

discover_sources() {
  if [[ "$(git -C "$source_root" rev-parse --show-toplevel 2>/dev/null || true)" == "$source_root" ]]; then
    while IFS= read -r -d '' relative; do
      printf '%s/%s\0' "$source_root" "$relative"
    done < <(git -C "$source_root" grep -lz '^//go:build ' -- cmd internal scripts || true)
    while IFS= read -r -d '' relative; do
      printf '%s/%s\0' "$source_root" "$relative"
    done < <(
      git -C "$source_root" ls-files -z --others --exclude-standard -- \
        'cmd/*.go' 'internal/*.go' 'scripts/*.go'
    )
    return
  fi
  local subtree
  for subtree in cmd internal scripts; do
    if [[ -d "$source_root/$subtree" ]]; then
      find "$source_root/$subtree" -type f -name '*.go' -print0
    fi
  done
}

while IFS= read -r -d '' source; do
  first_line="$(head -n 1 "$source")"
  [[ "$first_line" == "//go:build "* ]] || continue
  expression="${first_line#//go:build }"
  source_tiers=()
  while IFS= read -r token; do
    [[ -n "$token" ]] || continue
    if is_structural_tag "$token"; then
      continue
    fi
    if is_tier_tag "$token"; then
      tiers["$token"]=1
      source_tiers+=("$token")
    else
      unknown["$token"]=1
    fi
  done < <(grep -oE '[[:alpha:]_][[:alnum:]_.]*' <<<"$expression" || true)
  if (( ${#source_tiers[@]} > 0 )); then
    directory="${source%/*}"
    package="${directory#"$source_root"/}"
    [[ "$package" != "$directory" ]] || package="."
    for token in "${source_tiers[@]}"; do
      package_tiers["$package"]+=" $token"
    done
  fi
done < <(discover_sources)

if (( ${#unknown[@]} > 0 )); then
  echo "tagged-tier-compile: unclassified build tag(s):" >&2
  printf '  %s\n' "${!unknown[@]}" | sort >&2
  exit 1
fi
if (( ${#tiers[@]} == 0 )); then
  echo "tagged-tier-compile: no Aura tier tags found under $source_root" >&2
  exit 1
fi

mapfile -t sorted_tiers < <(printf '%s\n' "${!tiers[@]}" | sort)
if [[ "$mode" == "--list" ]]; then
  printf '%s\n' "${sorted_tiers[@]}"
  exit 0
fi

mapfile -t sorted_packages < <(printf '%s\n' "${!package_tiers[@]}" | sort)
render_package_tags() {
  local package="$1"
  tr ' ' '\n' <<<"${package_tiers[$package]}" | grep -v '^$' | sort -u | paste -sd, -
}

if [[ "$mode" == "--plan" ]]; then
  for package in "${sorted_packages[@]}"; do
    printf '%s\t%s\n' "$package" "$(render_package_tags "$package")"
  done
  exit 0
fi

cd "$repo_root"
echo "tagged-tier-compile: compiling ${#sorted_tiers[@]} tiers in ${#sorted_packages[@]} tagged packages"
for package in "${sorted_packages[@]}"; do
  tag_set="$(render_package_tags "$package")"
  echo "  ./$package [$tag_set]"
  go test -run '^$' -tags "$tag_set" "./$package"
done
echo "tagged-tier-compile: pass"
