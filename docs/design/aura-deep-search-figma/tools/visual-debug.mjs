#!/usr/bin/env node
import { createServer } from "node:http";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const artifactRoot = path.join(root, ".visual-debug");
const args = process.argv.slice(2);

function readFlag(name, fallback = undefined) {
  const equals = args.find((arg) => arg.startsWith(`${name}=`));
  if (equals) return equals.slice(name.length + 1);
  const index = args.indexOf(name);
  if (index >= 0 && args[index + 1] && !args[index + 1].startsWith("--")) {
    return args[index + 1];
  }
  return fallback;
}

const requestedPort = Number.parseInt(readFlag("--port", process.env.PORT || "8877"), 10);
const externalUrl = readFlag("--url");
const browserChoice = readFlag("--browser", "chromium");
const headed = args.includes("--headed");
const recordTrace = args.includes("--trace");
const host = "127.0.0.1";

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

function timestamp() {
  return new Date().toISOString().replace("T", "_").replace(/[:.]/g, "-").slice(0, 19);
}

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
        "cache-control": "no-store",
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

async function probe(url) {
  try {
    const response = await fetch(url, { cache: "no-store" });
    return response.ok;
  } catch {
    return false;
  }
}

async function listenWithFallback(startPort) {
  for (let port = startPort; port <= startPort + 20; port += 1) {
    const url = `http://${host}:${port}/figma-capture.html`;
    if (await probe(url)) {
      return { url, server: null, reused: true, port };
    }

    const server = createStaticServer();
    try {
      await new Promise((resolve, reject) => {
        server.once("error", reject);
        server.listen(port, host, resolve);
      });
      return { url, server, reused: false, port };
    } catch (error) {
      if (error?.code !== "EADDRINUSE") {
        throw error;
      }
    }
  }

  throw new Error(`No free port found from ${startPort} to ${startPort + 20}`);
}

function launchOptions() {
  const options = {
    headless: !headed,
  };

  if (browserChoice === "chrome") {
    options.channel = "chrome";
  } else if (browserChoice === "edge") {
    options.channel = "msedge";
  } else if (browserChoice !== "chromium") {
    throw new Error(`Unsupported browser "${browserChoice}". Use chromium, chrome, or edge.`);
  }

  return options;
}

async function sectionHandle(page, label) {
  const handle = await page.evaluateHandle((expectedLabel) => {
    for (const section of document.querySelectorAll("section.frame")) {
      const eyebrow = section.querySelector(".eyebrow")?.textContent?.trim();
      if (eyebrow === expectedLabel) {
        return section;
      }
    }
    return null;
  }, label);

  return handle.asElement();
}

function consoleEntry(message) {
  return {
    type: message.type(),
    text: message.text(),
    location: message.location(),
  };
}

const serverInfo = externalUrl ? { url: externalUrl, server: null, reused: true, port: null } : await listenWithFallback(requestedPort);
const runDir = path.join(artifactRoot, timestamp());
await mkdir(runDir, { recursive: true });

const browser = await chromium.launch(launchOptions());
const context = await browser.newContext({
  deviceScaleFactor: 1,
  viewport: { width: 3200, height: 1800 },
});

if (recordTrace) {
  await context.tracing.start({ screenshots: true, snapshots: true, sources: true });
}

const page = await context.newPage();
const consoleMessages = [];
const pageErrors = [];
page.on("console", (message) => {
  if (["error", "warning"].includes(message.type())) {
    consoleMessages.push(consoleEntry(message));
  }
});
page.on("pageerror", (error) => {
  pageErrors.push({ name: error.name, message: error.message, stack: error.stack });
});

const checks = [];
const failures = [];

function check(name, pass, details = {}) {
  const result = { name, pass: Boolean(pass), details };
  checks.push(result);
  if (!result.pass) {
    failures.push(result);
  }
}

