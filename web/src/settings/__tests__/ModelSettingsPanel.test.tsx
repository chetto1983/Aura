import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import { ModelSettingsPanel } from '../ModelSettingsPanel';

const SETTINGS_BODY = {
  restart_required: true,
  settings: [
    {
      key: 'AURA_LLM_MODEL',
      label: 'Primary LLM model',
      kind: 'string',
      secret: false,
      value: 'deepseek/deepseek-v4-flash:nitro',
      has_value: true,
      overridden: true,
    },
    {
      key: 'AURA_LLM_BASE_URL',
      label: 'Primary LLM base URL',
      kind: 'string',
      secret: false,
      value: 'https://openrouter.ai/api/v1',
      has_value: true,
      overridden: false,
    },
    {
      key: 'OPENROUTER_API_KEY',
      label: 'OpenRouter API key',
      kind: 'string',
      secret: true,
      value: 'sk-should-never-render',
      has_value: true,
      overridden: true,
    },
    {
      key: 'AURA_LLM_MAX_TOKENS',
      label: 'Max response tokens',
      kind: 'int',
      secret: false,
      value: '4096',
      has_value: true,
      overridden: false,
    },
    {
      key: 'AURA_MODEL_CONTEXT_WINDOW',
      label: 'Context window tokens',
      kind: 'int',
      secret: false,
      value: '1000000',
      has_value: true,
      overridden: false,
    },
    {
      key: 'AURA_MODEL_MAX_OUTPUT_TOKENS',
      label: 'Reserved output tokens',
      kind: 'int',
      secret: false,
      value: '32768',
      has_value: true,
      overridden: false,
    },
  ],
};

const LOAD_ERROR_TEXT = "Couldn't load runtime settings. Check the server and try again.";

interface RawSetting {
  readonly key: string;
  readonly label?: string;
  readonly kind?: 'string' | 'bool' | 'int';
  readonly secret?: boolean;
  readonly value?: string;
  readonly has_value?: boolean;
  readonly overridden?: boolean;
}

function buildItem(raw: RawSetting) {
  return {
    key: raw.key,
    label: raw.label ?? raw.key,
    kind: raw.kind ?? 'string',
    secret: raw.secret ?? false,
    value: raw.value ?? '',
    has_value: raw.has_value ?? false,
    overridden: raw.overridden ?? false,
  };
}

function settingsBody(items: ReturnType<typeof buildItem>[], restartRequired = false) {
  return { restart_required: restartRequired, settings: items };
}

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function urlOf(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
}

async function expectResetClears(targetKey: string) {
  const calls: string[] = [];
  let getCount = 0;
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = urlOf(input);
      const method = init?.method ?? 'GET';
      calls.push(`${method} ${url}`);
      if (method === 'DELETE') {
        return Promise.resolve(
          jsonResponse({ key: targetKey, deleted: true, restart_required: false }),
        );
      }
      getCount += 1;
      return Promise.resolve(
        jsonResponse(
          settingsBody([
            buildItem({ key: targetKey, value: 'x', has_value: true, overridden: getCount === 1 }),
          ]),
        ),
      );
    }),
  );

  render(<ModelSettingsPanel />);
  await screen.findByRole('heading', { name: 'Model routing' });
  fireEvent.click(screen.getByRole('button', { name: 'Reset' }));
  await waitFor(() => {
    expect(screen.queryByRole('button', { name: 'Reset' })).toBeNull();
  });
  expect(calls).toContain(`DELETE /api/settings/${targetKey}`);
}

