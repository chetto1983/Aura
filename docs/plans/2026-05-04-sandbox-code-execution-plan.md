# Sandbox Code Execution - Pyodide Implementation Plan

**Date:** 2026-05-04
**Status:** active
**Supersedes:** the earlier Isola sidecar task list in this file.

## Goal

Give Aura a bundled Python code-execution sandbox based on Pyodide, with a real
offline office/data package profile and no end-user dependency on Python, pip,
Docker, Node, or developer tooling.

## Architecture Decision

Use Pyodide as the product runtime backend.

Why:

- Pyodide ships CPython compiled to WebAssembly.
- The official package distribution already includes most of Aura's required
  office/data packages: `numpy`, `pandas`, `scipy`, `statsmodels`,
  `matplotlib`, `Pillow`, `PyMuPDF`, `beautifulsoup4`, `lxml`, `html5lib`,
  `pyarrow`, `python-calamine`, `xlrd`, `requests`, `pyyaml`,
  `python-dateutil`, `pytz`, `tzdata`, `regex`, and `rich`.
- Package loading can be pinned and served from a local bundle rather than a
  CDN/PyPI at runtime.
- Native-extension packages such as NumPy and pandas become realistic for
  normal office workflows.

Isola remains historical prototype code only. New work should move the
`internal/sandbox` package behind a runtime abstraction, then replace the
Isola runner with a Pyodide adapter.

Reference: https://pyodide.org/en/stable/usage/packages-in-pyodide.html

## Non-Negotiable Product Rules

- End users install Aura and run it. They do not install Python, pip, wheels,
  Docker, Node, Pyodide, or build tools.
- Release artifacts ship `runtime/pyodide/...` with pinned hashes.
- Normal execution loads packages only from the local Pyodide bundle.
- Missing or unhealthy runtime disables `execute_code` and reports a clear
  sandbox health state.
- `scheduler_safe` excludes executable-code tools. Scheduled jobs may propose
  tool changes, but durable writes remain review-gated by default.
- `save_tool` is never part of autonomous recurring defaults.

## Target Runtime Layout

```text
runtime/
  pyodide/
    pyodide.js / pyodide.mjs
    pyodide.asm.wasm
    python_stdlib.zip
    repodata.json
    packages/
      numpy-...
      pandas-...
      scipy-...
      ...
    aura-pyodide-manifest.json
    runner/
      aura-pyodide-runner[.exe]
```

The exact runner implementation is internal. It may wrap Pyodide through a
bundled JS runtime or another Aura-owned executable, but no host runtime may be
required from the user.

## Baseline Package Profile

Must import successfully during release smoke tests:

- `numpy`
- `pandas`
- `scipy`
- `statsmodels`
- `matplotlib`
- `PIL`
- `fitz` (`PyMuPDF`)
- `bs4`
- `lxml`
- `html5lib`
- `pyarrow`
- `python_calamine`
- `xlrd`
- `requests`
- `yaml`
- `dateutil`
- `pytz`
- `tzdata`
- `regex`
- `rich`

`openpyxl` is not in the checked Pyodide 0.29.3 built-in package list. Treat it
as a pinned pure-Python vendored wheel candidate, or choose another bundled
XLSX writer after a smoke test proves compatibility.

## Current Guardrail

The current tool perimeter is correct and must survive the architecture change:

- `sandbox_code` profile contains `execute_code`, `list_tools`, and
  `read_tool`.
- `scheduler_safe` excludes `execute_code`, `list_tools`, `read_tool`, and
  `save_tool`.
- Scheduled `agent_job` payloads that request only sandbox tools are rejected
  instead of silently falling back to broad defaults.

## Slice Plan

### sandbox.pyodide.1 - Runtime Abstraction

**Goal:** decouple `internal/sandbox` from Isola-specific host Python.

**Files:**

- `internal/sandbox/sandbox.go`
- `internal/sandbox/sandbox_test.go`
- `internal/telegram/setup.go`
- `internal/api/router.go` or current health wiring

**Implementation:**

- Introduce a runtime adapter boundary inside `internal/sandbox`.
- Preserve public `Manager.Execute`, `Manager.ValidateCode`, and health shape
  where possible.
- Add runtime kind/detail fields so health can say `pyodide`, `isola_legacy`,
  or `unavailable`.
- Keep existing Isola adapter only as a temporary legacy implementation behind
  the new boundary.

**Acceptance:**

- Existing sandbox manager/tool tests pass.
- Health tests prove unavailable/runtime-detail behavior.
- No scheduler-safe toolset regression.

### sandbox.pyodide.2 - Bundle Manifest and Probe

**Goal:** define and verify the local Pyodide bundle contract before executing
user code.

**Files:**

