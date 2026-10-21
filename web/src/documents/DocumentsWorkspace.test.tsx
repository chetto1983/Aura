import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { renderDocuments, stubServer } from './documentsTestServer';
import i18n from '@/i18n/i18n';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('DocumentsWorkspace', () => {
  it('shows each value under its column, marked when the operator did not set it', async () => {
    await act(() => i18n.changeLanguage('it'));
    stubServer();
    renderDocuments();
    const suggested = (await screen.findByText('fattura_2026_044.pdf')).closest('.wx-row');
    if (!(suggested instanceof HTMLElement)) throw new Error('no row for the suggested invoice');
    expect(within(suggested).getByText('regola')).toBeTruthy();
    expect(within(suggested).getByText('estrattore')).toBeTruthy();
    expect(within(suggested).getByText('Da rivedere')).toBeTruthy();
    const reviewed = screen.getByText('fattura_2026_031.pdf').closest('.wx-row');
    if (!(reviewed instanceof HTMLElement)) throw new Error('no row for the reviewed invoice');
    expect(within(reviewed).getByRole('button', { name: 'Gialli Import' })).toBeTruthy();
    expect(within(reviewed).getByText('27/03/2026')).toBeTruthy();
    expect(within(reviewed).queryByText('regola')).toBeNull();
    expect(within(reviewed).getByText('Rivisto')).toBeTruthy();
    expect(screen.getByText('24 documenti')).toBeTruthy();
  });

  it('counts each value of a list in the result, with Include, Exclude and Non assegnata', async () => {
    await act(() => i18n.changeLanguage('it'));
    const { lists } = stubServer();
    renderDocuments();
    await screen.findByText('fattura_2026_044.pdf');
    fireEvent.click(screen.getByRole('button', { name: 'Controparte' }));
    const menu = await screen.findByRole('dialog', { name: 'Controparte' });
    expect(within(menu).getByRole('checkbox', { name: /Non assegnata\s*3/ })).toBeTruthy();
    expect(within(menu).getByRole('checkbox', { name: /Gialli Import\s*9/ })).toBeTruthy();
    fireEvent.click(within(menu).getByRole('checkbox', { name: /Gialli Import/ }));
    await waitFor(() => {
      expect(lists.at(-1)?.getAll('counterparty')).toEqual(['c-gialli']);
    });
    fireEvent.click(within(menu).getByRole('radio', { name: 'Escludi' }));
    fireEvent.click(within(menu).getByRole('checkbox', { name: /Rossi SRL/ }));
    await waitFor(() => {
      expect(lists.at(-1)?.getAll('counterparty_not')).toEqual(['c-rossi']);
    });
    fireEvent.click(within(menu).getByRole('checkbox', { name: /Non assegnata/ }));
    await waitFor(() => {
      expect(lists.at(-1)?.getAll('unassigned')).toEqual(['counterparty']);
    });
  });

  it('filters by a value clicked in a row, and Azzera filtri clears every filter', async () => {
    await act(() => i18n.changeLanguage('it'));
    const { lists } = stubServer();
    renderDocuments();
    const reviewed = (await screen.findByText('fattura_2026_031.pdf')).closest('.wx-row');
    if (!(reviewed instanceof HTMLElement)) throw new Error('no row');
    fireEvent.click(within(reviewed).getByRole('button', { name: 'Gialli Import' }));
    await waitFor(() => {
      expect(lists.at(-1)?.getAll('counterparty')).toEqual(['c-gialli']);
    });
    expect(screen.getByRole('button', { name: /Controparte: Gialli Import/ })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Azzera filtri' }));
    await waitFor(() => {
      expect(lists.at(-1)?.getAll('counterparty')).toEqual([]);
    });
  });

  it('counts the documents to review, and shows them on a click', async () => {
    await act(() => i18n.changeLanguage('it'));
    const { lists } = stubServer(7);
    renderDocuments();
    const counter = await screen.findByRole('button', { name: /Suggerimenti\s*7/ });
    expect(counter.getAttribute('aria-pressed')).toBe('false');
    fireEvent.click(counter);
    await waitFor(() => {
      expect(lists.at(-1)?.get('needs_review')).toBe('true');
    });
    expect(counter.getAttribute('aria-pressed')).toBe('true');
  });
});