describe('ModelSettingsPanel', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('edits cloud/local model routing without rendering secret values', async () => {
    const calls: { url: string; init: RequestInit | undefined }[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url =
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        calls.push({ url, init });
        if (init?.method === 'PUT') {
          return Promise.resolve(new Response('{"ok":true}', { status: 200 }));
        }
        return Promise.resolve(
          new Response(JSON.stringify(SETTINGS_BODY), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        );
      }),
    );

    render(<ModelSettingsPanel onComplete={vi.fn()} />);

    expect(await screen.findByRole('heading', { name: 'Model routing' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Cloud' }).getAttribute('aria-pressed')).toBe('true');
    expect(screen.queryByDisplayValue('sk-should-never-render')).toBeNull();
    expect(screen.queryByText('sk-should-never-render')).toBeNull();
    expect(screen.getAllByText('Configured').length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole('button', { name: 'Local' }));
    fireEvent.change(screen.getByLabelText('Primary model'), {
      target: { value: 'qwen2.5-coder:14b' },
    });
    fireEvent.change(screen.getByLabelText('Primary base URL'), {
      target: { value: 'http://aura-vllm-chat:8000/v1' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save runtime settings' }));

    await waitFor(() => {
      expect(calls.some((call) => call.url === '/api/settings/AURA_LLM_BASE_URL')).toBe(true);
      expect(calls.some((call) => call.url === '/api/settings/AURA_LLM_MODEL')).toBe(true);
    });
    const baseURLCall = calls.find((call) => call.url === '/api/settings/AURA_LLM_BASE_URL');
    expect(baseURLCall?.init?.body).toBe(
      JSON.stringify({ value: 'http://aura-vllm-chat:8000/v1' }),
    );
  });

  it('recovers from a load error via retry and surfaces a failing reload', async () => {
    let getCount = 0;
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = urlOf(input);
        if (url === '/api/settings') {
          getCount += 1;
          if (getCount <= 2) {
            return Promise.resolve(new Response('boom', { status: 500 }));
          }
          return Promise.resolve(jsonResponse(SETTINGS_BODY));
        }
        return Promise.resolve(jsonResponse(SETTINGS_BODY));
      }),
    );

    render(<ModelSettingsPanel />);

    expect(await screen.findByText(LOAD_ERROR_TEXT)).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => {
      expect(getCount).toBe(2);
    });
    expect(await screen.findByText(LOAD_ERROR_TEXT)).toBeTruthy();

    fireEvent.click(await screen.findByRole('button', { name: 'Retry' }));
    expect(await screen.findByRole('heading', { name: 'Model routing' })).toBeTruthy();
  });

  it('toggles the cloud provider, edits secret and boolean fields, then saves', async () => {
    const calls: { url: string; method: string; body: string | undefined }[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url = urlOf(input);
        const method = init?.method ?? 'GET';
        calls.push({ url, method, body: typeof init?.body === 'string' ? init.body : undefined });
        if (method === 'PUT') {
          return Promise.resolve(jsonResponse({ ok: true }));
        }
        return Promise.resolve(jsonResponse(SETTINGS_BODY));
      }),
    );

    render(<ModelSettingsPanel onComplete={vi.fn()} />);
    await screen.findByRole('heading', { name: 'Model routing' });

    fireEvent.click(screen.getByRole('button', { name: 'Cloud' }));
    fireEvent.change(screen.getByLabelText('OpenRouter API key'), {
      target: { value: 'sk-or-newkey' },
    });
    // Toggle the boolean field through both states (on -> off -> on) so the
    // checkbox onChange covers both the 'true' and 'false' branches; the final
    // odd click leaves it enabled for the save assertion below.
    const visionCheckbox = screen.getByRole('checkbox');
    fireEvent.click(visionCheckbox);
    fireEvent.click(visionCheckbox);
    fireEvent.click(visionCheckbox);
    fireEvent.click(screen.getByRole('button', { name: 'Save runtime settings' }));

    expect(await screen.findByText('Runtime settings saved.')).toBeTruthy();
    const puts = calls.filter((call) => call.method === 'PUT');
    expect(
      puts.some(
        (call) =>
          call.url === '/api/settings/OPENROUTER_API_KEY' &&
          call.body === JSON.stringify({ value: 'sk-or-newkey' }),
      ),
    ).toBe(true);
    expect(
      puts.some(
        (call) =>
          call.url === '/api/settings/AURA_VISION_CLOUD' &&
          call.body === JSON.stringify({ value: 'true' }),
      ),
    ).toBe(true);
  });

  it('invokes onComplete from Continue and Skip when nothing changed', async () => {
    const onComplete = vi.fn();
    const calls: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        calls.push(`${init?.method ?? 'GET'} ${urlOf(input)}`);
        return Promise.resolve(jsonResponse(SETTINGS_BODY));
      }),
    );

    render(<ModelSettingsPanel onComplete={onComplete} />);
    await screen.findByRole('heading', { name: 'Model routing' });

    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    await waitFor(() => {
      expect(onComplete).toHaveBeenCalledTimes(1);
    });

    fireEvent.click(screen.getByRole('button', { name: 'Skip runtime setup' }));
    await waitFor(() => {
      expect(onComplete).toHaveBeenCalledTimes(2);
    });

    expect(calls.filter((call) => call.startsWith('PUT')).length).toBe(0);
  });

  it('surfaces the error state when a save request fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
        if ((init?.method ?? 'GET') === 'PUT') {
          return Promise.resolve(new Response('nope', { status: 500 }));
        }
        return Promise.resolve(jsonResponse(SETTINGS_BODY));
      }),
    );

    render(<ModelSettingsPanel />);
    await screen.findByRole('heading', { name: 'Model routing' });

    fireEvent.change(screen.getByLabelText('Primary model'), {
      target: { value: 'qwen2.5-coder:14b' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save runtime settings' }));

    expect(await screen.findByText(LOAD_ERROR_TEXT)).toBeTruthy();
  });

  it('resets an overridden setting, shows a spinner, and ignores concurrent resets', async () => {
    let resolveDelete: ((response: Response) => void) | undefined;
    const deletePromise = new Promise<Response>((resolve) => {
      resolveDelete = resolve;
    });
    const calls: string[] = [];
    let getCount = 0;
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url = urlOf(input);
        const method = init?.method ?? 'GET';
        calls.push(`${method} ${url}`);
        if (method === 'DELETE') {
          return deletePromise;
        }
        getCount += 1;
        return Promise.resolve(jsonResponse(SETTINGS_BODY));
      }),
    );

    render(<ModelSettingsPanel />);
    await screen.findByRole('heading', { name: 'Model routing' });

    const resetButtons = screen.getAllByRole('button', { name: 'Reset' });
    expect(resetButtons.length).toBeGreaterThanOrEqual(2);
    const [firstReset, secondReset] = resetButtons;
    if (firstReset === undefined || secondReset === undefined) {
      throw new Error('expected at least two Reset buttons');
    }

    fireEvent.click(firstReset);
    expect(screen.getByTestId('spinner')).toBeTruthy();
    expect(firstReset.hasAttribute('disabled')).toBe(true);

    fireEvent.click(secondReset);
    expect(calls.filter((call) => call.startsWith('DELETE')).length).toBe(1);

    resolveDelete?.(
      jsonResponse({ key: 'AURA_LLM_MODEL', deleted: true, restart_required: false }),
    );
    await waitFor(() => {
      expect(getCount).toBe(2);
    });
  });

  it('resets an overridden token setting', async () => {
    await expectResetClears('AURA_LLM_MAX_TOKENS');
  });

  it('resets an overridden backend setting', async () => {
    await expectResetClears('AURA_EMBED_BASE_URL');
  });

  it('shows the saving spinner and ignores clicks while a save is in flight', async () => {
    let resolvePut: ((response: Response) => void) | undefined;
    const putPromise = new Promise<Response>((resolve) => {
      resolvePut = resolve;
    });
    const calls: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const method = init?.method ?? 'GET';
        calls.push(`${method} ${urlOf(input)}`);
        if (method === 'PUT') {
          return putPromise;
        }
        return Promise.resolve(jsonResponse(SETTINGS_BODY));
      }),
    );

    render(<ModelSettingsPanel />);
    await screen.findByRole('heading', { name: 'Model routing' });

    fireEvent.change(screen.getByLabelText('Primary model'), {
      target: { value: 'local-model' },
    });
    const saveButton = screen.getByRole('button', { name: 'Save runtime settings' });
    fireEvent.click(saveButton);
    expect(saveButton.getAttribute('aria-busy')).toBe('true');
    expect(screen.getByTestId('spinner')).toBeTruthy();

    fireEvent.click(saveButton);

    resolvePut?.(jsonResponse({ ok: true }));
    expect(await screen.findByText('Runtime settings saved.')).toBeTruthy();
    expect(calls.filter((call) => call.startsWith('PUT')).length).toBe(1);
  });

  it('skips the ready state when a settings load resolves after unmount', async () => {
    let resolveGet: ((response: Response) => void) | undefined;
    const getPromise = new Promise<Response>((resolve) => {
      resolveGet = resolve;
    });
    vi.stubGlobal(
      'fetch',
      vi.fn(() => getPromise),
    );

    const { unmount } = render(<ModelSettingsPanel />);
    unmount();
    await act(async () => {
      resolveGet?.(jsonResponse(SETTINGS_BODY));
      await getPromise;
    });

    expect(screen.queryByRole('heading', { name: 'Model routing' })).toBeNull();
  });

  it('skips the error state when a settings load fails after unmount', async () => {
    let rejectGet: ((reason: Error) => void) | undefined;
    const getPromise = new Promise<Response>((_resolve, reject) => {
      rejectGet = reject;
    });
    vi.stubGlobal(
      'fetch',
      vi.fn(() => getPromise),
    );

    const { unmount } = render(<ModelSettingsPanel />);
    unmount();
    await act(async () => {
      rejectGet?.(new Error('boom'));
      await getPromise.catch(() => undefined);
    });

    expect(screen.queryByText(LOAD_ERROR_TEXT)).toBeNull();
  });
});
