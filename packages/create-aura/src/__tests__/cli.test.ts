import { describe, expect, it, vi } from 'vitest';

import { en } from '../messages/en.js';
import { runCli } from '../cli.js';
import { createFakeRunner, createPassingPreflightRunner, validSettings } from './cli-test-support.js';

// Local-preflight-gate scenarios (architecture/cpu/memory/disk/platform/existingInstall,
// plus the TRANSLATED_ERROR_CODES completeness checks) live in
// cli_local_preflight.test.ts -- split out when this file crossed the 600-LOC cap.
describe('create-aura CLI', () => {
  it('runs a local hardware preflight, probes with the real runner, installs, and cleans up', async () => {
    const events: string[] = [];
    const configCleanup = vi.fn(async () => { events.push('config-cleanup'); });
    const write = vi.fn();
    const runner = createFakeRunner(async (command) => {
      if (command === 'uname') return { stdout: 'aarch64\n', stderr: '', exitCode: 0 };
      if (command === 'getconf') return { stdout: '8\n', stderr: '', exitCode: 0 };
      if (command === 'curl') return { stdout: '', stderr: '', exitCode: 0 };
      if (command === 'sh') return { stdout: '41943040\n', stderr: '', exitCode: 0 };
      throw new Error(`unexpected command ${command}`);
    });
    const collectSettingsMock = vi.fn(async () => {
      events.push('settings');
      return validSettings;
    });

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      platform: 'linux',
      runner,
      collectTarget: vi.fn(async () => ({ mode: 'local' as const, installDir: '/opt/aura' })),
      collectSettings: collectSettingsMock,
      createConfig: vi.fn(async () => ({ path: '/tmp/config', cleanup: configCleanup })),
      installLocal: vi.fn(async () => { events.push('install'); }),
      write,
    });

    expect(code).toBe(0);
    expect(events).toEqual(['settings', 'install', 'config-cleanup']);
    // R1: local mode probes with the REAL runner (the probed machine IS the install target).
    expect(collectSettingsMock).toHaveBeenCalledWith(
      expect.anything(), expect.anything(), '/opt/aura', runner,
    );
    expect(runner.calls.some((call) => call.command === 'uname')).toBe(true);
    expect(write).toHaveBeenCalledWith('Target: this Linux device');
    expect(write).toHaveBeenCalledWith('Aura is starting.');
  });

  it('passes undefined as the probe runner in remote mode instead of probing the laptop', async () => {
    const events: string[] = [];
    const runner = createFakeRunner(async () => {
      throw new Error('the local runner must never be called to probe in remote mode');
    });
    const collectSettingsMock = vi.fn(async () => {
      events.push('settings');
      return validSettings;
    });
    const write = vi.fn();

    const code = await runCli(['--mode', 'remote'], {
      locale: 'en',
      runner,
      collectTarget: vi.fn(async () => ({
        mode: 'remote' as const,
        installDir: '/opt/aura',
        remote: { host: '192.168.1.40', port: 22, username: 'ubuntu' },
      })),
      collectSettings: collectSettingsMock,
      createConfig: vi.fn(async () => ({ path: '/tmp/config', cleanup: vi.fn() })),
      installRemote: vi.fn(async () => { events.push('install'); }),
      write,
    });

    expect(code).toBe(0);
    expect(events).toEqual(['settings', 'install']);
    expect(collectSettingsMock).toHaveBeenCalledWith(
      expect.anything(), expect.anything(), '/opt/aura', undefined,
    );
    expect(write).toHaveBeenCalledWith('Target: ubuntu@192.168.1.40:22');
  });

  it('returns zero without creating a config when final confirmation is declined', async () => {
    const createConfig = vi.fn();
    const write = vi.fn();

    const code = await runCli([], {
      collectTarget: vi.fn(async () => ({ mode: 'local' as const, installDir: '/opt/aura' })),
      runner: createPassingPreflightRunner(),
      platform: 'linux',
      collectSettings: vi.fn(async () => null),
      createConfig,
      write,
      locale: 'it-IT',
    });

    expect(code).toBe(0);
    expect(createConfig).not.toHaveBeenCalled();
    expect(write).toHaveBeenCalledWith('Installazione annullata prima di apportare modifiche.');
  });

  it('prints localized help and an injected package version', async () => {
    const write = vi.fn();

    await expect(runCli(['--help'], { locale: 'it-IT', write, version: '9.8.7' })).resolves.toBe(0);
    expect(write.mock.calls.flat().join('\n')).toContain('Installa o aggiorna');
    expect(write.mock.calls.flat().join('\n')).toContain('--mode local|remote');

    write.mockClear();
    await expect(runCli(['--version'], { write, version: '9.8.7' })).resolves.toBe(0);
    expect(write).toHaveBeenCalledWith('9.8.7');
  });

  it.each([
    ['--unknown'],
    ['--mode'],
    ['--mode', 'invalid'],
    ['--help', '--version'],
  ])('returns usage code 2 for invalid argv %j', async (...argv) => {
    const writeError = vi.fn();

    await expect(runCli(argv, { writeError })).resolves.toBe(2);
    expect(writeError).toHaveBeenCalled();
  });

  it('preserves an install error while still cleaning the config', async () => {
    const installError = new Error('primary install failure');
    const events: string[] = [];
    const writeError = vi.fn();

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      platform: 'linux',
      runner: createPassingPreflightRunner(),
      collectTarget: vi.fn(async () => ({ mode: 'local' as const, installDir: '/opt/aura' })),
      collectSettings: vi.fn(async () => validSettings),
      createConfig: vi.fn(async () => ({
        path: '/tmp/config',
        cleanup: async () => { events.push('config-cleanup'); },
      })),
      installLocal: vi.fn(async () => { events.push('install'); throw installError; }),
      write: vi.fn(),
      writeError,
    });

    expect(code).toBe(1);
    expect(events).toEqual(['install', 'config-cleanup']);
    expect(writeError.mock.calls.flat().join('\n')).toContain('primary install failure');
  });

  // R5/R6 (Task 6): installLocal now has a real default (local.ts's installLocal, resolving
  // the bundled artifact via resolveInstallerArtifact) instead of throwing
  // localInstallNotImplemented. Task 7 has not packaged install-appliance.run in this repo
  // yet, so exercising the real default -- by injecting no installLocal at all -- must still
  // fail, but with the honest, translated installerArtifactMissing message, not a crash.
  it('uses the real installLocal default and fails with a translated message because the Task 7 artifact does not exist yet', async () => {
    const writeError = vi.fn();

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      platform: 'linux',
      runner: createPassingPreflightRunner(),
      collectTarget: vi.fn(async () => ({ mode: 'local' as const, installDir: '/opt/aura' })),
      collectSettings: vi.fn(async () => validSettings),
      createConfig: vi.fn(async () => ({ path: '/tmp/config', cleanup: vi.fn() })),
      write: vi.fn(),
      writeError,
    });

    expect(code).toBe(1);
    expect(writeError).toHaveBeenCalledWith(en.installerArtifactMissing);
  });

  it('fails with a clear code when no installRemote is wired up yet (remote.ts lands later in Task 6)', async () => {
    const writeError = vi.fn();

    const code = await runCli(['--mode', 'remote'], {
      locale: 'en',
      collectTarget: vi.fn(async () => ({
        mode: 'remote' as const,
        installDir: '/opt/aura',
        remote: { host: '192.168.1.40', port: 22, username: 'ubuntu' },
      })),
      collectSettings: vi.fn(async () => validSettings),
      createConfig: vi.fn(async () => ({ path: '/tmp/config', cleanup: vi.fn() })),
      write: vi.fn(),
      writeError,
    });

    expect(code).toBe(1);
    expect(writeError.mock.calls.flat().join('\n')).toContain('remoteInstallNotImplemented');
  });

  it('returns zero on a prompt cancellation before any file exists', async () => {
    const write = vi.fn();
    const cancellation = new Error('User force closed the prompt');
    cancellation.name = 'ExitPromptError';

    const code = await runCli([], {
      locale: 'en',
      collectTarget: vi.fn(async () => { throw cancellation; }),
      write,
    });

    expect(code).toBe(0);
    expect(write).toHaveBeenCalledWith('Installation cancelled before making changes.');
  });

  it('warns and fails when the config cleanup itself throws, even though the install succeeded', async () => {
    const writeError = vi.fn();
    const write = vi.fn();

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      platform: 'linux',
      runner: createPassingPreflightRunner(),
      collectTarget: vi.fn(async () => ({ mode: 'local' as const, installDir: '/opt/aura' })),
      collectSettings: vi.fn(async () => validSettings),
      createConfig: vi.fn(async () => ({
        path: '/tmp/config',
        cleanup: async () => { throw new Error('unlink failed'); },
      })),
      installLocal: vi.fn(async () => {}),
      write,
      writeError,
    });

    expect(code).toBe(1);
    expect(writeError).toHaveBeenCalledWith('Warning: a temporary file could not be removed.');
    // The install itself succeeded -- "Aura is starting." must not print over a cleanup
    // failure the operator still needs to see.
    expect(write).not.toHaveBeenCalledWith('Aura is starting.');
  });
});
