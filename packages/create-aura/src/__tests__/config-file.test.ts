import { mkdtemp, readFile, readdir, rm, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';

import {
  createTemporaryInstallConfig,
  serializeInstallConfig,
} from '../config-file.js';
import type { InstallSettings } from '../types.js';
import { ValidationError } from '../validation.js';

const settings: InstallSettings = {
  installDir: '/opt/aura',
  appliance: true,
  gvisor: true,
  llmProvider: 'openrouter',
  llmBaseUrl: 'https://openrouter.ai/api/v1',
  llmModel: 'deepseek/deepseek-v4',
  openrouterApiKey: 'sk-or-v1-correct-horse-battery-staple',
  embedImage: 'ghcr.io/aura/embed-gemma:latest',
  embedNgl: '999',
};

const roots: string[] = [];

afterEach(async () => {
  await Promise.all(roots.splice(0).map((root) => rm(root, { recursive: true, force: true })));
});

describe('serializeInstallConfig', () => {
  it('base64-encodes every user-controlled value', () => {
    const serialized = serializeInstallConfig(settings);

    expect(serialized).toContain('format=1\n');
    expect(serialized).toContain(`install_dir_base64=${Buffer.from(settings.installDir).toString('base64')}\n`);
    expect(serialized).toContain(`llm_provider_base64=${Buffer.from(settings.llmProvider).toString('base64')}\n`);
    expect(serialized).toContain(`llm_base_url_base64=${Buffer.from(settings.llmBaseUrl).toString('base64')}\n`);
    expect(serialized).toContain(`llm_model_base64=${Buffer.from(settings.llmModel).toString('base64')}\n`);
    expect(serialized).toContain(`openrouter_api_key_base64=${Buffer.from(settings.openrouterApiKey ?? '').toString('base64')}\n`);
    expect(serialized).toContain(`embed_image_base64=${Buffer.from(settings.embedImage).toString('base64')}\n`);
    expect(serialized).toContain(`embed_ngl_base64=${Buffer.from(settings.embedNgl).toString('base64')}\n`);
    expect(serialized).not.toContain(settings.installDir);
    expect(serialized).not.toContain(settings.openrouterApiKey);
  });

  // install.sh's parse_install_config reads these two RAW and compares them with a literal
  // `= "true"` -- base64 would make that comparison false and silently produce a
  // non-appliance install, so this is the one pair the emitter must leave untouched.
  it('leaves appliance and gvisor unencoded', () => {
    const serialized = serializeInstallConfig(settings);
    const disabled = serializeInstallConfig({ ...settings, appliance: false, gvisor: false });

    expect(serialized).toContain('appliance=true\n');
    expect(serialized).toContain('gvisor=true\n');
    expect(disabled).toContain('appliance=false\n');
    expect(disabled).toContain('gvisor=false\n');
    expect(serialized).not.toContain(Buffer.from('true').toString('base64'));
  });

  it('uses an empty value, not the base64 of an empty string, when there is no api key', () => {
    const serialized = serializeInstallConfig({ ...settings, openrouterApiKey: undefined });

    expect(serialized).toContain('openrouter_api_key_base64=\n');
  });

  // install.sh's parse_install_config exits 2 on any key it does not name (scripts/
  // install.sh:271-287), so a test that only checks "install_dir is present" would pass
  // just as happily with a tenth key riding along that breaks the real installer.
  it('emits exactly the nine keys install.sh accepts', () => {
    const keys = serializeInstallConfig(settings).split('\n').filter(Boolean).slice(1)
      .map((l) => l.split('=')[0]).sort();
    expect(keys).toEqual([
      'appliance', 'embed_image_base64', 'embed_ngl_base64', 'gvisor', 'install_dir_base64',
      'llm_base_url_base64', 'llm_model_base64', 'llm_provider_base64',
      'openrouter_api_key_base64',
    ]);
  });

  // GNU `base64` wraps output at 76 columns; a wrapped value would insert an extra line
  // and break install.sh's key=value reader. Buffer.toString('base64') never wraps, so this
  // documents a property the emitter depends on rather than one it implements -- a 400-char
  // value is long enough that a wrapping encoder would have split it.
  it('does not wrap a long base64 value across lines', () => {
    const longSecret = 'x'.repeat(400);
    const serialized = serializeInstallConfig({ ...settings, openrouterApiKey: longSecret });
    const lines = serialized.split('\n');

    expect(Buffer.from(longSecret, 'utf8').toString('base64').length).toBeGreaterThan(76);
    // format=1 + 9 keys + the trailing '' from the join's final element = 11, regardless of
    // any single value's length -- a wrapped value would insert an extra line and break this.
    expect(lines).toHaveLength(11);
    expect(lines.filter((line) => line.startsWith('openrouter_api_key_base64=')).length).toBe(1);
  });

  // A decoded value carrying \n makes install.sh's set_env_value write two .env lines; its
  // own reader takes the first, docker compose takes the last, so the installer and the
  // running appliance would trust different values. Failing here, on the machine running
  // the wizard, is strictly better than failing after the config has crossed to the target.
  it('rejects a config value containing a line break', () => {
    expect(() => serializeInstallConfig({ ...settings, llmModel: 'a\nOPENROUTER_API_KEY=x' }))
      .toThrow(ValidationError);
  });
});

describe('createTemporaryInstallConfig', () => {
  it('creates a private file and removes its private directory', async () => {
    const root = await mkdtemp(join(tmpdir(), 'create-aura-test-'));
    roots.push(root);

    const temporary = await createTemporaryInstallConfig(settings, root);
    const contents = await readFile(temporary.path, 'utf8');

    expect(temporary.path.startsWith(root)).toBe(true);
    expect(contents).not.toContain(settings.openrouterApiKey);

    if (process.platform !== 'win32') {
      expect((await stat(temporary.directory)).mode & 0o777).toBe(0o700);
      expect((await stat(temporary.path)).mode & 0o777).toBe(0o600);
    }

    await temporary.cleanup();
    await expect(stat(temporary.directory)).rejects.toMatchObject({ code: 'ENOENT' });
    await expect(temporary.cleanup()).resolves.toBeUndefined();
  });

  // serializeInstallConfig runs inside the try block (it builds writeFile's second
  // argument), so a value it rejects exercises the same cleanup-on-throw path a disk error
  // would: the mkdtemp'd directory must not survive a failed write.
  it('removes its temporary directory and rethrows when serialization fails', async () => {
    const root = await mkdtemp(join(tmpdir(), 'create-aura-test-'));
    roots.push(root);

    await expect(
      createTemporaryInstallConfig({ ...settings, llmModel: 'a\nb' }, root),
    ).rejects.toThrow(ValidationError);
    await expect(readdir(root)).resolves.toHaveLength(0);
  });
});
