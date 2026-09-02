import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { basename, join } from 'node:path';
import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  REMOTE_INSTALL_SCRIPT,
  createSshProbeRunner,
  installRemote,
  preflightRemote,
  sshDestination,
} from '../remote.js';

const success = { stdout: '', stderr: '', exitCode: 0 };
const target = { host: '192.168.1.40', port: 22, username: 'ubuntu' };
const remoteId = '123e4567-e89b-42d3-a456-426614174000';
const roots: string[] = [];

const settings = {
  installDir: '/opt/aura',
  appliance: true,
  gvisor: false,
  llmProvider: 'openrouter',
  llmBaseUrl: 'https://openrouter.ai/api/v1',
  llmModel: 'vendor/model',
  openrouterApiKey: 'correct horse battery',
  embedImage: 'ghcr.io/ggml-org/llama.cpp:server',
  embedNgl: '0',
};

afterEach(async () => {
  await Promise.all(roots.splice(0).map((root) => rm(root, { recursive: true, force: true })));
});

async function makeTransferFiles(): Promise<{
  artifactPath: string;
  configPath: string;
  stagingRoot: string;
}> {
  const root = await mkdtemp(join(tmpdir(), 'create-aura-remote-test-'));
  roots.push(root);
  const artifactPath = join(root, 'install-appliance.run');
  const configPath = join(root, 'install.conf');
  const stagingRoot = join(root, 'staging');
  await mkdir(stagingRoot);
  await writeFile(artifactPath, '#!/bin/sh\n');
  await writeFile(configPath, 'format=1\n');
  return { artifactPath, configPath, stagingRoot };
}

