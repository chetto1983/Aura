import { describe, expect, it, vi } from 'vitest';

import { en } from '../messages/en.js';
import { it as italian } from '../messages/it.js';
import { ProcessExecutionError } from '../process.js';
import type { CommandRunner, ProcessResult } from '../process.js';
import { TRANSLATED_ERROR_CODES, runCli } from '../cli.js';

type FakeRunner = CommandRunner & { calls: Array<{ command: string; args: readonly string[] }> };

function createFakeRunner(
  responder: (command: string, args: readonly string[]) => Promise<ProcessResult>,
): FakeRunner {
  const calls: Array<{ command: string; args: readonly string[] }> = [];
  return {
    calls,
    async run(command, args = []) {
      calls.push({ command, args });
      return responder(command, args);
    },
  };
}

function createPassingPreflightRunner(): FakeRunner {
  return createFakeRunner(async (command) => {
    if (command === 'uname') return { stdout: 'x86_64\n', stderr: '', exitCode: 0 };
    if (command === 'getconf') return { stdout: '8\n', stderr: '', exitCode: 0 };
    // 41943040 KiB (40 GiB) clears both the memory floor (16 GiB) and the disk floor
    // (20 GiB), so one generic answer satisfies whichever `sh -c` check asks.
    if (command === 'sh') return { stdout: '41943040\n', stderr: '', exitCode: 0 };
    throw new Error(`unexpected command ${command}`);
  });
}

const validSettings = {
  installDir: '/opt/aura',
  appliance: true,
  gvisor: false,
  llmProvider: 'ollama',
  llmBaseUrl: 'http://localhost:11434',
  llmModel: 'llama3:8b',
  embedImage: 'ghcr.io/ggml-org/llama.cpp:server',
  embedNgl: '0',
};

