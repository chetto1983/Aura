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
};

const roots: string[] = [];

afterEach(async () => {
  await Promise.all(roots.splice(0).map((root) => rm(root, { recursive: true, force: true })));
});

describe('serializeInstallConfig', () => {
  // Format 2 carries infrastructure only. The model route, the model and the OpenRouter
  // management key are chosen by an admin in the first-run web setup, so no credential
  // crosses to the install target any more.
  it('writes format 2 with the install directory base64-encoded', () => {
    expect(serializeInstallConfig(settings)).toBe(
      `format=2\ninstall_dir_base64=${Buffer.from('/opt/aura').toString('base64')}\nappliance=true\ngvisor=true\n`,
    );
  });

  // install.sh's parse_install_config reads these two RAW and compares them with a literal
  // `= "true"` -- base64 would make that comparison false and silently produce a
  // non-appliance install, so this is the one pair the emitter must leave untouched.
  it('leaves appliance and gvisor unencoded', () => {
    const disabled = serializeInstallConfig({ ...settings, appliance: false, gvisor: false });

    expect(disabled).toContain('appliance=false\n');
    expect(disabled).toContain('gvisor=false\n');
    expect(serializeInstallConfig(settings)).not.toContain(Buffer.from('true').toString('base64'));
  });

  // install.sh's parse_install_config exits 2 on any key it does not name, so a test that
  // only checked for the install dir would pass just as happily with an extra key riding
  // along that breaks the real installer.
  it('emits exactly the three keys install.sh accepts', () => {
    const keys = serializeInstallConfig(settings).split('\n').filter(Boolean).slice(1)
      .map((l) => l.split('=')[0]).sort();
    expect(keys).toEqual(['appliance', 'gvisor', 'install_dir_base64']);
  });

  // install.sh detects the embed backend on the target, and the web setup owns the model route
  // and the key, so the wizard has neither an embed_ nor an llm_ answer to hand over.
  it('emits no embed_, llm_ or openrouter_ key', () => {
    expect(serializeInstallConfig(settings)).not.toMatch(/^(embed|llm|openrouter)_/m);
  });

  // GNU `base64` wraps output at 76 columns; a wrapped value would insert an extra line and
  // break install.sh's key=value reader. Buffer.toString('base64') never wraps, so this
  // documents a property the emitter depends on rather than one it implements.
  it('does not wrap a long base64 value across lines', () => {
    const longDir = `/opt/${'x'.repeat(400)}`;
    const lines = serializeInstallConfig({ ...settings, installDir: longDir }).split('\n');

    expect(Buffer.from(longDir, 'utf8').toString('base64').length).toBeGreaterThan(76);
    // format=2 + 3 keys + the trailing '' from the join's final element.
    expect(lines).toHaveLength(5);
  });

  // A decoded value carrying \n makes install.sh's set_env_value write two .env lines; its
  // own reader takes the first, docker compose takes the last. Failing here, on the machine
  // running the wizard, is strictly better than failing after the config crossed to the target.
  it('rejects a config value containing a line break', () => {
    expect(() => serializeInstallConfig({ ...settings, installDir: '/opt/a\nAURA_IMAGE=x' }))
      .toThrow(ValidationError);
  });
});

describe('createTemporaryInstallConfig', () => {
  it('creates a private file and removes its private directory', async () => {
    const root = await mkdtemp(join(tmpdir(), 'create-aura-test-'));
    roots.push(root);

    const temporary = await createTemporaryInstallConfig(settings, root);

    expect(temporary.path.startsWith(root)).toBe(true);
    expect(await readFile(temporary.path, 'utf8')).toBe(serializeInstallConfig(settings));

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
      createTemporaryInstallConfig({ ...settings, installDir: '/opt/a\nb' }, root),
    ).rejects.toThrow(ValidationError);
    await expect(readdir(root)).resolves.toHaveLength(0);
  });
});
