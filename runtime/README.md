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
directory. Required files and package artifacts are hash-checked. The manifest
probe only proves that the bundle contract is present; execution remains
disabled until Aura is wired to the bundled runner adapter.
