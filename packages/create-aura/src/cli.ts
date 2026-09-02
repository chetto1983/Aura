import { readFileSync } from 'node:fs';

import type { TemporaryFile } from './config-file.js';
import { createTemporaryInstallConfig } from './config-file.js';
import { createTranslator, detectLocale } from './i18n.js';
import type { MessageKey } from './i18n.js';
import {
  assertSufficientCpuCores,
  assertSufficientDiskSpace,
  assertSufficientMemory,
  normalizeArchitecture,
} from './preflight.js';
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
  prompt?: PromptPort;
  runner?: CommandRunner;
  collectTarget?: TargetCollector;
  collectSettings?: SettingsCollector;
  createConfig?: ConfigCreator;
  // local.ts / remote.ts (Task 6) are not ported yet, and the artifact they would invoke
  // does not exist until Task 7 packages it. Until then these have no real default: a run
  // that reaches the install phase without one injected fails with a clear, named error
  // instead of a TypeError -- see localInstallNotImplemented/remoteInstallNotImplemented
  // below. Every functional test in this file injects a fake.
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

// install.sh:126-135's ram_kib(): Linux reads /proc/meminfo's MemTotal (already KiB) directly;
// Darwin has no /proc, so it converts `sysctl -n hw.memsize` (bytes) to KiB; any other
// platform echoes 0, which then FAILS assertSufficientMemory's floor rather than skipping the
// gate -- install.sh refuses a machine it cannot measure, and this must too (F6, review round
// 2). Mirrored as one sh -c script, the same shape the disk check below already uses, so the
// wizard and the installer read the same number on the same machine and cannot disagree.
const RAM_KIB_SCRIPT = 'OS="$(uname -s)"; if [ "$OS" = "Linux" ] && [ -r /proc/meminfo ]; then '
  + "awk '/MemTotal:/ {print $2}' /proc/meminfo; elif [ \"$OS\" = \"Darwin\" ]; then "
  + 'bytes="$(sysctl -n hw.memsize 2>/dev/null || echo 0)"; echo $((bytes / 1024)); else echo 0; fi';

// install.sh:137-144's disk_free_kib(): the install directory does not exist yet on a first
// install, so it walks up to the nearest existing parent before measuring -- df on '/' when
// the real target is a separately-mounted /opt (the appliance default install dir is
// /opt/aura) reports free space on the wrong filesystem (F7, review round 2). $1 is the
// install dir, passed positionally the same way the reference passes an argument to a sh -c
// script: a throwaway program-name placeholder occupies $0, the real value is $1.
const DISK_FREE_KIB_SCRIPT = 'probe="$1"; while [ ! -e "$probe" ] && [ "$probe" != "/" ]; do '
  + 'probe="$(dirname "$probe")"; done; df -Pk "$probe" | awk \'NR==2 {print $4}\'';

// Ported out of wpt-iot's preflightLocal ahead of local.ts existing (Task 6 ports the rest
// of that file -- REQUIRED_COMMANDS/REQUIRED_HOSTS checks and the existing-install probe).
// These four gates are the ones install.sh itself enforces as hard failures (its
// architecture switch, and the cpu/mem/disk floors at scripts/install.sh:155-158), so
// failing here fails on the wizard's own machine before a local install even starts, instead
// of after the artifact has already run -- or, in remote mode, after it has crossed an scp
// (F2, review round 1: assertSufficientCpuCores/assertSufficientMemory existed since Task 2
// but had no caller anywhere, leaving two of the three hard gates unenforced). Local mode
// only: the runner always targets the CURRENT machine, which in local mode IS the install
// target: these checks do not need SSH the way GPU/Ollama probing (R1) or a remote
// existing-install check would.
async function runLocalPreflight(runner: CommandRunner, installDir: string): Promise<void> {
  normalizeArchitecture((await runner.run('uname', ['-m'])).stdout);
  // install.sh:118-124's cpu_count() branches on OS only because getconf might be absent;
  // it ships on every Linux distribution and on macOS, so this one call already covers both
  // platforms without branching -- `nproc` (F2's original fix) is Linux-only and made the
  // wizard refuse a Mac install.sh itself accepts (F6, review round 2).
  assertSufficientCpuCores((await runner.run('getconf', ['_NPROCESSORS_ONLN'])).stdout);
  assertSufficientMemory((await runner.run('sh', ['-c', RAM_KIB_SCRIPT])).stdout);
  assertSufficientDiskSpace((await runner.run('sh', [
    '-c',
    DISK_FREE_KIB_SCRIPT,
    'create-aura',
    installDir,
  ])).stdout);
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
      await runLocalPreflight(runner, target.installDir);
      write(t('localTargetSummary'));
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
      if (!dependencies.installLocal) throw new Error('localInstallNotImplemented');
      await dependencies.installLocal(runner, config, settings);
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
