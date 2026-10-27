import { describe, expect, it } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import { Suspense } from 'react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so preview t() keys resolve
import { PreviewByKind } from './previewDispatch';

// PreviewByKind is the preview modal's dispatch. A clip and a recording sit centred in the
// space under the header, the clip at its own width rather than a chat card's; the same
// renderers inside a chat message keep their card layout, which is why the stage lives here.

function renderKind(kind: 'video' | 'audio', mimeType: string) {
  return render(
    <Suspense fallback={null}>
      <PreviewByKind
        kind={kind}
        asset={{ assetId: 'clip/1', mimeType, fileName: 'clip' }}
        downloadFallback={null}
      />
    </Suspense>,
  );
}

describe('PreviewByKind', () => {
  it('centres a clip in the preview at its own width', async () => {
    const { container } = renderKind('video', 'video/mp4');
    await waitFor(() => {
      expect(container.querySelector('[data-slot="video-player"]')).not.toBeNull();
    });
    const stage = container.querySelector('[data-slot="media-stage"]');
    const player = container.querySelector('[data-slot="video-player"]');
    expect(stage?.classList.contains('justify-center')).toBe(true);
    expect(stage?.classList.contains('items-center')).toBe(true);
    expect(stage?.contains(player)).toBe(true);
    expect(player?.classList.contains('w-fit')).toBe(true);
  });

  it('centres a recording in the preview', async () => {
    const { container } = renderKind('audio', 'audio/mpeg');
    await waitFor(() => {
      expect(container.querySelector('[data-slot="audio-player"]')).not.toBeNull();
    });
    const stage = container.querySelector('[data-slot="media-stage"]');
    expect(stage?.classList.contains('justify-center')).toBe(true);
    expect(stage?.contains(container.querySelector('[data-slot="audio-player"]'))).toBe(true);
  });
});
