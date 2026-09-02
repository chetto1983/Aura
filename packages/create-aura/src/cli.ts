import { readFileSync } from 'node:fs';

import type { TemporaryFile } from './config-file.js';
import { createTemporaryInstallConfig } from './config-file.js';
import { createTranslator, detectLocale } from './i18n.js';
import type { MessageKey } from './i18n.js';
import { installLocal, preflightLocal, resolveInstallerArtifact } from './local.js';
import { collectSettings, collectTarget, inquirerPrompt } from './prompts.js';
import type { PromptPort, TargetSelection } from './prompts.js';
import { ProcessRunner } from './process.js';
import type { CommandRunner } from './process.js';
import type { InstallMode, InstallSettings, RemoteTarget } from './types.js';
import { ValidationError } from './validation.js';

type Writer = (message: string) => void;
type TargetCollector = (
  prompt: PromptPort,
  t: ReturnType<typeof createTranslator>,
  requestedMode?: InstallMode,
) => Promise<TargetSelection>;
type SettingsCollector = (
  prompt: PromptPort,
  t: ReturnType<typeof createTranslator>,
  installDir: string,
  probeRunner: CommandRunner | undefined,
) => Promise<InstallSettings | null>;
type ConfigCreator = (settings: InstallSettings) => Promise<TemporaryFile>;
type LocalInstaller = (
  runner: CommandRunner,
  config: TemporaryFile,
  settings: InstallSettings,
) => Promise<void>;
type RemoteInstaller = (
  runner: CommandRunner,
  target: RemoteTarget,
  config: TemporaryFile,
  settings: InstallSettings,
) => Promise<void>;

export interface CliDependencies {
  locale?: string;
  version?: string;
  // Local mode installs onto the machine running this CLI, so preflightLocal's platform gate
  // (Linux or macOS) reads the REAL host by default. Tests targeting this repo's own dev
  // machines (Windows) inject 'linux' to exercise a simulated local target instead.
  platform?: NodeJS.Platform;
  prompt?: PromptPort;
  runner?: CommandRunner;
  collectTarget?: TargetCollector;
  collectSettings?: SettingsCollector;
  createConfig?: ConfigCreator;
  // installLocal defaults to local.ts's real installLocal, resolving the bundled artifact
  // itself (see the localInstaller wiring below). installRemote has no such default yet --
  // remote.ts is not ported until the second half of Task 6 -- so a run that reaches the
  // remote install phase without one injected fails with a clear, named error
  // (remoteInstallNotImplemented) instead of a TypeError. Every functional test in this file
  // injects a fake for whichever path it exercises.
  installLocal?: LocalInstaller;
  installRemote?: RemoteInstaller;
  write?: Writer;
  writeError?: Writer;
}

interface ParsedArguments {
  action: 'install' | 'help' | 'version' | 'invalid';
  mode?: InstallMode;
}

// Exported so cli.test.ts can assert, in one place, that every mapped code resolves to a
// real message in both catalogues and that every preflight.ts error code is mapped (F3,
// review round 1: six of these had neither a map entry nor a catalogue key, so
// errorMessage() fell through to the raw thrown string and an operator on an unsupported
// architecture read "unsupportedArchitecture:riscv64" literally).
export const TRANSLATED_ERROR_CODES: Readonly<Record<string, MessageKey>> = {
  invalidHost: 'invalidHost',
  invalidPort: 'invalidPort',
  invalidUsername: 'invalidUsername',
  invalidInstallDir: 'invalidInstallDir',
  invalidBaseUrl: 'invalidBaseUrl',
  invalidModelId: 'invalidModelId',
  unsupportedArchitecture: 'unsupportedArchitecture',
  invalidDiskAvailability: 'invalidDiskAvailability',
  insufficientDiskSpace: 'insufficientDiskSpace',
  invalidCpuCount: 'invalidCpuCount',
  insufficientCpuCores: 'insufficientCpuCores',
  invalidMemoryAvailability: 'invalidMemoryAvailability',
  insufficientMemory: 'insufficientMemory',
  cleanupFailed: 'cleanupFailed',
  // Task 6: local.ts's preflightLocal now throws these, and installLocal's real default
  // (below) throws the artifact one when Task 7's bundled asset is absent.
  unsupportedLocalPlatform: 'unsupportedLocalPlatform',
  installerArtifactMissing: 'installerArtifactMissing',
};

function parseArguments(argv: readonly string[]): ParsedArguments {
  if (argv.length === 0) return { action: 'install' };
  if (argv.length === 1 && argv[0] === '--help') return { action: 'help' };
  if (argv.length === 1 && argv[0] === '--version') return { action: 'version' };
  if (
    argv.length === 2
    && argv[0] === '--mode'
    && (argv[1] === 'local' || argv[1] === 'remote')
  ) {
    return { action: 'install', mode: argv[1] };
  }
  return { action: 'invalid' };
}

function packageVersion(): string {
  const contents = readFileSync(new URL('../package.json', import.meta.url), 'utf8');
  const parsed: unknown = JSON.parse(contents);
  if (
    typeof parsed !== 'object'
    || parsed === null
    || !('version' in parsed)
    || typeof parsed.version !== 'string'
  ) {
    throw new Error('invalidPackageVersion');
  }
  return parsed.version;
}

