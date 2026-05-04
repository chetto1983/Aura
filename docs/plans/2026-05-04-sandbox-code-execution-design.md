# Sandbox Code Execution - Design

**Date:** 2026-05-04
**Status:** approved, architecture pivoted to Pyodide
**Backend:** Pyodide offline bundle (CPython compiled to WebAssembly, pinned packages)

---

## 1. Thesis

Aura can currently only reason; it cannot compute. When a user asks "what is
the correlation between X and Y in this CSV" or "run a simulation of this
scenario", Aura should execute code and return the result instead of merely
describing what it would do.

This phase gives Aura a bundled Python sandbox that can solve everyday office
and data tasks without asking the end user to install Python, pip, Docker,
Node, or developer tooling.

Four capabilities:

1. **Solve problems via code execution** - run Python in an isolated Pyodide
   runtime and return stdout/stderr/results.
2. **Build permanent tools** - save useful scripts to a reviewable tool
   registry that compounds over time.
3. **Learn from execution feedback** - file successful patterns and insights
   into the wiki.
4. **Autonomously identify weaknesses** - scheduled jobs can propose new
   tools, but durable writes remain review-gated by default.

---

## 2. Architecture

Three product components:

```text
Sandbox Manager -> bundled Pyodide runtime (ephemeral FS, pinned packages)
Tool Registry   -> wiki/tools/ (.py scripts + .md wiki pages)
Auto-Improve    -> scheduled job (gap detection -> proposals -> wiki filing)
```

- **Sandbox Manager** (`internal/sandbox/`) owns validation, timeouts, health,
  temp-file staging, output caps, and the runtime adapter boundary.
- **Pyodide Runtime Bundle** (`runtime/pyodide/`) ships with Aura releases:
  Pyodide JS/WASM assets, Python stdlib, and the offline package cache needed
  for office/data work.
- **Runtime Adapter** is an implementation detail. It may be a small bundled
  runner executable/process around Pyodide, but it must be shipped by Aura and
  auto-discovered. The user must never install Node, Python, pip, or Pyodide.
- **Tool Registry** (`wiki/tools/`) stores LLM-written Python scripts with
  companion wiki pages and index metadata.
- **Auto-Improve Scheduler** reviews conversation gaps and proposes tools. It
  does not silently grant executable-code tools to recurring jobs.

Isola is no longer the target backend. Existing Isola-sidecar code is treated
as a temporary prototype surface to be replaced behind the `internal/sandbox`
manager interface.

---

## 3. Execution Flow

```text
User request -> LLM decides code is needed -> LLM writes Python
-> Sandbox Manager validates AST and policy
-> Pyodide adapter starts a fresh runtime with local package index
-> required packages load from bundled offline cache
-> code runs with timeout/output caps and ephemeral filesystem
-> stdout/stderr/artifacts return to LLM
-> LLM answers user
-> optional tool proposal/save path updates registry + wiki
```

Key safety properties:

- **AST validation before execution** - Go-side policy rejects obvious dangerous
  constructs before the runtime sees code.
- **Offline by default** - package loading uses the bundled local Pyodide index
  and package cache, not CDNs or PyPI.
- **Network denied by default** - network access is a separate explicit
  capability and should not be available to scheduled jobs.
- **Ephemeral filesystem** - each run gets a scratch filesystem that is
  destroyed after execution; explicit input/output files are copied through
  controlled channels.
- **Timeout by process/runtime kill** - default 15 seconds, with hard output
  limits to avoid runaway responses.
- **No inherited secrets** - execution must not expose Aura environment
  variables, API keys, Telegram tokens, or database paths.

---

## 4. Pyodide Package Contract

Aura needs a real office/data runtime, not a toy `stdlib` sandbox. Pyodide is
the backend because the official distribution already includes many packages
Aura needs as WebAssembly builds and can load packages through
`pyodide.loadPackage()` or `micropip.install()`.

Baseline package profile for the release bundle:

- Tables/statistics: `numpy`, `pandas`, `scipy`, `statsmodels`
- Spreadsheet/data files: `xlrd`, `pyarrow`, `python-calamine`
- XLSX writing: prefer a Pyodide-built package when available; otherwise vendor
  a pinned pure-Python wheel such as `openpyxl` into the local package cache
- Charts/images: `matplotlib`, `Pillow`
- Documents/text: `PyMuPDF`, `beautifulsoup4`, `lxml`, `html5lib`
- Utilities: `requests`, `pyyaml`, `python-dateutil`, `pytz`, `tzdata`,
  `regex`, `rich`

Package notes from Pyodide 0.29.3 official package list:

- Built in and useful for Aura: `numpy`, `pandas`, `scipy`, `statsmodels`,
  `matplotlib`, `Pillow`, `PyMuPDF`, `beautifulsoup4`, `lxml`, `html5lib`,
  `pyarrow`, `python-calamine`, `xlrd`, `requests`, `pyyaml`,
  `python-dateutil`, `pytz`, `tzdata`, `regex`, `rich`.
- `openpyxl` is not in the built-in list checked on 2026-05-04. Treat it as a
  pinned vendored wheel or replace XLSX-write workflows with another bundled
  package after a smoke test proves compatibility.

Normal user workflows must not download packages at execution time. Runtime
CDN/PyPI access is allowed only for development experiments, never as the
release path.

Reference: https://pyodide.org/en/stable/usage/packages-in-pyodide.html

---

## 5. Tool Registry

```text
wiki/tools/
  index.md                 # catalog, LLM reads this first
  data_correlation.py      # script
  data-correlation.md      # wiki page: what, when, params
  csv_cleaner.py
  csv-cleaner.md
```

Each `.py` has a standard header with description, parameters, required
libraries, creation date, and usage hints. Tool creation should stay
reviewable when initiated by autonomous jobs. Direct interactive saves remain
an explicit tool/admin workflow.

---

## 6. Autonomous Improvement

Scheduled job, default dry-run:

1. **Identify gaps** - scan conversation archives for repeated unmet requests,
   low-confidence answers, and tasks that would benefit from code.
2. **Write proposals** - draft Python tools and smoke commands.
3. **File insights** - propose wiki/tool updates through the review queue.

Safety gate:

- `scheduler_safe` must not include `execute_code`, `list_tools`, `read_tool`,
  or `save_tool` by default.
- A separate `sandbox_code` profile may expose execution tools for explicit
  interactive or admin-approved runs.
- `save_tool` is a durable mutation path and stays outside recurring job
  defaults unless a later admin workflow approves it.

---

## 7. Cross-Platform Product Contract

Aura's product contract is "install Aura and go." The sandbox runtime is
bundled with release artifacts and probed at startup.

Supported product layout:

```text
runtime/
  pyodide/
    pyodide.js / pyodide.mjs
    pyodide.asm.wasm
    python_stdlib.zip
    packages/
    repodata.json
    aura-pyodide-manifest.json
    runner/
```

`aura-pyodide-manifest.json` records the Pyodide version, package list,
versions, hashes, and minimum smoke tests. Startup health fails closed:
`execute_code` is disabled when the bundle is missing, a required package is
missing, a hash check fails, or the smoke probe cannot import the baseline
profile.

No Docker. No admin rights. No daemon. No user-facing Python, pip, Node, or
Pyodide instructions.
