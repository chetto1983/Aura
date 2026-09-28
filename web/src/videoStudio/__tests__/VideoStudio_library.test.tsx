import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { StudioAssetRef } from '../../studio/studioApi';
import { LibraryPicker } from '../VideoStudio_library';

// The library list a panel offers, with the query stood in for: what it asks the library for,
// what it says while the list is on its way or could not come, and which asset it hands over.

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

type LibraryState =
  | { isPending: true; isError: false; data: undefined }
  | { isPending: false; isError: true; data: undefined }
  | { isPending: false; isError: false; data: readonly StudioAssetRef[] };

const library = vi.hoisted(() => ({
  state: undefined as unknown,
  asked: [] as unknown[][],
}));
vi.mock('../../studio/useStudio', () => ({
  useStudioLibrary: (...args: unknown[]) => {
    library.asked.push(args);
    return library.state;
  },
}));

function listing(data: readonly StudioAssetRef[]): LibraryState {
  return { isPending: false, isError: false, data };
}

const BED = { id: 'a1', file_name: 'bed.wav', mime_type: 'audio/wav' };
const CLIP = { id: 'v1', file_name: 'beach.mp4', mime_type: 'video/mp4' };
const STILL = { id: 'i1', file_name: 'poster.png', mime_type: 'image/png' };

beforeEach(() => {
  library.asked.length = 0;
  library.state = listing([]);
});

describe('LibraryPicker', () => {
  it('asks the library for the kinds it offers, and hands the asset picked over', () => {
    library.state = listing([BED]);
    const onPick = vi.fn();
    render(
      <LibraryPicker
        modalities={['audio']}
        empty="videoStudio.library.emptySounds"
        onPick={onPick}
      />,
    );
    expect(library.asked).toContainEqual([true, ['audio']]);
    fireEvent.click(screen.getByRole('button', { name: 'bed.wav' }));
    expect(onPick).toHaveBeenCalledWith(BED);
  });

  it('shows a picture as itself, and a video or a sound by its name', () => {
    library.state = listing([CLIP, STILL]);
    render(
      <LibraryPicker
        modalities={['video', 'image']}
        empty="videoStudio.library.emptyClips"
        onPick={vi.fn()}
      />,
    );
    const still = screen.getByRole('button', { name: 'poster.png' });
    expect(still.querySelector('img')?.getAttribute('src')).toBe('/api/assets/i1/download');
    expect(screen.getByRole('button', { name: 'beach.mp4' }).querySelector('img')).toBeNull();
  });

  it('offers no GIF among the clips, and says so when it was all there was', () => {
    const loop = { id: 'g1', file_name: 'loop.gif', mime_type: 'image/gif' };
    library.state = listing([CLIP, loop, STILL]);
    const view = render(
      <LibraryPicker
        modalities={['video', 'image']}
        empty="videoStudio.library.emptyClips"
        onPick={vi.fn()}
      />,
    );
    expect(screen.getAllByRole('button').map((button) => button.textContent)).toEqual([
      'beach.mp4',
      'poster.png',
    ]);
    library.state = listing([loop]);
    view.rerender(
      <LibraryPicker
        modalities={['video', 'image']}
        empty="videoStudio.library.emptyClips"
        onPick={vi.fn()}
      />,
    );
    expect(screen.getByText('videoStudio.library.emptyClips')).toBeTruthy();
  });

  it('says it is reading the library, then that it could not', () => {
    library.state = { isPending: true, isError: false, data: undefined };
    const view = render(
      <LibraryPicker
        modalities={['audio']}
        empty="videoStudio.library.emptySounds"
        onPick={vi.fn()}
      />,
    );
    expect(screen.getByRole('status').textContent).toBe('videoStudio.library.loading');
    library.state = { isPending: false, isError: true, data: undefined };
    view.rerender(
      <LibraryPicker
        modalities={['audio']}
        empty="videoStudio.library.emptySounds"
        onPick={vi.fn()}
      />,
    );
    expect(screen.getByRole('alert').textContent).toBe('videoStudio.library.failed');
  });

  it('says the library holds nothing of the kinds it offers, in its own words', () => {
    render(
      <LibraryPicker
        modalities={['video', 'image']}
        empty="videoStudio.library.emptyClips"
        onPick={vi.fn()}
      />,
    );
    expect(screen.getByText('videoStudio.library.emptyClips')).toBeTruthy();
    expect(screen.queryByRole('button')).toBeNull();
  });
});
