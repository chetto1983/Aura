import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n';
import SettingsWorkspace from '../SettingsWorkspace';

describe('SettingsWorkspace', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('mounts the runtime model settings workspace', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(JSON.stringify({ restart_required: false, settings: [] }), {
            status: 200,
          }),
        ),
      ),
    );

    render(<SettingsWorkspace onCreateIdentity={vi.fn()} />);

    expect(await screen.findByRole('heading', { name: 'Runtime settings' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Model routing' })).toBeTruthy();
  });

  it('exposes identity creation as a settings action', async () => {
    const onCreateIdentity = vi.fn();
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(JSON.stringify({ restart_required: false, settings: [] }), {
            status: 200,
          }),
        ),
      ),
    );

    render(<SettingsWorkspace onCreateIdentity={onCreateIdentity} />);

    fireEvent.click(await screen.findByRole('button', { name: 'Create identity' }));

    expect(onCreateIdentity).toHaveBeenCalledOnce();
  });
});
