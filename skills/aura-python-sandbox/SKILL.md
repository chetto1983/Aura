---
name: aura-python-sandbox
description: Use when Aura should write or run Python with execute_code, create computed artifacts, persist scripts, analyze files, or remember sandbox outputs.
---

# Aura Python Sandbox

## Overview

Use `execute_code` when a task needs computation, data processing, plots, simulations, or custom generated artifacts. The sandbox is ephemeral, so anything worth remembering must be written as an artifact under `/tmp/aura_out`.

## Core Rules

- Prefer typed file tools for ordinary documents: `create_xlsx`, `create_docx`, `create_pdf`.
- Use `execute_code` for computed scripts, transformations, analysis, plots, and custom exports.
- Keep `allow_network=false` unless the user explicitly asks for live HTTP access.
- Write durable outputs under `/tmp/aura_out`.
- When the script matters, write the script itself as `/tmp/aura_out/<name>.py`.
- Also write a small result note such as `/tmp/aura_out/<name>-result.md` explaining inputs, outputs, and assumptions.
- Never assume sandbox state survives between runs.

## Script Quality Bar

Python generated for Aura should be deterministic and reviewable.

- Put constants near the top.
- Use small functions with explicit inputs and outputs.
- Print a compact success summary to stdout.
- Use structured files for results: CSV, JSON, Markdown, PNG, or the original script.
- Treat truncation, skipped rows, parse failures, and missing values as explicit warnings.
- Avoid hidden network, clock, and random dependencies unless they are part of the task.

## Memory Pattern

For important code runs:

1. Read relevant skills first.
2. Write the Python script under `/tmp/aura_out`.
3. Write result artifacts under `/tmp/aura_out`.
4. Let Aura persist the artifacts as `sandbox_artifact` sources.
5. Use `list_sources` or `read_source` to confirm the script/result can be found later.

## Example

```python
from pathlib import Path

OUT = Path("/tmp/aura_out")
OUT.mkdir(parents=True, exist_ok=True)

script = """print('replace with real analysis')\n"""
(OUT / "analysis.py").write_text(script, encoding="utf-8")
(OUT / "analysis-result.md").write_text(
    "# Analysis Result\n\n- Status: complete\n- Network: disabled\n",
    encoding="utf-8",
)

print("created analysis.py and analysis-result.md")
```
