import { confirm, input, select } from '@inquirer/prompts';

import { discoverIdentityFiles } from './identity.js';
import type { Translator } from './i18n.js';
import type { InstallMode, InstallSettings, RemoteTarget } from './types.js';
import {
  validateHost,
  validateInstallDir,
  validateIdentityFile,
  validatePort,
  validateUsername,
} from './validation.js';

export interface SelectOptions {
  message: string;
  choices: readonly { name: string; value: string }[];
}

export interface InputOptions {
  message: string;
  default?: string;
}

export interface ConfirmOptions {
  message: string;
  default: boolean;
}

export interface PromptPort {
  select(options: SelectOptions): Promise<string>;
  input(options: InputOptions): Promise<string>;
  confirm(options: ConfirmOptions): Promise<boolean>;
}

export const inquirerPrompt: PromptPort = {
  select: (options) => select({ message: options.message, choices: [...options.choices] }),
  input: (options) => input(options),
  confirm: (options) => confirm(options),
};

export interface TargetSelection {
  mode: InstallMode;
  installDir: string;
  remote?: RemoteTarget;
}

// The key question, asked as a CHOICE when there is something to choose from. Asked as a
// free path only when the machine holds no key at all, because an empty text field is the
// one shape that quietly sends the operator back to six password prompts -- which is what
// the first version of this did.
const OTHER_PATH = '::type-another-path::';

async function askIdentityFile(prompt: PromptPort, t: Translator): Promise<string | undefined> {
  const discovered = discoverIdentityFiles();
  if (discovered.length === 0) {
    return validateIdentityFile(await prompt.input({ message: t('remoteIdentityFile'), default: '' }));
  }

  const chosen = await prompt.select({
    message: t('remoteIdentityChoose'),
    choices: [
      ...discovered.map((path) => ({ name: path, value: path })),
      { name: t('remoteIdentityOther'), value: OTHER_PATH },
      { name: t('remoteIdentityNone'), value: '' },
    ],
  });

  if (chosen !== OTHER_PATH) return validateIdentityFile(chosen);
  return validateIdentityFile(await prompt.input({ message: t('remoteIdentityFile'), default: '' }));
}

export async function collectTarget(
  prompt: PromptPort,
  t: Translator,
  requestedMode?: InstallMode,
): Promise<TargetSelection> {
  const mode = requestedMode ?? await prompt.select({
    message: t('modeQuestion'),
    choices: [
      { name: t('modeLocal'), value: 'local' },
      { name: t('modeRemote'), value: 'remote' },
    ],
  }) as InstallMode;

  let remote: RemoteTarget | undefined;
  if (mode === 'remote') {
    remote = {
      host: validateHost(await prompt.input({ message: t('remoteHost') })),
      port: validatePort(await prompt.input({ message: t('remotePort'), default: '22' })),
      // Aura's remote target is a clean Ubuntu Server mini-PC, not a Raspberry Pi -- 'ubuntu'
      // is that image's standard default account, replacing the reference's 'pi'.
      username: validateUsername(await prompt.input({ message: t('remoteUsername'), default: 'ubuntu' })),
      identityFile: await askIdentityFile(prompt, t),
    };
  }

  const installDir = validateInstallDir(await prompt.input({
    message: t('installDir'),
    // scripts/install.sh:86 -- /opt/aura is the appliance's own default installation path.
    default: '/opt/aura',
  }));

  return remote ? { mode, installDir, remote } : { mode, installDir };
}

// The installer asks for infrastructure only. The model route, the model and the OpenRouter
// management key are chosen by an admin in the first-run web setup (management-key design,
// decision 3), so nothing typed here is a credential.
export async function collectSettings(
  prompt: PromptPort,
  t: Translator,
  installDir: string,
): Promise<InstallSettings | null> {
  const appliance = await prompt.confirm({ message: t('applianceQuestion'), default: true });
  const gvisor = await prompt.confirm({ message: t('gvisorQuestion'), default: false });

  const confirmed = await prompt.confirm({ message: t('confirmInstall'), default: true });
  if (!confirmed) return null;

  return { installDir, appliance, gvisor };
}
