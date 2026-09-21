import { readdirSync, statSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';

// An empty key answer is legal but it is the expensive one: ssh then authenticates every
// one of an install's six connections on its own, and the operator types the password six
// times. Offering the keys that already exist is what makes the cheap path the default
// rather than something to opt into -- the first version of this prompt defaulted to empty
// and pressing Enter walked straight back into the passwords.
//
// A private key is recognised by its PUBLIC half sitting beside it. That is the only
// signal available without reading the file, and reading candidate private keys to sniff
// them would be both slower and a worse thing for an installer to do.
const NOT_A_KEY = new Set(['known_hosts', 'known_hosts.old', 'config', 'authorized_keys', 'environment']);

// Conventional names first, because a host that holds several keys usually means one
// default and some purpose-built ones; the rest follow in directory order so nothing that
// exists is hidden from the operator.
const CONVENTIONAL = ['id_ed25519', 'id_ecdsa', 'id_rsa'];

export function discoverIdentityFiles(home: string = homedir()): string[] {
  const sshDir = join(home, '.ssh');
  let entries: string[];
  try {
    entries = readdirSync(sshDir);
  } catch {
    return [];
  }

  const published = new Set(entries.filter((name) => name.endsWith('.pub')));
  const candidates = entries.filter((name) => {
    if (name.endsWith('.pub') || NOT_A_KEY.has(name)) return false;
    if (!published.has(`${name}.pub`)) return false;
    try {
      return statSync(join(sshDir, name)).isFile();
    } catch {
      return false;
    }
  });

  candidates.sort((left, right) => {
    const rank = (name: string) => {
      const index = CONVENTIONAL.indexOf(name);
      return index === -1 ? CONVENTIONAL.length : index;
    };
    return rank(left) - rank(right) || left.localeCompare(right);
  });

  return candidates.map((name) => join(sshDir, name));
}
