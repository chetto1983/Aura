import type { ReactNode } from 'react';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AppShell } from '../AppShell';
import i18n from '../i18n/i18n';
import { fetchOnboardingStatus } from '../onboarding/onboardingApi';
import type { DocumentItem } from '../documents/documentApi';
import type { ComposerDraftPrompt } from '../chat/Composer';

const DOCUMENT: DocumentItem = {
  id: 'document-1',
  identity_id: 'operator-1',
  scope: 'library',
  title: 'Handbook.pdf',
  tags: [],
  metadata: {},
  status: 'ready',
};

const CONVERSATION = {
  ID: 'conv-1',
  Title: 'Empty thread',
  TitleSet: true,
  IdentityID: 'operator-1',
  Status: 'active',
  Model: 'model',
  TotalInputTokens: 0,
  TotalOutputTokens: 0,
  TotalCachedTokens: 0,
  TotalCostUSD: 0,
  CreatedAt: '2026-07-16T00:00:00Z',
};

vi.mock('../onboarding/onboardingApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../onboarding/onboardingApi')>();
  return { ...actual, fetchOnboardingStatus: vi.fn() };
});

vi.mock('../admin/useAdmin', () => ({
  useCapabilities: () => ({ isAdmin: true, isLoading: false }),
}));

vi.mock('../shell/ShellHeader', async () => {
  const { LanguageSwitcher } = await import('../i18n/LanguageSwitcher');
  return {
    ShellHeader: ({
      onModeSelect,
    }: {
      readonly onModeSelect: (mode: 'chat' | 'documents') => void;
    }) => (
      <header>
        <nav aria-label="Surface controls">
          <button
            type="button"
            onClick={() => {
              onModeSelect('chat');
            }}
          >
            Chat
          </button>
          <button
            type="button"
            onClick={() => {
              onModeSelect('documents');
            }}
          >
            Documents
          </button>
        </nav>
        <LanguageSwitcher />
      </header>
    ),
  };
});

vi.mock('../shell/RuntimeStatusChip', () => ({ RuntimeStatusChip: () => null }));
vi.mock('../approvals/ApprovalBadge', () => ({ ApprovalBadge: () => null }));
vi.mock('../theme/ThemeSwitcher', () => ({ ThemeSwitcher: () => null }));
vi.mock('../conversations/SearchPanel', () => ({ SearchPanel: () => null }));
vi.mock('../conversations/ConversationSidebar', () => ({
  ConversationSidebar: () => <p>Conversation navigation</p>,
}));
vi.mock('../shell/ChatWorkspaceControls', () => ({ ChatWorkspaceControls: () => null }));
vi.mock('../chat/RuntimeFooter', () => ({ RuntimeFooter: () => null }));
vi.mock('../shell/BottomDock', () => ({ BottomDock: () => null }));
vi.mock('../shell/Drawer', () => ({ Drawer: () => null }));
vi.mock('../shell/ArtifactsShell', () => ({
  ArtifactsDrawer: () => null,
  ArtifactsResizablePanel: () => null,
}));
vi.mock('../approvals/ThreadApprovalCards', () => ({ ThreadApprovalCards: () => null }));
vi.mock('../approvals/useThreadApprovals', () => ({
  useThreadApprovals: () => ({
    approvals: [],
    isPending: false,
    onResolutionStarted: () => undefined,
    onResolutionFailed: () => undefined,
    onResolved: () => undefined,
  }),
}));
vi.mock('../chat/composer/useComposerSkills', () => ({ useComposerSkills: () => [] }));
vi.mock('../chat/composer/useReasoningCapabilities', () => ({
  useReasoningCapabilities: () => ({ levels: ['auto', 'off'], default: 'auto', detected: false }),
}));
vi.mock('../chat/voice/useVoiceCapabilities', () => ({
  useVoiceCapabilities: () => ({ tts: false, stt: false }),
}));
vi.mock('../chat/displays/SourceExplorerContext', () => ({
  SourceExplorerProvider: ({ children }: { readonly children: ReactNode }) => children,
}));

vi.mock('../documents/DocumentsWorkspace', () => ({
  default: ({ onAskDocument }: { readonly onAskDocument?: (document: DocumentItem) => void }) => (
    <button type="button" onClick={() => onAskDocument?.(DOCUMENT)}>
      Ask Handbook.pdf
    </button>
  ),
}));

vi.mock('../chat/ExternalStoreChat', async () => {
  const { useLayoutEffect, useRef, useState } = await import('react');
  function ExternalStoreChat({
    draftPrompt,
    onDraftPromptConsumed,
    onRequestDraftPrompt,
  }: {
    readonly draftPrompt?: ComposerDraftPrompt;
    readonly onDraftPromptConsumed?: (nonce: number) => void;
    readonly onRequestDraftPrompt?: (text: string) => void;
  }) {
    const [value, setValue] = useState('');
    const [appliedNonce, setAppliedNonce] = useState<number | undefined>(undefined);
    const inputRef = useRef<HTMLTextAreaElement | null>(null);
    useLayoutEffect(() => {
      if (draftPrompt === undefined || draftPrompt.nonce === appliedNonce) return;
      setValue(draftPrompt.text);
      setAppliedNonce(draftPrompt.nonce);
      inputRef.current?.focus();
      onDraftPromptConsumed?.(draftPrompt.nonce);
    }, [appliedNonce, draftPrompt, onDraftPromptConsumed]);
    return (
      <>
        <button type="button" onClick={() => onRequestDraftPrompt?.('Starter prompt')}>
          Request starter
        </button>
        <textarea
          ref={inputRef}
          aria-label="Draft prompt"
          data-nonce={appliedNonce}
          value={value}
          onChange={(event) => {
            setValue(event.currentTarget.value);
          }}
        />
      </>
    );
  }
  return { ExternalStoreChat };
});

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function requestURL(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
}

