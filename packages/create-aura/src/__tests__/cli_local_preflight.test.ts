import { describe, expect, it, vi } from 'vitest';

import { en } from '../messages/en.js';
import { it as italian } from '../messages/it.js';
import { ProcessExecutionError } from '../process.js';
import { TRANSLATED_ERROR_CODES, runCli } from '../cli.js';
import { createFakeRunner, createPassingPreflightRunner, validSettings } from './cli-test-support.js';

// Split out of cli.test.ts (which crossed the 600-LOC cap once local.ts's preflightLocal --
// REQUIRED_COMMANDS/REQUIRED_HOSTS/cpu/memory/disk/existingInstall -- landed behind cli.ts's
// local-mode branch): every scenario here exercises that one merged gate through runCli.
describe('create-aura CLI local preflight', () => {
  // Coverage target: cli.ts's local-mode branch is the caller that reaches preflight.ts's
  // normalizeArchitecture and assertSufficientDiskSpace through local.ts's preflightLocal. An
  // unsupported architecture must fail the install before a single question is asked.
  it('fails local preflight on an unsupported architecture before collecting settings', async () => {
    const collectSettingsMock = vi.fn();
    const writeError = vi.fn();
    const runner = createFakeRunner(async (command) => {
      if (command === 'sh') return { stdout: '', stderr: '', exitCode: 0 };
      if (command === 'uname') return { stdout: 'mips64\n', stderr: '', exitCode: 0 };
      throw new Error(`unexpected command ${command}`);
    });

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      platform: 'linux',
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
      if (command === 'sh' && args.some((arg) => arg.includes('df -Pk'))) {
        return { stdout: '1024\n', stderr: '', exitCode: 0 };
      }
      if (command === 'sh') return { stdout: '', stderr: '', exitCode: 0 };
      throw new Error(`unexpected command ${command}`);
    });

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      platform: 'linux',
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
      throw new ProcessExecutionError('sh', 127, '', 'command not found');
    });

    const code = await runCli(['--mode', 'local'], {
      platform: 'linux',
      runner,
      collectTarget: vi.fn(async () => ({ mode: 'local' as const, installDir: '/opt/aura' })),
      collectSettings: vi.fn(),
      write: vi.fn(),
      writeError,
    });

    expect(code).toBe(1);
    expect(writeError).toHaveBeenCalled();
  });

  // F2 (review round 1): install.sh refuses on THREE hard thresholds (cpu/ram/disk); the
  // wizard wired only disk. assertSufficientCpuCores/assertSufficientMemory existed but had
  // no caller anywhere outside their own unit test (dead code CLAUDE.md forbids). An
  // under-provisioned local target must fail here, before the artifact is even written.
  it("fails local preflight when the CPU core count is below install.sh's floor", async () => {
    const writeError = vi.fn();
    const collectSettingsMock = vi.fn(async () => validSettings);
    const runner = createFakeRunner(async (command) => {
      if (command === 'sh') return { stdout: '', stderr: '', exitCode: 0 };
      if (command === 'uname') return { stdout: 'x86_64\n', stderr: '', exitCode: 0 };
      if (command === 'getconf') return { stdout: '2\n', stderr: '', exitCode: 0 };
      throw new Error(`unexpected command ${command}`);
    });

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      platform: 'linux',
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
      if (command === 'sh') return { stdout: '', stderr: '', exitCode: 0 };
      throw new Error(`unexpected command ${command}`);
    });

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      platform: 'linux',
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

  // Task 6: local.ts's preflightLocal and installLocal introduce two new codes an operator
  // can actually hit (an unsupported host OS, a package missing its bundled artifact); the
  // same completeness rule applies to them.
  it('maps every Task 6 local-install error code to a translated, non-empty message in both catalogues', () => {
    const localCodes = ['unsupportedLocalPlatform', 'installerArtifactMissing'];

    for (const code of localCodes) {
      expect(TRANSLATED_ERROR_CODES).toHaveProperty(code);
      expect(en[TRANSLATED_ERROR_CODES[code] as keyof typeof en]).toBeTruthy();
      expect(italian[TRANSLATED_ERROR_CODES[code] as keyof typeof en]).toBeTruthy();
    }
  });

  it('translates an unsupported-architecture failure instead of surfacing the raw code', async () => {
    const writeError = vi.fn();
    const runner = createFakeRunner(async (command) => {
      if (command === 'sh') return { stdout: '', stderr: '', exitCode: 0 };
      if (command === 'uname') return { stdout: 'riscv64\n', stderr: '', exitCode: 0 };
      throw new Error(`unexpected command ${command}`);
    });

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      platform: 'linux',
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

  // A Windows or other unsupported host has no install.sh path to run at all in local mode.
  it('fails with a translated message when the wizard itself is not running on Linux or macOS', async () => {
    const writeError = vi.fn();
    const runner = createFakeRunner(async () => {
      throw new Error('the runner must never be called once the platform gate has failed');
    });

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      platform: 'win32',
      runner,
      collectTarget: vi.fn(async () => ({ mode: 'local' as const, installDir: '/opt/aura' })),
      collectSettings: vi.fn(),
      write: vi.fn(),
      writeError,
    });

    expect(code).toBe(1);
    expect(writeError).toHaveBeenCalledWith(en.unsupportedLocalPlatform);
  });

  // F6 (review round 2): the F2 fix ran `nproc` and read /proc/meminfo directly -- both
  // Linux-only, and install.sh:208 explicitly supports macOS via Docker Desktop. Before F2
  // the function used only `uname -m` and `df -Pk`, both portable, so F2 made the wizard
  // refuse a Mac install.sh accepts. Fix mirrors install.sh:118-135's own portable reads.
  it('reads CPU cores with getconf, the one call install.sh itself prefers over branching on OS', async () => {
    const runner = createPassingPreflightRunner();

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      platform: 'linux',
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
      platform: 'linux',
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
      if (command === 'curl') return { stdout: '', stderr: '', exitCode: 0 };
      if (command === 'sh' && args.some((arg) => arg.includes('MemTotal'))) {
        // As if install.sh's own Darwin branch had run: 32 GiB, expressed in KiB.
        return { stdout: `${32 * 1024 * 1024}\n`, stderr: '', exitCode: 0 };
      }
      if (command === 'sh') return { stdout: '41943040\n', stderr: '', exitCode: 0 };
      throw new Error(`unexpected command ${command}`);
    });

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      platform: 'linux',
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
    const runner = createPassingPreflightRunner();

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      platform: 'linux',
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

  // Task 6: local.ts's own re-run marker (compose.yaml, not the reference's
  // docker-compose.yml -- scripts/install.sh:767) now surfaces as a message, mirroring the
  // reference cli.ts's `if (preflight.existingInstall) write(t('existingInstall'))`.
  it('writes the existingInstall message when preflightLocal finds a prior install in place', async () => {
    const write = vi.fn();
    const runner = createFakeRunner(async (command, args) => {
      if (command === 'uname') return { stdout: 'x86_64\n', stderr: '', exitCode: 0 };
      if (command === 'getconf') return { stdout: '8\n', stderr: '', exitCode: 0 };
      if (command === 'curl') return { stdout: '', stderr: '', exitCode: 0 };
      if (command === 'sh' && args.some((arg) => arg.includes('compose.yaml'))) {
        return { stdout: 'true\n', stderr: '', exitCode: 0 };
      }
      if (command === 'sh') return { stdout: '41943040\n', stderr: '', exitCode: 0 };
      throw new Error(`unexpected command ${command}`);
    });

    const code = await runCli(['--mode', 'local'], {
      locale: 'en',
      platform: 'linux',
      runner,
      collectTarget: vi.fn(async () => ({ mode: 'local' as const, installDir: '/opt/aura' })),
      collectSettings: vi.fn(async () => validSettings),
      createConfig: vi.fn(async () => ({ path: '/tmp/config', cleanup: vi.fn() })),
      installLocal: vi.fn(async () => {}),
      write,
    });

    expect(code).toBe(0);
    expect(write).toHaveBeenCalledWith(en.existingInstall);
  });
});