describe('remote installer', () => {
  it('formats IPv4, hostname, and IPv6 destinations safely', () => {
    expect(sshDestination(target)).toBe('ubuntu@192.168.1.40');
    expect(sshDestination({ ...target, host: 'ubuntu-minipc.local' })).toBe('ubuntu@ubuntu-minipc.local');
    expect(sshDestination({ ...target, host: '2001:db8::1' })).toBe('ubuntu@[2001:db8::1]');
  });

  it('keeps native SSH input attached while probing the target for architecture, cpu, memory, and disk', async () => {
    const probeOutput = [
      'architecture=aarch64',
      'existing_install=true',
      'cpu_cores=8',
      'memory_kib=41943040',
      'disk_available_kb=41943040',
      '',
    ].join('\n');
    const runner = {
      run: vi.fn()
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce({ ...success, stdout: probeOutput }),
    };

    await expect(preflightRemote(runner, target, '/opt/aura', 'win32')).resolves.toEqual({
      architecture: 'arm64',
      existingInstall: true,
    });

    expect(runner.run).toHaveBeenCalledWith('ssh', ['-V']);
    expect(runner.run).toHaveBeenCalledWith('where.exe', ['scp.exe']);
    expect(runner.run).toHaveBeenLastCalledWith(
      'ssh',
      ['-p', '22', 'ubuntu@192.168.1.40', expect.stringMatching(/^printf %s [A-Za-z0-9+/=]+ \| base64 --decode \| sh -s -- L29wdC9hdXJh$/)],
      { terminal: true },
    );
    const remoteCommand = runner.run.mock.calls[2]?.[1]?.[3] as string;
    const encodedProbe = remoteCommand.split(' ')[2] ?? '';
    const probeScript = Buffer.from(encodedProbe, 'base64').toString('utf8');
    // R4: sudo/curl/openssl, not the reference's apt-get/systemctl/sudo/curl -- see local.ts.
    expect(probeScript).toContain('for REQUIRED_COMMAND in sudo curl openssl');
    // Aura's own re-run marker (scripts/install.sh:767), not the reference's docker-compose.yml.
    expect(probeScript).toContain('compose.yaml');
    // R2: install.sh's actual hard gate is cpu+mem+disk, not disk alone.
    expect(probeScript).toContain('_NPROCESSORS_ONLN');
    expect(probeScript).toContain('MemTotal');
    // R3: ghcr.io/get.docker.com kept, huggingface.co added, raw.githubusercontent.com removed.
    expect(probeScript).toContain('https://ghcr.io');
    expect(probeScript).toContain('https://get.docker.com');
    expect(probeScript).toContain('https://huggingface.co');
    expect(probeScript).not.toContain('raw.githubusercontent.com');
    // Aura has no per-device serial the way the reference's /etc/wpt/serial probe does.
    expect(probeScript).not.toContain('serial');
    expect(probeScript).not.toContain('/opt/aura');
  });

  it('rejects insufficient remote disk before installation', async () => {
    const runner = {
      run: vi.fn()
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce({ ...success, stdout: '/usr/bin/scp\n' })
        .mockResolvedValueOnce({
          ...success,
          stdout: 'architecture=x86_64\nexisting_install=false\ncpu_cores=8\nmemory_kib=41943040\ndisk_available_kb=1000\n',
        }),
    };

    await expect(preflightRemote(runner, target, '/opt/aura', 'linux'))
      .rejects.toThrow('insufficientDiskSpace:1000');
  });

  // R2 gap closed: the reference's own remote probe only ever measured disk. install.sh's
  // hard gate is cpu+mem+disk together (scripts/install.sh:172-187) -- a remote target with
  // enough disk but too few cores must still fail before a byte crosses scp.
  it('rejects insufficient remote CPU cores before installation', async () => {
    const runner = {
      run: vi.fn()
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce({ ...success, stdout: '/usr/bin/scp\n' })
        .mockResolvedValueOnce({
          ...success,
          stdout: 'architecture=x86_64\nexisting_install=false\ncpu_cores=2\nmemory_kib=41943040\ndisk_available_kb=41943040\n',
        }),
    };

    await expect(preflightRemote(runner, target, '/opt/aura', 'linux'))
      .rejects.toThrow('insufficientCpuCores:2');
  });

  it('rejects insufficient remote memory before installation', async () => {
    const runner = {
      run: vi.fn()
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce({ ...success, stdout: '/usr/bin/scp\n' })
        .mockResolvedValueOnce({
          ...success,
          stdout: 'architecture=x86_64\nexisting_install=false\ncpu_cores=8\nmemory_kib=1024\ndisk_available_kb=41943040\n',
        }),
    };

    await expect(preflightRemote(runner, target, '/opt/aura', 'linux'))
      .rejects.toThrow('insufficientMemory:1024');
  });

  it('rejects a remote client platform this wizard cannot run scp checks from', async () => {
    const runner = { run: vi.fn().mockResolvedValue(success) };

    await expect(preflightRemote(runner, target, '/opt/aura', 'darwin'))
      .rejects.toThrow('unsupportedRemoteClientPlatform');
  });

  // R6: makeself's runtime header parses argv against its own flags before it execs the
  // embedded script, so `install-appliance.run --config X` dies with "Unrecognized flag" and
  // install.sh never runs -- the wrapper the remote target actually executes must insert `--`
  // before `--config`. This is invisible in a review of runner argv (the flag lives inside
  // the transferred script, not in any ssh/scp call), so it is asserted on the script's own
  // source rather than smuggled into an argv check that would not exercise it.
  it('inserts -- before --config in the remote install wrapper script', () => {
    const separatorIndex = REMOTE_INSTALL_SCRIPT.indexOf(' -- ');
    const configIndex = REMOTE_INSTALL_SCRIPT.indexOf('--config');

    expect(separatorIndex).toBeGreaterThan(-1);
    expect(configIndex).toBeGreaterThan(-1);
    expect(separatorIndex).toBeLessThan(configIndex);
    expect(REMOTE_INSTALL_SCRIPT).toContain('sudo bash "$INSTALLER" -- --config "$CONFIG"');
  });

  it('transfers a wrapper so SSH and remote sudo keep interactive terminal input', async () => {
    const runner = { run: vi.fn().mockResolvedValue(success) };
    const files = await makeTransferFiles();

    await installRemote(
      runner,
      target,
      { path: files.artifactPath, cleanup: vi.fn() },
      { path: files.configPath, cleanup: vi.fn() },
      settings,
      remoteId,
      undefined,
      files.stagingRoot,
    );

    const installerPath = `/tmp/create-aura-${remoteId}-installer.sh`;
    const configPath = `/tmp/create-aura-${remoteId}-install.conf`;
    const wrapperPath = `/tmp/create-aura-${remoteId}-run.sh`;
    expect(runner.run).toHaveBeenCalledTimes(3);
    expect(runner.run.mock.calls[0]?.[0]).toBe('ssh');
    // R7: the OpenRouter key travels in redactions, not the reference's adminPassword --
    // this crosses an SSH session and often a terminal someone is screen-sharing.
    expect(runner.run.mock.calls[0]?.[2]).toEqual({ terminal: true, redactions: [settings.openrouterApiKey] });
    const scpCall = runner.run.mock.calls[1];
    expect(scpCall?.[0]).toBe('scp');
    expect(scpCall?.[1]?.slice(0, 2)).toEqual(['-P', '22']);
    expect(basename(scpCall?.[1]?.[2] as string)).toBe(`create-aura-${remoteId}-installer.sh`);
    expect(basename(scpCall?.[1]?.[3] as string)).toBe(`create-aura-${remoteId}-install.conf`);
    expect(basename(scpCall?.[1]?.[4] as string)).toBe(`create-aura-${remoteId}-run.sh`);
    expect(scpCall?.[1]?.[5]).toBe('ubuntu@192.168.1.40:/tmp/');
    expect(scpCall?.[2]).toEqual({ terminal: true, redactions: [settings.openrouterApiKey] });
    expect(runner.run).toHaveBeenLastCalledWith(
      'ssh',
      ['-tt', '-p', '22', 'ubuntu@192.168.1.40', 'sh', wrapperPath, installerPath, configPath],
      { terminal: true, redactions: [settings.openrouterApiKey] },
    );

    // The key legitimately travels in the `redactions` OPTION (ProcessRunner.execute reads
    // it to scrub live stdout/stderr) -- it must never additionally appear in the argv the
    // reference test itself checked this way, comparing only [command, args] pairs.
    const argvOnly = runner.run.mock.calls.map((call) => [call[0], call[1]]);
    expect(JSON.stringify(argvOnly)).not.toContain(settings.openrouterApiKey);
    const cleanupCommand = runner.run.mock.calls[0]?.[1]?.[3] as string;
    const encodedCleanup = cleanupCommand.split(' ')[2] ?? '';
    const cleanupScript = Buffer.from(encodedCleanup, 'base64').toString('utf8');
    expect(cleanupScript).toContain('-mtime +0');
    expect(cleanupScript).toContain('-user "$(id -un)"');
    expect(cleanupScript).toContain('create-aura-');
    expect(cleanupScript).toContain('-run.sh');
  });

  it('rejects a malformed remote id before running commands', async () => {
    const runner = { run: vi.fn() };

    await expect(installRemote(
      runner,
      target,
      { path: '/tmp/installer', cleanup: vi.fn() },
      { path: '/tmp/config', cleanup: vi.fn() },
      { installDir: '/opt/aura', appliance: true, gvisor: false, llmProvider: 'ollama', llmBaseUrl: 'http://x', llmModel: 'm', embedImage: 'i', embedNgl: '0' },
      '../../unsafe',
    )).rejects.toThrow('invalidRemoteId');
    expect(runner.run).not.toHaveBeenCalled();
  });

  it('a failed scp aborts before the install ever runs, and its exit code propagates', async () => {
    const scpError = new Error('scp: connection refused');
    const files = await makeTransferFiles();
    const runner = {
      run: vi.fn()
        .mockResolvedValueOnce(success) // stale cleanup
        .mockRejectedValueOnce(scpError) // scp itself fails
        .mockResolvedValueOnce(success), // best-effort remote cleanup
    };

    await expect(installRemote(
      runner,
      target,
      { path: files.artifactPath, cleanup: vi.fn() },
      { path: files.configPath, cleanup: vi.fn() },
      settings,
      remoteId,
      undefined,
      files.stagingRoot,
    )).rejects.toBe(scpError);

    // Only stale cleanup + the failed scp + the best-effort remote cleanup ran -- the
    // install ssh call (`sh runnerPath installerPath configPath`) must never fire.
    expect(runner.run).toHaveBeenCalledTimes(3);
    expect(runner.run.mock.calls[1]?.[0]).toBe('scp');
    expect(runner.run.mock.calls.some((call) => call[1]?.includes('run.sh') && call[0] === 'ssh' && call[1]?.[0] === '-tt')).toBe(false);
  });

  it('preserves the installation error when best-effort cleanup also fails', async () => {
    const installError = new Error('install failed');
    const warnings: string[] = [];
    const files = await makeTransferFiles();
    const runner = {
      run: vi.fn()
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        .mockRejectedValueOnce(installError)
        .mockRejectedValueOnce(new Error('cleanup failed')),
    };

    await expect(installRemote(
      runner,
      target,
      { path: files.artifactPath, cleanup: vi.fn() },
      { path: files.configPath, cleanup: vi.fn() },
      settings,
      remoteId,
      (message) => warnings.push(message),
      files.stagingRoot,
    )).rejects.toBe(installError);
    expect(warnings).toEqual(['remoteCleanupFailed']);
  });
});

