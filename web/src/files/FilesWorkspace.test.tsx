import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import FilesWorkspace from './FilesWorkspace';
import i18n from '@/i18n/i18n';
import { setTheme } from '@/theme/applyTheme';

// The provider is an HTTP client; the widget under test only needs it to answer the first
// listing, so the stub is the four bus methods FilesWorkspace actually calls.
vi.mock('./filesApi', async () => {
  const actual = await vi.importActual<typeof import('./filesApi')>('./filesApi');
  return {
    ...actual,
    createFileManagerProvider: () => ({
      loadFiles: () => Promise.resolve([{ id: 'chat', name: 'chat', type: 'folder', lazy: true }]),
      on: () => undefined,
      setNext: () => undefined,
      exec: () => Promise.resolve(),
    }),
  };
});

function themeClass(container: HTMLElement): string {
  return container.querySelector('.wx-theme')?.className ?? '';
}

describe('FilesWorkspace', () => {
  it('renders the widget chrome in the cockpit language', async () => {
    setTheme('dark');
    await act(() => i18n.changeLanguage('it'));
    const { container } = render(<FilesWorkspace />);
    // "Add New" is the widget's own word pack, not Aura's i18n resources: it proves the
    // Locale provider reached the component instead of being a no-op on a foreign context.
    await waitFor(() => {
      expect(container.textContent).toContain('Aggiungi');
    });
    expect(container.textContent).not.toContain('Add New');
  });

  it('themes the menus it renders outside its own wrapper', async () => {
    setTheme('dark');
    await act(() => i18n.changeLanguage('it'));
    const { container } = render(<FilesWorkspace />);
    fireEvent.click(await screen.findByText('Aggiungi'));
    // The dropdown is portalled next to the app root, OUTSIDE the themed wrapper, so its
    // only styling is the class the widget's theme context stamps on the portal itself.
    // A second copy of @svar-ui/react-core makes that context resolve empty and the class
    // comes out `wx--theme`, which no stylesheet defines: an unstyled white menu over the
    // dark cockpit (operator, 2026-09-09).
    const portal = await waitFor(() => {
      const found = [...document.querySelectorAll('[class*="-theme"]')].find(
        (node) => !container.contains(node),
      );
      if (!found) throw new Error('the menu never left the wrapper');
      return found;
    });
    expect(portal.className).toContain('wx-willow-dark-theme');
    expect(portal.textContent).toContain('Nuova cartella');
  });

  it('follows a theme switch made while it is mounted', async () => {
    setTheme('dark');
    const { container } = render(<FilesWorkspace />);
    await waitFor(() => {
      expect(themeClass(container)).toContain('wx-willow-dark-theme');
    });
    act(() => {
      setTheme('light');
    });
    await waitFor(() => {
      expect(themeClass(container)).toContain('wx-willow-theme');
    });
    act(() => {
      setTheme('dark');
    });
    await waitFor(() => {
      expect(themeClass(container)).toContain('wx-willow-dark-theme');
    });
  });
});
