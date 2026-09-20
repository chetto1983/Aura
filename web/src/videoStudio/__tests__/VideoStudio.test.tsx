import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '../../i18n/i18n';
import type { VideoProject } from '../project';
import { lastSavedProject } from '../projectStore';
import type { StudioOpen } from '../VideoStudio_sources';

// The workspace is where the parts become an editor, so these tests read the REAL bundle rather
// than a `t` that echoes keys: a refusal reaches the screen through `t(error.reasonKey)`, which
// the static i18n usage gate cannot see, and a key that resolves to nothing would otherwise ship
// as an empty alert.

const renderers = vi.hoisted(() => ({ instances: [] as { destroyed: number }[] }));

vi.mock('@videoflow/renderer-dom', () => ({
  default: class {
    loadedFonts: Record<string, string> = {};
    destroyed = 0;
    constructor() {
      renderers.instances.push(this);
    }
    loadFont(): Promise<void> {
      return Promise.resolve();
    }
    loadVideo(): Promise<void> {
      return Promise.resolve();
    }
    seek(): Promise<void> {
      return Promise.resolve();
    }
    destroy(): void {
      this.destroyed += 1;
    }
  },
}));

vi.mock('@videoflow/renderer-browser', () => ({
  default: class {
    loadedFonts: Record<string, string> = {};
  },
}));

const flow = vi.hoisted(() => ({ exportProject: vi.fn() }));
vi.mock('../videoflow', async (original) => ({
  ...(await original<typeof import('../videoflow')>()),
  exportProject: flow.exportProject,
}));

const media = vi.hoisted(() => ({ probeVideo: vi.fn() }));
vi.mock('../../mediaEdit/videoMedia', () => media);

const downloadBlob = vi.hoisted(() => vi.fn());
vi.mock('../../mediaEdit/download', () => ({ downloadBlob }));

const store = vi.hoisted(() => ({ saveProject: vi.fn(), loadProject: vi.fn() }));
// Partially: `projectFileName` is the real rule, and it is what names the download.
vi.mock('../projectStore', async (original) => ({
  ...(await original<typeof import('../projectStore')>()),
  ...store,
}));

const assets = vi.hoisted(() => ({ presignAsset: vi.fn(), finalizeAsset: vi.fn() }));
vi.mock('../../chat/attachments/api', () => assets);
vi.mock('../../chat/attachments/upload', () => ({ putWithProgress: () => Promise.resolve() }));

const { default: VideoStudio } = await import('../VideoStudio');

function project(): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 1920, height: 1080 },
    fps: 25,
    sources: [
      {
        id: 'src-a',
        assetId: 'asset-a',
        kind: 'video',
        duration: 20,
        size: { width: 1920, height: 1080 },
      },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false },
      { id: 'clip-2', sourceId: 'src-a', duration: 4, sourceStart: 4, muted: false },
    ],
    // The title hangs on clip-1, so removing that clip takes it with it.
    overlays: [
      {
        id: 'lane-1',
        items: [
          {
            id: 'title-1',
            kind: 'text',
            anchor: { clipId: 'clip-1', offset: 1 },
            duration: 2,
            props: { text: 'Ciao' },
          },
        ],
      },
    ],
  };
}

function mount(open: StudioOpen = { kind: 'project', project: project() }, onSaved = vi.fn()) {
  const onClose = vi.fn();
  const view = render(<VideoStudio open={open} onClose={onClose} onSaved={onSaved} />);
  return { ...view, onClose, onSaved };
}

function button(key: string): HTMLElement {
  return screen.getByRole('button', { name: i18n.t(key) });
}

function item(key: string, index: number): HTMLElement {
  return screen.getByRole('button', { name: i18n.t(key, { index }) });
}

/** The warning dialog, named so it is not confused with the editor's own full-screen layer —
 *  `MediaEditorLayer` is a `role="dialog"` too. */
function confirmation(): Promise<HTMLElement> {
  return screen.findByRole('dialog', { name: i18n.t('videoStudio.confirm.title', { count: 1 }) });
}

