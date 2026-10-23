import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { EmbeddingSpacePanel } from '../EmbeddingSpacePanel';

function respond(status: number, body: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify(body), {
          status,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    ),
  );
}

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <EmbeddingSpacePanel />
    </QueryClientProvider>,
  );
}

const tally = (type: string, other = 0, rejected = 0) => ({
  type,
  in_space: 10,
  other_space: other,
  no_vector: 1,
  rejected,
});

describe('EmbeddingSpacePanel', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('starts closed when every family is dense, with the gate in its header', async () => {
    respond(200, {
      space: 'es1-e0aa6accf0b79c6b',
      space_label: 'local embeddinggemma-300M-Q8_0.gguf, 768d, recipe 1',
      documents_space: 'es1-e0aa6accf0b79c6b',
      documents_space_label: 'local',
      floors_calibrated: true,
      tenants: [
        {
          identity_id: 'id-1',
          families: [
            { family: 'memory', space: 'es1-a', open: true, types: [tally('FACT')] },
            { family: 'documents', space: 'es1-a', open: true, types: [tally('Passage')] },
          ],
          stuck_documents: [],
          ingest_status: 'ready',
          ingest_errors: 0,
        },
      ],
    });
    renderPanel();

    const toggle = await screen.findByRole('button', { name: /Embedding space/ });
    expect(await within(toggle).findByText('Dense')).toBeTruthy();
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByRole('table')).toBeNull();

    fireEvent.click(toggle);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByRole('table', { name: 'Memory' })).toBeTruthy();
    expect(screen.getByText(/es1-e0aa6accf0b79c6b/)).toBeTruthy();

    fireEvent.click(toggle);
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('opens by itself on a keyword-only family, and the operator can still close it', async () => {
    respond(200, {
      space: 'es1-b',
      space_label: 'local',
      documents_space: 'es1-b',
      documents_space_label: 'local',
      floors_calibrated: true,
      tenants: [
        {
          identity_id: 'id-1',
          families: [
            { family: 'documents', space: 'es1-a', open: false, types: [tally('Passage', 3)] },
          ],
          stuck_documents: [],
          ingest_status: 'ready',
          ingest_errors: 0,
        },
      ],
    });
    renderPanel();

    const toggle = await screen.findByRole('button', { name: /Embedding space/ });
    expect(await within(toggle).findByText('1 family keyword only')).toBeTruthy();
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    fireEvent.click(toggle);
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('shows each family, its gate and the documents stuck in another space', async () => {
    respond(200, {
      space: 'es1-e0aa6accf0b79c6b',
      space_label: 'local embeddinggemma-300M-Q8_0.gguf, 768d, recipe 1',
      documents_space: 'es1-e0aa6accf0b79c6b',
      documents_space_label: 'local',
      floors_calibrated: true,
      tenants: [
        {
          identity_id: 'id-1',
          families: [
            { family: 'memory', space: 'es1-a', open: true, types: [tally('FACT', 0, 2)] },
            { family: 'documents', space: 'es1-a', open: false, types: [tally('Passage', 3)] },
          ],
          stuck_documents: [{ file_name: 'scan.png', source_key: 'library/a/scan.png' }],
          ingest_status: 'ready',
          ingest_errors: 2,
        },
      ],
    });
    renderPanel();

    expect(await screen.findByText(/es1-e0aa6accf0b79c6b/)).toBeTruthy();
    const memory = screen.getByRole('table', { name: 'Memory' });
    expect(within(memory).getByRole('rowheader', { name: 'Facts' })).toBeTruthy();
    expect(screen.getByText('Dense')).toBeTruthy();
    expect(screen.getByText('Keyword only')).toBeTruthy();
    expect(within(screen.getByRole('table', { name: 'Documents' })).getByText('3')).toBeTruthy();
    expect(screen.getByText('scan.png')).toBeTruthy();
    expect(screen.getByText('The ingest reported 2 errors.')).toBeTruthy();
    expect(screen.queryByText(/uncalibrated/)).toBeNull();
  });

  it('says so when no identity has anything stored, and when the floors are uncalibrated', async () => {
    respond(200, {
      space: 'es1-cloud',
      space_label: 'openrouter vendor/embed',
      documents_space: 'es1-cloud',
      documents_space_label: '',
      floors_calibrated: false,
      tenants: [],
    });
    renderPanel();
    // Nothing stored and nothing wrong: the panel starts closed, as on a healthy box.
    fireEvent.click(await screen.findByRole('button', { name: /Embedding space/ }));
    expect(await screen.findByText('No identity has memory or documents yet.')).toBeTruthy();
    expect(
      screen.getByText(
        'Relevance thresholds are uncalibrated for this model in memory, documents or both.',
      ),
    ).toBeTruthy();
  });

  it('names why the space cannot be read', async () => {
    respond(200, { space_error: 'sidecar down', tenants: [] });
    renderPanel();
    expect((await screen.findByRole('alert')).textContent).toBe(
      'Aura cannot name its embedding space: sidecar down',
    );
  });

  it('reports a failed read', async () => {
    respond(502, { error: 'embedding space report unavailable' });
    renderPanel();
    expect((await screen.findByRole('alert')).textContent).toBe(
      'The embedding state could not be read.',
    );
  });
});
