#!/usr/bin/env node
// check_share_wire_contract.cjs — plan 37F-05 (WEBSHARE-02/03, T-37F-30). Asserts, by
// extraction rather than eyeballing, that every `json:"..."` tag on the Go
// internal/share/snapshot.go Snapshot family also appears in the TypeScript mirror
// web/src/chat/share/shareTypes.ts. A tag rename or omission on either side is a wire-contract
// drift that the render side would otherwise only discover at runtime — this script fails the
// build instead.
'use strict';

const fs = require('node:fs');
const path = require('node:path');

const repoRoot = path.resolve(__dirname, '..');
const goFile = path.join(repoRoot, 'internal', 'share', 'snapshot.go');
const tsFile = path.join(repoRoot, 'web', 'src', 'chat', 'share', 'shareTypes.ts');

function readOrFail(filePath) {
  try {
    return fs.readFileSync(filePath, 'utf8');
  } catch (err) {
    console.error(`check_share_wire_contract: cannot read ${filePath}: ${err.message}`);
    process.exit(1);
  }
}

const goSource = readOrFail(goFile);
const tsSource = readOrFail(tsFile);

// Extract every `json:"tag[,option...]"` tag literal, then keep only the bare tag name
// (the part before the first comma) — options like `omitempty` are not part of the wire key.
// `json:"-"` (a field explicitly excluded from JSON) is skipped: it never reaches the wire.
const tagPattern = /json:"([^"]*)"/g;
const tags = new Set();
let match;
while ((match = tagPattern.exec(goSource)) !== null) {
  const raw = match[1];
  const bare = raw.split(',')[0];
  if (bare === '' || bare === '-') continue;
  tags.add(bare);
}

if (tags.size === 0) {
  console.error(`check_share_wire_contract: found zero json tags in ${goFile} — did the file move or the struct change shape?`);
  process.exit(1);
}

const missing = [...tags].filter((tag) => !tsSource.includes(tag));

if (missing.length > 0) {
  console.error('check_share_wire_contract: the following Go json tags are missing from shareTypes.ts:');
  for (const tag of missing) console.error(`  - ${tag}`);
  console.error(`Go source: ${goFile}`);
  console.error(`TS mirror: ${tsFile}`);
  process.exit(1);
}

console.log(`check_share_wire_contract: OK — ${String(tags.size)} Go json tag(s) all present in shareTypes.ts`);
process.exit(0);
