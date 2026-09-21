import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { describe, expect, it, vi } from 'vitest';

import { createTranslator } from '../i18n.js';
import { collectSettings, collectTarget } from '../prompts.js';

describe('collectTarget', () => {
  it('collects and validates one remote target', async () => {
    const prompt = {
      select: vi.fn().mockResolvedValue('remote'),
      input: vi.fn()
        .mockResolvedValueOnce('192.168.1.40')
        .mockResolvedValueOnce('22')
        .mockResolvedValueOnce('ubuntu')
        .mockResolvedValueOnce('')
        .mockResolvedValueOnce('/opt/aura/'),
      confirm: vi.fn(),
    };

    await expect(collectTarget(prompt, createTranslator('en'))).resolves.toEqual({
      mode: 'remote',
      installDir: '/opt/aura',
      remote: { host: '192.168.1.40', port: 22, username: 'ubuntu' },
    });
    expect(prompt.select).toHaveBeenCalledOnce();
  });

  // An empty key is the documented answer and must not become the string "": ssh -i ''
  // is not "no key", it is an unreadable one, and the operator lands back on six password
  // prompts wondering what they did wrong.
  it('treats an empty key answer as no key at all', async () => {
    const prompt = {
      select: vi.fn().mockResolvedValue('remote'),
      input: vi.fn()
        .mockResolvedValueOnce('192.168.1.40')
        .mockResolvedValueOnce('22')
        .mockResolvedValueOnce('ubuntu')
        .mockResolvedValueOnce('   ')
        .mockResolvedValueOnce('/opt/aura'),
      confirm: vi.fn(),
    };

    const collected = await collectTarget(prompt, createTranslator('en'));
    expect(collected.remote?.identityFile).toBeUndefined();
  });

  it('keeps a readable key and refuses one it cannot read', async () => {
    const readable = fileURLToPath(import.meta.url);
    const ask = (key: string) => ({
      select: vi.fn().mockResolvedValue('remote'),
      input: vi.fn()
        .mockResolvedValueOnce('192.168.1.40')
        .mockResolvedValueOnce('22')
        .mockResolvedValueOnce('ubuntu')
        .mockResolvedValueOnce(key)
        .mockResolvedValueOnce('/opt/aura'),
      confirm: vi.fn(),
    });

    const collected = await collectTarget(ask(readable), createTranslator('en'));
    expect(collected.remote?.identityFile).toBe(readable);

    await expect(collectTarget(ask('/nope/missing-key'), createTranslator('en')))
      .rejects.toMatchObject({ code: 'unreadableIdentityFile' });
  });

  it('uses a requested mode without asking the mode question', async () => {
    const prompt = {
      select: vi.fn(),
      input: vi.fn().mockResolvedValue('/opt/aura'),
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
  // The installer asks for infrastructure only. The model route, the model and the OpenRouter
  // management key are chosen by an admin in the first-run web setup (management-key design,
  // decision 3), so there is nothing here to type a key or a model into.
  it('asks the appliance, gVisor and confirmation questions and nothing else', async () => {
    const prompt = {
      select: vi.fn(),
      input: vi.fn(),
      confirm: vi.fn()
        .mockResolvedValueOnce(true) // appliance
        .mockResolvedValueOnce(false) // gvisor
        .mockResolvedValueOnce(true), // confirmInstall
    };

    await expect(collectSettings(prompt, createTranslator('en'), '/opt/aura')).resolves.toEqual({
      installDir: '/opt/aura',
      appliance: true,
      gvisor: false,
    });
    expect(prompt.confirm).toHaveBeenCalledTimes(3);
    expect(prompt.select).not.toHaveBeenCalled();
    expect(prompt.input).not.toHaveBeenCalled();
  });

  it('returns null when the final confirmation is declined', async () => {
    const prompt = {
      select: vi.fn(),
      input: vi.fn(),
      confirm: vi.fn()
        .mockResolvedValueOnce(true)
        .mockResolvedValueOnce(false)
        .mockResolvedValueOnce(false), // confirmInstall declined
    };

    await expect(collectSettings(prompt, createTranslator('en'), '/opt/aura')).resolves.toBeNull();
  });
});

// The regression that mattered to the operator: the first version asked for the key as a
// free text field defaulting to empty, so pressing Enter -- the obvious thing -- walked
// straight into a password per connection. When the machine holds keys, the question must
// be a choice among them.
describe('collectTarget key selection', () => {
  it('offers the discovered keys instead of an empty text field', async () => {
    const home = await mkdtemp(join(tmpdir(), 'create-aura-prompt-'));
    const keyPath = join(home, 'aura_appliance');
    await writeFile(keyPath, 'private');

    vi.resetModules();
    vi.doMock('../identity.js', () => ({ discoverIdentityFiles: () => [keyPath] }));
    const { collectTarget: collect } = await import('../prompts.js');

    const prompt = {
      select: vi.fn()
        .mockResolvedValueOnce('remote')
        .mockResolvedValueOnce(keyPath),
      input: vi.fn()
        .mockResolvedValueOnce('192.168.1.40')
        .mockResolvedValueOnce('22')
        .mockResolvedValueOnce('ubuntu')
        .mockResolvedValueOnce('/opt/aura'),
      confirm: vi.fn(),
    };

    const collected = await collect(prompt, createTranslator('en'));
    expect(collected.remote?.identityFile).toBe(keyPath);
    // The key question went through select, and consumed no text field of its own: the
    // four inputs are host, port, username and install dir.
    expect(prompt.select).toHaveBeenCalledTimes(2);
    expect(prompt.input).toHaveBeenCalledTimes(4);
    // Declining a key stays possible, and is the empty value.
    const keyQuestion = prompt.select.mock.calls[1]?.[0] as { choices: { value: string }[] };
    expect(keyQuestion.choices.map((choice) => choice.value)).toContain('');

    vi.doUnmock('../identity.js');
    vi.resetModules();
    await rm(home, { recursive: true, force: true });
  });
});
