import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import test from "node:test";

const installer = await fs.readFile(
  path.join(import.meta.dirname, "install-pyodide-bundle.mjs"),
  "utf8",
);

test("runner template mounts input files before executing Python", () => {
  assert.match(installer, /function safeArtifactName\(name\)/);
  assert.match(installer, /function mountInputFiles\(pyodide, inputFiles\)/);
  assert.match(installer, /safeArtifactName\(path\.basename\(hostPath\)\)/);
  assert.match(installer, /FS\.writeFile\(name, bytes\)/);

  const mountCall = installer.indexOf("await mountInputFiles(pyodide, request.input_files);");
  const pythonCall = installer.indexOf("await pyodide.runPythonAsync(String(request.code || \"\"));");
  assert.ok(mountCall > 0, "runner must call mountInputFiles");
  assert.ok(pythonCall > 0, "runner must execute Python");
  assert.ok(mountCall < pythonCall, "input files must be mounted before Python runs");
});
