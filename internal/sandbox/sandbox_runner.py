"""Thin wrapper that executes Python code inside an Isola WASM sandbox.

Called by the Go sandbox manager via os/exec. Reads code from a temp
file path, runs it inside an ephemeral Isola sandbox, prints the
result as JSON to stdout.
"""
import sys
import json
import asyncio
import argparse
import time


async def run_in_sandbox(code: str, timeout: int, allow_network: bool) -> dict:
    from isola import build_template

    start = time.monotonic()
    template = await build_template("python")

    mounts = {}
    env_vars = {}
    http = allow_network

    try:
        async with template.create(
            mounts=mounts,
            env=env_vars,
            http=http,
        ) as sandbox:
            result = await sandbox.run(code, timeout=timeout)
            elapsed = time.monotonic() - start
            return {
                "ok": True,
                "stdout": result.stdout or "",
                "stderr": result.stderr or "",
                "exit_code": result.exit_code,
                "elapsed_ms": int(elapsed * 1000),
            }
    except TimeoutError:
        return {
            "ok": False,
            "stdout": "",
            "stderr": f"execution timed out after {timeout}s",
            "exit_code": -1,
            "elapsed_ms": int((time.monotonic() - start) * 1000),
        }
    except Exception as exc:
        return {
            "ok": False,
            "stdout": "",
            "stderr": f"sandbox error: {exc}",
            "exit_code": -2,
            "elapsed_ms": int((time.monotonic() - start) * 1000),
        }


def main():
    parser = argparse.ArgumentParser(description="Isola sandbox runner")
    parser.add_argument("--code-file", required=True, help="Path to Python code file")
    parser.add_argument("--timeout", type=int, default=15, help="Execution timeout in seconds")
    parser.add_argument("--network", action="store_true", default=False, help="Allow network access")
    args = parser.parse_args()

    with open(args.code_file, "r", encoding="utf-8") as f:
        code = f.read()

    result = asyncio.run(run_in_sandbox(code, args.timeout, args.network))
    print(json.dumps(result))


if __name__ == "__main__":
    main()
