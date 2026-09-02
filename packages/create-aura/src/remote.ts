import { randomUUID } from 'node:crypto';
import { chmod, copyFile, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { isIP } from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import type { TemporaryFile } from './config-file.js';
import {
  assertSufficientCpuCores,
  assertSufficientDiskSpace,
  assertSufficientMemory,
  normalizeArchitecture,
} from './preflight.js';
import type { CommandRunner } from './process.js';
import type { InstallSettings, PreflightResult, RemoteTarget } from './types.js';
import { validateHost, validatePort, validateUsername } from './validation.js';

const REMOTE_ID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

// R4: the same REQUIRED_COMMANDS as local.ts's preflightLocal, and for the same reasons --
// the exact same install.sh runs on this target, whether reached over SSH or run in place.
// See local.ts for the per-command justification (sudo/curl/openssl unconditional; apt-get,
// systemctl, and docker itself deliberately excluded).
// R3: the same REQUIRED_HOSTS -- ghcr.io and get.docker.com kept, raw.githubusercontent.com
// removed (the npm package carries the payload), huggingface.co added (install.sh:357's
// unconditional embedding-model probe).
// R2 (this file's own gap, closed): the reference's own REMOTE_PROBE measured disk alone.
// install.sh's actual hard gate is cpu+mem+disk together (scripts/install.sh:172-187), so
// cpu_cores and memory_kib are measured here too -- a remote target must clear the same three
// floors a local one does, not just one of them. Reading /proc/meminfo directly (no Darwin
// branch, unlike local.ts's RAM_KIB_SCRIPT) is safe because the case above already asserts
// the remote target is Linux.
const REMOTE_PROBE = `set -eu
exec 2>&1
INSTALL_DIR="$(printf '%s' "$1" | base64 --decode)"
case "$INSTALL_DIR" in
  /*) ;;
  *) echo 'invalid install directory' >&2; exit 1 ;;
esac
[ "$INSTALL_DIR" != "/" ] || { echo 'invalid install directory' >&2; exit 1; }
[ "$(uname -s)" = "Linux" ] || { echo 'Linux target required' >&2; exit 1; }
for REQUIRED_COMMAND in sudo curl openssl; do
  command -v "$REQUIRED_COMMAND" >/dev/null 2>&1 || {
    echo "missing command: $REQUIRED_COMMAND" >&2
    exit 1
  }
done
ARCHITECTURE="$(uname -m)"
case "$ARCHITECTURE" in
  aarch64|arm64|x86_64|amd64) ;;
  *) echo "unsupported architecture: $ARCHITECTURE" >&2; exit 1 ;;
esac
EXISTING_INSTALL=false
[ -f "$INSTALL_DIR/compose.yaml" ] && EXISTING_INSTALL=true
CPU_CORES="$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 0)"
case "$CPU_CORES" in
  ''|*[!0-9]*) echo 'unable to determine cpu core count' >&2; exit 1 ;;
esac
MEMORY_KIB="$(awk '/MemTotal:/ {print $2}' /proc/meminfo 2>/dev/null || echo 0)"
case "$MEMORY_KIB" in
  ''|*[!0-9]*) echo 'unable to determine memory availability' >&2; exit 1 ;;
esac
DISK_PROBE="$INSTALL_DIR"
while [ ! -e "$DISK_PROBE" ] && [ "$DISK_PROBE" != "/" ]; do
  DISK_PROBE="$(dirname "$DISK_PROBE")"
done
DISK_AVAILABLE_KB="$(df -Pk "$DISK_PROBE" | awk 'NR == 2 { print $4 }')"
case "$DISK_AVAILABLE_KB" in
  ''|*[!0-9]*) echo 'unable to determine disk availability' >&2; exit 1 ;;
esac
for URL in https://ghcr.io https://get.docker.com https://huggingface.co; do
  curl -fsS -o /dev/null -m 10 "$URL"
done
printf 'architecture=%s\\n' "$ARCHITECTURE"
printf 'existing_install=%s\\n' "$EXISTING_INSTALL"
printf 'cpu_cores=%s\\n' "$CPU_CORES"
printf 'memory_kib=%s\\n' "$MEMORY_KIB"
printf 'disk_available_kb=%s\\n' "$DISK_AVAILABLE_KB"
`;

const STALE_CLEANUP = `set -eu
exec 2>&1
find /tmp -xdev -maxdepth 1 -type f -user "$(id -un)" -mtime +0 \\
  \\( -name 'create-aura-????????-????-????-????-????????????-installer.sh' \\
     -o -name 'create-aura-????????-????-????-????-????????????-install.conf' \\
     -o -name 'create-aura-????????-????-????-????-????????????-run.sh' \\) \\
  -delete
`;

// R6: `--` before `--config`, mirroring local.ts's installLocal -- makeself's runtime header
// parses argv against ITS OWN flag set before it execs the embedded script, so
// `install-appliance.run --config X` dies with "Unrecognized flag" and install.sh never runs.
// Exported (unlike the reference's private constant) so remote.test.ts can assert the fix
// directly on the script's own source: the flag never appears in any ssh/scp argv, only
// inside this transferred file, so an argv-only assertion would not exercise it.
export const REMOTE_INSTALL_SCRIPT = `#!/bin/sh
set -eu
exec 2>&1
INSTALLER="$1"
CONFIG="$2"
RUNNER="$0"
cleanup() { rm -f -- "$CONFIG" "$INSTALLER" "$RUNNER"; }
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
chmod 700 "$INSTALLER"
chmod 600 "$CONFIG"
sudo bash "$INSTALLER" -- --config "$CONFIG"
`;

const CURRENT_CLEANUP = `set -eu
exec 2>&1
rm -f -- "$1" "$2" "$3"
`;

export type WarningSink = (message: string) => void;

interface StagedRemoteFiles extends TemporaryFile {
  installerPath: string;
  configPath: string;
  runnerPath: string;
}

function remoteScriptCommand(script: string, args: readonly string[] = []): string {
  const encodedScript = Buffer.from(script, 'utf8').toString('base64');
  const suffix = args.length === 0 ? '' : ` -- ${args.join(' ')}`;
  return `printf %s ${encodedScript} | base64 --decode | sh -s${suffix}`;
}

async function stageRemoteFiles(
  artifact: TemporaryFile,
  config: TemporaryFile,
  remoteId: string,
  tempRoot: string,
): Promise<StagedRemoteFiles> {
  const directory = await mkdtemp(join(tempRoot, 'create-aura-upload-'));
  const installerPath = join(directory, `create-aura-${remoteId}-installer.sh`);
  const configPath = join(directory, `create-aura-${remoteId}-install.conf`);
  const runnerPath = join(directory, `create-aura-${remoteId}-run.sh`);

  try {
    await chmod(directory, 0o700);
    await copyFile(artifact.path, installerPath);
    await copyFile(config.path, configPath);
    await writeFile(runnerPath, REMOTE_INSTALL_SCRIPT, { mode: 0o700, flag: 'wx' });
    await chmod(installerPath, 0o700);
    await chmod(configPath, 0o600);
    await chmod(runnerPath, 0o700);
  } catch (error) {
    await rm(directory, { recursive: true, force: true });
    throw error;
  }

  return {
    path: directory,
    installerPath,
    configPath,
    runnerPath,
    cleanup: async () => rm(directory, { recursive: true, force: true }),
  };
}

// Ported unchanged (task instructions): validated defensively even though collectTarget
// already validated host/username/port once at collection time.
export function sshDestination(target: RemoteTarget): string {
  const username = validateUsername(target.username);
  const host = validateHost(target.host);
  return `${username}@${isIP(host) === 6 ? `[${host}]` : host}`;
}

function parseProbeOutput(output: string): PreflightResult {
  const values = new Map<string, string>();
  for (const line of output.split(/\r?\n/)) {
    const separator = line.indexOf('=');
    if (separator <= 0) continue;
    values.set(line.slice(0, separator), line.slice(separator + 1));
  }

  const rawArchitecture = values.get('architecture');
  const rawExisting = values.get('existing_install');
  const rawCpuCores = values.get('cpu_cores');
  const rawMemory = values.get('memory_kib');
  const rawDisk = values.get('disk_available_kb');
  if (
    rawArchitecture === undefined
    || !['true', 'false'].includes(rawExisting ?? '')
    || rawCpuCores === undefined
    || rawMemory === undefined
    || rawDisk === undefined
  ) {
    throw new Error('invalidRemotePreflightOutput');
  }

  // R2: the same three hard floors install.sh:172-187 enforces locally, run against the
  // TARGET's own numbers -- not the operator's laptop.
  assertSufficientCpuCores(rawCpuCores);
  assertSufficientMemory(rawMemory);
  assertSufficientDiskSpace(rawDisk);

  return {
    architecture: normalizeArchitecture(rawArchitecture),
    existingInstall: rawExisting === 'true',
  };
}

export async function preflightRemote(
  runner: CommandRunner,
  target: RemoteTarget,
  installDir: string,
  platform: NodeJS.Platform = process.platform,
): Promise<PreflightResult> {
  await runner.run('ssh', ['-V']);
  if (platform === 'win32') {
    await runner.run('where.exe', ['scp.exe']);
  } else if (platform === 'linux') {
    await runner.run('sh', [
      '-c',
      'command -v "$1" >/dev/null 2>&1',
      'create-aura',
      'scp',
    ]);
  } else {
    throw new Error('unsupportedRemoteClientPlatform');
  }

  const result = await runner.run(
    'ssh',
    [
      '-p',
      String(validatePort(String(target.port))),
      sshDestination(target),
      remoteScriptCommand(REMOTE_PROBE, [Buffer.from(installDir, 'utf8').toString('base64')]),
    ],
    { terminal: true },
  );

  return parseProbeOutput(result.stdout);
}

export async function installRemote(
  runner: CommandRunner,
  target: RemoteTarget,
  artifact: TemporaryFile,
  config: TemporaryFile,
  settings: InstallSettings,
  remoteId: string = randomUUID(),
  warn: WarningSink = (message) => process.stderr.write(`${message}\n`),
  tempRoot = tmpdir(),
): Promise<void> {
  if (!REMOTE_ID_PATTERN.test(remoteId)) throw new Error('invalidRemoteId');

  const destination = sshDestination(target);
  const port = String(validatePort(String(target.port)));
  const installerPath = `/tmp/create-aura-${remoteId}-installer.sh`;
  const configPath = `/tmp/create-aura-${remoteId}-install.conf`;
  const runnerPath = `/tmp/create-aura-${remoteId}-run.sh`;
  // R7: the OpenRouter key travels in redactions on the remote path too -- its output crosses
  // an SSH session and often a terminal someone is screen-sharing, so it matters MORE here
  // than on the local path, not less.
  const redactions = [settings.openrouterApiKey];

  await runner.run(
    'ssh',
    ['-p', port, destination, remoteScriptCommand(STALE_CLEANUP)],
    { terminal: true, redactions },
  );

  const staged = await stageRemoteFiles(artifact, config, remoteId, tempRoot);
  let operationFailed = false;
  let operationError: unknown;
  try {
    await runner.run(
      'scp',
      [
        '-P',
        port,
        staged.installerPath,
        staged.configPath,
        staged.runnerPath,
        `${destination}:/tmp/`,
      ],
      { terminal: true, redactions },
    );
    await runner.run(
      'ssh',
      ['-tt', '-p', port, destination, 'sh', runnerPath, installerPath, configPath],
      { terminal: true, redactions },
    );
  } catch (error) {
    operationFailed = true;
    operationError = error;
    try {
      await runner.run(
        'ssh',
        [
          '-p',
          port,
          destination,
          remoteScriptCommand(CURRENT_CLEANUP, [installerPath, configPath, runnerPath]),
        ],
        { terminal: true, redactions },
      );
    } catch {
      warn('remoteCleanupFailed');
    }
  }

  try {
    await staged.cleanup();
  } catch (cleanupError) {
    warn('localStagingCleanupFailed');
    if (!operationFailed) throw cleanupError;
  }

  if (operationFailed) throw operationError;
}

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, "'\\''")}'`;
}

// R2 (Task 6 controller ruling): collectSettings's GPU/Ollama probes (modelroute.ts) take a
// plain CommandRunner and know nothing about SSH -- Task 5's own ruling forbids teaching them
// ("Nothing inside collectSettings or modelroute.ts may learn what an SSH is"). This wraps one
// so its run(command, args) executes ON THE REMOTE TARGET via sshDestination instead of on the
// machine running the wizard. Each argument is single-quoted because ssh joins the trailing
// argv into ONE string for the remote shell to re-parse (it does not preserve argv
// boundaries the way execa's direct spawn does): an operator-typed Ollama base URL reaching
// modelroute.ts's `docker run ... wget ... "$url"` unquoted would let shell metacharacters in
// that URL execute on the target.
export function createSshProbeRunner(runner: CommandRunner, target: RemoteTarget): CommandRunner {
  const destination = sshDestination(target);
  const port = String(validatePort(String(target.port)));

  return {
    async run(command, args = [], options) {
      return runner.run(
        'ssh',
        ['-p', port, destination, ...[command, ...args].map(shellQuote)],
        options,
      );
    },
  };
}