describe('createSshProbeRunner', () => {
  // R2 (Task 6 controller ruling): the trap this guards is an implementation that probes the
  // OPERATOR'S OWN LAPTOP instead of the target -- exactly the class of bug
  // modelroute.test.ts's host-only reachability test was built to catch. A naive assertion
  // that 'docker' and 'run' merely appear SOMEWHERE in the recorded call would pass for that
  // wrong implementation too; the command name itself must be 'ssh', with the probed command
  // appearing later in argv.
  it('wraps the probe command in ssh against the target instead of running it on the laptop', async () => {
    const runner = { run: vi.fn().mockResolvedValue(success) };
    const sshRunner = createSshProbeRunner(runner, target);

    await sshRunner.run('docker', ['run', '--rm', 'alpine', 'wget', '-qO-', 'http://host.docker.internal:11434/api/tags']);

    expect(runner.run).toHaveBeenCalledTimes(1);
    const [command, args] = runner.run.mock.calls[0] as [string, string[], unknown];
    expect(command).toBe('ssh');
    expect(args[0]).toBe('-p');
    expect(args[1]).toBe('22');
    expect(args[2]).toBe('ubuntu@192.168.1.40');
    const remoteCommand = args.slice(3).join(' ');
    expect(remoteCommand.indexOf('docker')).toBeGreaterThan(-1);
    expect(remoteCommand.indexOf('run')).toBeGreaterThan(remoteCommand.indexOf('docker'));
  });

  it('wraps a zero-argument probe command (nvidia-smi) the same way', async () => {
    const runner = { run: vi.fn().mockResolvedValue(success) };
    const sshRunner = createSshProbeRunner(runner, target);

    await sshRunner.run('nvidia-smi');

    expect(runner.run.mock.calls[0]?.[0]).toBe('ssh');
    const args = runner.run.mock.calls[0]?.[1] as string[];
    expect(args.slice(3).join(' ')).toContain('nvidia-smi');
  });

  it('shell-quotes each argument so an operator-typed value cannot break out of the remote command', async () => {
    const runner = { run: vi.fn().mockResolvedValue(success) };
    const sshRunner = createSshProbeRunner(runner, target);

    await sshRunner.run('docker', ['run', 'http://host;rm -rf /']);

    const args = runner.run.mock.calls[0]?.[1] as string[];
    expect(args).toContain("'http://host;rm -rf /'");
  });

  it('forwards run options (e.g. redactions) to the underlying local runner', async () => {
    const runner = { run: vi.fn().mockResolvedValue(success) };
    const sshRunner = createSshProbeRunner(runner, target);

    await sshRunner.run('nvidia-smi', [], { redactions: ['secret'] });

    expect(runner.run.mock.calls[0]?.[2]).toEqual({ redactions: ['secret'] });
  });
});
