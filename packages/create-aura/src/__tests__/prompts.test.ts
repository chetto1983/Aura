import { describe, expect, it, vi } from 'vitest';

import { serializeInstallConfig } from '../config-file.js';
import { createTranslator } from '../i18n.js';
import { CPU_EMBED_IMAGE, CUDA_EMBED_IMAGE } from '../modelroute.js';
import { collectSettings, collectTarget } from '../prompts.js';
import type { InstallSettings } from '../types.js';
import { ProcessExecutionError } from '../process.js';
import type { CommandRunner, ProcessResult } from '../process.js';

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

describe('collectTarget', () => {
  it('collects and validates one remote target', async () => {
    const prompt = {
      select: vi.fn().mockResolvedValue('remote'),
      input: vi.fn()
        .mockResolvedValueOnce('192.168.1.40')
        .mockResolvedValueOnce('22')
        .mockResolvedValueOnce('ubuntu')
        .mockResolvedValueOnce('/opt/aura/'),
      password: vi.fn(),
      confirm: vi.fn(),
    };

    await expect(collectTarget(prompt, createTranslator('en'))).resolves.toEqual({
      mode: 'remote',
      installDir: '/opt/aura',
      remote: { host: '192.168.1.40', port: 22, username: 'ubuntu' },
    });
    expect(prompt.select).toHaveBeenCalledOnce();
  });

  it('uses a requested mode without asking the mode question', async () => {
    const prompt = {
      select: vi.fn(),
      input: vi.fn().mockResolvedValue('/opt/aura'),
      password: vi.fn(),
      confirm: vi.fn(),
    };

    await expect(collectTarget(prompt, createTranslator('it'), 'local')).resolves.toEqual({
      mode: 'local',
      installDir: '/opt/aura',
    });
    expect(prompt.select).not.toHaveBeenCalled();
  });
});