/** Select the first item of `key`, ask to remove it and agree to lose its overlays. */
async function removeSelected(key: string): Promise<void> {
  fireEvent.click(item(key, 1));
  fireEvent.click(button('videoStudio.command.remove'));
  const dialog = await confirmation();
  fireEvent.click(
    within(dialog).getByRole('button', { name: i18n.t('videoStudio.confirm.proceed') }),
  );
  await waitFor(() => {
    expect(
      screen.queryByRole('button', { name: i18n.t('videoStudio.timeline.clip', { index: 2 }) }),
    ).toBeNull();
  });
}

beforeEach(() => {
  renderers.instances = [];
  flow.exportProject.mockResolvedValue(new Blob(['x'], { type: 'video/mp4' }));
  media.probeVideo.mockResolvedValue({
    duration: 9,
    width: 1280,
    height: 720,
    hasAudio: true,
    decodable: true,
  });
  store.saveProject.mockResolvedValue('file-1');
  store.loadProject.mockResolvedValue({ project: project(), missing: [] });
  assets.presignAsset.mockResolvedValue({
    asset: { id: 'asset-new' },
    upload: { upload_url: 'https://store/put', method: 'PUT', required_headers: {} },
  });
  assets.finalizeAsset.mockResolvedValue({ id: 'asset-new' });
});

afterEach(() => {
  vi.clearAllMocks();
  downloadBlob.mockReset();
  localStorage.clear();
});

