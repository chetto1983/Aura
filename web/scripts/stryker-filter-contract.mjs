import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from 'node:fs';
import { basename, dirname, join, relative, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  collectTestName,
  VITEST_LEGACY_TEST_NAME_SEPARATOR,
  VITEST_MODERN_TEST_NAME_SEPARATOR,
} from '../node_modules/@stryker-mutator/vitest-runner/dist/src/test-helpers.js';

const root = realpathSync(fileURLToPath(new URL('..', import.meta.url)));
// The runner chooses its separator in a private getter (#testNameSeparator in
// vitest-test-runner.js): the modern one from Vitest 5 on. Pin the installed major so
// a downgrade cannot silently send the legacy separator to a Vitest that ignores it.
const vitestMajor = Number(
  JSON.parse(readFileSync(join(root, 'node_modules/vitest/package.json'), 'utf8')).version.split(
    '.',
  )[0],
);
assert(
  vitestMajor >= 5,
  `the runner selects the modern separator only from Vitest 5; installed ${vitestMajor}`,
);
const sourceRoot = realpathSync(join(root, 'src'));
const directory = mkdtempSync(join(sourceRoot, 'stryker-filter-contract-'));
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
    [VITEST_LEGACY_TEST_NAME_SEPARATOR, 0],
    [VITEST_MODERN_TEST_NAME_SEPARATOR, 1],
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
