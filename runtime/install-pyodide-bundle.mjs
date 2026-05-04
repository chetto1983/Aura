#!/usr/bin/env node

import { createHash } from "node:crypto";
import { createWriteStream } from "node:fs";
import fs from "node:fs/promises";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { pipeline } from "node:stream/promises";

const PYODIDE_VERSION = "0.29.3";
const NODE_WIN_X64_VERSION = "22.13.1";
const NODE_WIN_X64_ZIP = "node-v22.13.1-win-x64.zip";
const BASE_IMPORTS = [
  "numpy",
  "pandas",
  "scipy",
  "statsmodels",
  "matplotlib",
  "PIL",
  "fitz",
  "bs4",
  "lxml",
  "html5lib",
  "pyarrow",
  "python_calamine",
  "xlrd",
  "requests",
  "yaml",
  "dateutil",
  "pytz",
  "tzdata",
  "regex",
  "rich",
];
const BASE_PACKAGES = [
  "numpy",
  "pandas",
  "scipy",
  "statsmodels",
  "matplotlib",
  "pillow",
  "pymupdf",
  "beautifulsoup4",
  "lxml",
  "html5lib",
  "pyarrow",
  "python-calamine",
  "xlrd",
  "requests",
  "pyyaml",
  "python-dateutil",
  "pytz",
  "tzdata",
  "regex",
  "rich",
];
const CORE_FILES = [
  "pyodide.mjs",
  "pyodide.js",
  "pyodide.asm.js",
  "pyodide.asm.wasm",
  "python_stdlib.zip",
];

const opts = parseArgs(process.argv.slice(2));
const repoRoot = path.resolve(import.meta.dirname, "..");
const runtimeDir = path.resolve(opts.runtimeDir || path.join(repoRoot, "runtime", "pyodide"));
const downloadsDir = path.resolve(opts.downloadsDir || path.join(repoRoot, "runtime", "_downloads"));
assertInside(repoRoot, runtimeDir, "runtime dir");
assertInside(repoRoot, downloadsDir, "downloads dir");

await fs.rm(runtimeDir, { recursive: true, force: true });
await fs.mkdir(runtimeDir, { recursive: true });
await fs.mkdir(downloadsDir, { recursive: true });

const pyodideDir = await installPyodidePackage(downloadsDir);
const lock = JSON.parse(await fs.readFile(path.join(pyodideDir, "pyodide-lock.json"), "utf8"));
const selectedPackages = resolvePackageClosure(lock, BASE_PACKAGES);

for (const file of CORE_FILES) {
  await fs.copyFile(path.join(pyodideDir, file), path.join(runtimeDir, file));
}
await fs.mkdir(path.join(runtimeDir, "packages"), { recursive: true });

for (const name of selectedPackages) {
  const meta = lock.packages[name];
  const fileName = path.basename(meta.file_name);
  const dest = path.join(runtimeDir, "packages", fileName);
  await downloadIfMissing(
    `https://cdn.jsdelivr.net/pyodide/v${PYODIDE_VERSION}/full/${fileName}`,
    dest,
  );
  const got = await sha256File(dest);
  if (meta.sha256 && got !== meta.sha256) {
    throw new Error(`sha256 mismatch for ${fileName}`);
  }
}

const localLock = {
  ...lock,
  packages: Object.fromEntries(selectedPackages.map((name) => {
    const meta = { ...lock.packages[name] };
    meta.file_name = `packages/${path.basename(meta.file_name)}`;
    return [name, meta];
  })),
};
await writeJSON(path.join(runtimeDir, "pyodide-lock.json"), localLock);
await writeJSON(path.join(runtimeDir, "repodata.json"), localLock);
await writeRunner(runtimeDir);
if (opts.withNodeWinX64) {
  await installWindowsNodeRunner(downloadsDir, path.join(runtimeDir, "runner"));
}
await writeManifest(runtimeDir, localLock, selectedPackages);

