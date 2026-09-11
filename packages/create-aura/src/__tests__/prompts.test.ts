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