function errorMessage(error: unknown, t: ReturnType<typeof createTranslator>): string {
  if (!(error instanceof Error)) return String(error);
  const rawCode = error instanceof ValidationError ? error.code : error.message;
  const code = rawCode.split(':', 1)[0] ?? rawCode;
  const key = TRANSLATED_ERROR_CODES[code];
  return key ? t(key) : error.message;
}

function isPromptCancellation(error: unknown): boolean {
  return error instanceof Error && error.name === 'ExitPromptError';
}

export async function runCli(
  argv: readonly string[],
  dependencies: CliDependencies = {},
): Promise<number> {
  const locale = detectLocale(
    dependencies.locale ?? Intl.DateTimeFormat().resolvedOptions().locale,
  );
  const t = createTranslator(locale);
  const write = dependencies.write ?? ((message) => process.stdout.write(`${message}\n`));
  const writeError = dependencies.writeError ?? ((message) => process.stderr.write(`${message}\n`));
  const parsed = parseArguments(argv);

  if (parsed.action === 'invalid') {
    writeError(t('usageError'));
    return 2;
  }
  if (parsed.action === 'help') {
    write([
      t('cliDescription'),
      '',
      t('helpUsage'),
      t('helpMode'),
      t('helpHelp'),
      t('helpVersion'),
    ].join('\n'));
    return 0;
  }
  if (parsed.action === 'version') {
    write(dependencies.version ?? packageVersion());
    return 0;
  }

  const prompt = dependencies.prompt ?? inquirerPrompt;
  const runner = dependencies.runner ?? new ProcessRunner();
  const targetCollector = dependencies.collectTarget ?? collectTarget;
  const settingsCollector = dependencies.collectSettings ?? collectSettings;
  const configCreator = dependencies.createConfig ?? createTemporaryInstallConfig;
  // R5/R6: the real installer runs the bundled makeself artifact (Task 7 packages it beside
  // this module); resolving it here, once, keeps local.ts/remote.ts's own exported
  // installLocal/installRemote matching the reference's ported (runner, artifact, config,
  // settings) shape while cli.ts's injected defaults keep the 4-/5-arg shape the rest of this
  // file already declares.
  const localInstaller: LocalInstaller = dependencies.installLocal ?? (async (installRunner, installConfig, installSettings) => {
    const artifact = await resolveInstallerArtifact();
    await installLocal(installRunner, artifact, installConfig, installSettings);
  });

  let config: TemporaryFile | undefined;
  let operationError: unknown;
  let installed = false;

  try {
    const target = await targetCollector(prompt, t, parsed.mode);
    write(t('phasePreflight'));

    // R1: the wizard runs on the operator's laptop, but in remote mode the install lands on
    // a different machine. local mode passes the real runner -- the probed machine IS the
    // target. Remote mode passes undefined for now: probing the laptop and presenting the
    // answer as if it were the target's is exactly the bug this seam exists to prevent.
    // Task 6 supplies an SSH-wrapping runner so the remote path probes the target for real;
    // nothing in collectSettings or here needs to change when it does.
    let probeRunner: CommandRunner | undefined;
    if (target.mode === 'remote') {
      if (!target.remote) throw new Error('invalidRemoteTarget');
      write(t('remoteTargetSummary', {
        user: target.remote.username,
        host: target.remote.host,
        port: target.remote.port,
      }));
    } else {
      // R1 (carried Task 5 debt, closed): the hardware/command/host gate that used to live
      // here as a small local-only runLocalPreflight now lives in local.ts's preflightLocal,
      // merged with the REQUIRED_COMMANDS/REQUIRED_HOSTS/existing-install checks Task 6 ports
      // from the reference -- one gate, not two copies of it.
      const preflight = await preflightLocal(runner, target.installDir, dependencies.platform);
      write(t('localTargetSummary'));
      if (preflight.existingInstall) write(t('existingInstall'));
      probeRunner = runner;
    }

    const settings = await settingsCollector(prompt, t, target.installDir, probeRunner);
    if (settings === null) {
      write(t('cancelled'));
      return 0;
    }

    config = await configCreator(settings);

    write(t('phaseInstall'));
    if (target.mode === 'remote') {
      if (!target.remote) throw new Error('invalidRemoteTarget');
      if (!dependencies.installRemote) throw new Error('remoteInstallNotImplemented');
      await dependencies.installRemote(runner, target.remote, config, settings);
    } else {
      await localInstaller(runner, config, settings);
    }
    installed = true;
  } catch (error) {
    if (isPromptCancellation(error) && !config) {
      write(t('cancelled'));
      return 0;
    }
    operationError = error;
  }

  if (config) {
    write(t('phaseCleanup'));
    try {
      await config.cleanup();
    } catch {
      writeError(t('cleanupWarning'));
      operationError ??= new Error('cleanupFailed');
    }
  }

  if (operationError !== undefined) {
    writeError(errorMessage(operationError, t));
    return 1;
  }
  if (installed) write(t('installComplete'));
  return 0;
}
