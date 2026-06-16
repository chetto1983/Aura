import { afterEach, describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import i18n from '../i18n/i18n';
import { NotFoundView } from '../routes/NotFoundView';

describe('NotFoundView', () => {
  afterEach(async () => {
    await i18n.changeLanguage('en');
  });

  it('renders the client-side 404 with a link back to the dashboard', () => {
    render(
      <MemoryRouter>
        <NotFoundView />
      </MemoryRouter>,
    );
    expect(screen.getByRole('heading', { name: 'Page not found' })).toBeTruthy();
    const link = screen.getByRole('link', { name: 'Return to dashboard' });
    expect(link.getAttribute('href')).toBe('/');
  });

  it('renders the Italian 404 copy after a language switch', async () => {
    await i18n.changeLanguage('it');
    render(
      <MemoryRouter>
        <NotFoundView />
      </MemoryRouter>,
    );
    expect(screen.getByRole('heading', { name: 'Pagina non trovata' })).toBeTruthy();
  });
});
