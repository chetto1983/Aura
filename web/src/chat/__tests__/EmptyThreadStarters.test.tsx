import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import i18n from '../../i18n/i18n';
import { AppShell } from '../../AppShell';

const conversation = {
  ID: 'conv-1',
  Title: 'Empty thread',
  TitleSet: true,
  IdentityID: 'op',
  Status: 'active',
  Model: 'm',
  TotalInputTokens: 0,
  TotalOutputTokens: 0,
  TotalCachedTokens: 0,
  TotalCostUSD: 0,
  CreatedAt: '2026-07-16T00:00:00Z',
};

const ASYNC_TIMEOUT_MS = 120000;

const COPY = {
  en: [
    ['Research a topic', 'Research [topic] and compare the most reliable sources.'],
    ['Analyze a file', "Analyze the file I'll attach and summarize the key findings."],
    ['Create an artifact', 'Create a [report/table/document] about [topic].'],
    ['Automate a task', 'Help me plan and automate [repeatable task].'],
  ],
  it: [
    ['Cerca un argomento', 'Cerca [argomento] e confronta le fonti più affidabili.'],
    ['Analizza un file', 'Analizza il file che allegherò e riassumi i risultati principali.'],
    ['Crea un artefatto', 'Crea un [rapporto/tabella/documento] su [argomento].'],
    ["Automatizza un'attività", 'Aiutami a pianificare e automatizzare [attività ripetitiva].'],
  ],
} as const;

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function requestURL(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
}

function renderShell(requests: string[]) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = requestURL(input);
      requests.push(url);
      if (url === '/threads/conv-1/messages') {
        return Promise.resolve(json({ type: 'MESSAGES_SNAPSHOT', messages: [] }));
      }
      if (url === '/api/composer/skills') return Promise.resolve(json({ skills: [] }));
      if (url === '/api/voice/capabilities') {
        return Promise.resolve(json({ tts: false, stt: false }));
      }
      if (url === '/api/onboarding/status') return Promise.resolve(json({ required: false }));
      if (url.includes('/rot-events') || url.includes('/api/approvals')) {
        return Promise.resolve(json([]));
      }
      if (url.includes('/api/conversations/search')) return Promise.resolve(json([]));
      if (url === '/api/conversations/conv-1') return Promise.resolve(json(conversation));
      if (url.startsWith('/api/conversations')) return Promise.resolve(json([conversation]));
      return Promise.resolve(json({}));
    }),
  );
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/c/conv-1']}>
        <Routes>
          <Route path="/" element={<AppShell />} />
          <Route path="/c/:id" element={<AppShell />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(async () => {
  vi.unstubAllGlobals();
  await act(async () => {
    await i18n.changeLanguage('en');
  });
});

describe('EmptyThreadStarters', () => {
  it(
    'renders the exact English starters and replaces an editable focused draft without side effects',
    async () => {
      await i18n.changeLanguage('en');
      const requests: string[] = [];
      renderShell(requests);

      const suggestions = await screen.findByLabelText('Suggestions', {}, { timeout: 10000 });
      const buttons = within(suggestions).getAllByRole('button');
      expect(buttons.map((button) => button.textContent)).toEqual(COPY.en.map(([label]) => label));
      expect(suggestions.className).toContain('grid-cols-1');
      expect(suggestions.className).toContain('sm:grid-cols-2');
      for (const button of buttons) {
        expect(button.tagName).toBe('BUTTON');
        expect(button.getAttribute('type')).toBe('button');
        expect(button.className).toContain('min-h-11');
        expect(button.className).toContain('min-w-0');
        expect(button.className).toContain('whitespace-normal');
        expect(button.className).toContain('text-start');
      }
      const emptyBody = screen.getByText('Type a prompt below to start this run.');
      expect(
        emptyBody.compareDocumentPosition(suggestions) & Node.DOCUMENT_POSITION_FOLLOWING,
      ).not.toBe(0);

      const input = screen.getByPlaceholderText('Ask Aura');
      fireEvent.click(screen.getByRole('button', { name: COPY.en[0][0] }));
      await waitFor(() => {
        expect(input).toHaveProperty('value', COPY.en[0][1]);
        expect(document.activeElement).toBe(input);
      });
      fireEvent.change(input, { target: { value: 'temporary edit' } });
      expect(input).toHaveProperty('value', 'temporary edit');
      fireEvent.click(screen.getByRole('button', { name: COPY.en[0][0] }));
      await waitFor(() => {
        expect(input).toHaveProperty('value', COPY.en[0][1]);
      });

      const fileInput = document.querySelector('input[type="file"]');
      if (!(fileInput instanceof HTMLInputElement)) throw new Error('expected attachment picker');
      const pickerClicks = vi.fn();
      fileInput.addEventListener('click', pickerClicks);
      for (const [label, body] of COPY.en.slice(1)) {
        fireEvent.click(screen.getByRole('button', { name: label }));
        await waitFor(() => {
          expect(input).toHaveProperty('value', body);
        });
      }
      expect(pickerClicks).not.toHaveBeenCalled();
      expect(requests.filter((url) => url.includes('/agent/run'))).toEqual([]);
    },
    ASYNC_TIMEOUT_MS,
  );

  it(
    'renders every exact Italian label and inserts every exact Italian body',
    async () => {
      await i18n.changeLanguage('it');
      const requests: string[] = [];
      renderShell(requests);

      const input = await screen.findByPlaceholderText('Chiedi ad Aura', {}, { timeout: 10000 });
      for (const [label, body] of COPY.it) {
        fireEvent.click(screen.getByRole('button', { name: label }));
        await waitFor(() => {
          expect(input).toHaveProperty('value', body);
        });
      }
      expect(requests.filter((url) => url.includes('/agent/run'))).toEqual([]);
    },
    ASYNC_TIMEOUT_MS,
  );
});
