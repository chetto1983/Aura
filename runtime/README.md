# Aura Runtime Assets

Release builds should place optional bundled runtimes here.

The code-execution sandbox looks for Python in this layout before any
operator override:

- Windows: `runtime/python/python.exe`
- macOS/Linux: `runtime/python/bin/python3`

Do not require end users to install Python, pip, Isola, Docker, or a
developer toolchain. System Python is only a development/operator fallback
when `SANDBOX_ALLOW_SYSTEM_PYTHON=true` or `SANDBOX_PYTHON_PATH` is set.

The long-term product target is a Go-embedded WASI runtime with bundled
Python artifacts, so Aura can ship as one installable product.