console.log(`Pyodide ${PYODIDE_VERSION} bundle ready at ${path.relative(repoRoot, runtimeDir)}`);
console.log(`packages=${selectedPackages.length} windows_node=${opts.withNodeWinX64}`);

function parseArgs(args) {
  const out = { runtimeDir: "", downloadsDir: "", withNodeWinX64: false };
  for (let i = 0; i < args.length; i++) {
    switch (args[i]) {
      case "--runtime-dir":
        out.runtimeDir = args[++i] || "";
        break;
      case "--downloads-dir":
        out.downloadsDir = args[++i] || "";
        break;
      case "--with-node-win-x64":
        out.withNodeWinX64 = true;
        break;
      default:
        throw new Error(`unknown argument: ${args[i]}`);
    }
  }
  return out;
}

async function installPyodidePackage(root) {
  const prefix = path.join(root, "npm");
  const pkgDir = path.join(prefix, "node_modules", "pyodide");
  const pkgJSON = path.join(pkgDir, "package.json");
  try {
    const pkg = JSON.parse(await fs.readFile(pkgJSON, "utf8"));
    if (pkg.version === PYODIDE_VERSION) return pkgDir;
  } catch {
    // install below
  }
  await fs.rm(prefix, { recursive: true, force: true });
  const result = spawnSync(npmCommand(), [
    "install",
    `pyodide@${PYODIDE_VERSION}`,
    "--prefix",
    prefix,
    "--ignore-scripts",
    "--no-audit",
    "--no-fund",
  ], { stdio: "inherit", shell: process.platform === "win32" });
  if (result.status !== 0) {
    throw new Error("npm install pyodide failed");
  }
  return pkgDir;
}

function resolvePackageClosure(lock, roots) {
  const seen = new Set();
  const visit = (name) => {
    if (seen.has(name)) return;
    const meta = lock.packages[name];
    if (!meta) throw new Error(`package ${name} missing from pyodide-lock.json`);
    seen.add(name);
    for (const dep of meta.depends || []) visit(dep);
  };
  for (const root of roots) visit(root);
  return [...seen].sort();
}

async function downloadIfMissing(url, dest) {
  try {
    const stat = await fs.stat(dest);
    if (stat.size > 0) return;
  } catch {
    // download below
  }
  await fs.mkdir(path.dirname(dest), { recursive: true });
  const response = await fetch(url);
  if (!response.ok) throw new Error(`download ${url}: ${response.status}`);
  await pipeline(response.body, createWriteStream(dest));
}

async function installWindowsNodeRunner(downloads, runnerDir) {
  const zipPath = path.join(downloads, NODE_WIN_X64_ZIP);
  await downloadIfMissing(`https://nodejs.org/dist/v${NODE_WIN_X64_VERSION}/${NODE_WIN_X64_ZIP}`, zipPath);
  const extractDir = path.join(downloads, "node-win-x64");
  await fs.rm(extractDir, { recursive: true, force: true });
  await fs.mkdir(extractDir, { recursive: true });
  extractZip(zipPath, extractDir);
  const expandedRoot = path.join(extractDir, `node-v${NODE_WIN_X64_VERSION}-win-x64`);
  await copyDir(expandedRoot, path.join(runnerDir, "node-win-x64"));
}

function extractZip(zipPath, dest) {
  const command = process.platform === "win32" ? "powershell" : "unzip";
  const args = process.platform === "win32"
    ? ["-NoProfile", "-Command", "Expand-Archive", "-LiteralPath", zipPath, "-DestinationPath", dest, "-Force"]
    : ["-q", zipPath, "-d", dest];
  const result = spawnSync(command, args, { stdio: "inherit" });
  if (result.status !== 0) throw new Error(`extract ${path.basename(zipPath)} failed`);
}

