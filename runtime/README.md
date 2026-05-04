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
