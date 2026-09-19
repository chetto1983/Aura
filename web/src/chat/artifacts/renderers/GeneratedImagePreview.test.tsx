import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so t() keys resolve
import { OpenEditorContext } from '../../../mediaEdit/mediaEditorContext';
import { AssetSourceContext, type AssetSource } from './assetSourceContext';
import GeneratedImagePreview from './GeneratedImagePreview';

// GeneratedImagePreview (spec §5): an inline image fed by the asset source, with a
// keyboard-reachable fullscreen view, a download that is the tier's own asset URL (never a
// provider URL), and a copy that writes the loaded image and says so when the clipboard
// refuses.

type FetchFn = (url: string, init?: RequestInit) => Promise<Response>;

const props = { assetId: 'img/1', mimeType: 'image/png', fileName: 'lake.png' };

let fetchMock: ReturnType<typeof vi.fn<FetchFn>>;
let clipboardWrite: ReturnType<typeof vi.fn<(items: unknown[]) => Promise<void>>>;
let items: { readonly data: Record<string, Blob> }[];

beforeEach(() => {
  let minted = 0;
  URL.createObjectURL = vi.fn(() => {
    minted += 1;
    return `blob:image-${String(minted)}`;
  });
  URL.revokeObjectURL = vi.fn();
  fetchMock = vi.fn<FetchFn>(() =>
    Promise.resolve({
      ok: true,
      status: 200,
      blob: () => Promise.resolve(new Blob(['pixels'], { type: 'image/png' })),
    } as unknown as Response),
  );
  vi.stubGlobal('fetch', fetchMock);
  items = [];
  vi.stubGlobal(
    'ClipboardItem',
    class {
      readonly data: Record<string, Blob>;
      constructor(data: Record<string, Blob>) {
        this.data = data;
        items.push(this);
      }
    },
  );
  clipboardWrite = vi.fn<(items: unknown[]) => Promise<void>>(() => Promise.resolve());
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: { write: clipboardWrite },
  });
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

async function loadedImage(): Promise<HTMLImageElement> {
  const img = await screen.findByRole('img', { name: 'lake.png' });
  if (!(img instanceof HTMLImageElement)) throw new Error('expected an <img>');
  fireEvent.load(img);
  return img;
}

describe('GeneratedImagePreview', () => {
  it('shows the loading state, then the image from the relabelled object URL', async () => {
    render(<GeneratedImagePreview {...props} />);
    expect(screen.getByRole('status')).toBeTruthy();
    const img = await loadedImage();
    expect(img.getAttribute('src')).toBe('blob:image-1');
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/assets/img%2F1/download');
    expect(screen.getByText('lake.png')).toBeTruthy();
  });

  it('shows the shared error state when the asset cannot be fetched', async () => {
    fetchMock.mockImplementationOnce(() =>
      Promise.resolve({ ok: false, status: 404 } as unknown as Response),
    );
    render(<GeneratedImagePreview {...props} />);
    await waitFor(() => {
      expect(screen.getByRole('alert').textContent).toContain('404');
    });
    expect(screen.queryByRole('img')).toBeNull();
  });

  it('downloads through the identity asset route by asset ID', async () => {
    render(<GeneratedImagePreview {...props} />);
    await loadedImage();
    const link = screen.getByRole('link', { name: 'Download lake.png' });
    expect(link.getAttribute('href')).toBe('/api/assets/img%2F1/download');
    expect(link.getAttribute('download')).toBe('lake.png');
    expect(link.hasAttribute('data-required-touch-target')).toBe(true);
    expect(link.className).toContain('min-h-[44px]');
  });

  it('downloads through a mounted share tier, keeping its token-scoped URL', async () => {
    const shareTier: AssetSource = {
      assetUrl: (id) => `/s/tok/asset/${encodeURIComponent(id)}`,
      streamUrl: (id) => `/s/tok/asset/${encodeURIComponent(id)}/stream`,
      credentials: 'omit',
    };
    render(
      <AssetSourceContext.Provider value={shareTier}>
        <GeneratedImagePreview {...props} />
      </AssetSourceContext.Provider>,
    );
    await loadedImage();
    expect(screen.getByRole('link', { name: 'Download lake.png' }).getAttribute('href')).toBe(
      '/s/tok/asset/img%2F1',
    );
  });

  it('opens the fullscreen view from the keyboard and closes it with Escape', async () => {
    render(<GeneratedImagePreview {...props} />);
    await loadedImage();
    const trigger = screen.getByRole('button', { name: 'View lake.png full screen' });
    expect(trigger.tagName).toBe('BUTTON');
    act(() => {
      trigger.focus();
    });
    fireEvent.click(trigger);
    const dialog = await screen.findByRole('dialog', { name: 'lake.png' });
    const close = screen.getByRole('button', { name: 'Close full screen view' });
    await waitFor(() => {
      expect(dialog.contains(document.activeElement)).toBe(true);
    });
    expect(close.className).toContain('size-11');
    const zoomed = dialog.querySelector('img');
    expect(zoomed?.getAttribute('src')).toBe('blob:image-1');
    fireEvent.keyDown(document.activeElement ?? dialog, { key: 'Escape' });
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    expect(document.activeElement).toBe(trigger);
  });

  it('copies the loaded image, not another URL, and confirms it', async () => {
    render(<GeneratedImagePreview {...props} />);
    await loadedImage();
    fireEvent.click(screen.getByRole('button', { name: 'Copy image' }));
    await screen.findByRole('button', { name: 'Copied' });
    expect(screen.queryByRole('alert')).toBeNull();
    expect(fetchMock.mock.calls.map((call) => call[0])).toEqual([
      '/api/assets/img%2F1/download',
      'blob:image-1',
    ]);
    expect(clipboardWrite).toHaveBeenCalledTimes(1);
    expect(Object.keys(items[0]?.data ?? {})).toEqual(['image/png']);
  });

  it('reports a clipboard failure instead of swallowing it', async () => {
    clipboardWrite.mockImplementationOnce(() => Promise.reject(new Error('denied')));
    render(<GeneratedImagePreview {...props} />);
    await loadedImage();
    fireEvent.click(screen.getByRole('button', { name: 'Copy image' }));
    expect((await screen.findByRole('alert')).textContent).toBe(
      "Couldn't copy the image. Try again or download it.",
    );
    expect(screen.queryByRole('button', { name: 'Copied' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Copy image' }));
    await screen.findByRole('button', { name: 'Copied' });
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('reports a failure when the browser has no clipboard API', async () => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined });
    render(<GeneratedImagePreview {...props} />);
    await loadedImage();
    fireEvent.click(screen.getByRole('button', { name: 'Copy image' }));
    expect(await screen.findByRole('alert')).toBeTruthy();
  });

  it('hands the image to the editor from its actions', async () => {
    const open = vi.fn();
    render(
      <OpenEditorContext.Provider value={open}>
        <GeneratedImagePreview assetId="img/1" mimeType="image/png" fileName="beach.png" />
      </OpenEditorContext.Provider>,
    );
    await screen.findByRole('img', { name: 'beach.png' });
    fireEvent.click(screen.getByRole('button', { name: 'Edit beach.png' }));
    expect(open).toHaveBeenCalledWith({ assetId: 'img/1', kind: 'image' });
  });

  it('keeps its copy and download controls at the 44px touch floor', async () => {
    render(<GeneratedImagePreview {...props} />);
    await loadedImage();
    const copy = screen.getByRole('button', { name: 'Copy image' });
    expect(copy.hasAttribute('data-required-touch-target')).toBe(true);
    expect(copy.className).toContain('min-w-[44px]');
  });
});
