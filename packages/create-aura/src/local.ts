import { access } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

import type { TemporaryFile } from './config-file.js';
import {
  assertSufficientCpuCores,
  assertSufficientDiskSpace,
  assertSufficientMemory,
  normalizeArchitecture,
} from './preflight.js';
import type { CommandRunner } from './process.js';
import type { InstallSettings, PreflightResult } from './types.js';

// R4 (Task 6 controller ruling): derived from scripts/install.sh, not copied from the
// reference's ['apt-get', 'systemctl', 'sudo', 'curl'].
//   sudo    -- installLocal below always runs the artifact as `sudo bash ...`; if the sudo
//              binary itself is missing, that top-level command fails outright, and this
//              check catches it before a secret-bearing config file is even written.
//   curl    -- install.sh's ensure_embed_model (scripts/install.sh:371-395) sends an
//              unconditional HEAD request to size the embedding model on EVERY run, model
//              already present or not; curl is never optional.
//   openssl -- write_env_if_missing and ensure_internal_env_secrets (scripts/install.sh:465,
//              536) each call `command -v openssl` themselves and exit 1 if it is absent, on
//              a fresh install AND on every re-run.
// apt-get and systemctl are dropped: apt-get would refuse the macOS target install.sh:216-226
// explicitly supports via Docker Desktop, and systemctl only gates --appliance
// (scripts/install.sh:691-719), a choice collectSettings makes AFTER this preflight runs --
// gating on it here would refuse a machine a non-appliance install would have accepted.
// docker itself is deliberately NOT required: install_docker (scripts/install.sh:198-236)
// self-installs it via curl+sudo on Linux when absent, so requiring it up front would refuse
// a fresh box the installer would have provisioned on its own.
const REQUIRED_COMMANDS = ['sudo', 'curl', 'openssl'] as const;

// R3 (Task 6 controller ruling): raw.githubusercontent.com comes OUT -- the npm package
// carries the payload (spec decision 5), so install.sh's download_file reads
// AURA_PAYLOAD_DIR and never touches RAW_BASE; requiring that host would re-assert a trust
// path this design deliberately removed. huggingface.co goes IN: install.sh:357 fetches
// embeddinggemma-300M-Q8_0.gguf from there, the one pinned artifact in the whole product,
// and ensure_embed_model's HEAD probe against it is unconditional (see curl, above).
const REQUIRED_HOSTS = [
  'https://ghcr.io',
  'https://get.docker.com',
  'https://huggingface.co',
] as const;

// install.sh:126-135's ram_kib(): Linux reads /proc/meminfo's MemTotal (already KiB) directly;
// Darwin has no /proc, so it converts `sysctl -n hw.memsize` (bytes) to KiB; any other
// platform echoes 0, which then FAILS assertSufficientMemory's floor rather than skipping the
// gate -- install.sh refuses a machine it cannot measure, and this must too.
const RAM_KIB_SCRIPT = 'OS="$(uname -s)"; if [ "$OS" = "Linux" ] && [ -r /proc/meminfo ]; then '
  + "awk '/MemTotal:/ {print $2}' /proc/meminfo; elif [ \"$OS\" = \"Darwin\" ]; then "
  + 'bytes="$(sysctl -n hw.memsize 2>/dev/null || echo 0)"; echo $((bytes / 1024)); else echo 0; fi';

// install.sh:137-144's disk_free_kib(): the install directory does not exist yet on a first
// install, so it walks up to the nearest existing parent before measuring -- df on '/' when
// the real target is a separately-mounted /opt (the appliance default install dir is
// /opt/aura) would report free space on the wrong filesystem. $1 is the install dir, passed
// positionally: a throwaway program-name placeholder occupies $0.
const DISK_FREE_KIB_SCRIPT = 'probe="$1"; while [ ! -e "$probe" ] && [ "$probe" != "/" ]; do '
  + 'probe="$(dirname "$probe")"; done; df -Pk "$probe" | awk \'NR==2 {print $4}\'';

export async function preflightLocal(
  runner: CommandRunner,
  installDir: string,
  platform: NodeJS.Platform = process.platform,
): Promise<PreflightResult> {
  // install.sh:81-88, :216-230 supports Linux and macOS (via Docker Desktop); anything else
  // has no install.sh path to run at all and belongs in remote mode instead.
  if (platform !== 'linux' && platform !== 'darwin') throw new Error('unsupportedLocalPlatform');

  for (const command of REQUIRED_COMMANDS) {
    await runner.run('sh', [
      '-c',
      'command -v "$1" >/dev/null 2>&1',
      'create-aura',
      command,
    ]);
  }

  const architecture = normalizeArchitecture((await runner.run('uname', ['-m'])).stdout);
  // install.sh:118-124's cpu_count() branches on OS only because getconf might be absent;
  // it ships on every Linux distribution and on macOS, so this one call already covers both
  // platforms without branching -- `nproc` would be Linux-only and refuse a Mac install.sh
  // itself accepts.
  assertSufficientCpuCores((await runner.run('getconf', ['_NPROCESSORS_ONLN'])).stdout);
  assertSufficientMemory((await runner.run('sh', ['-c', RAM_KIB_SCRIPT])).stdout);
  assertSufficientDiskSpace((await runner.run('sh', [
    '-c',
    DISK_FREE_KIB_SCRIPT,
    'create-aura',
    installDir,
  ])).stdout);

  // install.sh writes compose.yaml (not docker-compose.yml) directly under INSTALL_DIR
  // (scripts/install.sh:767); that is Aura's own re-run marker, not the reference's.
  const existingInstall = (await runner.run('sh', [
    '-c',
    'if test -f "$1/compose.yaml"; then printf "true\\n"; else printf "false\\n"; fi',
    'create-aura',
    installDir,
  ])).stdout.trim() === 'true';

  for (const host of REQUIRED_HOSTS) {
    await runner.run('curl', ['-fsS', '-o', '/dev/null', '-m', '10', host]);
  }

  return { architecture, existingInstall };
}

// R5 (Task 6 controller ruling): Task 7 packages packages/create-aura/install-appliance.run
// beside this module. Resolved via import.meta.url rather than a hardcoded relative path so
// it is correct BOTH from src/ under vitest and from dist/ when published -- both sit one
// level under the package root. Until Task 7 lands the file is genuinely absent here; this
// must fail with a clear, named error rather than let a missing artifact become a skipped
// step or (worse) a silent no-op install.
export async function resolveInstallerArtifact(): Promise<TemporaryFile> {
  const path = fileURLToPath(new URL('../install-appliance.run', import.meta.url));
  try {
    await access(path);
  } catch {
    throw new Error('installerArtifactMissing');
  }
  return { path, cleanup: async () => {} };
}

export async function installLocal(
  runner: CommandRunner,
  artifact: TemporaryFile,
  config: TemporaryFile,
  settings: InstallSettings,
): Promise<void> {
  // R6 (Task 6 controller ruling): makeself's runtime header parses argv against its OWN
  // flag set before it execs the embedded script, so `artifact --config X` dies with
  // "Unrecognized flag" and install.sh never runs. `--` tells the header to stop parsing and
  // hand everything after it to install.sh untouched.
  await runner.run(
    'sudo',
    ['bash', artifact.path, '--', '--config', config.path],
    { redactions: [settings.openrouterApiKey] },
  );
}