async function writeRunner(root) {
  const runnerDir = path.join(root, "runner");
  await fs.mkdir(runnerDir, { recursive: true });
  await fs.writeFile(path.join(runnerDir, "aura-pyodide-runner.mjs"), runnerMJS(), "utf8");
  await fs.writeFile(path.join(runnerDir, "aura-pyodide-runner.cmd"), runnerCMD(), "utf8");
  const shPath = path.join(runnerDir, "aura-pyodide-runner");
  await fs.writeFile(shPath, runnerSH(), "utf8");
  await fs.chmod(shPath, 0o755);
}

async function writeManifest(root, localLock, selectedPackages) {
  const files = [];
  for (const file of [...CORE_FILES, "pyodide-lock.json", "repodata.json"]) {
    files.push({ path: file, sha256: await sha256File(path.join(root, file)), required: true });
  }
  const packages = [];
  for (const name of selectedPackages) {
    const meta = localLock.packages[name];
    packages.push({
      name,
      import_name: (meta.imports || [name])[0],
      version: meta.version || "",
      path: meta.file_name,
      sha256: await sha256File(path.join(root, meta.file_name)),
      required: true,
    });
  }
  await writeJSON(path.join(root, "aura-pyodide-manifest.json"), {
    schema_version: 1,
    runtime: "pyodide",
    pyodide_version: PYODIDE_VERSION,
    files,
    packages,
    smoke_imports: BASE_IMPORTS,
  });
}

async function writeJSON(file, value) {
  await fs.writeFile(file, `${JSON.stringify(value, null, 2)}\n`, "utf8");
}

async function sha256File(file) {
  const h = createHash("sha256");
  h.update(await fs.readFile(file));
  return h.digest("hex");
}

async function copyDir(src, dest) {
  await fs.rm(dest, { recursive: true, force: true });
  await fs.mkdir(dest, { recursive: true });
  for (const entry of await fs.readdir(src, { withFileTypes: true })) {
    const from = path.join(src, entry.name);
    const to = path.join(dest, entry.name);
    if (entry.isDirectory()) {
      await copyDir(from, to);
    } else if (entry.isFile()) {
      await fs.copyFile(from, to);
    }
  }
}

function assertInside(root, target, label) {
  const rel = path.relative(root, target);
  if (rel === "" || rel.startsWith("..") || path.isAbsolute(rel)) {
    throw new Error(`${label} must stay inside repository: ${target}`);
  }
}

function npmCommand() {
  return process.platform === "win32" ? "npm.cmd" : "npm";
}

