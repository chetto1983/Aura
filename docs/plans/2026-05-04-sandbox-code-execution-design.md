# Sandbox Code Execution — Design

**Date:** 2026-05-04
**Status:** approved
**Backend:** Isola (CPython in WASM, capability-based sandbox)

---

## 1. Thesis

Aura can currently only reason — it can't compute. When a user asks "what's the correlation between X and Y in this CSV" or "run a simulation of this scenario," Aura is stuck describing what it would do instead of doing it.

This phase gives Aura a sandbox where it writes and executes Python code, then uses the results to answer questions, build permanent tools, and autonomously improve over time.

Four capabilities:

1. **Solve problems via code execution** — run Python in an isolated WASM sandbox, return results
2. **Build permanent tools** — save useful scripts to a tool registry that compounds
3. **Learn from execution feedback** — file insights and successful code into the wiki
4. **Autonomously identify and fix weaknesses** — scheduled job finds gaps, writes new tools

---

## 2. Architecture

Three new Go components:

```
Sandbox Manager ──► Isola WASM (ephemeral, airgapped, tmpfs-only)
Tool Registry  ──► wiki/tools/ (.py scripts + .md wiki pages)
Auto-Improve   ──► scheduled job (gap detection → tool writing → wiki filing)
```

- **Sandbox Manager** (`internal/sandbox/`) — manages Isola sandbox lifecycle: creates ephemeral WASM instances, executes Python, returns stdout/stderr, enforces timeouts. AST validation as defense-in-depth before WASM isolation.
- **Tool Registry** (`wiki/tools/`) — directory of LLM-written Python scripts with frontmatter metadata. LLM discovers tools via `tools/index.md`, registers new ones after successful execution.
- **Auto-Improve Scheduler** — periodic job that reviews conversation archives for gaps, writes tools to fill them, files insights into wiki.

---

## 3. Execution Flow

```
User request → LLM decides code needed → writes Python → Sandbox Mgr validates AST
→ Isola WASM executes (airgap, tmpfs, 15s timeout) → stdout/stderr back to LLM
→ LLM answers user → optionally saves tool to registry + wiki entry
```

Key safety properties:
- **AST validation** before execution (Go-side, using Python's `ast` module) — rejects dangerous patterns even before WASM sees the code
- **Airgap by default** — no network access unless explicitly granted
- **tmpfs-only filesystem** — scratch directory, destroyed with sandbox
- **15-second default timeout** — wall-clock limit
- **Ephemeral** — sandbox destroyed after every execution

---

## 4. Tool Registry

```
wiki/tools/
├── index.md                 # catalog, LLM reads this first
├── data_correlation.py       # script
├── data-correlation.md       # wiki page: what, when, params
├── csv_cleaner.py
├── csv-cleaner.md
└── ...
```

Each `.py` has a standard header with description, parameters, required libraries, creation date, and usage hints. The LLM owns the registry entirely — writes, updates, deduplicates. No human curation needed.

---

## 5. Autonomous Improvement

Scheduled job (default: nightly), three passes:

1. **Identify gaps** — scan conversation archives for "I can't do that" responses, low-confidence answers, repeated similar requests with no existing tool
2. **Write tools** — for each gap, write a Python tool and register it
3. **File insights** — synthesize patterns into wiki pages, update index.md and log.md

Safety gate: dry-run mode (proposes changes via Telegram for owner approval) vs auto-apply mode. Default is dry-run.

---

## 6. Cross-Platform

Aura's product contract is "install Aura and go." The sandbox runtime must therefore be bundled with release artifacts, not installed manually by the user.

Supported product layout:

- Windows: `runtime/python/python.exe`
- macOS/Linux: `runtime/python/bin/python3`

Aura probes the bundled runtime at startup by building the Isola Python template. If the probe fails, `execute_code` stays disabled and the dashboard health rollup shows the sandbox as unavailable. `SANDBOX_PYTHON_PATH` and `SANDBOX_ALLOW_SYSTEM_PYTHON=true` are reserved for operators and CI, not for normal end-user setup. Aura must not use system Python by default.

No Docker, no admin rights, no daemon, and no user-facing Python/pip instructions.

Long-term product target: replace the host Python sidecar with a Go-embedded WASI host plus bundled Python artifacts. `wazero` is the preferred direction to evaluate because it is pure Go and keeps Aura installable as a normal application. Isola remains acceptable only when it can be shipped as a bundled runtime that passes the startup probe on every release platform.

The sandbox must not stop at pure Python. Everyday office work needs a real data/document stack, including native-extension packages such as NumPy and pandas.

Default bundled package profile:

- Tables/statistics: `numpy`, `pandas`, `scipy`, `statsmodels`
- Spreadsheet/data files: `openpyxl` or equivalent XLSX writer support, `xlrd`, `pyarrow`, `python-calamine`
- Charts/images: `matplotlib`, `Pillow`
- Documents/text: `PyMuPDF`, `beautifulsoup4`, `lxml`, `html5lib`
- Utilities: `requests`, `pyyaml`, `python-dateutil`, `pytz`, `tzdata`, `regex`, `rich`

This pushes the runtime choice toward Pyodide-style packaging: Pyodide officially ships many of these packages as WebAssembly builds and supports loading additional pure-Python wheels through `micropip`. For Aura releases, package loading must be offline and pinned: no runtime CDN/PyPI dependency for normal user workflows.
