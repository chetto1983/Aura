import { afterEach, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import SettingsWorkspace from '../SettingsWorkspace';

afterEach(() => {
  vi.unstubAllGlobals();
});

it('lets a member connect their own ChatGPT account without exposing provider or model administration', async () => {
  const fetchMock = vi.fn((input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    const json =
      url === '/api/me'
        ? { identity_id: 'member', capabilities: ['agent.run', 'governance.write'] }
        : url === '/api/settings/chatgpt/status'
          ? { connected: false, email: '', plan_enabled: false, status: 'disconnected' }
          : {};
    return Promise.resolve(new Response(JSON.stringify(json), { status: 200 }));
  });
  vi.stubGlobal('fetch', fetchMock);
  render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <SettingsWorkspace onCreateIdentity={vi.fn()} />
    </QueryClientProvider>,
  );
  await screen.findByRole('heading', { name: 'Your ChatGPT account' });
  await screen.findByRole('button', { name: 'Continue with ChatGPT' });
  expect(screen.queryByRole('radiogroup', { name: 'Primary model provider' })).toBeNull();
  expect(screen.queryByRole('combobox', { name: 'Primary model' })).toBeNull();
  expect(fetchMock.mock.calls.some(([input]) => input === '/api/settings')).toBe(false);
});
