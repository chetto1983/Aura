import { useState } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { StudioFrames } from '../StudioFrames';
import type { StudioImageRef, StudioModel } from '../studioApi';
import { cheapestOptions, type StudioDraft } from '../studioForm';

// The composer's image row. The rules under test are the ones the design states about what a
// model will accept: one start frame for a clip, an end frame only once there is something to
// start from, references up to the declared ceiling and no further.

const uploadStudioFrame = vi.hoisted(() => vi.fn());
vi.mock('../frameUpload', () => ({ uploadStudioFrame }));

const SUNSET: StudioImageRef = { id: 'a-sunset', file_name: 'sunset.png', mime_type: 'image/png' };
const HARBOUR: StudioImageRef = {
  id: 'a-harbour',
  file_name: 'harbour.png',
  mime_type: 'image/png',
};

const BOTH_FRAMES: StudioModel = {
  id: 'google/veo-3.1-lite',
  durations: [4],
  resolutions: ['720p'],
  aspect_ratios: ['16:9'],
  frame_images: ['first_frame', 'last_frame'],
  audio: false,
  seed: false,
};

const START_ONLY: StudioModel = { ...BOTH_FRAMES, frame_images: ['first_frame'] };

const TWO_REFERENCES: StudioModel = {
  id: 'black-forest-labs/flux-3',
  aspect_ratios: ['1:1'],
  audio: false,
  seed: false,
  reference_max: 2,
};

function mountRow(model: StudioModel, kind: 'image' | 'video', images: readonly StudioImageRef[]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const seen: StudioDraft[] = [];

  function Host() {
    const [draft, setDraft] = useState<StudioDraft>({
      kind,
      model: model.id,
      prompt: '',
      options: cheapestOptions(model),
      images,
      endFrame: undefined,
    });
    return (
      <StudioFrames
        draft={draft}
        model={model}
        onChange={(next) => {
          seen.push(next);
          setDraft(next);
        }}
      />
    );
  }

  render(
    <QueryClientProvider client={client}>
      <Host />
    </QueryClientProvider>,
  );
  return seen;
}

describe('StudioFrames', () => {
  it('offers the end frame once a start frame is set, on a model that declares it', () => {
    mountRow(BOTH_FRAMES, 'video', [SUNSET]);
    expect(screen.getByRole('button', { name: 'End frame' })).toBeTruthy();
    // The one slot is filled, so the add tile is gone and only its remove control is left.
    expect(screen.getByRole('button', { name: 'Remove sunset.png' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Start frame' })).toBeNull();
  });

  it('fills and empties the end frame without touching the start frame', async () => {
    const seen = mountRow(BOTH_FRAMES, 'video', [SUNSET]);
    uploadStudioFrame.mockResolvedValue(HARBOUR);
    fireEvent.keyDown(screen.getByRole('button', { name: 'End frame' }), { key: 'Enter' });
    fireEvent.click(screen.getByRole('menuitem', { name: 'Upload a file' }));
    // Only the end tile is empty, so there is exactly one file input on screen.
    fireEvent.change(screen.getByLabelText('Upload a file'), {
      target: { files: [new File(['b'], 'harbour.png', { type: 'image/png' })] },
    });

    await vi.waitFor(() => {
      expect(seen.at(-1)?.endFrame).toEqual(HARBOUR);
    });
    expect(seen.at(-1)?.images).toEqual([SUNSET]);

    fireEvent.click(screen.getByRole('button', { name: 'Remove harbour.png' }));
    expect(seen.at(-1)?.endFrame).toBeUndefined();
    // The start frame is untouched: the two slots are not one control.
    expect(seen.at(-1)?.images).toEqual([SUNSET]);
  });

  it('never offers an end frame on a model that declares only the first', () => {
    mountRow(START_ONLY, 'video', [SUNSET]);
    expect(screen.queryByRole('button', { name: 'End frame' })).toBeNull();
  });

  it('drops the end frame when the start frame it depended on is removed', () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const seen: StudioDraft[] = [];
    function Host() {
      const [draft, setDraft] = useState<StudioDraft>({
        kind: 'video',
        model: BOTH_FRAMES.id,
        prompt: '',
        options: cheapestOptions(BOTH_FRAMES),
        images: [SUNSET],
        endFrame: HARBOUR,
      });
      return (
        <StudioFrames
          draft={draft}
          model={BOTH_FRAMES}
          onChange={(next) => {
            seen.push(next);
            setDraft(next);
          }}
        />
      );
    }
    render(
      <QueryClientProvider client={client}>
        <Host />
      </QueryClientProvider>,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Remove sunset.png' }));
    // A clip cannot end at a frame it never started from.
    expect(seen.at(-1)?.images).toEqual([]);
    expect(seen.at(-1)?.endFrame).toBeUndefined();
  });

  it('removes only the reference that was clicked, and counts what is left', () => {
    const seen = mountRow(TWO_REFERENCES, 'image', [SUNSET, HARBOUR]);
    expect(screen.getByText('2 of 2')).toBeTruthy();
    // At the ceiling there is nothing left to add.
    expect(screen.queryByRole('button', { name: 'References' })).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Remove sunset.png' }));
    expect(seen.at(-1)?.images).toEqual([HARBOUR]);
    // The empty tile returns as soon as there is room again.
    expect(screen.getByRole('button', { name: 'References' })).toBeTruthy();
    expect(screen.getByText('1 of 2')).toBeTruthy();
  });

  it('appends an attached reference instead of replacing the one already there', () => {
    const seen = mountRow(TWO_REFERENCES, 'image', [SUNSET]);
    fireEvent.keyDown(screen.getByRole('button', { name: 'References' }), { key: 'Enter' });
    fireEvent.click(screen.getByRole('menuitem', { name: 'Upload a file' }));
    uploadStudioFrame.mockResolvedValue(HARBOUR);
    fireEvent.change(screen.getByLabelText('Upload a file'), {
      target: { files: [new File(['b'], 'harbour.png', { type: 'image/png' })] },
    });

    return vi.waitFor(() => {
      expect(seen.at(-1)?.images).toEqual([SUNSET, HARBOUR]);
    });
  });
});
