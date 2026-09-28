import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { IEntity } from '@svar-ui/react-filemanager';
import FilesWorkspace from './FilesWorkspace';
import i18n from '@/i18n/i18n';

// What the workspace does with the provider's answers: the listing, a folder fetched on
// demand, a failure of either, and the provider's own correction of a rename.
const provider = vi.hoisted(() => ({
  loadFiles: vi.fn<(id: string) => Promise<IEntity[]>>(),
  handlers: new Map<string, (ev: unknown) => void>(),
}));

vi.mock('./filesApi', async () => {
  const actual = await vi.importActual<typeof import('./filesApi')>('./filesApi');
  return {
    ...actual,
    createFileManagerProvider: () => ({
      loadFiles: provider.loadFiles,
      on: (name: string, handler: (ev: unknown) => void) => provider.handlers.set(name, handler),
      setNext: () => undefined,
      exec: () => Promise.resolve(),
    }),
  };
});

// A new array of new rows per answer: the store writes into the rows it is handed (a fetched
// folder's `lazy` turns false), and a shared fixture would carry that into the next test.
function root(): IEntity[] {
  return [
    { id: '/docs', type: 'folder', lazy: true },
    { id: '/clip.mp4', type: 'file', size: 4 },
  ];
}

beforeEach(async () => {
  await act(() => i18n.changeLanguage('en'));
  provider.handlers.clear();
  provider.loadFiles
    .mockReset()
    .mockImplementation((id) =>
      Promise.resolve(id === '' ? root() : [{ id: '/docs/inner.txt', type: 'file', size: 2 }]),
    );
});

// The file panel's card; the sidebar tree renders a node with the same id for every folder.
async function card(container: HTMLElement, id: string): Promise<HTMLElement> {
  return waitFor(() => {
    const found = container.querySelector<HTMLElement>(`[data-id=":body"] [data-id=":${id}"]`);
    if (found === null) throw new Error(`no card for ${id}`);
    return found;
  });
}

// The widget attaches its card handlers in an effect after the cards paint, and a double-click
// delivered before that is lost; so it is repeated until the folder is asked for.
async function openFolder(container: HTMLElement, id: string): Promise<void> {
  const folder = await card(container, id);
  await waitFor(() => {
    fireEvent.doubleClick(folder);
    expect(provider.loadFiles).toHaveBeenCalledWith(id);
  });
}

describe('FilesWorkspace and its provider', () => {
  it('says so when the listing cannot be read', async () => {
    provider.loadFiles.mockRejectedValue(new Error('offline'));
    render(<FilesWorkspace />);

    expect((await screen.findByRole('alert')).textContent).toBe(i18n.t('files.loadFailed'));
  });

  it('fetches a folder the first time it is opened', async () => {
    const { container } = render(<FilesWorkspace />);
    await openFolder(container, '/docs');

    await card(container, '/docs/inner.txt');
  });

  it('keeps a folder it could not read closed, and says why', async () => {
    provider.loadFiles.mockImplementation((id) =>
      id === '' ? Promise.resolve(root()) : Promise.reject(new Error('offline')),
    );
    const { container } = render(<FilesWorkspace />);
    await openFolder(container, '/docs');

    expect((await screen.findByRole('alert')).textContent).toBe(i18n.t('files.loadFailed'));
    expect(container.querySelector('[data-id=":/docs/inner.txt"]')).toBeNull();
  });

  it('shows the name the store kept when it is not the one asked for', async () => {
    const { container } = render(<FilesWorkspace />);
    await card(container, '/clip.mp4');

    act(() => {
      provider.handlers.get('file-renamed')?.({ id: '/clip.mp4', newId: '/clip (1).mp4' });
    });

    await card(container, '/clip (1).mp4');
    expect(container.querySelector('[data-id=":body"] [data-id=":/clip.mp4"]')).toBeNull();
  });

  it('opens a file in a new tab when nothing is picking files', async () => {
    const opened = vi.spyOn(window, 'open').mockImplementation(() => null);
    const { container } = render(<FilesWorkspace />);
    const clip = await card(container, '/clip.mp4');

    // Repeated until the widget answers: see FilesWorkspace.test.tsx on the dropped first
    // double-click.
    await waitFor(() => {
      fireEvent.doubleClick(clip);
      expect(opened).toHaveBeenCalledWith(
        '/api/filemanager/direct?id=%2Fclip.mp4',
        '_blank',
        'noopener,noreferrer',
      );
    });
    opened.mockRestore();
  });

  it('never lets a slower, older listing overwrite a newer one', async () => {
    let answerFirst: (files: IEntity[]) => void = () => undefined;
    provider.loadFiles
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            answerFirst = resolve;
          }),
      )
      .mockImplementation(() => Promise.resolve([{ id: '/fresh.txt', type: 'file', size: 1 }]));
    const { container } = render(<FilesWorkspace />);

    // A language switch asks for the listing again, while the first answer is still out.
    await act(() => i18n.changeLanguage('it'));
    await card(container, '/fresh.txt');
    await act(async () => {
      answerFirst([{ id: '/stale.txt', type: 'file', size: 1 }]);
      await Promise.resolve();
    });

    expect(container.querySelector('[data-id=":/stale.txt"]')).toBeNull();
    expect(container.querySelector('[data-id=":body"] [data-id=":/fresh.txt"]')).not.toBeNull();
  });
});
