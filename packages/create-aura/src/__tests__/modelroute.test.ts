import { afterEach, describe, expect, it, vi } from 'vitest';

import { ProcessExecutionError } from '../process.js';
import type { CommandRunner, ProcessResult } from '../process.js';
import {
  CPU_EMBED_IMAGE,
  CUDA_EMBED_IMAGE,
  probeGpu,
  probeOllama,
} from '../modelroute.js';

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

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('probeGpu', () => {
  it('returns the CUDA image pair when nvidia-smi exits 0', async () => {
    const runner = createFakeRunner(async () => ({ stdout: '', stderr: '', exitCode: 0 }));

    await expect(probeGpu(runner)).resolves.toEqual({
      cuda: true,
      embedImage: CUDA_EMBED_IMAGE,
      embedNgl: '99',
    });
    expect(runner.calls).toEqual([{ command: 'nvidia-smi', args: [] }]);
  });

  // CommandRunner.run rejects rather than returning a non-zero exitCode (process.ts's
  // ProcessRunner.execute), so an absent nvidia-smi reaches probeGpu as a rejected promise,
  // never as a result to branch on.
  it('returns the CPU image pair and does not throw when nvidia-smi is absent', async () => {
    const runner = createFakeRunner(async () => {
      throw new ProcessExecutionError('nvidia-smi', 127, '', 'command not found');
    });

    await expect(probeGpu(runner)).resolves.toEqual({
      cuda: false,
      embedImage: CPU_EMBED_IMAGE,
      embedNgl: '0',
    });
  });
});

describe('probeOllama', () => {
  const dockerArgvFor = (tagsUrl: string) => [
    'run', '--rm', '--add-host', 'host.docker.internal:host-gateway', 'alpine',
    'wget', '-qO-', '--timeout=5', tagsUrl,
  ];

  it.each([
    ['http://host.docker.internal:11434/v1', 'http://host.docker.internal:11434/api/tags'],
    ['http://host.docker.internal:11434/v1/', 'http://host.docker.internal:11434/api/tags'],
  ])('derives the tags URL from the OpenAI-compatible base URL %s', async (baseUrl, tagsUrl) => {
    const runner = createFakeRunner(async () => ({
      stdout: JSON.stringify({ models: [] }),
      stderr: '',
      exitCode: 0,
    }));

    await probeOllama(runner, baseUrl);

    expect(runner.calls).toEqual([{ command: 'docker', args: dockerArgvFor(tagsUrl) }]);
  });

  it('lists the model ids from an /api/tags body', async () => {
    const runner = createFakeRunner(async () => ({
      stdout: JSON.stringify({ models: [{ name: 'llama3:8b' }, { name: 'mistral:latest' }] }),
      stderr: '',
      exitCode: 0,
    }));

    await expect(probeOllama(runner, 'http://host.docker.internal:11434/v1')).resolves.toEqual({
      reachable: true,
      models: ['llama3:8b', 'mistral:latest'],
    });
  });

  it('drops a tag entry with no name and treats a response with no models array as empty', async () => {
    const withBlankEntry = createFakeRunner(async () => ({
      stdout: JSON.stringify({ models: [{ name: 'llama3:8b' }, {}] }),
      stderr: '',
      exitCode: 0,
    }));
    const withNoModelsKey = createFakeRunner(async () => ({
      stdout: JSON.stringify({}),
      stderr: '',
      exitCode: 0,
    }));

    await expect(
      probeOllama(withBlankEntry, 'http://host.docker.internal:11434/v1'),
    ).resolves.toEqual({ reachable: true, models: ['llama3:8b'] });
    await expect(
      probeOllama(withNoModelsKey, 'http://host.docker.internal:11434/v1'),
    ).resolves.toEqual({ reachable: true, models: [] });
  });

  it('reports unreachable without throwing when the probe container cannot connect', async () => {
    const runner = createFakeRunner(async () => {
      throw new ProcessExecutionError('docker', 1, '', "wget: can't connect to remote host");
    });

    await expect(probeOllama(runner, 'http://127.0.0.1:11434/v1')).resolves.toEqual({
      reachable: false,
      models: [],
    });
  });

  // The regression this guards: "Hyper-V port forwarding lies -- probe via docker network,
  // not 127.0.0.1" (project memory). The fake here answers 200 on the host (via a stubbed
  // global fetch) but refuses from inside the container (via the fake CommandRunner) -- the
  // same split a host-only Ollama produces against a containerised Aura. If probeOllama ever
  // regressed to querying the host directly, this test would see fetch's 200 and report
  // reachable: true; asserting on the recorded call proves the probe went through docker run
  // instead, for the reason the container-visible answer is the only one that matters.
  it('reports a host-only Ollama as unreachable even though a host fetch would succeed', async () => {
    const fakeFetch = vi.fn(async () => ({
      ok: true,
      status: 200,
      json: async () => ({ models: [{ name: 'llama3:8b' }] }),
    }));
    vi.stubGlobal('fetch', fakeFetch);
    const runner = createFakeRunner(async () => {
      throw new ProcessExecutionError('docker', 1, '', "wget: can't connect to remote host");
    });

    await expect(probeOllama(runner, 'http://host.docker.internal:11434/v1')).resolves.toEqual({
      reachable: false,
      models: [],
    });
    expect(fakeFetch).not.toHaveBeenCalled();
    expect(runner.calls).toHaveLength(1);
    expect(runner.calls[0]?.command).toBe('docker');
    expect(runner.calls[0]?.args).toContain('run');
  });
});