describe('create-aura CLI', () => {
  it('runs a local hardware preflight, probes with the real runner, installs, and cleans up', async () => {
    const events: string[] = [];
    const configCleanup = vi.fn(async () => { events.push('config-cleanup'); });
    const write = vi.fn();
    const runner = createFakeRunner(async (command) => {
      if (command === 'uname') return { stdout: 'aarch64\n', stderr: '', exitCode: 0 };
      if (command === 'getconf') return { stdout: '8\n', stderr: '', exitCode: 0 };
      if (command === 'sh') return { stdout: '41943040\n', stderr: '', exitCode: 0 };
      throw new Error(`unexpected command ${command}`);
    });
    const collectSettingsMock = vi.fn(async () => {
      events.push('settings');
      return validSettings;
    });

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
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

  it('fails with a clear code when no installLocal is wired up yet (no default exists before Task 6)', async () => {
    const writeError = vi.fn();

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      runner: createPassingPreflightRunner(),
      collectTarget: vi.fn(async () => ({ mode: 'local' as const, installDir: '/opt/aura' })),
      collectSettings: vi.fn(async () => validSettings),
      createConfig: vi.fn(async () => ({ path: '/tmp/config', cleanup: vi.fn() })),
      write: vi.fn(),
      writeError,
    });

    expect(code).toBe(1);
    expect(writeError.mock.calls.flat().join('\n')).toContain('localInstallNotImplemented');
  });

  // Coverage target: cli.ts's own local preflight step is the caller that reaches
  // preflight.ts's normalizeArchitecture and assertSufficientDiskSpace -- both ported but,
  // before this task, uncalled by any test (progress.md: "the branch shortfall arrives with
  // Tasks 5-6"). An unsupported architecture must fail the install before a single question
  // is asked.
  it('fails local preflight on an unsupported architecture before collecting settings', async () => {
    const collectSettingsMock = vi.fn();
    const writeError = vi.fn();
    const runner = createFakeRunner(async (command) => {
      if (command === 'uname') return { stdout: 'mips64\n', stderr: '', exitCode: 0 };
      throw new Error(`unexpected command ${command}`);
    });

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      runner,
      collectTarget: vi.fn(async () => ({ mode: 'local' as const, installDir: '/opt/aura' })),
      collectSettings: collectSettingsMock,
      write: vi.fn(),
      writeError,
    });

    expect(code).toBe(1);
    expect(collectSettingsMock).not.toHaveBeenCalled();
    // F3 (review round 1): this used to assert the raw 'unsupportedArchitecture' code was
    // shown -- that was the bug. See the dedicated F3 test below for the full assertion
    // (translated message shown, raw code never shown).
    expect(writeError).toHaveBeenCalledWith(en.unsupportedArchitecture);
  });

  it('fails local preflight with a translated message when disk space is insufficient', async () => {
    const writeError = vi.fn();
    const runner = createFakeRunner(async (command, args) => {
      if (command === 'uname') return { stdout: 'x86_64\n', stderr: '', exitCode: 0 };
      if (command === 'getconf') return { stdout: '8\n', stderr: '', exitCode: 0 };
      if (command === 'sh' && args.some((arg) => arg.includes('MemTotal'))) {
        return { stdout: '41943040\n', stderr: '', exitCode: 0 };
      }
      if (command === 'sh') return { stdout: '1024\n', stderr: '', exitCode: 0 };
      throw new Error(`unexpected command ${command}`);
    });

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      runner,
      collectTarget: vi.fn(async () => ({ mode: 'local' as const, installDir: '/opt/aura' })),
      collectSettings: vi.fn(),
      write: vi.fn(),
      writeError,
    });

    expect(code).toBe(1);
    expect(writeError).toHaveBeenCalledWith(
      'Not enough free disk space on the installation target.',
    );
  });

  it('reports a ProcessExecutionError from the preflight runner as a failure, not a crash', async () => {
    const writeError = vi.fn();
    const runner = createFakeRunner(async () => {
      throw new ProcessExecutionError('uname', 127, '', 'command not found');
    });

    const code = await runCli(['--mode', 'local'], {
      runner,
      collectTarget: vi.fn(async () => ({ mode: 'local' as const, installDir: '/opt/aura' })),
      collectSettings: vi.fn(),
      write: vi.fn(),
      writeError,
    });

    expect(code).toBe(1);
    expect(writeError).toHaveBeenCalled();
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

  // F2 (review round 1): install.sh refuses on THREE hard thresholds (cpu/ram/disk); the
  // wizard wired only disk. assertSufficientCpuCores/assertSufficientMemory existed but had
  // no caller anywhere outside their own unit test (dead code CLAUDE.md forbids). An
  // under-provisioned local target must fail here, before the artifact is even written.
  it("fails local preflight when the CPU core count is below install.sh's floor", async () => {
    const writeError = vi.fn();
    const collectSettingsMock = vi.fn(async () => validSettings);
    const runner = createFakeRunner(async (command) => {
      if (command === 'uname') return { stdout: 'x86_64\n', stderr: '', exitCode: 0 };
      if (command === 'getconf') return { stdout: '2\n', stderr: '', exitCode: 0 };
      if (command === 'sh') return { stdout: '41943040\n', stderr: '', exitCode: 0 };
      throw new Error(`unexpected command ${command}`);
    });

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      runner,
      collectTarget: vi.fn(async () => ({ mode: 'local' as const, installDir: '/opt/aura' })),
      collectSettings: collectSettingsMock,
      createConfig: vi.fn(async () => ({ path: '/tmp/config', cleanup: vi.fn() })),
      installLocal: vi.fn(async () => {}),
      write: vi.fn(),
      writeError,
    });

    expect(code).toBe(1);
    expect(collectSettingsMock).not.toHaveBeenCalled();
    expect(writeError).toHaveBeenCalledWith('Not enough CPU cores on the installation target.');
  });

  it("fails local preflight when memory is below install.sh's floor", async () => {
    const writeError = vi.fn();
    const collectSettingsMock = vi.fn(async () => validSettings);
    const runner = createFakeRunner(async (command, args) => {
      if (command === 'uname') return { stdout: 'x86_64\n', stderr: '', exitCode: 0 };
      if (command === 'getconf') return { stdout: '8\n', stderr: '', exitCode: 0 };
      if (command === 'sh' && args.some((arg) => arg.includes('MemTotal'))) {
        return { stdout: '1024\n', stderr: '', exitCode: 0 };
      }
      if (command === 'sh') return { stdout: '41943040\n', stderr: '', exitCode: 0 };
      throw new Error(`unexpected command ${command}`);
    });

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      runner,
      collectTarget: vi.fn(async () => ({ mode: 'local' as const, installDir: '/opt/aura' })),
      collectSettings: collectSettingsMock,
      createConfig: vi.fn(async () => ({ path: '/tmp/config', cleanup: vi.fn() })),
      installLocal: vi.fn(async () => {}),
      write: vi.fn(),
      writeError,
    });

    expect(code).toBe(1);
    expect(collectSettingsMock).not.toHaveBeenCalled();
    expect(writeError).toHaveBeenCalledWith('Not enough memory on the installation target.');
  });

  // F3 (review round 1): six preflight.ts error codes had neither a TRANSLATED_ERROR_CODES
  // entry nor a catalogue key, so an operator on an unsupported architecture read the raw
  // string "unsupportedArchitecture:riscv64". i18n.test.ts's en/it parity check cannot catch
  // this class of bug: a key missing from BOTH catalogues is still "identical key sets".
  it('maps every preflight error code to a translated, non-empty message in both catalogues', () => {
    const preflightCodes = [
      'unsupportedArchitecture',
      'invalidDiskAvailability',
      'insufficientDiskSpace',
      'invalidCpuCount',
      'insufficientCpuCores',
      'invalidMemoryAvailability',
      'insufficientMemory',
    ];

    for (const code of preflightCodes) {
      expect(TRANSLATED_ERROR_CODES).toHaveProperty(code);
    }
    for (const key of Object.values(TRANSLATED_ERROR_CODES)) {
      expect(en[key]).toBeTruthy();
      expect(italian[key]).toBeTruthy();
    }
  });

  it('translates an unsupported-architecture failure instead of surfacing the raw code', async () => {
    const writeError = vi.fn();
    const runner = createFakeRunner(async (command) => {
      if (command === 'uname') return { stdout: 'riscv64\n', stderr: '', exitCode: 0 };
      throw new Error(`unexpected command ${command}`);
    });

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      runner,
      collectTarget: vi.fn(async () => ({ mode: 'local' as const, installDir: '/opt/aura' })),
      collectSettings: vi.fn(),
      write: vi.fn(),
      writeError,
    });

    expect(code).toBe(1);
    expect(writeError.mock.calls.flat().join('\n')).not.toContain('unsupportedArchitecture:riscv64');
    expect(writeError).toHaveBeenCalledWith(en.unsupportedArchitecture);
  });

  // F6 (review round 2): the F2 fix ran `nproc` and read /proc/meminfo directly -- both
  // Linux-only, and install.sh:208 explicitly supports macOS via Docker Desktop. Before F2
  // the function used only `uname -m` and `df -Pk`, both portable, so F2 made the wizard
  // refuse a Mac install.sh accepts. Fix mirrors install.sh:118-135's own portable reads.
  it('reads CPU cores with getconf, the one call install.sh itself prefers over branching on OS', async () => {
    const runner = createPassingPreflightRunner();

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      runner,
      collectTarget: vi.fn(async () => ({ mode: 'local' as const, installDir: '/opt/aura' })),
      collectSettings: vi.fn(async () => validSettings),
      createConfig: vi.fn(async () => ({ path: '/tmp/config', cleanup: vi.fn() })),
      installLocal: vi.fn(async () => {}),
      write: vi.fn(),
    });

    expect(code).toBe(0);
    const getconfCall = runner.calls.find((call) => call.command === 'getconf');
    expect(getconfCall?.args).toEqual(['_NPROCESSORS_ONLN']);
    expect(runner.calls.some((call) => call.command === 'nproc')).toBe(false);
  });

  it('reads memory with a script that falls back to sysctl on macOS, not /proc/meminfo alone', async () => {
    const runner = createPassingPreflightRunner();

    await runCli(['--mode', 'local'], {
      locale: 'en',
      runner,
      collectTarget: vi.fn(async () => ({ mode: 'local' as const, installDir: '/opt/aura' })),
      collectSettings: vi.fn(async () => validSettings),
      createConfig: vi.fn(async () => ({ path: '/tmp/config', cleanup: vi.fn() })),
      installLocal: vi.fn(async () => {}),
      write: vi.fn(),
    });

    const memoryCall = runner.calls.find(
      (call) => call.command === 'sh' && call.args.some((arg) => arg.includes('MemTotal')),
    );
    const script = memoryCall?.args[1] ?? '';
    // install.sh:126-135 mirrored verbatim: Linux reads /proc/meminfo, Darwin falls back to
    // `sysctl -n hw.memsize` (bytes, converted to KiB), and an unknown platform echoes 0 --
    // which then FAILS the floor rather than skipping the gate (install.sh's own property).
    expect(script).toContain('/proc/meminfo');
    expect(script).toContain('Darwin');
    expect(script).toContain('sysctl -n hw.memsize');
    expect(script).toContain('else echo 0');
  });

  it('accepts a macOS-shaped memory reading (sysctl bytes converted to KiB) that clears the floor', async () => {
    const collectSettingsMock = vi.fn(async () => validSettings);
    const runner = createFakeRunner(async (command, args) => {
      if (command === 'uname') return { stdout: 'x86_64\n', stderr: '', exitCode: 0 };
      if (command === 'getconf') return { stdout: '8\n', stderr: '', exitCode: 0 };
      if (command === 'sh' && args.some((arg) => arg.includes('MemTotal'))) {
        // As if install.sh's own Darwin branch had run: 32 GiB, expressed in KiB.
        return { stdout: `${32 * 1024 * 1024}\n`, stderr: '', exitCode: 0 };
      }
      if (command === 'sh') return { stdout: '41943040\n', stderr: '', exitCode: 0 };
      throw new Error(`unexpected command ${command}`);
    });

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      runner,
      collectTarget: vi.fn(async () => ({ mode: 'local' as const, installDir: '/opt/aura' })),
      collectSettings: collectSettingsMock,
      createConfig: vi.fn(async () => ({ path: '/tmp/config', cleanup: vi.fn() })),
      installLocal: vi.fn(async () => {}),
      write: vi.fn(),
    });

    expect(code).toBe(0);
    expect(collectSettingsMock).toHaveBeenCalled();
  });

  // F7 (review round 2): `df -Pk /` measures the root filesystem, but install.sh:137-154
  // probes the INSTALL DIRECTORY, walking up to the nearest existing parent because the
  // directory does not exist yet on a first install. With the appliance default of
  // /opt/aura on a server where /opt is its own volume, the old check reported a number
  // about the wrong disk.
  it("probes the install directory's disk space, walking up like install.sh, not the root filesystem", async () => {
    const collectSettingsMock = vi.fn(async () => validSettings);
    const runner = createFakeRunner(async (command, args) => {
      if (command === 'uname') return { stdout: 'x86_64\n', stderr: '', exitCode: 0 };
      if (command === 'getconf') return { stdout: '8\n', stderr: '', exitCode: 0 };
      if (command === 'sh' && args.some((arg) => arg.includes('MemTotal'))) {
        return { stdout: '41943040\n', stderr: '', exitCode: 0 };
      }
      if (command === 'sh') return { stdout: '41943040\n', stderr: '', exitCode: 0 };
      throw new Error(`unexpected command ${command}`);
    });

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      runner,
      collectTarget: vi.fn(async () => ({ mode: 'local' as const, installDir: '/opt/aura' })),
      collectSettings: collectSettingsMock,
      createConfig: vi.fn(async () => ({ path: '/tmp/config', cleanup: vi.fn() })),
      installLocal: vi.fn(async () => {}),
      write: vi.fn(),
    });

    expect(code).toBe(0);
    const diskCall = runner.calls.find(
      (call) => call.command === 'sh' && call.args.some((arg) => arg.includes('df -Pk')),
    );
    expect(diskCall?.args[1]).toContain('dirname');
    // The install dir travels as the script's $1, exactly like the reference's own
    // positional-argument convention for a sh -c script (a throwaway $0 placeholder first).
    expect(diskCall?.args.at(-1)).toBe('/opt/aura');
    expect(diskCall?.args).not.toContain('/');
  });
});
