import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Label } from './documentsApi';
import { LABELS, renderDocuments, stubServer } from './documentsTestServer';
import { LabelsAdmin } from './LabelsAdmin';
import i18n from '@/i18n/i18n';

beforeEach(async () => {
  await act(() => i18n.changeLanguage('it'));
});

afterEach(() => {
  vi.unstubAllGlobals();
});

async function openLists() {
  renderDocuments('/documents/lists');
  await screen.findByRole('heading', { name: 'Liste e regole', level: 1 });
}

/** entry is one value of the open list, found by its name. */
async function entry(name: string) {
  const item = (await screen.findByRole('heading', { name, level: 3 })).closest('li');
  if (item === null) throw new Error(`no entry for ${name}`);
  return item;
}

async function newType() {
  fireEvent.click(screen.getByRole('button', { name: 'Nuovo tipo' }));
  return screen.findByRole('form', { name: 'Nuovo tipo' });
}

describe('the lists', () => {
  it('shows each value with its rule in words, its count and "Filtra documenti"', async () => {
    const { lists } = stubServer();
    await openLists();
    const fattura = await entry('Fattura');
    expect(fattura.textContent).toContain('una qualsiasi delle parole «FATTURA»');
    await waitFor(() => {
      expect(fattura.textContent).toContain('20 documenti');
    });
    expect((await entry('Contratto')).textContent).toContain('nessuna regola: solo a mano');
    fireEvent.click(within(fattura).getByRole('button', { name: 'Filtra documenti: Fattura' }));
    await waitFor(() => {
      expect(lists.at(-1)?.getAll('type')).toEqual(['t-fattura']);
    });
    expect(screen.queryByRole('heading', { name: 'Liste e regole', level: 1 })).toBeNull();
  });

  it('shows no count while the counts load, rather than zero', () => {
    stubServer();
    render(
      <QueryClientProvider client={new QueryClient()}>
        <LabelsAdmin
          kind="type"
          labels={LABELS.type as readonly Label[]}
          counts={undefined}
          onFilter={vi.fn()}
        />
      </QueryClientProvider>,
    );
    expect(
      screen.getByRole('heading', { name: 'Fattura', level: 3 }).closest('li')?.textContent,
    ).not.toMatch(/\d+ documenti/);
  });

  it('shows the pattern box only for a rule that reads one', async () => {
    stubServer();
    await openLists();
    const editor = await newType();
    expect(within(editor).queryByLabelText('Testo da cercare')).toBeNull();
    for (const name of ['Una qualsiasi', 'Tutte', 'Esatta', 'Regex', 'Approssimata']) {
      fireEvent.click(within(editor).getByRole('radio', { name }));
      expect(within(editor).getByLabelText('Testo da cercare')).toBeTruthy();
    }
    fireEvent.click(within(editor).getByRole('radio', { name: 'Nessuna regola' }));
    expect(within(editor).queryByLabelText('Testo da cercare')).toBeNull();
  });

  it("shows the server's message when it refuses a rule", async () => {
    const message = 'invalid matching pattern: error parsing regexp: missing closing ): `(fattura`';
    const { writes } = stubServer(7, undefined, {
      status: 400,
      body: { error: 'invalid', message },
    });
    await openLists();
    const editor = await newType();
    fireEvent.change(within(editor).getByLabelText('Nome'), {
      target: { value: 'Fattura passiva' },
    });
    fireEvent.click(within(editor).getByRole('radio', { name: 'Regex' }));
    fireEvent.change(within(editor).getByLabelText('Testo da cercare'), {
      target: { value: '(fattura' },
    });
    fireEvent.click(within(editor).getByRole('button', { name: 'Salva' }));
    expect((await within(editor).findByRole('alert')).textContent).toContain(message);
    expect(writes[0]).toEqual({
      method: 'POST',
      path: '/api/documents/labels',
      body: {
        kind: 'type',
        name: 'Fattura passiva',
        match_algorithm: 'regex',
        match_pattern: '(fattura',
        case_insensitive: true,
      },
    });
  });

  it('saves a rule, and says how many documents it suggested', async () => {
    const { writes } = stubServer();
    await openLists();
    fireEvent.click(
      within(await entry('Contratto')).getByRole('button', { name: 'Modifica Contratto' }),
    );
    const editor = await screen.findByRole('form', { name: 'Modifica Contratto' });
    fireEvent.click(within(editor).getByRole('radio', { name: 'Esatta' }));
    fireEvent.change(within(editor).getByLabelText('Testo da cercare'), {
      target: { value: 'contratto di fornitura' },
    });
    fireEvent.click(within(editor).getByRole('button', { name: 'Salva' }));
    expect((await screen.findByRole('status')).textContent).toContain('3 documenti suggeriti');
    expect(writes[0]).toEqual({
      method: 'PUT',
      path: '/api/documents/labels/t-contratto',
      body: {
        kind: 'type',
        name: 'Contratto',
        match_algorithm: 'exact',
        match_pattern: 'contratto di fornitura',
        case_insensitive: true,
      },
    });
  });

  it('deletes a value after a confirmation', async () => {
    const { writes } = stubServer();
    await openLists();
    fireEvent.click(
      within(await entry('Contratto')).getByRole('button', { name: 'Elimina Contratto' }),
    );
    const ask = await screen.findByRole('dialog', { name: 'Eliminare «Contratto»?' });
    fireEvent.click(within(ask).getByRole('button', { name: 'Elimina' }));
    await waitFor(() => {
      expect(writes).toEqual([
        { method: 'DELETE', path: '/api/documents/labels/t-contratto', body: undefined },
      ]);
    });
  });
});
