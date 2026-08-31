#!/usr/bin/env node
// npx veneer over scripts/install.sh — every install decision lives THERE, this
// file only hands over. Exists so a fresh machine needs one memorable command:
//
//   sudo npx github:chetto1983/Aura -- --appliance
//
// npm packs only this file and install.sh (see package.json "files"); install.sh
// then fetches the appliance payload for its ref, so the two entry points
// (curl | bash and npx) share one implementation and one payload source.
import { spawnSync } from 'node:child_process';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const script = join(dirname(fileURLToPath(import.meta.url)), 'install.sh');
const res = spawnSync('bash', [script, ...process.argv.slice(2)], { stdio: 'inherit' });
if (res.error) {
  console.error(`bash is required to run the Aura installer: ${res.error.message}`);
  process.exit(1);
}
process.exit(res.status ?? 1);
