import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { ImageTile } from '../ImageTile';
import type { StudioImageRef } from '../studioApi';

// ImageTile owns the whole attach lifecycle, so what is asserted here is what the operator
// can reach: the two ways in, what each one reports back, what a refusal says out loud, and
// that a slot the model cannot use says WHY rather than merely going grey.

const uploadStudioFrame = vi.hoisted(() => vi.fn());
vi.mock('../frameUpload', () => ({ uploadStudioFrame }));

const SUNSET: StudioImageRef = {
  id: 'asset-sunset',
  file_name: 'sunset.png',
  mime_type: 'image/png',
};
const HARBOUR: StudioImageRef = {
  id: 'asset-harbour',
  file_name: 'harbour.png',
  mime_type: 'image/png',
};

function mountTile(over: Partial<Parameters<typeof ImageTile>[0]> = {}) {
  const onPick = vi.fn();
  const onRemove = vi.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <ImageTile
        label="Start frame"
        image={undefined}
        disabledReason={undefined}
        onPick={onPick}
        onRemove={onRemove}
        {...over}
      />
    </QueryClientProvider>,
  );
  return { onPick, onRemove };
}

function openMenu() {
  // Radix opens a menu on pointerdown; a click alone leaves it shut.
  fireEvent.keyDown(screen.getByRole('button', { name: 'Start frame' }), { key: 'Enter' });
}

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

function stubLibrary(assets: readonly StudioImageRef[]) {
  const spy = vi.fn((input: RequestInfo | URL) => {
    const url = urlOf(input);
    if (url.startsWith('/api/studio/library')) {
      return Promise.resolve(new Response(JSON.stringify({ assets }), { status: 200 }));
    }
    return Promise.reject(new Error(`unexpected fetch: ${url}`));
  });
  vi.stubGlobal('fetch', spy);
  return spy;
}

afterEach(() => {
  vi.unstubAllGlobals();
  uploadStudioFrame.mockReset();
});

describe('ImageTile', () => {
  it('offers the two ways in, and only those', () => {
    mountTile();
    openMenu();
    expect(screen.getByRole('menuitem', { name: 'Upload a file' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Your images' })).toBeTruthy();
    expect(screen.getAllByRole('menuitem')).toHaveLength(2);
  });

  it('uploads the chosen file and reports the frame it became', async () => {
    uploadStudioFrame.mockResolvedValue(SUNSET);
    const { onPick } = mountTile();
    const file = new File(['bytes'], 'sunset.png', { type: 'image/png' });
    fireEvent.change(screen.getByLabelText('Upload a file'), { target: { files: [file] } });

    await waitFor(() => {
      expect(onPick).toHaveBeenCalledWith(SUNSET);
    });
    expect(uploadStudioFrame.mock.calls[0]?.[0]).toBe(file);
  });

  it('says why an upload was refused instead of leaving an empty tile', async () => {
    uploadStudioFrame.mockRejectedValue(new Error('That file is larger than 20 MB.'));
    const { onPick } = mountTile();
    fireEvent.change(screen.getByLabelText('Upload a file'), {
      target: { files: [new File(['bytes'], 'huge.png', { type: 'image/png' })] },
    });

    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('That file is larger than 20 MB.');
    expect(onPick).not.toHaveBeenCalled();
  });

  it('lists the identity library and reports the image that was picked', async () => {
    const fetchSpy = stubLibrary([SUNSET, HARBOUR]);
    const { onPick } = mountTile();
    openMenu();
    fireEvent.click(screen.getByRole('menuitem', { name: 'Your images' }));

    const harbour = await screen.findByRole('button', { name: 'harbour.png' });
    expect(
      fetchSpy.mock.calls.some(([input]) => urlOf(input).startsWith('/api/studio/library')),
    ).toBe(true);
    fireEvent.click(harbour);
    // The one that was clicked, not the first row of the list.
    expect(onPick).toHaveBeenCalledWith(HARBOUR);
    expect(onPick).toHaveBeenCalledTimes(1);
    // Picking closes the picker: a popover left open covers the bar it was opened from.
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'harbour.png' })).toBeNull();
    });
  });

  it('reads the library only once it is opened', () => {
    const fetchSpy = stubLibrary([SUNSET]);
    mountTile();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('calls the library empty only once the route has answered', async () => {
    const fetchSpy = stubLibrary([]);
    mountTile();
    openMenu();
    fireEvent.click(screen.getByRole('menuitem', { name: 'Your images' }));
    // Empty-because-loading is not empty-because-absent: the sentence must not be on screen
    // while the request is still in flight.
    expect(screen.queryByText('No images uploaded yet.')).toBeNull();
    expect(screen.getByRole('status').textContent).toBe('Reading your images…');
    expect(await screen.findByText('No images uploaded yet.')).toBeTruthy();
    expect(fetchSpy).toHaveBeenCalledTimes(1);
  });

  it('carries aria-disabled and the reason when the model cannot use the slot', () => {
    mountTile({ disabledReason: 'That model takes no images.' });
    const tile = screen.getByRole('button', { name: 'Start frame' });
    expect(tile.getAttribute('aria-disabled')).toBe('true');
    const reason = tile.getAttribute('aria-describedby');
    expect(reason === null ? null : document.getElementById(reason)?.textContent).toBe(
      'That model takes no images.',
    );
    // A disabled slot offers no way in at all.
    fireEvent.keyDown(tile, { key: 'Enter' });
    expect(screen.queryByRole('menuitem')).toBeNull();
  });

  it('shows the attached thumbnail and removes it on click', () => {
    const { onRemove } = mountTile({ image: SUNSET });
    const thumbnail = screen.getByAltText('sunset.png');
    expect(thumbnail.getAttribute('src')).toBe('/api/assets/asset-sunset/download');
    // A filled tile is not a menu: it is the one control that empties the slot.
    expect(screen.queryByRole('button', { name: 'Start frame' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Remove sunset.png' }));
    expect(onRemove).toHaveBeenCalledTimes(1);
  });
});
