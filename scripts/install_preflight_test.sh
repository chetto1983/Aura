#!/usr/bin/env bash
# preflight_hw had no test at all, which is how a memory floor that could never be met
# survived: nothing exercised the comparison, and no operator had yet installed onto a
# 16 GB box with install.sh (the reference appliance was provisioned by hand).
#
# install.sh is sourceable (its AURA_EXECUTED guard), so the three hardware readers are
# stubbed here and preflight_hw is driven against known numbers instead of the host's.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT

# shellcheck source=/dev/null
source "$repo_root/scripts/install.sh"

INSTALL_DIR="${INSTALL_DIR:-/opt/aura}"

STUB_CPUS=8
STUB_MEM_KIB=$((32 * 1024 * 1024))
STUB_DISK_KIB=$((60 * 1024 * 1024))

cpu_count() { echo "$STUB_CPUS"; }
ram_kib() { echo "$STUB_MEM_KIB"; }
disk_free_kib() { echo "$STUB_DISK_KIB"; }

failures=0

# preflight_hw exits 1 rather than returning, so it runs in a subshell; `if` exempts the
# call from set -e so a refusal is data instead of an abort.
expect() {
  local want="$1" label="$2" err="$scratch/err"
  local got=0
  if ( preflight_hw ) 2>"$err"; then got=0; else got=1; fi
  if [ "$got" != "$want" ]; then
    echo "FAIL: ${label}: expected $( [ "$want" = 0 ] && echo accept || echo refuse ), got $( [ "$got" = 0 ] && echo accept || echo refuse )" >&2
    sed 's/^/      /' "$err" >&2
    failures=$((failures + 1))
  fi
}

# The regression this file exists for. MemTotal on a 16 GB machine lands around 15.5 GiB
# once firmware, kernel and integrated-GPU reservations are taken out; against the old
# 16 GiB floor that machine was refused on every hardware, always.
STUB_MEM_KIB=$((16268000))
expect 0 "a real 16 GB machine (MemTotal 15.51 GiB) is accepted"

STUB_MEM_KIB=$((15 * 1024 * 1024))
expect 0 "exactly at the 15 GiB floor is accepted"

STUB_MEM_KIB=$((15 * 1024 * 1024 - 1))
expect 1 "one KiB under the floor is refused"

# The floor still has to separate: an 8 GB box reports nowhere near 15 GiB.
STUB_MEM_KIB=$((8120000))
expect 1 "an 8 GB machine is refused"

STUB_MEM_KIB=$((32 * 1024 * 1024))

STUB_CPUS=4
expect 0 "exactly 4 cores is accepted"
STUB_CPUS=3
expect 1 "3 cores is refused"
STUB_CPUS=8

STUB_DISK_KIB=$((20 * 1024 * 1024))
expect 0 "exactly 20 GiB free disk is accepted"
STUB_DISK_KIB=$((20 * 1024 * 1024 - 1))
expect 1 "one KiB under the disk floor is refused"
STUB_DISK_KIB=$((60 * 1024 * 1024))

# The escape hatch must still open, or an operator who knowingly accepts an under-spec box
# has no way past a gate that is now measurably conservative.
STUB_CPUS=1
STUB_MEM_KIB=1
STUB_DISK_KIB=1
AURA_INSTALL_SKIP_HW=1
expect 0 "AURA_INSTALL_SKIP_HW=1 waves through a box that fails every gate"

if [ "$failures" -ne 0 ]; then
  echo "FAIL: ${failures} preflight expectation(s) not met" >&2
  exit 1
fi
echo "ok: install.sh preflight_hw"