function renderShell() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route path="*" element={<AppShell />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(async () => {
  localStorage.clear();
  await i18n.changeLanguage('it');
  vi.mocked(fetchOnboardingStatus).mockResolvedValue({
    required: false,
    completed: true,
    skipped: false,
  });
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = requestURL(input);
      if (url === '/api/conversations/conv-1') return Promise.resolve(json(CONVERSATION));
      if (url.startsWith('/api/conversations')) return Promise.resolve(json([CONVERSATION]));
      if (url.startsWith('/api/assets')) return Promise.resolve(json([]));
      if (url.includes('/rot-events') || url.includes('/api/approvals')) {
        return Promise.resolve(json([]));
      }
      if (url === '/api/composer/skills') return Promise.resolve(json({ skills: [] }));
      if (url === '/api/voice/capabilities') {
        return Promise.resolve(json({ tts: false, stt: false }));
      }
      return Promise.resolve(json({ ok: true, ready: true, deps: {} }));
    }),
  );
});

afterEach(async () => {
  vi.unstubAllGlobals();
  await act(async () => {
    await i18n.changeLanguage('en');
  });
});

describe('AppShell prompt draft integration', () => {
  it('does not resurrect an acknowledged starter prompt after the chat remounts', async () => {
    let rendered!: ReturnType<typeof renderShell>;
    await act(async () => {
      rendered = renderShell();
      await Promise.resolve();
    });
    const shell = rendered.container.firstElementChild;
    if (!(shell instanceof HTMLElement)) throw new Error('expected application shell');

    fireEvent.click(await screen.findByRole('button', { name: 'Request starter' }));
    const input = await screen.findByRole('textbox', { name: 'Draft prompt' });
    await waitFor(() => {
      expect(input).toHaveProperty('value', 'Starter prompt');
      expect(input.getAttribute('data-nonce')).toBe('1');
    });

    fireEvent.click(screen.getByRole('button', { name: 'Documents' }));
    await waitFor(() => {
      expect(shell.getAttribute('data-surface')).toBe('documents');
    });
    fireEvent.click(screen.getByRole('button', { name: 'Chat' }));
    await waitFor(() => {
      expect(shell.getAttribute('data-surface')).toBe('chat');
    });

    const remountedInput = await screen.findByRole('textbox', { name: 'Draft prompt' });
    expect(remountedInput).toHaveProperty('value', '');
    expect(remountedInput.getAttribute('data-nonce')).toBeNull();
  });

  it('uses the current locale and a fresh nonce when asking the same document twice', async () => {
    await import('../documents/DocumentsWorkspace');
    let rendered!: ReturnType<typeof renderShell>;
    await act(async () => {
      rendered = renderShell();
      await Promise.resolve();
    });
    const { container } = rendered;
    const fetchSpy = vi.mocked(fetch);
    const shell = container.firstElementChild;
    if (!(shell instanceof HTMLElement)) throw new Error('expected application shell');

    await act(async () => {
      fireEvent.click(
        within(screen.getByRole('navigation', { name: 'Surface controls' })).getByRole('button', {
          name: 'Documents',
        }),
      );
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(shell.getAttribute('data-surface')).toBe('documents');
    });
    const italianAsk = await screen.findByRole(
      'button',
      { name: 'Ask Handbook.pdf' },
      { timeout: 5000 },
    );
    await act(async () => {
      fireEvent.click(italianAsk);
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(shell.getAttribute('data-surface')).toBe('chat');
    });

    const input = await screen.findByRole('textbox', { name: 'Draft prompt' });
    expect(input).toHaveProperty(
      'value',
      'Rispondi da "Handbook.pdf". Riassumi i punti chiave e cita le evidenze del documento che usi.',
    );
    expect(input.getAttribute('data-nonce')).toBe('1');
    expect(document.activeElement).toBe(input);
    fireEvent.change(input, { target: { value: 'modifica temporanea' } });

    await act(async () => {
      fireEvent.click(
        within(screen.getByRole('navigation', { name: 'Surface controls' })).getByRole('button', {
          name: 'Documents',
        }),
      );
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(shell.getAttribute('data-surface')).toBe('documents');
    });
    const languageChanged = new Promise<void>((resolve) => {
      const onLanguageChanged = () => {
        i18n.off('languageChanged', onLanguageChanged);
        resolve();
      };
      i18n.on('languageChanged', onLanguageChanged);
    });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'English' }));
      await languageChanged;
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(document.documentElement.lang).toBe('en');
    });
    const englishAsk = await screen.findByRole('button', { name: 'Ask Handbook.pdf' });
    await act(async () => {
      fireEvent.click(englishAsk);
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(shell.getAttribute('data-surface')).toBe('chat');
    });

    const englishInput = await screen.findByRole('textbox', { name: 'Draft prompt' });
    await waitFor(() => {
      expect(englishInput).toHaveProperty(
        'value',
        'Answer from "Handbook.pdf". Summarize the key points and cite the document evidence you use.',
      );
      expect(document.activeElement).toBe(englishInput);
    });
    expect(englishInput.getAttribute('data-nonce')).toBe('2');
    const navigation = screen.getByRole('complementary', { name: 'Navigation' });
    expect(within(navigation).queryByRole('group', { name: 'Suggestions' })).toBeNull();
    expect(
      fetchSpy.mock.calls
        .map(([input]) => requestURL(input))
        .filter((url) => url.includes('/agent/run')),
    ).toEqual([]);
    await act(async () => {
      await new Promise<void>((resolve) => {
        setTimeout(resolve, 0);
      });
    });
  }, 15000);
});
