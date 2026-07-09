import { afterEach, describe, expect, it, vi } from 'vitest';
import { type ReactNode } from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n'; // side-effect: initialise i18next so the panel's t() keys resolve
import type { Asset } from '../attachments/types';
import { listThreadAssets } from '../attachments/api';
import { downloadAll } from './downloadAll';
import { ArtifactsPanel } from './ArtifactsPanel';

// ArtifactsPanel (D-13/D-17): the container. listThreadAssets is mocked so the REAL
// useThreadArtifacts query runs (proving the agent-only, newest-first projection
// end-to-end through a QueryClientProvider); downloadAll is mocked to a controllable
// promise (to pin the invoke + disable-during-run + N/M progress); PreviewModal is
// mocked to a marker so open/close is exercised without loading the heavy renderer
// chunk graph.

vi.mock('../attachments/api', () => ({ listThreadAssets: vi.fn() }));
vi.mock('./downloadAll', () => ({ downloadAll: vi.fn() }));
vi.mock('./PreviewModal', () => ({
  PreviewModal: ({ active, onClose }: { active?: Asset; onClose: () => void }) =>
    active ? (
      <div data-testid="preview-modal">
        <span>{active.file_name}</span>
        <button type="button" onClick={onClose}>
          close-modal
        </button>
      </div>
    ) : null,
}));

const mockList = vi.mocked(listThreadAssets);
const mockDownloadAll = vi.mocked(downloadAll);

function asset(over: Partial<Asset>): Asset {
  return {
    id: 'a',
    status: 'accepted',
    modality: 'document',
    file_name: 'f.docx',
    mime_type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    declared_size_bytes: 10,
    size_bytes: 10,
    source_kind: 'agent',
    ...over,
  };
}

// ASC server order (oldest first) — the panel must render newest-first.
const ASC_LIST: readonly Asset[] = [
  asset({ id: 'old', file_name: 'old.docx', created_at: '2026-01-01T00:00:00Z' }),
  asset({ id: 'new', file_name: 'new.docx', created_at: '2026-01-09T00:00:00Z' }),
];

function renderPanel(threadId = 't-1') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const onClose = vi.fn();
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }
  const utils = render(<ArtifactsPanel threadId={threadId} onClose={onClose} />, {
    wrapper: Wrapper,
  });
  return { ...utils, onClose };
}

describe('ArtifactsPanel', () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders rows newest-first from the ASC server list', async () => {
    mockList.mockResolvedValue([...ASC_LIST]);
    const { container } = renderPanel();
    await waitFor(() => {
      expect(container.querySelectorAll('li')).toHaveLength(2);
    });
    const items = [...container.querySelectorAll('li')];
    expect(items[0]?.textContent).toContain('new.docx');
    expect(items[1]?.textContent).toContain('old.docx');
  });

  it('shows a graceful empty-state when the thread has no artifacts', async () => {
    mockList.mockResolvedValue([]);
    renderPanel();
    await waitFor(() => {
      expect(screen.getByText(/no artifacts in this conversation/i)).toBeTruthy();
    });
    expect(screen.getByText(/files the agent delivers appear here/i)).toBeTruthy();
  });

  it('calls onClose from the header toggle', async () => {
    mockList.mockResolvedValue([]);
    const { onClose } = renderPanel();
    await waitFor(() => {
      expect(screen.getByText(/no artifacts/i)).toBeTruthy();
    });
    fireEvent.click(screen.getByRole('button', { name: /toggle the artifacts panel/i }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('invokes downloadAll and disables the button + shows N/M progress during the run', async () => {
    mockList.mockResolvedValue([...ASC_LIST]);
    // A controllable run: it reports 1/2 progress then parks until we release it.
    let release!: () => void;
    mockDownloadAll.mockImplementation((_assets, onProgress) => {
      onProgress(1, 2);
      return new Promise<void>((resolve) => {
        release = resolve;
      });
    });

    renderPanel();
    // The button is disabled until the query resolves (no accepted rows yet).
    const btn = await screen.findByRole('button', { name: /download all/i });
    await waitFor(() => {
      expect(btn).toHaveProperty('disabled', false);
    });
    fireEvent.click(btn);

    await waitFor(() => {
      expect(mockDownloadAll).toHaveBeenCalledTimes(1);
    });
    // downloadAll received the full row set (it skips degraded rows internally).
    expect(mockDownloadAll.mock.calls[0]?.[0]).toHaveLength(2);

    // Disabled + N/M progress while the run is in flight.
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /downloading 1 of 2/i })).toBeTruthy();
    });
    expect(screen.getByRole('button', { name: /downloading 1 of 2/i })).toHaveProperty(
      'disabled',
      true,
    );

    release();
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /download all/i })).toHaveProperty(
        'disabled',
        false,
      );
    });
  });

  it('disables "Scarica tutto" when there are no accepted rows', async () => {
    mockList.mockResolvedValue([asset({ id: 'p', status: 'processing' })]);
    renderPanel();
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /download all/i })).toHaveProperty(
        'disabled',
        true,
      );
    });
    expect(mockDownloadAll).not.toHaveBeenCalled();
  });

  it('opens the lazy PreviewModal on a row click and closes it', async () => {
    mockList.mockResolvedValue([...ASC_LIST]);
    const { container } = renderPanel();
    await waitFor(() => {
      expect(container.querySelectorAll('li')).toHaveLength(2);
    });
    // Click the newest row's preview body (first li).
    const firstRow = container.querySelector('li');
    if (firstRow === null) throw new Error('no row rendered');
    fireEvent.click(within(firstRow as HTMLElement).getByRole('button'));

    const modal = await screen.findByTestId('preview-modal');
    expect(modal.textContent).toContain('new.docx');

    fireEvent.click(screen.getByRole('button', { name: 'close-modal' }));
    await waitFor(() => {
      expect(screen.queryByTestId('preview-modal')).toBeNull();
    });
  });
});
