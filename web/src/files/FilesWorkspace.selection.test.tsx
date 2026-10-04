import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi, type MockInstance } from 'vitest';
import FilesWorkspace from './FilesWorkspace';
import i18n from '@/i18n/i18n';
import { emulateHomeScreen, leaveHomeScreen, sharedFiles } from '@/test/iosHomeScreen';

// Several files at once, driven through the REAL widget: its cards, its ⋮ menu, its delete
// confirmation. Only the provider is stubbed, and it is the provider that proves what the
// widget would have sent over the wire.
const provider = vi.hoisted(() => ({ exec: vi.fn() }));

vi.mock('./filesApi', async () => {
  const actual = await vi.importActual<typeof import('./filesApi')>('./filesApi');
  return {
    ...actual,
    createFileManagerProvider: () => ({
      loadFiles: () =>
        Promise.resolve([
          { id: '/docs', type: 'folder', lazy: true },
          { id: '/alpha.txt', type: 'file', size: 5 },
          { id: '/bravo.txt', type: 'file', size: 5 },
          { id: '/charlie.txt', type: 'file', size: 7 },
        ]),
      on: () => undefined,
      setNext: () => undefined,
      exec: provider.exec,
    }),
  };
});

let saved: string[];
let clickSpy: MockInstance<() => void>;

beforeEach(async () => {
  await act(() => i18n.changeLanguage('en'));
  provider.exec.mockReset().mockResolvedValue(undefined);
  saved = [];
  // A download is an anchor click; recording the href is recording the request it would make.
  clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
    this: HTMLAnchorElement,
  ) {
    saved.push(this.getAttribute('href') ?? '');
  });
});

afterEach(() => {
  clickSpy.mockRestore();
});

// The widget attaches its card handlers in an effect after the cards paint, and a click before
// that is lost. A plain click is safe to repeat, so one is repeated until the widget answers,
// then the selection is emptied again: every test starts with live handlers and nothing chosen.
async function mount() {
  const { container } = render(<FilesWorkspace />);
  await waitFor(() => {
    fireEvent.click(cardOf(container, '/charlie.txt'));
    expect(selectedIds(container)).toEqual(['/charlie.txt']);
  });
  const panel = container.querySelector<HTMLElement>('.wx-cards[data-id=":body"]');
  if (panel === null) throw new Error('no card panel');
  fireEvent.click(panel);
  await expectSelected(container, []);
  return { container, panel };
}

// The file panel's card, not the sidebar tree's node for the same folder: pressing that one
// opens the folder instead of selecting it.
function cardOf(container: HTMLElement, id: string): HTMLElement {
  const card = container.querySelector<HTMLElement>(`[data-id=":body"] [data-id=":${id}"]`);
  if (card === null) throw new Error(`no card for ${id}`);
  return card;
}

// The card's own ⋮ button: the only menu a phone can reach, since it has no right click.
function moreOf(container: HTMLElement, id: string): HTMLElement {
  const more = container.querySelector<HTMLElement>(`[data-action-id=":${id}"]`);
  if (more === null) throw new Error(`no ⋮ for ${id}`);
  return more;
}

// Scoped to the file panel: the sidebar tree marks the open folder `.wx-selected` too.
function selectedIds(container: HTMLElement): string[] {
  return [...container.querySelectorAll('[data-id=":body"] .wx-item.wx-selected')].map(
    (node) => node.getAttribute('data-id')?.slice(1) ?? '',
  );
}

async function chooseFromMenu(label: string): Promise<void> {
  const menu = await waitFor(() => {
    const found = document.querySelector<HTMLElement>('[data-wx-menu="true"]');
    if (found === null) throw new Error('the menu never opened');
    return found;
  });
  // The label, not the hotkey hint beside it: "Delete" is printed as both.
  fireEvent.click(within(menu).getByText(label, { selector: '.wx-value' }));
}

// The card handler selects on a 1 ms timer, so every assertion on the selection waits for it.
async function expectSelected(container: HTMLElement, ids: string[]): Promise<void> {
  await waitFor(() => {
    expect(selectedIds(container)).toEqual(ids);
  });
}

async function selectAlphaAndCharlie(container: HTMLElement): Promise<void> {
  fireEvent.click(cardOf(container, '/alpha.txt'));
  fireEvent.click(cardOf(container, '/charlie.txt'), { ctrlKey: true });
  await expectSelected(container, ['/alpha.txt', '/charlie.txt']);
  fireEvent.click(moreOf(container, '/charlie.txt'));
}

