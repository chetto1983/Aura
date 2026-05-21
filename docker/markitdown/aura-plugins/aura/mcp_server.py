"""
Aura entry point for the markitdown MCP sidecar.

markitdown-mcp 0.0.1a4 hardcodes MarkItDown() without enable_plugins, so the
sidecar ignores installed plugin entry points. This wrapper keeps the same MCP
transport shape but constructs MarkItDown with MARKITDOWN_ENABLE_PLUGINS.
"""

import argparse
import os
import sys

import uvicorn
from markitdown import MarkItDown
from markitdown_mcp.__main__ import create_starlette_app
from mcp.server.fastmcp import FastMCP


_TRUE_VALUES = {"1", "true", "yes", "on"}
_mcp = FastMCP("markitdown")
_converter: MarkItDown | None = None


def _plugins_enabled() -> bool:
    raw = os.getenv("MARKITDOWN_ENABLE_PLUGINS", "false").strip().lower()
    return raw in _TRUE_VALUES


def _markitdown() -> MarkItDown:
    global _converter
    if _converter is None:
        _converter = MarkItDown(enable_plugins=_plugins_enabled())
    return _converter


@_mcp.tool()
async def convert_to_markdown(uri: str) -> str:
    """Convert a resource described by an http:, https:, file: or data: URI."""
    return _markitdown().convert_uri(uri).markdown


def main() -> None:
    parser = argparse.ArgumentParser(description="Run Aura's MarkItDown MCP server")
    parser.add_argument(
        "--http",
        action="store_true",
        help="Run Streamable HTTP/SSE transport instead of STDIO.",
    )
    parser.add_argument(
        "--sse",
        action="store_true",
        help="Deprecated alias for --http.",
    )
    parser.add_argument(
        "--host", default=None, help="Host to bind when using HTTP."
    )
    parser.add_argument(
        "--port", type=int, default=None, help="Port to bind when using HTTP."
    )
    args = parser.parse_args()

    use_http = args.http or args.sse
    if not use_http and (args.host or args.port):
        parser.error(
            "Host and port arguments are only valid with streamable HTTP/SSE."
        )
        sys.exit(1)

    if use_http:
        starlette_app = create_starlette_app(_mcp._mcp_server, debug=True)
        uvicorn.run(
            starlette_app,
            host=args.host if args.host else "127.0.0.1",
            port=args.port if args.port else 3001,
        )
        return

    _mcp.run()


if __name__ == "__main__":
    main()
