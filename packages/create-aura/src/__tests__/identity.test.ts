import { mkdtemp, mkdir, rm, writeFile, symlink } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { afterEach, describe, expect, it } from 'vitest';

import { discoverIdentityFiles } from '../identity.js';

const roots: string[] = [];

afterEach(async () => {
  await Promise.all(roots.splice(0).map((root) => rm(root, { recursive: true, force: true })));
});

async function makeHome(files: Record<string, string>): Promise<string> {
  const home = await mkdtemp(join(tmpdir(), 'create-aura-home-'));
  roots.push(home);
  await mkdir(join(home, '.ssh'), { recursive: true });
  for (const [name, contents] of Object.entries(files)) {
    await writeFile(join(home, '.ssh', name), contents);
  }
  return home;
}

describe('discoverIdentityFiles', () => {
  it('offers only private keys whose public half sits beside them', async () => {
    const home = await makeHome({
      aura_appliance: 'private',
      'aura_appliance.pub': 'public',
      // A public key with no private half is not something ssh -i can use.
      orphan: '',
      'stray.pub': 'public',
      known_hosts: 'host key material',
      'known_hosts.old': 'older host key material',
      config: 'Host x',
    });

    expect(discoverIdentityFiles(home)).toEqual([join(home, '.ssh', 'aura_appliance')]);
  });

  it('puts the conventional names first and still lists everything else', async () => {
    const home = await makeHome({
      zeta: 'private', 'zeta.pub': 'public',
      id_rsa: 'private', 'id_rsa.pub': 'public',
      alpha: 'private', 'alpha.pub': 'public',
      id_ed25519: 'private', 'id_ed25519.pub': 'public',
    });

    // Conventional order first, then the rest alphabetically -- a host with several keys
    // usually has one default and some purpose-built ones, and none of them may vanish
    // from the list just because it is unconventional.
    expect(discoverIdentityFiles(home).map((path) => path.split(/[\\/]/).pop())).toEqual([
      'id_ed25519',
      'id_rsa',
      'alpha',
      'zeta',
    ]);
  });

  it('returns nothing rather than throwing when there is no .ssh at all', async () => {
    const home = await mkdtemp(join(tmpdir(), 'create-aura-home-'));
    roots.push(home);
    expect(discoverIdentityFiles(home)).toEqual([]);
  });

  it('ignores a directory that happens to have a .pub beside it', async () => {
    const home = await makeHome({ 'nested.pub': 'public' });
    await mkdir(join(home, '.ssh', 'nested'));
    expect(discoverIdentityFiles(home)).toEqual([]);
  });

  it('survives a broken symlink among the candidates', async () => {
    const home = await makeHome({ 'dangling.pub': 'public', real: 'private', 'real.pub': 'public' });
    try {
      await symlink(join(home, '.ssh', 'gone'), join(home, '.ssh', 'dangling'));
    } catch {
      return; // symlinks need privilege on some Windows setups; the rest of the suite covers the path
    }
    expect(discoverIdentityFiles(home)).toEqual([join(home, '.ssh', 'real')]);
  });
});
