# `seccomp.json` audit trail

Aura sandbox 2a positive seccomp allowlist for the `aura-sandbox` container.

## Provenance (D-10)

- **Baseline:** moby `profiles/seccomp/default.json`, fetched from tag **`v27.5.1`**
  on **2026-06-01**. (The moby `master`/`main` raw path 404s — the profile is
  served reliably from tagged releases; a tag is also more auditable than a
  moving branch.) The moby default is already a *positive allowlist*
  (`defaultAction: SCMP_ACT_ERRNO`), multi-arch by-name, and the most
  battle-tested published profile — so we **harden from it by subtraction**
  rather than hand-authoring ~80 syscalls (RESEARCH § Don't Hand-Roll).
- **Flattening:** moby ships several CAP-gated *conditional* ALLOW groups
  (e.g. `clone`/`ptrace` family behind `CAP_SYS_PTRACE`, mount behind
  `CAP_SYS_ADMIN`). The `aura-sandbox` service runs `cap_drop: ALL`, so those
  conditional groups are inert. We therefore flatten moby's full ALLOW name set
  into a single unconditional by-name `SCMP_ACT_ALLOW` group, then subtract the
  banned names below. This keeps the profile simple to audit while losing
  nothing functional under `cap_drop: ALL`.

## What was removed from the ALLOW set

**Hard-exclude dangerous set (D-10):**
`ptrace`, `unshare`, `process_vm_readv`, `bpf`, `kexec_load`, `userfaultfd`,
`mount`.

> Note: `kexec_load` and `userfaultfd` are **not** in moby's default ALLOW set
> to begin with, so they were already denied by `defaultAction: SCMP_ACT_ERRNO`
> — the profile denies them by omission, which is the intended posture.

**Listener socket syscalls stay allowed; outbound `connect` is denied.** The
sidecar is an HTTP server, so `socket`/`bind`/`listen`/`accept` and related
receive/send syscalls must be available for the container to boot. The compose
healthcheck validates the sidecar process via `/proc/1/cmdline` instead of
calling loopback HTTP from inside the sandbox, so `connect` can remain denied by
seccomp. The non-masquerading `aura-sandbox-egressless` bridge is retained as an
extra network backstop; SC#3 asserts an external connect does not succeed.

The actively-removed-from-ALLOW set is enumerated verbatim in the `_comment` key
of `seccomp.json`.

## Multi-arch (D-11)

`architectures` lists **`SCMP_ARCH_X86_64`**, **`SCMP_ARCH_X86`**,
**`SCMP_ARCH_X32`**, **`SCMP_ARCH_AARCH64`**, and **`SCMP_ARCH_ARM`** by name.
Docker rejects profiles that specify both `archMap` and `architectures`, so Aura
uses one Docker-compatible dialect at a time. libseccomp resolves the syscall
*numbers* per-arch at load time — **no syscall is referenced by number** (x86
numbers != arm64 numbers). x86_64 is validated live; arm64 is validated under
QEMU in CI with the tracked caveat that QEMU syscall emulation can diverge from
a real arm64 kernel (real-DGX confirmation remains a pre-production arm64
obligation).

## Backstops

- The deterministic 18-scenario bench (05-04) backstops over-permissiveness
  (config-regressions must stay 0).
- The 05-03 integration negative tests prove the excluded syscalls return
  `EPERM` (`ptrace`/`socket`/`connect`/`unshare`/`mount`), while the positive
  `import numpy` smoke proves the floor is not *too* tight for the baked
  C-extensions (`T-05-02-SECCOMP-FIT`).

## Regenerating

Re-fetch the moby baseline at a fixed tag, flatten the ALLOW names, subtract the
two sets above, set `defaultAction: SCMP_ACT_ERRNO` + `defaultErrnoRet: 1`, and
keep exactly one Docker-compatible architecture dialect (`architectures` or
`archMap`). Update the tag + date in this file and the `_comment` key when you do.
