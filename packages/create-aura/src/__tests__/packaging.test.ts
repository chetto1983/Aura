import { readFileSync } from 'node:fs';

import { describe, expect, it } from 'vitest';

const pkg = JSON.parse(
  readFileSync(new URL('../../package.json', import.meta.url), 'utf8'),
) as {
  name: string;
  files: string[];
  scripts: Record<string, string>;
  bin: Record<string, string>;
};

// Decision 5: the package CARRIES the payload, so nothing is fetched at install time. A
// tarball without the artifact cannot install anything, and every other test in this suite
// would still pass -- local.ts's resolveInstallerArtifact is the only thing that would
// notice, on the operator's machine, after they had already run npx.
describe('packaging', () => {
  it('lists the artifact in package.json files', () => {
    expect(pkg.files).toContain('install-appliance.run');
  });

  // prepack is what npm runs before `npm pack` and `npm publish`, so wiring the artifact
  // build there is what makes the guarantee above true for a real publish rather than only
  // for a developer who remembered to run one more command.
  it('builds the artifact as part of prepack', () => {
    expect(pkg.scripts.prepack).toContain('build:artifact');
    expect(pkg.scripts['build:artifact']).toBe('node scripts/build-artifact.mjs');
  });

  // The published bin must point into dist/, not src/: the tarball ships no TypeScript.
  // The bin's NAME is read from the manifest instead of being written here: renaming the
  // published package is a packaging decision, shipping TypeScript is a defect, and only
  // the second one is what this assertion is for.
  it('points its bin at the compiled entry point', () => {
    const entries = Object.entries(pkg.bin);
    expect(entries).toHaveLength(1);
    expect(entries[0]?.[1]).toBe('dist/bin.js');
  });

  // `npx <pkg>` runs the bin whose name matches the package; a bin named anything else
  // makes the one command the README documents fail on a machine that has never installed
  // it. The rename to create-aura-appliance is exactly when this could have broken.
  it('names its bin after the package so npx resolves it', () => {
    expect(Object.keys(pkg.bin)).toEqual([pkg.name]);
  });
});
