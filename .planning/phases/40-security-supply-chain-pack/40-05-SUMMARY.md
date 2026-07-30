---
phase: 40-security-supply-chain-pack
plan: 05
status: complete
completed: 2026-07-30
requirements: [SEC-05]
---

# Plan 40-05 Summary — immutable CI and release SBOMs

## Outcome

F-051 / SEC-05 is closed:

- every third-party `uses:` reference in all five workflow files is pinned to
  a verified 40-character commit SHA with an inline release-major comment;
- CodeQL `init` and `analyze` use the same commit;
- `deadcode`, `govulncheck`, and `go-mutesting` no longer install from
  `@latest`;
- GoReleaser is pinned to `v2.17.1`;
- the release workflow installs Syft through a SHA-pinned official action;
- GoReleaser emits SBOMs for both release archives and the source archive;
- CI runs a self-tested repository gate that rejects floating action refs,
  missing version comments, and `go install ...@latest`, including inside
  multiline `run: |` blocks.

The action SHAs were resolved from their official GitHub repositories on
2026-07-30. The Go tool versions were resolved through the Go module proxy.

## Verification

- `bash scripts/workflow_pin_gate_test.sh` — pass.
- `bash scripts/workflow_pin_gate.sh` — pass.
- independent `rg` floating-ref search — zero matches.
- `goreleaser check` — configuration valid.
- `actionlint v1.7.12` over all workflows — zero findings.

## Pinned action set

The pinned set covers `actions/{checkout,setup-go,setup-node,cache,upload-artifact}`,
Docker build/login/QEMU actions, CodeQL, `dorny/paths-filter`, the GoReleaser
action, `crazy-max/ghaction-github-runtime`, and Anchore's Syft downloader.
