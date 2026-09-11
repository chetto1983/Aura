import { chmod, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import type { InstallSettings } from './types.js';
import { assertNoLineBreak } from './validation.js';

export interface TemporaryFile {
  path: string;
  cleanup: () => Promise<void>;
}

export interface TemporaryInstallConfig extends TemporaryFile {
  path: string;
  directory: string;
}

// install.sh's parse_install_config decodes every *_base64 value and only then re-checks
// it for a line break (parse_install_config in scripts/install.sh), because a newline that survived would
// make set_env_value write a second .env line -- install.sh's own reader takes the first
// occurrence, docker compose takes the last, so the installer and the running appliance
// would end up trusting different values. Asserting here catches the same fault on the
// machine running the wizard, before the config file crosses to the install target.
function encode(value: string): string {
  assertNoLineBreak(value, 'invalidConfigValue');
  return Buffer.from(value, 'utf8').toString('base64');
}

export function serializeInstallConfig(settings: InstallSettings): string {
  return [
    // Format 2 carries infrastructure only: the model route and the OpenRouter key are chosen
    // in the first-run web setup, so no credential crosses to the install target.
    'format=2',
    `install_dir_base64=${encode(settings.installDir)}`,
    // appliance and gvisor are the two install.sh reads RAW; base64-ing them would make
    // its literal `= "true"` comparison false and silently produce a non-appliance install.
    `appliance=${settings.appliance ? 'true' : 'false'}`,
    `gvisor=${settings.gvisor ? 'true' : 'false'}`,
    '',
  ].join('\n');
}

export async function createTemporaryInstallConfig(
  settings: InstallSettings,
  tempRoot = tmpdir(),
): Promise<TemporaryInstallConfig> {
  const directory = await mkdtemp(join(tempRoot, 'create-aura-'));
  const path = join(directory, 'install.conf');

  try {
    await chmod(directory, 0o700);
    await writeFile(path, serializeInstallConfig(settings), { encoding: 'utf8', mode: 0o600 });
    await chmod(path, 0o600);
  } catch (error) {
    await rm(directory, { recursive: true, force: true });
    throw error;
  }

  return {
    path,
    directory,
    cleanup: async () => rm(directory, { recursive: true, force: true }),
  };
}