function runnerMJS() {
  return `import { loadPyodide } from "../pyodide.mjs";
import path from "node:path";

const OUTPUT_DIR = "/tmp/aura_out";
const MAX_ARTIFACTS = 10;
const MAX_ARTIFACT_BYTES = 5 * 1024 * 1024;

function argValue(name, fallback = "") {
  const idx = process.argv.indexOf(name);
  return idx >= 0 && idx + 1 < process.argv.length ? process.argv[idx + 1] : fallback;
}

async function readStdin() {
  const chunks = [];
  for await (const chunk of process.stdin) chunks.push(chunk);
  return Buffer.concat(chunks).toString("utf8");
}

function artifactMime(name) {
  const lower = name.toLowerCase();
  if (lower.endsWith(".png")) return "image/png";
  if (lower.endsWith(".jpg") || lower.endsWith(".jpeg")) return "image/jpeg";
  if (lower.endsWith(".svg")) return "image/svg+xml";
  if (lower.endsWith(".csv")) return "text/csv; charset=utf-8";
  if (lower.endsWith(".txt") || lower.endsWith(".log") || lower.endsWith(".md")) return "text/plain; charset=utf-8";
  if (lower.endsWith(".json")) return "application/json";
  if (lower.endsWith(".pdf")) return "application/pdf";
  if (lower.endsWith(".xlsx")) return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet";
  return "application/octet-stream";
}

function safeArtifactName(name) {
  return String(name || "").replace(/[^A-Za-z0-9._-]/g, "_").slice(0, 80);
}

function ensureOutputDir(pyodide) {
  const FS = pyodide.FS;
  if (!FS.analyzePath("/tmp").exists) FS.mkdir("/tmp");
  if (!FS.analyzePath(OUTPUT_DIR).exists) FS.mkdirTree(OUTPUT_DIR);
}

function collectArtifacts(pyodide) {
  const FS = pyodide.FS;
  if (!FS.analyzePath(OUTPUT_DIR).exists) return [];
  const names = FS.readdir(OUTPUT_DIR).filter((name) => name !== "." && name !== "..").sort();
  const artifacts = [];
  for (const rawName of names) {
    if (artifacts.length >= MAX_ARTIFACTS) break;
    const name = safeArtifactName(rawName);
    if (!name || name !== rawName) continue;
    const filePath = OUTPUT_DIR + "/" + name;
    const info = FS.analyzePath(filePath);
    if (!info.exists || !FS.isFile(info.object.mode)) continue;
    const bytes = FS.readFile(filePath, { encoding: "binary" });
    if (bytes.length > MAX_ARTIFACT_BYTES) continue;
    artifacts.push({
      name,
      mime_type: artifactMime(name),
      size_bytes: bytes.length,
      content_base64: Buffer.from(bytes).toString("base64"),
    });
  }
  return artifacts;
}

const started = Date.now();
const runtimeDir = path.resolve(argValue("--runtime-dir", path.join(import.meta.dirname, "..")));
try {
  const request = JSON.parse(await readStdin());
  const indexURL = runtimeDir + path.sep;
  const pyodide = await loadPyodide({ indexURL, packageBaseUrl: indexURL, lockFileURL: path.join(runtimeDir, "pyodide-lock.json") });
  let stdout = "";
  let stderr = "";
  pyodide.setStdout({ batched: (text) => { stdout += text + "\\n"; } });
  pyodide.setStderr({ batched: (text) => { stderr += text + "\\n"; } });
  const lock = JSON.parse(await import("node:fs/promises").then(fs => fs.readFile(path.join(runtimeDir, "pyodide-lock.json"), "utf8")));
  const importToPackage = new Map();
  for (const [name, meta] of Object.entries(lock.packages || {})) {
    importToPackage.set(name, name);
    for (const imp of meta.imports || []) importToPackage.set(imp, name);
  }
  const packages = [...new Set((Array.isArray(request.packages) ? request.packages : []).map((name) => importToPackage.get(name) || name))];
  if (packages.length > 0) {
    await pyodide.loadPackage(packages, { messageCallback: () => {}, errorCallback: (msg) => { stderr += msg + "\\n"; } });
  }
  ensureOutputDir(pyodide);
  await pyodide.runPythonAsync(String(request.code || ""));
  const artifacts = collectArtifacts(pyodide);
  process.stdout.write(JSON.stringify({ ok: true, stdout, stderr, exit_code: 0, elapsed_ms: Date.now() - started, artifacts }));
} catch (error) {
  process.stdout.write(JSON.stringify({ ok: false, stdout: "", stderr: String(error && error.stack || error), exit_code: 1, elapsed_ms: Date.now() - started }));
}
`;
}

function runnerCMD() {
  return `@echo off
setlocal
set "DIR=%~dp0"
if exist "%DIR%node-win-x64\\node.exe" (
  "%DIR%node-win-x64\\node.exe" "%DIR%aura-pyodide-runner.mjs" %*
) else (
  node "%DIR%aura-pyodide-runner.mjs" %*
)
`;
}

function runnerSH() {
  return `#!/usr/bin/env sh
set -eu
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if [ -x "$DIR/node-linux-x64/bin/node" ]; then
  NODE="$DIR/node-linux-x64/bin/node"
else
  NODE=node
fi
exec "$NODE" "$DIR/aura-pyodide-runner.mjs" "$@"
`;
}
