#!/usr/bin/env node
// Builds the self-extracting installer this package CARRIES (spec decision 5): the npm
// tarball ships the payload rather than fetching it, which is why the reference's
// artifact.ts / installer-manifest.ts / stamp-installer.mjs / verify-installer-manifest.mjs
// are deliberately not ported. Nothing is downloaded at install time, and
// raw.githubusercontent.com is out of the install trust path entirely.
//
// This is a thin caller of scripts/build_installer.sh, not a second implementation. That
// script derives the payload list from install.sh's own download_file calls, so a file
// added there cannot be forgotten here.
import { spawnSync } from 'node:child_process';
import { existsSync, statSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const packageRoot = fileURLToPath(new URL('..', import.meta.url));
const repoRoot = fileURLToPath(new URL('../../..', import.meta.url));
const output = fileURLToPath(new URL('../install-appliance.run', import.meta.url));

// No skip-as-green: a missing makeself must fail the build loudly. An artifact that
// silently did not get built produces a tarball whose every other test still passes and
// which cannot install anything -- the exact failure the packaging test guards from the
// other side.
const probe = spawnSync('bash', ['-c', 'command -v makeself >/dev/null 2>&1'], { stdio: 'ignore' });
if (probe.status !== 0) {
  console.error('FAIL: makeself is required to build the installer artifact (apt-get install -y makeself)');
  process.exit(1);
}

const build = spawnSync(
  'bash',
  [`${repoRoot}/scripts/build_installer.sh`, output],
  { cwd: packageRoot, stdio: 'inherit' },
);
if (build.status !== 0) {
  console.error(`FAIL: scripts/build_installer.sh exited ${build.status ?? 'on a signal'}`);
  process.exit(build.status === null ? 1 : build.status);
}

// build_installer.sh reporting success while writing nothing would be the worst outcome:
// prepack would continue and publish a payload-less tarball.
if (!existsSync(output) || statSync(output).size === 0) {
  console.error(`FAIL: ${output} was not written`);
  process.exit(1);
}
console.log(`built ${output} (${statSync(output).size} bytes)`);
