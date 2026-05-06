import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import vm from "node:vm";
import test from "node:test";

const installer = await fs.readFile(
  path.join(import.meta.dirname, "install-pyodide-bundle.mjs"),
  "utf8",
);

test("generated runner mounts input file bytes before executing Python", async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "aura-pyodide-runner-"));
  const runnerDir = path.join(root, "runner");
  const inputDir = path.join(root, "inputs");
  const logPath = path.join(root, "events.jsonl");
  const workbookPath = path.join(inputDir, "workbook.xlsx");
  const workbookBytes = Buffer.from("fake workbook bytes");

  await fs.mkdir(runnerDir);
  await fs.mkdir(inputDir);
  await fs.writeFile(workbookPath, workbookBytes);
  await fs.writeFile(path.join(root, "pyodide-lock.json"), JSON.stringify({ packages: {} }));
  await fs.writeFile(path.join(root, "pyodide.mjs"), fakePyodideModule(), "utf8");
  await fs.writeFile(path.join(runnerDir, "aura-pyodide-runner.mjs"), runnerTemplate(), "utf8");

  const result = spawnSync(process.execPath, [
    path.join(runnerDir, "aura-pyodide-runner.mjs"),
    "--runtime-dir",
    root,
  ], {
    input: JSON.stringify({ code: "Path('workbook.xlsx')", input_files: [workbookPath] }),
    encoding: "utf8",
    env: { ...process.env, AURA_FAKE_PYODIDE_LOG: logPath },
  });

  assert.equal(result.status, 0, result.stderr);
  const response = JSON.parse(result.stdout);
  assert.equal(response.ok, true, response.stderr);

  const events = (await fs.readFile(logPath, "utf8"))
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line));
  assert.deepEqual(events, [
    { event: "writeFile", name: "workbook.xlsx", hex: workbookBytes.toString("hex") },
    { event: "runPythonAsync", hasWorkbook: true, hex: workbookBytes.toString("hex") },
  ]);
});

function runnerTemplate() {
  const start = installer.indexOf("function runnerMJS() {");
  const end = installer.indexOf("\nfunction runnerCMD()", start);
  assert.ok(start >= 0 && end > start, "runnerMJS function must exist in installer");
  const source = installer.slice(start, end);
  return vm.runInNewContext(`${source}\nrunnerMJS();`);
}

function fakePyodideModule() {
  return `
import fs from "node:fs";

const files = new Map();
const dirs = new Set(["/", "/tmp"]);

function log(event) {
  fs.appendFileSync(process.env.AURA_FAKE_PYODIDE_LOG, JSON.stringify(event) + "\\n");
}

function normalize(filePath) {
  return filePath.startsWith("/") ? filePath : "/" + filePath;
}

export async function loadPyodide() {
  return {
    FS: {
      analyzePath(filePath) {
        const normalized = normalize(filePath);
        if (files.has(filePath) || files.has(normalized)) {
          return { exists: true, object: { mode: "file" } };
        }
        if (dirs.has(normalized)) {
          return { exists: true, object: { mode: "dir" } };
        }
        return { exists: false, object: null };
      },
      mkdir(filePath) {
        dirs.add(normalize(filePath));
      },
      mkdirTree(filePath) {
        dirs.add(normalize(filePath));
      },
      writeFile(name, bytes) {
        const buffer = Buffer.from(bytes);
        files.set(name, buffer);
        log({ event: "writeFile", name, hex: buffer.toString("hex") });
      },
      readdir(filePath) {
        return dirs.has(normalize(filePath)) ? [".", ".."] : [];
      },
      isFile(mode) {
        return mode === "file";
      },
      readFile(filePath) {
        return files.get(filePath) || files.get(normalize(filePath)) || Buffer.alloc(0);
      },
    },
    setStdout() {},
    setStderr() {},
    async loadPackage() {},
    async runPythonAsync() {
      const bytes = files.get("workbook.xlsx");
      log({
        event: "runPythonAsync",
        hasWorkbook: Boolean(bytes),
        hex: bytes ? bytes.toString("hex") : "",
      });
    },
  };
}
`;
}
