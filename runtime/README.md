# Aura Runtime Assets

Release builds should place optional bundled runtimes here.

The code-execution sandbox targets a bundled Pyodide runtime:

```text
runtime/
  pyodide/
    pyodide.js / pyodide.mjs
    pyodide.asm.wasm
    python_stdlib.zip
    repodata.json
    packages/
    aura-pyodide-manifest.json
    runner/
```

Do not require end users to install Python, pip, Docker, Node, Pyodide, or a
developer toolchain. Any runner needed to host Pyodide must be part of Aura's
release artifact.

## Default Package Profile

The bundled sandbox should support everyday office/data work out of the box:

- `numpy`, `pandas`, `scipy`, `statsmodels`
- spreadsheet/data IO: `xlrd`, `pyarrow`, `python-calamine`; vendor
  `openpyxl` only if a pinned pure-Python wheel smoke test passes
- charts/images: `matplotlib`, `Pillow`
- document/text extraction: `PyMuPDF`, `beautifulsoup4`, `lxml`, `html5lib`
- utility stack: `requests`, `pyyaml`, `python-dateutil`, `pytz`, `tzdata`,
  `regex`, `rich`

Package sources must be pinned and bundled. Normal user workflows must not
download wheels from PyPI/CDNs at execution time.

## Pyodide Manifest

Every bundled runtime must include `runtime/pyodide/aura-pyodide-manifest.json`.
Aura validates this file at startup before it can enable `execute_code`.

```json
{
  "schema_version": 1,
  "runtime": "pyodide",
  "pyodide_version": "0.29.3",
  "files": [
    {
      "path": "pyodide.mjs",
      "sha256": "<64 lowercase hex chars>",
      "required": true
    },
    {
      "path": "pyodide.asm.wasm",
      "sha256": "<64 lowercase hex chars>",
      "required": true
    },
    {
      "path": "python_stdlib.zip",
      "sha256": "<64 lowercase hex chars>",
      "required": true
    },
    {
      "path": "repodata.json",
      "sha256": "<64 lowercase hex chars>",
      "required": true
    }
  ],
  "packages": [
    {
      "name": "numpy",
      "import_name": "numpy",
      "version": "2.2.5",
      "path": "packages/numpy-...",
      "sha256": "<64 lowercase hex chars>",
      "required": true
    }
  ],
  "smoke_imports": [
    "numpy",
    "pandas",
    "scipy",
    "statsmodels",
    "matplotlib",
    "PIL",
    "fitz",
    "bs4",
    "lxml",
    "html5lib",
    "pyarrow",
    "python_calamine",
    "xlrd",
    "requests",
    "yaml",
    "dateutil",
    "pytz",
    "tzdata",
    "regex",
    "rich"
  ]
}
```

Manifest paths are relative to `SANDBOX_RUNTIME_DIR` and must stay inside that
directory. Required files and package artifacts are hash-checked. Aura registers
`execute_code` only when both the bundle and runner are healthy; missing or
invalid runtime assets leave the tool disabled and surface sandbox health.

## Local Bundle Smoke

The local development bundle is ignored by git because it is a release artifact,
not source. To rebuild it from pinned release inputs, run:

```powershell
node runtime/install-pyodide-bundle.mjs --runtime-dir runtime/pyodide --with-node-win-x64
```

That command installs Pyodide 0.29.3 from npm, resolves Aura's baseline package
closure from `pyodide-lock.json`, downloads package artifacts with hash checks,
writes `aura-pyodide-manifest.json`, and adds the runner scripts plus a bundled
Windows Node runtime for release archives.

After installing `runtime/pyodide/`, run the repeatable package smoke with:

```powershell
go run ./cmd/debug_sandbox --smoke
```

The smoke validates bundle availability and runs arithmetic, data imports,
spreadsheet read, matplotlib artifact creation, and PDF/text extraction against
local Pyodide files.

To test the same runtime through Aura's registered `execute_code` tool boundary,
run:

```powershell
go run ./cmd/debug_sandbox --tool-smoke
```

This constructs the Pyodide runner/manager, registers `execute_code`, and checks
that `sum(range(1, 101))` returns `5050`.

GoReleaser runs the same installer and smoke before building archives. Release
archives include `runtime/pyodide/**`, so Windows users do not need to install
Node, Python, pip, Docker, or Pyodide separately.

For the narrower adapter test, run:

```powershell
$env:AURA_SANDBOX_LIVE='1'
$env:SANDBOX_PYODIDE_RUNNER='runtime\pyodide\runner\aura-pyodide-runner.cmd'
go test ./internal/sandbox -run TestPyodideRunner_LivePyodideBundle -count=1 -v
```

The adapter test validates the manifest, starts the bundled runner, computes
`sum(range(101))`, and imports the baseline package profile from local files.
