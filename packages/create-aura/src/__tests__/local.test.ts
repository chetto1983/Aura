import { describe, expect, it, vi } from 'vitest';

import { installLocal, preflightLocal, resolveInstallerArtifact } from '../local.js';

const success = { stdout: '', stderr: '', exitCode: 0 };

describe('local installer', () => {
  it('rejects local mode outside Linux and macOS before running commands', async () => {
    const runner = { run: vi.fn() };

    await expect(preflightLocal(runner, '/opt/aura', 'win32'))
      .rejects.toThrow('unsupportedLocalPlatform');
    expect(runner.run).not.toHaveBeenCalled();
  });

  it('checks required commands, capabilities, existing state, and required hosts', async () => {
    const runner = {
      run: vi.fn()
        // REQUIRED_COMMANDS: sudo, curl, openssl
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        // uname -m
        .mockResolvedValueOnce({ ...success, stdout: 'aarch64\n' })
        // getconf _NPROCESSORS_ONLN
        .mockResolvedValueOnce({ ...success, stdout: '8\n' })
        // memory script
        .mockResolvedValueOnce({ ...success, stdout: '41943040\n' })
        // disk script
        .mockResolvedValueOnce({ ...success, stdout: '41943040\n' })
        // existing-install probe
        .mockResolvedValueOnce({ ...success, stdout: 'true\n' })
        // REQUIRED_HOSTS: ghcr.io, get.docker.com, huggingface.co
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success),
    };

    await expect(preflightLocal(runner, '/opt/aura', 'linux')).resolves.toEqual({
      architecture: 'arm64',
      existingInstall: true,
    });

    expect(runner.run).toHaveBeenCalledTimes(11);
    for (const command of ['sudo', 'curl', 'openssl']) {
      expect(runner.run).toHaveBeenCalledWith(
        'sh',
        ['-c', expect.stringContaining('command -v "$1"'), 'create-aura', command],
      );
    }
    // Aura's own install.sh writes compose.yaml directly under the install dir
    // (scripts/install.sh:767); the reference's docker-compose.yml is the wrong marker here.
    expect(runner.run).toHaveBeenCalledWith(
      'sh',
      ['-c', expect.stringContaining('test -f "$1/compose.yaml"'), 'create-aura', '/opt/aura'],
    );
    for (const host of ['https://ghcr.io', 'https://get.docker.com', 'https://huggingface.co']) {
      expect(runner.run).toHaveBeenCalledWith(
        'curl',
        ['-fsS', '-o', '/dev/null', '-m', '10', host],
      );
    }
    // R3: raw.githubusercontent.com must NOT be probed -- the npm package carries the
    // payload, so install.sh's download_file never touches it (spec decision 5).
    expect(runner.run).not.toHaveBeenCalledWith(
      'curl',
      expect.arrayContaining(['https://raw.githubusercontent.com']),
    );
  });

  it('accepts macOS as a supported local platform (install.sh:208 Docker Desktop path)', async () => {
    const runner = {
      run: vi.fn()
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce({ ...success, stdout: 'x86_64\n' })
        .mockResolvedValueOnce({ ...success, stdout: '8\n' })
        .mockResolvedValueOnce({ ...success, stdout: '41943040\n' })
        .mockResolvedValueOnce({ ...success, stdout: '41943040\n' })
        .mockResolvedValueOnce({ ...success, stdout: 'false\n' })
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success),
    };

    await expect(preflightLocal(runner, '/opt/aura', 'darwin')).resolves.toEqual({
      architecture: 'amd64',
      existingInstall: false,
    });
  });

  it('rejects a local target with less than 20 GiB free', async () => {
    const runner = {
      run: vi.fn()
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce({ ...success, stdout: 'x86_64\n' })
        .mockResolvedValueOnce({ ...success, stdout: '8\n' })
        .mockResolvedValueOnce({ ...success, stdout: '41943040\n' })
        .mockResolvedValueOnce({ ...success, stdout: '1000\n' }),
    };

    await expect(preflightLocal(runner, '/opt/aura', 'linux'))
      .rejects.toThrow('insufficientDiskSpace:1000');
  });

  // F2-equivalent for local.ts's own merged preflight: install.sh:172 refuses below 4 cores,
  // and this gate must fail before the (more expensive) disk/hosts checks run.
  it('rejects a local target with fewer than 4 CPU cores', async () => {
    const runner = {
      run: vi.fn()
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce({ ...success, stdout: 'x86_64\n' })
        .mockResolvedValueOnce({ ...success, stdout: '2\n' }),
    };

    await expect(preflightLocal(runner, '/opt/aura', 'linux'))
      .rejects.toThrow('insufficientCpuCores:2');
  });

  it('rejects an unsupported architecture before checking cpu, memory, or disk', async () => {
    const runner = {
      run: vi.fn()
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce({ ...success, stdout: 'riscv64\n' }),
    };

    await expect(preflightLocal(runner, '/opt/aura', 'linux'))
      .rejects.toThrow('unsupportedArchitecture:riscv64');
    expect(runner.run).toHaveBeenCalledTimes(4);
  });

  it('propagates a missing required command untranslated, exactly like the reference', async () => {
    const runner = {
      run: vi.fn().mockRejectedValueOnce(new Error('sh exited with code 1')),
    };

    await expect(preflightLocal(runner, '/opt/aura', 'linux'))
      .rejects.toThrow('sh exited with code 1');
    expect(runner.run).toHaveBeenCalledTimes(1);
  });

  it('reads memory with the Linux/Darwin fallback script, not /proc/meminfo alone', async () => {
    const runner = {
      run: vi.fn()
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce({ ...success, stdout: 'x86_64\n' })
        .mockResolvedValueOnce({ ...success, stdout: '8\n' })
        .mockResolvedValueOnce({ ...success, stdout: '41943040\n' })
        .mockResolvedValueOnce({ ...success, stdout: '41943040\n' })
        .mockResolvedValueOnce({ ...success, stdout: 'false\n' })
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success),
    };

    await preflightLocal(runner, '/opt/aura', 'linux');

    const memoryCall = runner.run.mock.calls.find(
      (call) => call[0] === 'sh' && (call[1] as string[])[1]?.includes('MemTotal'),
    );
    expect(memoryCall?.[1]?.[1]).toContain('/proc/meminfo');
    expect(memoryCall?.[1]?.[1]).toContain('Darwin');
    expect(memoryCall?.[1]?.[1]).toContain('sysctl -n hw.memsize');
  });

  it('probes the install directory disk space, walking up like install.sh, not the root filesystem', async () => {
    const runner = {
      run: vi.fn()
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce({ ...success, stdout: 'x86_64\n' })
        .mockResolvedValueOnce({ ...success, stdout: '8\n' })
        .mockResolvedValueOnce({ ...success, stdout: '41943040\n' })
        .mockResolvedValueOnce({ ...success, stdout: '41943040\n' })
        .mockResolvedValueOnce({ ...success, stdout: 'false\n' })
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success)
        .mockResolvedValueOnce(success),
    };

    await preflightLocal(runner, '/opt/aura', 'linux');

    const diskCall = runner.run.mock.calls.find(
      (call) => call[0] === 'sh' && (call[1] as string[])[1]?.includes('df -Pk'),
    );
    expect(diskCall?.[1]?.[1]).toContain('dirname');
    expect(diskCall?.[1]?.at(-1)).toBe('/opt/aura');
    expect(diskCall?.[1]).not.toContain('/');
  });

  it('runs sudo with a `--` separator before --config so makeself hands off to install.sh', async () => {
    const runner = { run: vi.fn().mockResolvedValue(success) };
    const apiKey = 'sk-or-v1-correct-horse-battery-staple';

    await installLocal(
      runner,
      { path: '/tmp/install-appliance.run', cleanup: vi.fn() },
      { path: '/tmp/install.conf', cleanup: vi.fn() },
      {
        installDir: '/opt/aura',
        appliance: true,
        gvisor: false,
        llmProvider: 'openrouter',
        llmBaseUrl: 'https://openrouter.ai/api/v1',
        llmModel: 'vendor/model',
        openrouterApiKey: apiKey,
        embedImage: 'ghcr.io/ggml-org/llama.cpp:server',
        embedNgl: '0',
      },
    );

    expect(runner.run).toHaveBeenCalledWith(
      'sudo',
      ['bash', '/tmp/install-appliance.run', '--', '--config', '/tmp/install.conf'],
      { redactions: [apiKey] },
    );
    // R6: makeself's own header parses argv against ITS flags before exec'ing install.sh --
    // `--` must be present, and it must come strictly before `--config`, not merely appear
    // somewhere in the array.
    const argv = runner.run.mock.calls[0]?.[1] as string[];
    expect(argv).toContain('--');
    expect(argv.indexOf('--')).toBeLessThan(argv.indexOf('--config'));
    expect(JSON.stringify(runner.run.mock.calls[0]?.[1])).not.toContain(apiKey);
  });

  it('resolves the bundled installer artifact next to the package root', async () => {
    // R5: Task 7 has not landed yet in this repo, so the real artifact genuinely does not
    // exist -- this must fail loudly with a named error, never silently skip the install.
    await expect(resolveInstallerArtifact()).rejects.toThrow('installerArtifactMissing');
  });
});
