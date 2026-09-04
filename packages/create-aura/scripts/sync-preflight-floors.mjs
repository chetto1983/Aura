#!/usr/bin/env node
// Mirrors scripts/install.sh's HARD preflight floors into a TypeScript module.
//
// install.sh owns these numbers alone, and it has to: preflight_hw runs before any
// download_file, and the `curl | bash` entry point ships that one file, so the shell
// installer cannot source them from anywhere else. This CLI must refuse exactly what
// install.sh refuses -- and a second, hand-maintained copy drifted the first time the
// floor moved (2026-09-04: bash went to 14 GiB, TypeScript stayed at 15).
//
// `--check` verifies instead of writing, so a stale mirror fails the build and CI rather
// than shipping a wizard that disagrees with the installer it drives.
import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const installer = join(here, '..', '..', '..', 'scripts', 'install.sh');
const target = join(here, '..', 'src', 'preflight-floors.ts');

// Only the hard floors. install.sh's warn_mem/warn_disk stay behind deliberately: a wizard
// that blocks where the installer merely warns is worse than one that says nothing.
const FLOORS = [
  { name: 'MINIMUM_CPU_CORES', pattern: /"\$cpus"\s+-lt\s+(\d+)\b/, gib: false },
  { name: 'MINIMUM_MEMORY_KB', pattern: /hard_mem=\$\(\((\d+)\s*\*\s*1024\s*\*\s*1024\)\)/, gib: true },
  { name: 'MINIMUM_DISK_KB', pattern: /hard_disk=\$\(\((\d+)\s*\*\s*1024\s*\*\s*1024\)\)/, gib: true },
];

const source = readFileSync(installer, 'utf8');
const body = FLOORS.map((floor) => {
  const match = source.match(floor.pattern);
  if (!match) {
    console.error(`sync-preflight-floors: ${floor.name} not found in scripts/install.sh`);
    process.exit(1);
  }
  const raw = Number(match[1]);
  return floor.gib
    ? `export const ${floor.name} = ${raw * 1024 * 1024}; // ${raw} GiB`
    : `export const ${floor.name} = ${raw};`;
}).join('\n');

const contents = `// GENERATED FILE -- do not edit by hand.
// Mirrored from scripts/install.sh by scripts/sync-preflight-floors.mjs, which is the only
// place allowed to write it. install.sh owns these floors because it must stay
// self-contained; see that script for why each number is what it is. Change them there,
// then run \`npm run sync:floors\`. \`npm run build\` refuses to build a stale mirror.
${body}
`;

if (process.argv.includes('--check')) {
  let current = '';
  try {
    current = readFileSync(target, 'utf8');
  } catch {
    console.error('sync-preflight-floors: src/preflight-floors.ts is missing; run `npm run sync:floors`');
    process.exit(1);
  }
  if (current !== contents) {
    console.error('sync-preflight-floors: src/preflight-floors.ts is stale against scripts/install.sh.');
    console.error('Run `npm run sync:floors` and commit the result.');
    process.exit(1);
  }
  console.log('sync-preflight-floors: mirror matches scripts/install.sh');
} else {
  writeFileSync(target, contents);
  console.log(`sync-preflight-floors: wrote ${target}`);
}
