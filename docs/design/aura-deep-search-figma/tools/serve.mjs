#!/usr/bin/env node
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const args = new Set(process.argv.slice(2));
const host = process.env.HOST || "127.0.0.1";
const portArg = process.argv.find((arg) => arg.startsWith("--port="));
const requestedPort = Number.parseInt(
  process.env.PORT || portArg?.slice("--port=".length) || "8877",
  10,
);

const mimeTypes = new Map([
  [".css", "text/css; charset=utf-8"],
  [".html", "text/html; charset=utf-8"],
  [".js", "text/javascript; charset=utf-8"],
  [".json", "application/json; charset=utf-8"],
  [".md", "text/markdown; charset=utf-8"],
  [".png", "image/png"],
  [".svg", "image/svg+xml; charset=utf-8"],
  [".txt", "text/plain; charset=utf-8"],
]);

function createStaticServer() {
  return createServer(async (request, response) => {
    try {
      const requestUrl = new URL(request.url || "/", `http://${request.headers.host || host}`);
      const decodedPath = decodeURIComponent(requestUrl.pathname);
      const relativePath = decodedPath === "/" ? "figma-capture.html" : decodedPath.replace(/^\/+/, "");
      const filePath = path.resolve(root, relativePath);
      const relativeToRoot = path.relative(root, filePath);

      if (relativeToRoot.startsWith("..") || path.isAbsolute(relativeToRoot)) {
        response.writeHead(403, { "content-type": "text/plain; charset=utf-8" });
        response.end("Forbidden");
        return;
      }

      const content = await readFile(filePath);
      const type = mimeTypes.get(path.extname(filePath).toLowerCase()) || "application/octet-stream";
      response.writeHead(200, {
        "cache-control": args.has("--no-cache") ? "no-store" : "no-cache",
        "content-type": type,
      });
      response.end(content);
    } catch (error) {
      const statusCode = error?.code === "ENOENT" ? 404 : 500;
      response.writeHead(statusCode, { "content-type": "text/plain; charset=utf-8" });
      response.end(statusCode === 404 ? "Not found" : String(error?.message || error));
    }
  });
}

async function listenWithFallback(startPort) {
  for (let port = startPort; port <= startPort + 20; port += 1) {
    const server = createStaticServer();
    try {
      await new Promise((resolve, reject) => {
        server.once("error", reject);
        server.listen(port, host, resolve);
      });
      return { server, port };
    } catch (error) {
      if (error?.code !== "EADDRINUSE") {
        throw error;
      }
    }
  }

  throw new Error(`No free port found from ${startPort} to ${startPort + 20}`);
}

const { port } = await listenWithFallback(requestedPort);
console.log(`Aura Figma capture server: http://${host}:${port}/figma-capture.html`);
