"""Exercise documents_mcp over streamable-http, the way a host would."""
import asyncio, json, os, sys

from mcp import ClientSession
from mcp.client.streamable_http import streamablehttp_client

URL = os.environ.get("MCP_URL", "http://127.0.0.1:8099/mcp")


async def main():
    async with streamablehttp_client(URL) as (read, write, _):
        async with ClientSession(read, write) as s:
            await s.initialize()
            tools = await s.list_tools()
            print("TOOLS:", [t.name for t in tools.tools])
            for t in tools.tools:
                first = (t.description or "").strip().splitlines()[0]
                print(f"  - {t.name}: {first}")

            async def call(name, args):
                r = await s.call_tool(name, args)
                txt = r.content[0].text if r.content else "{}"
                try:
                    return json.loads(txt)
                except Exception:
                    return txt

            print("\n--- document_resolve ---")
            print(json.dumps(await call("document_resolve", {"params": {}}), ensure_ascii=False)[:400])

            print("\n--- document_search: domanda con risposta ---")
            out = await call("document_search", {"params": {
                "query": "Qual e' il numero di ordinazione del Sensor Module External SME20?"}})
            print(json.dumps(out, ensure_ascii=False)[:500])

            print("\n--- document_search: domanda SENZA risposta (deve astenersi) ---")
            out = await call("document_search", {"params": {
                "query": "Qual e' il prezzo di listino della Control Unit CU320-2?"}})
            print(json.dumps(out, ensure_ascii=False)[:400])


asyncio.run(main())
