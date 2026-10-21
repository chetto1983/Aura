import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DETAILS, PAGE, renderDetail, renderDocuments, stubServer } from './documentsTestServer';
import i18n from '@/i18n/i18n';

beforeEach(async () => {
  await act(() => i18n.changeLanguage('it'));
});

afterEach(() => {
  vi.unstubAllGlobals();
});

/** openInvoice opens the invoice that waits for review from the list, by its name. */
async function openInvoice() {
  renderDocuments();
  fireEvent.click(await screen.findByRole('button', { name: 'fattura_2026_044.pdf' }));
  await screen.findByRole('heading', { name: 'fattura_2026_044.pdf' });
}

const useGialli = () => {
  fireEvent.click(screen.getByRole('button', { name: 'Usa il suggerimento Gialli Import' }));
};

describe('document detail', () => {
  it('shows each suggestion under its field, with its source and evidence', async () => {
    stubServer();
    await openInvoice();
    const counterparty = screen.getByRole('group', { name: 'Controparte' });
    expect(
      within(counterparty).getByRole('button', { name: 'Usa il suggerimento Gialli Import' }),
    ).toBeTruthy();
    expect(counterparty.textContent).toContain('estrattore');
    expect(counterparty.textContent).toContain('Cliente | Gialli Import');
    const type = screen.getByRole('group', { name: 'Tipo' });
    expect(type.textContent).toContain('regola');
    expect(type.textContent).toContain('any "FATTURA"');
    const date = screen.getByRole('group', { name: 'Data documento' });
    expect(
      within(date).getByRole('button', { name: 'Usa il suggerimento 14/04/2026' }),
    ).toBeTruthy();
  });

  it('puts a suggestion in the form with a click, without saving it', async () => {
    const { writes } = stubServer();
    await openInvoice();
    expect(screen.getByRole('button', { name: 'Salva dettagli' })).toHaveProperty('disabled', true);
    useGialli();
    expect(screen.getByRole('combobox', { name: 'Controparte' }).textContent).toContain(
      'Gialli Import',
    );
    expect(screen.getByRole('button', { name: 'Salva dettagli' })).toHaveProperty(
      'disabled',
      false,
    );
    expect(writes).toEqual([]);
  });

  it('rejects a suggestion with ×, and the rejection is written at once', async () => {
    const { writes } = stubServer();
    await openInvoice();
    fireEvent.click(screen.getByRole('button', { name: 'Rifiuta il suggerimento Gialli Import' }));
    await waitFor(() => {
      expect(writes).toHaveLength(1);
    });
    expect(writes[0]).toEqual({
      method: 'PATCH',
      path: '/api/documents/d2',
      body: { set: [], remove: [{ field: 'counterparty', value_key: 'c-gialli' }] },
    });
  });

  it('saves only the fields that changed', async () => {
    const { writes } = stubServer();
    await openInvoice();
    useGialli();
    fireEvent.change(screen.getByLabelText('Data documento'), { target: { value: '2026-04-15' } });
    fireEvent.click(screen.getByRole('button', { name: 'Salva dettagli' }));
    await waitFor(() => {
      expect(writes).toHaveLength(1);
    });
    expect(writes[0]).toEqual({
      method: 'PATCH',
      path: '/api/documents/d2',
      body: {
        set: [
          { field: 'counterparty', value_key: 'c-gialli' },
          { field: 'document_date', value_key: '2026-04-15' },
        ],
        remove: [],
      },
    });
  });

  it('"Rivisto, salva e avanti" saves, marks it reviewed and opens the next current result', async () => {
    const { writes } = stubServer();
    const original = fetch;
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url = new URL(
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url,
          location.origin,
        );
        if (url.pathname === '/api/documents')
          return Promise.resolve(
            Response.json({ ...PAGE, rows: [...PAGE.rows, DETAILS.d3], total: 3 }),
          );
        return original(input, init);
      }),
    );
    await openInvoice();
    useGialli();
    fireEvent.click(screen.getByRole('button', { name: 'Rivisto, salva e avanti' }));
    expect(await screen.findByRole('heading', { name: 'ddt_2026_118.pdf' })).toBeTruthy();
    expect(writes.map((w) => `${w.method} ${w.path}`)).toEqual([
      'PATCH /api/documents/d2',
      'POST /api/documents/d2/review',
    ]);
  });

  it('asks before closing with unsaved changes, and closes at once without them', async () => {
    stubServer();
    await openInvoice();
    useGialli();
    fireEvent.click(screen.getByRole('button', { name: 'Documenti' }));
    let ask = await screen.findByRole('dialog', { name: 'Chiudere senza salvare?' });
    fireEvent.click(within(ask).getByRole('button', { name: 'Resta' }));
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    expect(screen.getByRole('heading', { name: 'fattura_2026_044.pdf' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Annulla modifiche' }));
    fireEvent.click(screen.getByRole('button', { name: 'Documenti' }));
    expect(
      await screen.findByRole('checkbox', { name: 'Seleziona fattura_2026_031.pdf' }),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'fattura_2026_044.pdf' }));
    await screen.findByRole('heading', { name: 'fattura_2026_044.pdf' });
    useGialli();
    fireEvent.click(screen.getByRole('button', { name: 'Documenti' }));
    ask = await screen.findByRole('dialog', { name: 'Chiudere senza salvare?' });
    fireEvent.click(within(ask).getByRole('button', { name: 'Esci senza salvare' }));
    expect(
      await screen.findByRole('checkbox', { name: 'Seleziona fattura_2026_031.pdf' }),
    ).toBeTruthy();
  });

  it('saves a proposed counterparty as the name it is, for the server to make a label of', async () => {
    const { writes } = stubServer();
    renderDetail('d3');
    fireEvent.click(
      await screen.findByRole('button', { name: 'Usa il suggerimento Verdi SpA (nuova)' }),
    );
    fireEvent.click(screen.getByRole('button', { name: 'Salva dettagli' }));
    await waitFor(() => {
      expect(writes).toHaveLength(1);
    });
    expect(writes[0]).toEqual({
      method: 'PATCH',
      path: '/api/documents/d3',
      body: { set: [{ field: 'counterparty', value_key: 'new:Verdi SpA' }], remove: [] },
    });
  });

  it('opens PDF directly in Craft with a named original download', async () => {
    stubServer();
    renderDetail('d2');
    await screen.findByRole('heading', { name: 'Anteprima' });
    expect(await screen.findByRole('link', { name: 'Scarica originale' })).toBeTruthy();
  });

  it('lists the history, and says when the operator has not reviewed it yet', async () => {
    stubServer();
    renderDetail('d2');
    fireEvent.mouseDown(await screen.findByRole('tab', { name: 'Storico' }), {
      button: 0,
      ctrlKey: false,
    });
    const history = await screen.findByRole('list', { name: 'Storico' });
    const lines = within(history)
      .getAllByRole('listitem')
      .map((item) => item.textContent);
    expect(lines[0]).toContain('Tipo: Fattura');
    expect(lines[0]).toContain('impostato da regola');
    expect(lines[1]).toContain('Tag: ricambi');
    expect(lines[1]).toContain('impostato da te');
    expect(screen.getByText('Non ancora rivisto da te')).toBeTruthy();
  });
});
