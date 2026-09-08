import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from 'node:fs';
import { basename, dirname, join, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = realpathSync(fileURLToPath(new URL('..', import.meta.url)));
const compiler = JSON.parse(
  readFileSync(join(root, 'node_modules/typescript/package.json'), 'utf8'),
);
const engine = JSON.parse(
  readFileSync(join(root, 'node_modules/oxlint-tsgolint/package.json'), 'utf8'),
);
// tsgolint encodes the compiler patch in thousands, with its own patch in the remainder.
const [major, minor, patch] = engine.version.split('.').map(Number);
assert.equal(
  compiler.version,
  [major, minor, Math.floor(patch / 1000)].join('.'),
  'Compiler and typed linter must agree',
);
const results = join(root, 'src');
assert(realpathSync(results).startsWith(root + sep));
const directory = mkdtempSync(join(results, 'lint-contract-'));

const cases = [
  {
    name: 'valid',
    code: 'export function value(): Promise<number> { return Promise.resolve(1); }',
  },
  {
    name: 'floating',
    rule: 'typescript(no-floating-promises)',
    code: 'export function run(): void { Promise.resolve(1); }',
  },
  {
    name: 'unsafe',
    rule: 'typescript(no-unsafe-assignment)',
    code: 'export function parse(raw: string): string { const value: string = JSON.parse(raw); return value; }',
  },
  {
    name: 'hooks',
    rule: 'react-hooks(rules-of-hooks)',
    code: 'import { useState } from "react"; export function Bad({ enabled }: { enabled: boolean }) { if (enabled) { const [value] = useState(0); return <span>{value}</span>; } return null; }',
  },
  {
    name: 'effect',
    rule: 'react-compiler(set-state-in-effect)',
    code: 'import { useEffect, useState } from "react"; export function BadEffect() { const [value, setValue] = useState(0); useEffect(() => { setValue(1); }, []); return <span>{value}</span>; }',
  },
  {
    name: 'async-effect',
    code: 'import { useEffect, useState } from "react"; export function AsyncEffect() { const [value, setValue] = useState(0); useEffect(() => { void Promise.resolve(1).then(setValue); }, []); return <span>{value}</span>; }',
  },
  {
    name: 'accessibility',
    rule: 'jsx-a11y(alt-text)',
    code: 'export function Image() { return <img src="/image.png" />; }',
  },
  {
    name: 'imports',
    rule: 'import-order(order)',
    code: 'import { useState } from "react"; import { readFile } from "node:fs/promises"; export { useState, readFile };',
  },
];

try {
  for (const test of cases) writeFileSync(join(directory, `${test.name}.tsx`), test.code);
  const result = spawnSync(
    process.execPath,
    [
      join(root, 'node_modules/oxlint/bin/oxlint'),
      '--config',
      join(root, '.oxlintrc.json'),
      '--type-aware',
      '--format',
      'json',
      directory,
    ],
    { cwd: root, encoding: 'utf8', windowsHide: true, timeout: 60000 },
  );
  if (result.error) throw result.error;
  assert.equal(result.status, 1, result.stderr || result.stdout);
  const report = JSON.parse(result.stdout);
  assert.equal(report.number_of_files, cases.length, 'Every lint probe must execute');
  for (const test of cases) {
    const diagnostics = report.diagnostics.filter(
      (item) => basename(item.filename) === `${test.name}.tsx`,
    );
    if (test.rule) {
      assert(
        diagnostics.some((item) => item.code === test.rule),
        `${test.name}: missing ${test.rule}: ${JSON.stringify(diagnostics)}`,
      );
    } else {
      assert.deepEqual(diagnostics, [], `${test.name}: valid code rejected`);
    }
  }
  console.log(`lint-contract: ${cases.length} real compiler/plugin probes passed`);
} finally {
  assert.equal(dirname(realpathSync(directory)), realpathSync(results));
  rmSync(directory, { recursive: true, force: true });
}