- `runtime/README.md`
- `internal/sandbox/manifest.go`
- `internal/sandbox/manifest_test.go`
- `.env.example`

**Implementation:**

- Add `runtime/pyodide/aura-pyodide-manifest.json` schema documentation.
- Add manifest loader with path containment and hash validation.
- Add required package list and package import smoke definitions.
- Add config names for product runtime paths, e.g. `SANDBOX_RUNTIME_DIR`,
  while keeping old Python-path config as legacy/dev only until removed.

**Acceptance:**

- Manifest tests cover missing files, hash mismatch, package omissions, and
  happy path.
- `.env.example` documents Pyodide product defaults without user install steps.

### sandbox.pyodide.3 - Pyodide Runner Adapter

**Goal:** execute simple Python through the bundled Pyodide runtime.

**Files:**

- `internal/sandbox/pyodide_runner.go`
- `internal/sandbox/pyodide_runner_test.go`
- `runtime/README.md`

**Implementation:**

- Define runner JSON protocol: code, timeout, allow_network, packages, input
  files, output file allowlist.
- Start the bundled runner executable from `runtime/pyodide/runner/`.
- Pass only sanitized runtime environment.
- Kill the runner on timeout.
- Capture stdout/stderr/result JSON with existing output caps.

**Acceptance:**

- Hermetic fake-runner tests prove command args, env filtering, timeout, and
  JSON parsing.
- Live Pyodide test is opt-in and skips cleanly when the bundle is absent.

### sandbox.pyodide.4 - Offline Package Smoke

**Goal:** prove the office/data profile loads from the local bundle.

**Files:**

- `internal/sandbox/package_smoke.go`
- `internal/sandbox/package_smoke_test.go`
- `cmd/debug_sandbox/main.go`

**Implementation:**

- Add a debug command that runs:
  - arithmetic smoke: `print(sum(range(101)))`
  - data smoke: import `numpy`, `pandas`, `scipy`, `statsmodels`
  - spreadsheet smoke: read a small CSV/XLSX fixture
  - chart smoke: render a matplotlib PNG artifact
  - PDF/text smoke: import `fitz`, `bs4`, `lxml`
- Report unavailable bundle as a skip, not as silent success.

**Acceptance:**

- `go test ./internal/sandbox ./internal/toolsets ./internal/scheduler ./internal/telegram`
  passes.
- `go run ./cmd/debug_sandbox --smoke` passes on a machine with the bundled
  Pyodide runtime, or clearly reports `runtime unavailable` locally.

### sandbox.pyodide.5 - Switch execute_code to Pyodide

**Goal:** make `execute_code` use the Pyodide adapter when the bundle is
healthy.

**Files:**

- `internal/telegram/setup.go`
- `internal/tools/exec.go`
- `internal/api` health files
- `web/src/components/HealthDashboard.tsx`
- embedded `internal/api/dist/`

**Implementation:**

- Prefer Pyodide runtime health over the legacy Isola path.
- Disable `execute_code` when required package smoke fails.
- Update tool descriptions to mention bundled Pyodide package availability,
  not stdlib-only/Isola.
- Update dashboard health text.

**Acceptance:**

- Go targeted tests pass.
- `cd web; npm run build` passes when dashboard copy/assets change.
- Health rollup shows runtime kind and package profile status.

### sandbox.pyodide.6 - Retire Isola Product Path

**Goal:** remove user-facing Isola/Python product assumptions.

**Files:**

- `internal/sandbox/sandbox_runner.py`
- `internal/sandbox/requirements.txt`
- `internal/config/config.go`
- `.env.example`
- docs/runtime docs

**Implementation:**

- Delete or quarantine Isola sidecar files once Pyodide execution is green.
- Remove product references to `pip install isola`.
- Keep only explicit legacy/dev toggles if needed for tests.

**Acceptance:**

- `rg -n "Isola|pip install|SANDBOX_PYTHON_PATH|SANDBOX_ALLOW_SYSTEM_PYTHON"`
  shows only legacy/deprecation notes or no hits.
- Full `verify-go.ps1` passes.

## Post-Implementation Verification

Before closing the Pyodide migration:

1. `go test ./internal/sandbox ./internal/tools ./internal/toolsets`
2. `go test ./internal/scheduler ./internal/telegram`
3. `go build ./...`
4. `go vet ./...`
5. `go run ./cmd/debug_sandbox --smoke`
6. Dashboard build if health UI changed:
   `cd web; npm run build`
7. Manual Telegram smoke:
   "Run Python and tell me `sum(range(1, 101))`."
8. Office smoke:
   "Read this CSV/XLSX and calculate summary statistics."

Expected result: Aura computes with bundled Pyodide, can import the baseline
packages offline, and never asks the end user to install developer tooling.