describe('FilesWorkspace selection', () => {
  it('lets a pointer without modifier keys select several files and delete them together', async () => {
    const { container } = await mount();

    fireEvent.click(moreOf(container, '/alpha.txt'));
    await chooseFromMenu('Select');
    await expectSelected(container, ['/alpha.txt']);

    // A plain tap: no Ctrl, no Shift -- all a phone has.
    fireEvent.click(cardOf(container, '/bravo.txt'));
    await expectSelected(container, ['/alpha.txt', '/bravo.txt']);

    fireEvent.click(moreOf(container, '/bravo.txt'));
    await chooseFromMenu('Delete');
    fireEvent.click(await screen.findByRole('button', { name: 'OK' }));

    await waitFor(() => {
      expect(provider.exec).toHaveBeenCalledWith(
        'delete-files',
        expect.objectContaining({ ids: ['/alpha.txt', '/bravo.txt'] }),
      );
    });
  });

  it('keeps a selection of several files whole when one card’s ⋮ is pressed', async () => {
    const { container } = await mount();
    fireEvent.click(cardOf(container, '/alpha.txt'));
    fireEvent.click(cardOf(container, '/bravo.txt'), { ctrlKey: true });
    await expectSelected(container, ['/alpha.txt', '/bravo.txt']);

    fireEvent.click(moreOf(container, '/bravo.txt'));
    // The widget re-selects the pressed card on a 1 ms timer; outlast it before asserting.
    await act(() => new Promise((resolve) => setTimeout(resolve, 20)));

    expect(selectedIds(container)).toEqual(['/alpha.txt', '/bravo.txt']);
  });

  it('downloads every selected file, and no folder, from the menu for several', async () => {
    const { container } = await mount();
    fireEvent.click(cardOf(container, '/docs'));
    fireEvent.click(cardOf(container, '/alpha.txt'), { ctrlKey: true });
    fireEvent.click(cardOf(container, '/charlie.txt'), { ctrlKey: true });
    await expectSelected(container, ['/docs', '/alpha.txt', '/charlie.txt']);

    fireEvent.click(moreOf(container, '/charlie.txt'));
    await chooseFromMenu('Download');

    await waitFor(
      () => {
        expect(saved).toEqual([
          '/api/filemanager/direct?id=%2Falpha.txt&download=true',
          '/api/filemanager/direct?id=%2Fcharlie.txt&download=true',
        ]);
      },
      { timeout: 2000 },
    );
  });

  it('downloads the one file its own menu was opened on', async () => {
    const { container } = await mount();
    fireEvent.click(moreOf(container, '/bravo.txt'));
    await chooseFromMenu('Download');

    await waitFor(() => {
      expect(saved).toEqual(['/api/filemanager/direct?id=%2Fbravo.txt&download=true']);
    });
  });

  // A download navigates iOS's home-screen app to a file page with no way back (prd.md §3):
  // the file opens in the cockpit's preview, whose save goes through the share sheet.
  it('opens the preview instead of downloading in the iOS home-screen app', async () => {
    Object.defineProperty(navigator, 'standalone', { value: true, configurable: true });
    const read = vi.fn(() => Promise.resolve({ ok: true, status: 200, text: () => 'bravo' }));
    vi.stubGlobal('fetch', read);
    try {
      const { container } = await mount();
      fireEvent.click(moreOf(container, '/bravo.txt'));
      await chooseFromMenu('Download');

      await screen.findByRole('dialog', { name: 'bravo.txt' });
      await waitFor(() => {
        expect(read).toHaveBeenCalledWith('/api/filemanager/direct?id=%2Fbravo.txt', {
          credentials: 'same-origin',
          signal: expect.any(AbortSignal) as AbortSignal,
        });
      });
      expect(saved).toEqual([]);
    } finally {
      vi.unstubAllGlobals();
      Reflect.deleteProperty(navigator, 'standalone');
    }
  });

  // iOS's home-screen app: the selected files go to ONE share sheet instead of one download
  // each, every one of which would navigate the app to iOS's file page (prd.md §3).
  it('hands the selected files to one share sheet in the iOS home-screen app', async () => {
    const sheet = emulateHomeScreen();
    const read = vi.fn((url: string) =>
      Promise.resolve({ ok: true, status: 200, blob: () => Promise.resolve(new Blob([url])) }),
    );
    vi.stubGlobal('fetch', read);
    try {
      const { container } = await mount();
      await selectAlphaAndCharlie(container);
      await chooseFromMenu('Download');

      await waitFor(() => {
        expect(sheet.share).toHaveBeenCalledTimes(1);
      });
      expect(sharedFiles(sheet).map((file) => file.name)).toEqual(['alpha.txt', 'charlie.txt']);
      expect(read.mock.calls.map(([url]) => url)).toEqual([
        '/api/filemanager/direct?id=%2Falpha.txt&download=true',
        '/api/filemanager/direct?id=%2Fcharlie.txt&download=true',
      ]);
      expect(saved).toEqual([]);
    } finally {
      vi.unstubAllGlobals();
      leaveHomeScreen();
    }
  });

  // The menu closes as soon as it is used, so the Save a lapsed tap needs lives on a bar.
  it('keeps a Save on the bar when the tap lapsed while the files were fetched', async () => {
    const sheet = emulateHomeScreen();
    sheet.activation.isActive = false;
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve({ ok: true, status: 200, blob: () => Promise.resolve(new Blob(['x'])) }),
      ),
    );
    try {
      const { container } = await mount();
      await selectAlphaAndCharlie(container);
      await chooseFromMenu('Download');

      const save = await screen.findByRole('button', { name: 'Save' });
      expect(screen.getByRole('status').textContent).toContain('2 files');
      expect(sheet.share).not.toHaveBeenCalled();
      fireEvent.click(save);
      await waitFor(() => {
        expect(sheet.share).toHaveBeenCalledTimes(1);
      });
      await waitFor(() => {
        expect(screen.queryByRole('status')).toBeNull();
      });
    } finally {
      vi.unstubAllGlobals();
      leaveHomeScreen();
    }
  });

  it('goes back to single selection once nothing is selected', async () => {
    const { container } = await mount();
    fireEvent.click(moreOf(container, '/alpha.txt'));
    await chooseFromMenu('Select');
    await expectSelected(container, ['/alpha.txt']);

    // Tapping the only selected file again empties the selection...
    fireEvent.click(cardOf(container, '/alpha.txt'));
    await expectSelected(container, []);
    // ...so the next taps select one file each, the way they did before.
    fireEvent.click(cardOf(container, '/bravo.txt'));
    await expectSelected(container, ['/bravo.txt']);
    fireEvent.click(cardOf(container, '/charlie.txt'));
    await expectSelected(container, ['/charlie.txt']);
  });

  it('still extends a range with Shift while selecting', async () => {
    const { container } = await mount();
    fireEvent.click(moreOf(container, '/alpha.txt'));
    await chooseFromMenu('Select');
    await expectSelected(container, ['/alpha.txt']);

    fireEvent.click(cardOf(container, '/charlie.txt'), { shiftKey: true });

    await expectSelected(container, ['/alpha.txt', '/bravo.txt', '/charlie.txt']);
  });

  it('ends the selection with a tap on empty space', async () => {
    const { container, panel } = await mount();
    fireEvent.click(moreOf(container, '/alpha.txt'));
    await chooseFromMenu('Select');
    fireEvent.click(cardOf(container, '/bravo.txt'));
    await expectSelected(container, ['/alpha.txt', '/bravo.txt']);

    fireEvent.click(panel);
    await expectSelected(container, []);

    fireEvent.click(cardOf(container, '/charlie.txt'));
    fireEvent.click(cardOf(container, '/alpha.txt'));
    await expectSelected(container, ['/alpha.txt']);
  });

  it('names the new menu entries in the cockpit language', async () => {
    await act(() => i18n.changeLanguage('it'));
    const { container } = await mount();

    fireEvent.click(moreOf(container, '/alpha.txt'));
    await chooseFromMenu('Seleziona');
    fireEvent.click(cardOf(container, '/bravo.txt'));
    await expectSelected(container, ['/alpha.txt', '/bravo.txt']);

    fireEvent.click(moreOf(container, '/bravo.txt'));
    await chooseFromMenu('Scarica');
    await waitFor(() => {
      expect(saved[0]).toBe('/api/filemanager/direct?id=%2Falpha.txt&download=true');
    });
  });
});