try {
  await page.goto(serverInfo.url, { waitUntil: "domcontentloaded" });
  await page.waitForFunction(() => document.documentElement.dataset.captureReady === "true", null, { timeout: 15000 });
  await page.waitForTimeout(500);

  await page.screenshot({ path: path.join(runDir, "desktop-viewport.png") });

  const labelsToCapture = [
    ["05 Backend capability map", "backend-map.png"],
    ["06 Screens / imported source board", "screens-board.png"],
    ["07 Prototype QA", "prototype-qa.png"],
  ];

  for (const [label, filename] of labelsToCapture) {
    const element = await sectionHandle(page, label);
    if (!element) {
      check(`section exists: ${label}`, false);
      continue;
    }
    await element.screenshot({ path: path.join(runDir, filename) });
  }

  const domReport = await page.evaluate(() => {
    const all = (selector) => Array.from(document.querySelectorAll(selector));
    const sections = all("section.frame").map((section) => {
      const rect = section.getBoundingClientRect();
      return {
        eyebrow: section.querySelector(".eyebrow")?.textContent?.trim() || "",
        top: Math.round(rect.top + window.scrollY),
        width: Math.round(rect.width),
        height: Math.round(rect.height),
      };
    });

    const horizontalOverflows = all(".capability-card, .component-row, .panel, td, .flow-step h3, .flow-step p")
      .filter((element) => !element.classList.contains("flow-step"))
      .filter((element) => element.scrollWidth > element.clientWidth + 2)
      .map((element) => ({
        selector:
          element.className ||
          element.tagName.toLowerCase(),
        text: (element.textContent || "").replace(/\s+/g, " ").trim().slice(0, 140),
        scrollWidth: element.scrollWidth,
        clientWidth: element.clientWidth,
      }));

    return {
      title: document.title,
      captureReady: document.documentElement.dataset.captureReady === "true",
      sectionCount: sections.length,
      sections,
      capabilityCardCount: document.querySelectorAll(".capability-card").length,
      sourceSvgLoaded: Boolean(document.querySelector("#source-svg svg")),
      horizontalOverflows,
    };
  });

  const sectionTops = new Map(domReport.sections.map((section) => [section.eyebrow, section.top]));
  check("document title names Aura Figma infrastructure", domReport.title.includes("Aura Deep Search"));
  check("captureReady set after SVG import", domReport.captureReady);
  check("expected project section count", domReport.sectionCount === 8, { actual: domReport.sectionCount });
  check("backend capability cards are complete", domReport.capabilityCardCount === 12, { actual: domReport.capabilityCardCount });
  check("source SVG imported", domReport.sourceSvgLoaded);
  check("backend map before screens", sectionTops.get("05 Backend capability map") < sectionTops.get("06 Screens / imported source board"), {
    backendTop: sectionTops.get("05 Backend capability map"),
    screensTop: sectionTops.get("06 Screens / imported source board"),
  });
  check("screens before prototype QA", sectionTops.get("06 Screens / imported source board") < sectionTops.get("07 Prototype QA"), {
    screensTop: sectionTops.get("06 Screens / imported source board"),
    qaTop: sectionTops.get("07 Prototype QA"),
  });
  check("no obvious horizontal text overflow", domReport.horizontalOverflows.length === 0, {
    count: domReport.horizontalOverflows.length,
    examples: domReport.horizontalOverflows.slice(0, 5),
  });

  check("no runtime page errors", pageErrors.length === 0, { count: pageErrors.length });

  const report = {
    ok: failures.length === 0,
    url: serverInfo.url,
    server: {
      reused: serverInfo.reused,
      port: serverInfo.port,
    },
    browser: browserChoice,
    headed,
    trace: recordTrace,
    artifacts: {
      directory: runDir,
      desktopViewport: path.join(runDir, "desktop-viewport.png"),
      backendMap: path.join(runDir, "backend-map.png"),
      screensBoard: path.join(runDir, "screens-board.png"),
      prototypeQa: path.join(runDir, "prototype-qa.png"),
      trace: recordTrace ? path.join(runDir, "trace.zip") : null,
    },
    checks,
    consoleMessages,
    pageErrors,
    domReport,
  };

  if (recordTrace) {
    await context.tracing.stop({ path: path.join(runDir, "trace.zip") });
  }

  await writeFile(path.join(runDir, "report.json"), `${JSON.stringify(report, null, 2)}\n`, "utf8");
  console.log(JSON.stringify({
    ok: report.ok,
    url: report.url,
    artifactDirectory: report.artifacts.directory,
    screenshots: [
      report.artifacts.desktopViewport,
      report.artifacts.backendMap,
      report.artifacts.screensBoard,
      report.artifacts.prototypeQa,
    ],
    failedChecks: failures,
    consoleWarningsOrErrors: consoleMessages.length,
  }, null, 2));

  process.exitCode = report.ok ? 0 : 1;
} finally {
  if (recordTrace && context.tracing) {
    try {
      await context.tracing.stop({ path: path.join(runDir, "trace.zip") });
    } catch {
      // Trace may already be stopped after a successful run.
    }
  }
  await browser.close();
  await serverInfo.server?.close();
}