describe('collectSettings', () => {
  // Task 5 Step 4 assertion #1: the OpenRouter route asks for a key, the Ollama route does
  // not. With no probeRunner (remote-mode fallback, or no probe available yet), both routes
  // fall back to typed input for base url/model and a yes/no GPU question.
  it('asks for an OpenRouter API key on the OpenRouter route and applies the GPU answer', async () => {
    const prompt = {
      select: vi.fn().mockResolvedValueOnce('openrouter'),
      input: vi.fn()
        .mockResolvedValueOnce('https://openrouter.ai/api/v1')
        .mockResolvedValueOnce('deepseek/deepseek-v4'),
      password: vi.fn().mockResolvedValueOnce('sk-or-v1-correct-horse-battery-staple'),
      confirm: vi.fn()
        .mockResolvedValueOnce(true) // appliance
        .mockResolvedValueOnce(false) // gvisor
        .mockResolvedValueOnce(true) // has GPU? (no probeRunner -> yes/no fallback, R1)
        .mockResolvedValueOnce(true), // confirmInstall
    };

    const settings = await collectSettings(prompt, createTranslator('en'), '/opt/aura', undefined);

    expect(prompt.password).toHaveBeenCalledOnce();
    expect(settings).toEqual({
      installDir: '/opt/aura',
      appliance: true,
      gvisor: false,
      llmProvider: 'openrouter',
      llmBaseUrl: 'https://openrouter.ai/api/v1',
      llmModel: 'deepseek/deepseek-v4',
      openrouterApiKey: 'sk-or-v1-correct-horse-battery-staple',
      embedImage: CUDA_EMBED_IMAGE,
      embedNgl: '99',
    });
  });

  // Task 5 Step 4 assertion #2: the Ollama route never prompts for a key, yet
  // serializeInstallConfig must still emit an empty openrouter_api_key_base64 line -- Ruling
  // R5: install.sh's parse_install_config exit 2s on a MISSING key, not an empty one.
  it('does not ask for an API key on the Ollama route, and the config still emits an empty one', async () => {
    const prompt = {
      select: vi.fn().mockResolvedValueOnce('ollama'),
      input: vi.fn()
        .mockResolvedValueOnce('http://localhost:11434')
        .mockResolvedValueOnce('llama3:8b'),
      password: vi.fn(),
      confirm: vi.fn()
        .mockResolvedValueOnce(true) // appliance
        .mockResolvedValueOnce(false) // gvisor
        .mockResolvedValueOnce(false) // has GPU? -> no
        .mockResolvedValueOnce(true), // confirmInstall
    };

    const settings = await collectSettings(prompt, createTranslator('en'), '/opt/aura', undefined);

    expect(prompt.password).not.toHaveBeenCalled();
    expect(settings?.openrouterApiKey).toBeUndefined();
    expect(settings).toMatchObject({
      llmProvider: 'ollama',
      llmBaseUrl: 'http://localhost:11434',
      llmModel: 'llama3:8b',
      embedImage: CPU_EMBED_IMAGE,
      embedNgl: '0',
    });

    const serialized = serializeInstallConfig(settings as InstallSettings);
    expect(serialized).toContain('openrouter_api_key_base64=\n');
  });

  it('returns null when the final confirmation is declined', async () => {
    const prompt = {
      select: vi.fn().mockResolvedValueOnce('ollama'),
      input: vi.fn()
        .mockResolvedValueOnce('http://localhost:11434')
        .mockResolvedValueOnce('llama3:8b'),
      password: vi.fn(),
      confirm: vi.fn()
        .mockResolvedValueOnce(true)
        .mockResolvedValueOnce(false)
        .mockResolvedValueOnce(false)
        .mockResolvedValueOnce(false), // confirmInstall declined
    };

    await expect(
      collectSettings(prompt, createTranslator('en'), '/opt/aura', undefined),
    ).resolves.toBeNull();
  });

  // R1: in local mode cli.ts passes the real process runner, so both probes run for real
  // instead of asking the operator anything -- this is the seam Task 6's SSH-wrapping
  // runner plugs into for remote mode without collectSettings ever knowing what an SSH is.
  it('uses the injected runner to probe the GPU and list Ollama models when one is provided', async () => {
    const runner = createFakeRunner(async (command) => {
      if (command === 'nvidia-smi') return { stdout: '', stderr: '', exitCode: 0 };
      if (command === 'docker') {
        return {
          stdout: JSON.stringify({ models: [{ name: 'llama3:8b' }, { name: 'mistral:latest' }] }),
          stderr: '',
          exitCode: 0,
        };
      }
      throw new Error(`unexpected command ${command}`);
    });
    const prompt = {
      select: vi.fn()
        .mockResolvedValueOnce('ollama')
        .mockResolvedValueOnce('mistral:latest'), // picked from the probed list
      input: vi.fn().mockResolvedValueOnce('http://host.docker.internal:11434'),
      password: vi.fn(),
      confirm: vi.fn()
        .mockResolvedValueOnce(true) // appliance
        .mockResolvedValueOnce(false) // gvisor
        .mockResolvedValueOnce(true), // confirmInstall
    };

    const settings = await collectSettings(prompt, createTranslator('en'), '/opt/aura', runner);

    expect(settings).toMatchObject({
      llmModel: 'mistral:latest',
      embedImage: CUDA_EMBED_IMAGE,
      embedNgl: '99',
    });
    // The GPU yes/no fallback question must NOT be asked once a runner can probe for real.
    expect(prompt.confirm).toHaveBeenCalledTimes(3);
    expect(runner.calls.some((call) => call.command === 'nvidia-smi')).toBe(true);
    expect(runner.calls.some((call) => call.command === 'docker')).toBe(true);
  });

  // R2 (Task 4 review, landed here): a probe failure must not be reported as "Ollama is not
  // running" -- the same reachable:false comes from a refused endpoint, a missing docker
  // daemon, an unpullable alpine image, or a non-JSON reply, and blaming the endpoint sends
  // the operator to debug a box that may be fine.
  it('falls back to manual model entry with a neutral message when the probe cannot reach the endpoint', async () => {
    const runner = createFakeRunner(async (command) => {
      if (command === 'nvidia-smi') {
        throw new ProcessExecutionError('nvidia-smi', 127, '', 'not found');
      }
      if (command === 'docker') {
        throw new ProcessExecutionError('docker', 1, '', "wget: can't connect to remote host");
      }
      throw new Error(`unexpected command ${command}`);
    });
    const prompt = {
      select: vi.fn().mockResolvedValueOnce('ollama'),
      input: vi.fn()
        .mockResolvedValueOnce('http://host.docker.internal:11434')
        .mockResolvedValueOnce('llama3:8b'),
      password: vi.fn(),
      confirm: vi.fn()
        .mockResolvedValueOnce(true)
        .mockResolvedValueOnce(false)
        .mockResolvedValueOnce(true),
    };

    const settings = await collectSettings(prompt, createTranslator('en'), '/opt/aura', runner);

    expect(settings?.llmModel).toBe('llama3:8b');
    const manualEntryCall = prompt.input.mock.calls[1]?.[0] as { message: string } | undefined;
    expect(manualEntryCall?.message).toBe(
      'Could not reach that Ollama endpoint from here. Enter the model id you want to use',
    );
    expect(manualEntryCall?.message).not.toContain('Ollama is not running');
  });

  // C1 (Critical, review round 1): the offered default was 'http://localhost:11434'. Both
  // halves were wrong -- probeOllama's request runs INSIDE the throwaway alpine container,
  // where 'localhost' means the container itself (modelroute.ts's own comment: "an Ollama on
  // the host is not at 127.0.0.1 as seen from there"), so the default could never probe
  // reachable; and internal/llm/config.go:21's defaultBaseURL carries /v1, which every other
  // Ollama base URL in this repo also spells with (scripts/install_config_test.sh:19,
  // internal/agui/settings_api_test.go:218, internal/channels/telegram/commands_spend_test.go:157)
  // -- without it the installed Aura requests .../11434/chat/completions, which Ollama does
  // not serve, and answers no turn.
  it('offers the container-reachable, /v1-suffixed Ollama base URL as the default', async () => {
    // Two `input` calls happen with no probeRunner (base url, then manual model entry, which
    // has no `default` of its own) -- capture every call's default rather than assuming
    // which one is first, and returning each call's own default (falling back to a plain
    // model id when there is none) keeps both prompt.input calls valid instead of throwing.
    const capturedDefaults: Array<string | undefined> = [];
    const prompt = {
      select: vi.fn().mockResolvedValueOnce('ollama'),
      input: vi.fn(async (options: { default?: string }) => {
        capturedDefaults.push(options.default);
        return options.default ?? 'llama3:8b';
      }),
      password: vi.fn(),
      confirm: vi.fn()
        .mockResolvedValueOnce(true)
        .mockResolvedValueOnce(false)
        .mockResolvedValueOnce(false)
        .mockResolvedValueOnce(true),
    };

    await collectSettings(prompt, createTranslator('en'), '/opt/aura', undefined);

    expect(capturedDefaults[0]).toBe('http://host.docker.internal:11434/v1');
  });

  // The assertion that would have caught C1 on its own: accept the default exactly as
  // offered and prove the probe it feeds derives the real container-visible tags URL, not
  // just that the string looks plausible in isolation.
  it('probes the exact tags URL derived from the default Ollama base URL when the operator accepts it', async () => {
    const runner = createFakeRunner(async (command) => {
      if (command === 'nvidia-smi') {
        throw new ProcessExecutionError('nvidia-smi', 127, '', 'not found');
      }
      if (command === 'docker') {
        return {
          stdout: JSON.stringify({ models: [{ name: 'llama3:8b' }] }),
          stderr: '',
          exitCode: 0,
        };
      }
      throw new Error(`unexpected command ${command}`);
    });
    const prompt = {
      select: vi.fn()
        .mockResolvedValueOnce('ollama')
        .mockResolvedValueOnce('llama3:8b'),
      input: vi.fn(async (options: { default?: string }) => options.default ?? ''),
      password: vi.fn(),
      confirm: vi.fn()
        .mockResolvedValueOnce(true)
        .mockResolvedValueOnce(false)
        .mockResolvedValueOnce(true),
    };

    const settings = await collectSettings(prompt, createTranslator('en'), '/opt/aura', runner);

    expect(settings?.llmBaseUrl).toBe('http://host.docker.internal:11434/v1');
    const dockerCall = runner.calls.find((call) => call.command === 'docker');
    expect(dockerCall?.args).toContain('http://host.docker.internal:11434/api/tags');
  });
});
