import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from 'node:fs';
import { basename, dirname, join, relative, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { collectTestName } from '../node_modules/@stryker-mutator/vitest-runner/dist/src/test-helpers.js';
import { VitestTestRunner } from '../node_modules/@stryker-mutator/vitest-runner/dist/src/vitest-test-runner.js';

const root = realpathSync(fileURLToPath(new URL('..', import.meta.url)));
const sourceRoot = realpathSync(join(root, 'src'));
const directory = mkdtempSync(join(sourceRoot, 'stryker-filter-contract-'));
const runner = new VitestTestRunner({}, {}, '__stryker__');
const task = {
  name: 'executes the selected test',
  suite: { name: 'inner suite', suite: { name: 'outer suite' } },
};
const fixture = join(directory, 'filter.test.ts');

try {
  writeFileSync(
    fixture,
    `import { describe, expect, it } from 'vitest';
describe('outer suite', () => {
  describe('inner suite', () => {
    it('executes the selected test', () => { expect(2 + 3).toBe(5); });
  });
});
`,
  );
  for (const [separator, expected] of [
    [' ', 0],
    [runner.testNameSeparator, 1],
  ]) {
    const report = join(directory, `result-${String(expected)}.json`);
    const run = spawnSync(
      process.execPath,
      [
        join(root, 'node_modules/vitest/vitest.mjs'),
        'run',
        relative(root, fixture),
        '--testNamePattern',
        `^${collectTestName(task, separator)}$`,
        '--reporter=json',
        '--outputFile',
        report,
      ],
      { cwd: root, encoding: 'utf8', timeout: 60000 },
    );
    assert.equal(run.status, 0, run.stderr || run.stdout);
    const result = JSON.parse(readFileSync(report, 'utf8'));
    assert.equal(result.numPassedTests, expected, 'Stryker must select the intended Vitest test');
  }
  console.log('Stryker/Vitest name-filter contract passed; no mutations executed.');
} finally {
  const target = realpathSync(directory);
  assert.equal(dirname(target), sourceRoot);
  assert(basename(target).startsWith('stryker-filter-contract-'));
  assert(target.startsWith(root + sep));
  rmSync(target, { recursive: true, force: true });
}