describe('VideoStudio', () => {
  it('opens on the project it was handed and draws the three panels', async () => {
    mount();
    expect(
      await screen.findByRole('group', { name: i18n.t('videoStudio.timeline.label') }),
    ).toBeTruthy();
    expect(screen.getByTestId('video-stage')).toBeTruthy();
    expect(
      screen.getByRole('region', { name: i18n.t('videoStudio.inspector.label') }),
    ).toBeTruthy();
    expect(
      screen.getByRole('button', { name: i18n.t('videoStudio.timeline.clip', { index: 1 }) }),
    ).toBeTruthy();
  });

  it.each([
    ['en', 'There is nothing to cut here: the playhead sits on a clip’s edge.'],
    ['it', "Qui non c'è niente da tagliare: l'indicatore è sul bordo di una clip."],
  ])('says in %s why a command was refused', async (language, sentence) => {
    await i18n.changeLanguage(language);
    try {
      mount();
      await screen.findByTestId('video-stage');
      // The playhead is at 0, which is clip-1's own edge: splitAt refuses rather than making a
      // clip a nanosecond long.
      fireEvent.click(button('videoStudio.command.split'));
      expect((await screen.findByRole('alert')).textContent).toBe(sentence);
    } finally {
      await i18n.changeLanguage('en');
    }
  });

  it('refuses a source the browser cannot decode before a byte is uploaded', async () => {
    media.probeVideo.mockRejectedValue(new Error('no video track'));
    mount();
    await screen.findByTestId('video-stage');

    const input = screen.getByLabelText(i18n.t('videoStudio.source.pick'));
    fireEvent.change(input, {
      target: { files: [new File(['x'], 'clip.mp4', { type: 'video/mp4' })] },
    });

    expect((await screen.findByRole('alert')).textContent).toBe(
      i18n.t('videoStudio.refusal.sourceUndecodable'),
    );
    expect(assets.presignAsset).not.toHaveBeenCalled();
  });

  it('uploads a source the browser can decode and puts it on the lane', async () => {
    mount();
    await screen.findByTestId('video-stage');

    fireEvent.change(screen.getByLabelText(i18n.t('videoStudio.source.pick')), {
      target: { files: [new File(['x'], 'clip.mp4', { type: 'video/mp4' })] },
    });

    await waitFor(() => {
      expect(
        screen.getByRole('button', { name: i18n.t('videoStudio.timeline.clip', { index: 3 }) }),
      ).toBeTruthy();
    });
    expect(assets.finalizeAsset).toHaveBeenCalledWith('asset-new');
  });

  it('takes a still through the same door and files it as an image', async () => {
    // The spec's scope line is "several clips AND images in sequence on the video lane". The
    // model, the renderer adapter and the lane carried the branch from the first task; this is
    // the door that reaches them.
    vi.stubGlobal(
      'createImageBitmap',
      vi.fn(() => Promise.resolve({ width: 800, height: 600, close: () => undefined })),
    );
    mount();
    await screen.findByTestId('video-stage');

    fireEvent.change(screen.getByLabelText(i18n.t('videoStudio.source.pick')), {
      target: { files: [new File(['x'], 'still.png', { type: 'image/png' })] },
    });

    await waitFor(() => {
      expect(
        screen.getByRole('button', { name: i18n.t('videoStudio.timeline.clip', { index: 3 }) }),
      ).toBeTruthy();
    });
    // Filed by what it is: a hint of 'video' would put a .png under media/ beside the clips.
    expect(assets.presignAsset).toHaveBeenCalledWith(
      expect.objectContaining({ modality_hint: 'image', file_name: 'still.png' }),
    );
    // Five seconds on screen, and the still itself has no length to run past.
    expect(
      screen.getByRole('button', { name: i18n.t('videoStudio.timeline.clip', { index: 3 }) })
        .textContent,
    ).toBe('00:05.0');
  });

  it('says an overlay will go BEFORE the edit lands, and leaves the project alone if it is refused', async () => {
    mount();
    await screen.findByTestId('video-stage');
    fireEvent.click(item('videoStudio.timeline.clip', 1));
    fireEvent.click(button('videoStudio.command.remove'));

    const dialog = await confirmation();
    expect(within(dialog).getByText(i18n.t('videoStudio.confirm.body', { count: 1 }))).toBeTruthy();

    fireEvent.click(
      within(dialog).getByRole('button', { name: i18n.t('videoStudio.confirm.cancel') }),
    );
    // Still two clips and still a title: the question was asked without applying anything.
    await waitFor(() => {
      expect(
        screen.getByRole('button', { name: i18n.t('videoStudio.timeline.clip', { index: 2 }) }),
      ).toBeTruthy();
    });
    expect(item('videoStudio.timeline.overlayText', 1)).toBeTruthy();
  });

  it('removes the clip and its title once the operator agrees', async () => {
    mount();
    await screen.findByTestId('video-stage');
    await removeSelected('videoStudio.timeline.clip');

    expect(
      screen.queryByRole('button', {
        name: i18n.t('videoStudio.timeline.overlayText', { index: 1 }),
      }),
    ).toBeNull();
    expect(
      screen.queryByRole('button', { name: i18n.t('videoStudio.timeline.clip', { index: 2 }) }),
    ).toBeNull();
  });

  it('undoes the confirmed edit as one step', async () => {
    mount();
    await screen.findByTestId('video-stage');
    await removeSelected('videoStudio.timeline.clip');

    fireEvent.click(button('videoStudio.command.undo'));
    await waitFor(() => {
      expect(
        screen.getByRole('button', { name: i18n.t('videoStudio.timeline.clip', { index: 2 }) }),
      ).toBeTruthy();
    });
    expect(
      screen.getByRole('button', {
        name: i18n.t('videoStudio.timeline.overlayText', { index: 1 }),
      }),
    ).toBeTruthy();
  });

  /** Walk the playhead forward `seconds` whole seconds — shift-arrow is a second, plain is a
   *  frame — which is the only way to move it without a layout jsdom does not have. */
  function scrubForward(seconds: number) {
    const slider = screen.getByRole('slider', { name: i18n.t('videoStudio.timeline.playhead') });
    for (let step = 0; step < seconds; step += 1) {
      fireEvent.keyDown(slider, { key: 'ArrowRight', shiftKey: true });
    }
  }

  function lane(index: number): HTMLElement | null {
    return screen.queryByText(i18n.t('videoStudio.timeline.overlayLane', { index }));
  }

  it('puts a second title on the lane that is free rather than opening another', async () => {
    mount();
    await screen.findByTestId('video-stage');
    // `lane-1` already holds a title over clip-1's 1s..3s. At 4s — clip-2's first frame — a new
    // three-second title runs 4s..7s and meets nothing, so it belongs on that same lane.
    scrubForward(4);
    fireEvent.click(button('videoStudio.command.addText'));

    await waitFor(() => {
      expect(screen.getAllByRole('button', { name: /^Title/ })).toHaveLength(2);
    });
    expect(lane(1)).toBeTruthy();
    expect(lane(2)).toBeNull();
  });

  it('opens the next lane for a title that would cover an instant already taken', async () => {
    mount();
    await screen.findByTestId('video-stage');
    // The playhead is at 0, so this title runs 0s..3s and overlaps the one already on `lane-1`.
    // A lane on demand is what the spec asks for, and this is the demand.
    fireEvent.click(button('videoStudio.command.addText'));

    await waitFor(() => {
      expect(lane(2)).toBeTruthy();
    });
    // One item per lane: the label counts within its own lane, so two "Title 1"s are two lanes
    // holding one title each — not one lane holding two.
    expect(
      screen.getAllByRole('button', {
        name: i18n.t('videoStudio.timeline.overlayText', { index: 1 }),
      }),
    ).toHaveLength(2);
  });

  it('selects the title it just added, although the command cannot return its id', async () => {
    mount();
    await screen.findByTestId('video-stage');
    fireEvent.click(button('videoStudio.command.addText'));

    // The inspector shows an overlay's fields only for a selected overlay.
    expect(await screen.findByLabelText(i18n.t('videoStudio.inspector.text'))).toBeTruthy();
  });

  it('draws the export bar with its values and cancels the work it started', async () => {
    let signal: AbortSignal | undefined;
    flow.exportProject.mockImplementation(
      (_p: unknown, _u: unknown, options: { signal?: AbortSignal }) =>
        new Promise((_resolve, reject) => {
          signal = options.signal;
          options.signal?.addEventListener('abort', () => {
            reject(new DOMException('aborted', 'AbortError'));
          });
        }),
    );
    mount();
    await screen.findByTestId('video-stage');
    fireEvent.click(button('videoStudio.export.action'));

    const bar = await screen.findByRole('progressbar', {
      name: i18n.t('videoStudio.export.progress'),
    });
    expect(bar.getAttribute('aria-valuemin')).toBe('0');
    expect(bar.getAttribute('aria-valuemax')).toBe('100');
    expect(bar.getAttribute('aria-valuenow')).toBe('0');

    fireEvent.click(button('videoStudio.export.cancel'));
    await waitFor(() => {
      expect(signal?.aborted).toBe(true);
    });
    expect(downloadBlob).not.toHaveBeenCalled();
    // An abort is the operator's own decision: it is not reported back as a failure.
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('downloads the finished export', async () => {
    mount();
    await screen.findByTestId('video-stage');
    fireEvent.click(button('videoStudio.export.action'));
    await waitFor(() => {
      expect(downloadBlob).toHaveBeenCalledWith(expect.any(Blob), 'demo.mp4');
    });
  });

  it('saves the project and says where it went', async () => {
    const { onSaved } = mount();
    await screen.findByTestId('video-stage');
    fireEvent.click(button('videoStudio.save.action'));

    await waitFor(() => {
      expect(store.saveProject).toHaveBeenCalled();
    });
    expect(onSaved).toHaveBeenCalledWith('file-1');
    expect(await screen.findByText(i18n.t('videoStudio.save.saved'))).toBeTruthy();
  });

  it('says so when a saved project names a source the library no longer holds', async () => {
    store.loadProject.mockResolvedValue({ project: project(), missing: ['src-a'] });
    mount({ kind: 'saved', assetId: 'file-1' });

    expect((await screen.findByRole('alert')).textContent).toBe(
      i18n.t('videoStudio.refusal.sourceMissingAsset'),
    );
  });

  it('will not preview or export a project whose source is gone', async () => {
    store.loadProject.mockResolvedValue({ project: project(), missing: ['src-a'] });
    mount({ kind: 'saved', assetId: 'file-1' });
    await screen.findByRole('alert');

    // No renderer at all: a VideoFlow layer over a URL that 404s is a black rectangle that
    // reads as a preview, and an export of it is a file of black frames that reports success.
    expect(screen.queryByTestId('video-stage')).toBeNull();
    expect(screen.getByText(i18n.t('videoStudio.unplayable', { count: 2 }))).toBeTruthy();

    const exportButton = button('videoStudio.export.action');
    expect(exportButton.hasAttribute('disabled')).toBe(true);
    expect(exportButton.getAttribute('title')).toBe(
      i18n.t('videoStudio.refusal.sourceMissingAsset'),
    );
    fireEvent.click(exportButton);
    await waitFor(() => {
      expect(renderers.instances.length).toBe(0);
    });
    expect(flow.exportProject).not.toHaveBeenCalled();
    expect(downloadBlob).not.toHaveBeenCalled();
  });

  it('gives the preview and the export back once the clips that used it are gone', async () => {
    store.loadProject.mockResolvedValue({ project: project(), missing: ['src-a'] });
    mount({ kind: 'saved', assetId: 'file-1' });
    await screen.findByRole('alert');

    // Both clips play the missing source; removing them is the only way cycle 1 offers to
    // unblock, and the lane stays editable precisely so it can be taken.
    await removeSelected('videoStudio.timeline.clip');
    fireEvent.click(item('videoStudio.timeline.clip', 1));
    fireEvent.click(button('videoStudio.command.remove'));

    await waitFor(() => {
      expect(screen.getByTestId('video-stage')).toBeTruthy();
    });
    // Nothing on the lane now, so the export refuses for the OTHER reason and says which.
    expect(button('videoStudio.export.action').getAttribute('title')).toBe(
      i18n.t('videoStudio.export.empty'),
    );
  });

  it('drops a selection the edit took away, so Remove is never live over nothing', async () => {
    mount();
    await screen.findByTestId('video-stage');
    await removeSelected('videoStudio.timeline.clip');

    expect(button('videoStudio.command.remove').hasAttribute('disabled')).toBe(true);
    expect(screen.getByText(i18n.t('videoStudio.inspector.empty'))).toBeTruthy();
  });

  it('drops a selection an undo took away', async () => {
    mount();
    await screen.findByTestId('video-stage');
    fireEvent.click(button('videoStudio.command.addText'));
    await screen.findByLabelText(i18n.t('videoStudio.inspector.text'));

    fireEvent.click(button('videoStudio.command.undo'));
    await waitFor(() => {
      expect(screen.queryByLabelText(i18n.t('videoStudio.inspector.text'))).toBeNull();
    });
    expect(button('videoStudio.command.remove').hasAttribute('disabled')).toBe(true);
  });

  it('keeps a selection an edit left alone', async () => {
    mount();
    await screen.findByTestId('video-stage');
    fireEvent.click(item('videoStudio.timeline.clip', 2));
    // Muting clip 2 rebuilds the lane by value; the clip is still there and stays selected.
    fireEvent.click(screen.getByLabelText(i18n.t('videoStudio.inspector.mute')));

    await waitFor(() => {
      expect(button('videoStudio.command.remove').hasAttribute('disabled')).toBe(false);
    });
  });

  it('says so when the first source re-frames an empty project', async () => {
    media.probeVideo.mockResolvedValue({
      duration: 6,
      width: 1080,
      height: 1920,
      hasAudio: true,
      decodable: true,
    });
    mount({ kind: 'project', project: { ...project(), sources: [], video: [], overlays: [] } });
    await screen.findByTestId('video-stage');

    fireEvent.change(screen.getByLabelText(i18n.t('videoStudio.source.pick')), {
      target: { files: [new File(['x'], 'tall.mp4', { type: 'video/mp4' })] },
    });

    expect(
      await screen.findByText(i18n.t('videoStudio.frameAdopted', { width: 1080, height: 1920 })),
    ).toBeTruthy();
  });

  it('says nothing about the frame when a second source does not change it', async () => {
    media.probeVideo.mockResolvedValue({
      duration: 6,
      width: 1080,
      height: 1920,
      hasAudio: true,
      decodable: true,
    });
    mount();
    await screen.findByTestId('video-stage');

    fireEvent.change(screen.getByLabelText(i18n.t('videoStudio.source.pick')), {
      target: { files: [new File(['x'], 'tall.mp4', { type: 'video/mp4' })] },
    });

    await waitFor(() => {
      expect(
        screen.getByRole('button', { name: i18n.t('videoStudio.timeline.clip', { index: 3 }) }),
      ).toBeTruthy();
    });
    expect(
      screen.queryByText(i18n.t('videoStudio.frameAdopted', { width: 1080, height: 1920 })),
    ).toBeNull();
  });

  it('remembers where the save went, so the Studio can offer it back after a reload', async () => {
    mount();
    await screen.findByTestId('video-stage');
    fireEvent.click(button('videoStudio.save.action'));

    await waitFor(() => {
      expect(lastSavedProject()).toBe('file-1');
    });
  });

  it('stops the preview renderer when it closes', async () => {
    const { unmount } = mount();
    await screen.findByTestId('video-stage');
    unmount();
    expect(renderers.instances.every((instance) => instance.destroyed > 0)).toBe(true);
  });
});
