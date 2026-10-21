import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { renderDocuments, stubServer } from './documentsTestServer';
import i18n from '@/i18n/i18n';

beforeEach(async () => {
  await act(() => i18n.changeLanguage('it'));
});

afterEach(() => {
  vi.unstubAllGlobals();
});

async function openFields() {
  renderDocuments('/documents/lists');
  // Radix activates a tab on mousedown, as a pointer press does.
  fireEvent.mouseDown(await screen.findByRole('tab', { name: 'Campi personalizzati' }));
}

describe('the custom fields', () => {
  it('shows each field with its type in words, and a choice with its options', async () => {
    stubServer();
    await openFields();
    const stato = (await screen.findByRole('heading', { name: 'Stato', level: 3 })).closest('li');
    expect(stato?.textContent).toContain('Scelta: aperta, pagata');
    const numero = screen.getByRole('heading', { name: 'Numero fattura', level: 3 }).closest('li');
    expect(numero?.textContent).toContain('Testo');
  });

  it('asks for options only for a choice, and saves them', async () => {
    const { writes } = stubServer();
    await openFields();
    fireEvent.click(await screen.findByRole('button', { name: 'Nuovo campo' }));
    const editor = await screen.findByRole('form', { name: 'Nuovo campo' });
    expect(within(editor).queryByLabelText('Scelte, una per riga')).toBeNull();
    fireEvent.change(within(editor).getByLabelText('Nome'), { target: { value: 'Pagamento' } });
    fireEvent.click(within(editor).getByRole('radio', { name: 'Scelta' }));
    fireEvent.change(within(editor).getByLabelText('Scelte, una per riga'), {
      target: { value: 'bonifico\n\nRiBa\n' },
    });
    fireEvent.click(within(editor).getByRole('button', { name: 'Salva' }));
    await waitFor(() => {
      expect(writes).toHaveLength(1);
    });
    expect(writes[0]).toEqual({
      method: 'POST',
      path: '/api/documents/custom-fields',
      body: { name: 'Pagamento', data_type: 'select', options: ['bonifico', 'RiBa'] },
    });
  });
});
