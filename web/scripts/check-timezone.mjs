import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import ts from 'typescript';

const root = path.resolve(import.meta.dirname, '..');
const sourcePath = path.join(root, 'src', 'lib', 'timezone.ts');
const source = fs.readFileSync(sourcePath, 'utf8');
const compiled = ts.transpileModule(source, {
  compilerOptions: {
    module: ts.ModuleKind.ES2022,
    target: ts.ScriptTarget.ES2022,
  },
}).outputText;
const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'aura-timezone-'));
const modulePath = path.join(tmpDir, 'timezone.mjs');
fs.writeFileSync(modulePath, compiled);

const {
  fromDateTimeLocalValueInTimeZone,
  toDateTimeLocalValueInTimeZone,
} = await import(pathToFileURL(modulePath).href);

assert.equal(
  toDateTimeLocalValueInTimeZone('2026-07-01T10:00:00Z', 'Europe/Rome'),
  '2026-07-01T12:00',
);
assert.equal(
  toDateTimeLocalValueInTimeZone('2026-01-01T10:00:00Z', 'Europe/Rome'),
  '2026-01-01T11:00',
);
assert.equal(
  fromDateTimeLocalValueInTimeZone('2026-07-01T12:00', 'Europe/Rome'),
  '2026-07-01T10:00:00.000Z',
);
assert.equal(
  fromDateTimeLocalValueInTimeZone('2026-01-01T11:00', 'Europe/Rome'),
  '2026-01-01T10:00:00.000Z',
);

console.log('timezone conversion checks passed');
