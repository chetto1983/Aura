import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import i18n from '../../i18n/i18n';
import type { VideoProject } from '../project';

// The workspace's own controls that VideoStudio.test.tsx does not drive: the play loop, redo, the
// full-screen preview, a selected junction surviving an edit, and the phone bar's handlers. On the
// real bundle, like its sibling, so every sentence shown is one the operator would read.

const renderers = vi.hoisted(() => ({
  instances: [] as { onFrame?: (frame: number) => void }[],
}));

vi.mock('@videoflow/renderer-dom', () => ({
  default: class {
    loadedFonts: Record<string, string> = {};
    stopped = 0;
    onFrame?: (frame: number) => void;
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
    play(): Promise<void> {
      return Promise.resolve();
    }
    stop(): void {
      this.stopped += 1;
    }
    destroy(): void {
      // Nothing to release: the renderer is a stand-in.
    }
  },
}));

vi.mock('@videoflow/renderer-browser', () => ({
  default: class {
    loadedFonts: Record<string, string> = {};
  },
}));

const media = vi.hoisted(() => ({ probeVideo: vi.fn(), probeAudio: vi.fn() }));
vi.mock('../../mediaEdit/videoMedia', () => media);

const assets = vi.hoisted(() => ({
  presignAsset: vi.fn(),
  finalizeAsset: vi.fn(),
  finalizeMediaAsset: vi.fn(),
}));
vi.mock('../../chat/attachments/api', () => assets);
vi.mock('../../chat/attachments/upload', () => ({ putWithProgress: () => Promise.resolve() }));
// Add a clip's panel lists the library; the list is VideoStudio_library.test.tsx's to judge.
vi.mock('../VideoStudio_library', () => ({ LibraryPicker: () => null }));

const { default: VideoStudio } = await import('../VideoStudio');

function film(): VideoProject {
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
    overlays: [],
  };
}

async function mount(): Promise<void> {
  render(<VideoStudio open={{ kind: 'project', project: film() }} onClose={vi.fn()} />);
  await screen.findByRole('button', { name: i18n.t('videoStudio.timeline.clip', { index: 1 }) });
}

function button(key: string, values: Record<string, unknown> = {}): HTMLElement {
  return screen.getByRole('button', { name: i18n.t(key, values) });
}

function playhead(): HTMLElement {
  return screen.getByRole('slider', { name: i18n.t('videoStudio.timeline.playhead') });
}

afterEach(() => {
  renderers.instances.length = 0;
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe('VideoStudio, the transport', () => {
  it('plays to the end and stops, and plays again from the start', async () => {
    await mount();
    fireEvent.click(button('videoStudio.transport.play'));
    act(() => {
      renderers.instances.at(-1)?.onFrame?.(8 * 25);
    });
    expect(playhead().getAttribute('aria-valuenow')).toBe('8');
    act(() => {
      renderers.instances.at(-1)?.onFrame?.(0);
    });
    // Stopped at the end, so the button offers play again — and play starts over.
    fireEvent.click(button('videoStudio.transport.play'));
    expect(playhead().getAttribute('aria-valuenow')).toBe('0');
    expect(button('videoStudio.transport.pause')).toBeTruthy();
  });
});

describe('VideoStudio, the history', () => {
  it('redoes what was undone', async () => {
    await mount();
    fireEvent.keyDown(playhead(), { key: 'ArrowRight', shiftKey: true });
    fireEvent.click(button('videoStudio.command.split'));
    await screen.findByRole('button', { name: i18n.t('videoStudio.timeline.clip', { index: 3 }) });
    fireEvent.click(button('videoStudio.command.undo'));
    await waitFor(() => {
      expect(
        screen.queryByRole('button', { name: i18n.t('videoStudio.timeline.clip', { index: 3 }) }),
      ).toBeNull();
    });
    fireEvent.click(button('videoStudio.command.redo'));
    expect(
      await screen.findByRole('button', {
        name: i18n.t('videoStudio.timeline.clip', { index: 3 }),
      }),
    ).toBeTruthy();
  });
});

describe('VideoStudio, the full-screen preview', () => {
  it('asks the browser for full screen, and says so when it refuses', async () => {
    const request = vi.fn(() => Promise.resolve());
    Object.defineProperty(HTMLElement.prototype, 'requestFullscreen', {
      configurable: true,
      value: request,
    });
    await mount();
    fireEvent.click(button('videoStudio.stage.fullscreen'));
    expect(request).toHaveBeenCalledOnce();
    request.mockImplementation(() => Promise.reject(new Error('denied')));
    fireEvent.click(button('videoStudio.stage.fullscreen'));
    expect((await screen.findByRole('alert')).textContent).toBe(
      i18n.t('videoStudio.stage.fullscreenFailed'),
    );
  });
});

describe('VideoStudio, a selected junction', () => {
  it('opens the transition panel, and stays selected across an edit that keeps both clips', async () => {
    await mount();
    fireEvent.click(button('videoStudio.timeline.transition', { index: 1, next: 2 }));
    const properties = document.querySelector<HTMLElement>('.video-studio-properties');
    if (properties === null) throw new Error('the properties panel is missing');
    expect(properties.dataset.mobileOpen).toBe('true');
    // An edit elsewhere on the lane — a split of the second clip at 6 s — keeps the junction's two
    // clips. The ruler steps a second per shifted arrow.
    for (let step = 0; step < 6; step += 1) {
      fireEvent.keyDown(playhead(), { key: 'ArrowRight', shiftKey: true });
    }
    fireEvent.click(button('videoStudio.command.split'));
    await screen.findByRole('button', { name: i18n.t('videoStudio.timeline.clip', { index: 3 }) });
    expect(
      button('videoStudio.timeline.transition', { index: 1, next: 2 }).getAttribute('aria-pressed'),
    ).toBe('true');
  });
});

describe('VideoStudio, the phone bar', () => {
  it('backs out of the panel, then out of the selection, and offers the add actions', async () => {
    await mount();
    const bar = screen.getByRole('navigation', { name: i18n.t('videoStudio.mobileTools') });
    fireEvent.click(within(bar).getByRole('button', { name: i18n.t('videoStudio.mobile.adjust') }));
    const properties = document.querySelector<HTMLElement>('.video-studio-properties');
    expect(properties?.dataset.mobileOpen).toBe('true');
    fireEvent.click(within(bar).getByRole('button', { name: i18n.t('videoStudio.mobile.back') }));
    expect(properties?.dataset.mobileOpen).toBe('false');
    fireEvent.click(within(bar).getByRole('button', { name: i18n.t('videoStudio.mobile.back') }));
    const picker = screen.getByLabelText(i18n.t('videoStudio.source.pick'));
    const pick = vi.spyOn(picker, 'click');
    fireEvent.click(
      within(bar).getByRole('button', { name: i18n.t('videoStudio.command.addSource') }),
    );
    // Add a clip offers the device and the library: the device is one more press away.
    const panel = await screen.findByRole('dialog', {
      name: i18n.t('videoStudio.clipPanel.title'),
    });
    fireEvent.click(
      within(panel).getByRole('button', { name: i18n.t('videoStudio.clipPanel.upload') }),
    );
    expect(pick).toHaveBeenCalledOnce();
  });

  it('splits and removes what is selected', async () => {
    await mount();
    const bar = screen.getByRole('navigation', { name: i18n.t('videoStudio.mobileTools') });
    fireEvent.keyDown(playhead(), { key: 'ArrowRight', shiftKey: true });
    fireEvent.click(within(bar).getByRole('button', { name: i18n.t('videoStudio.mobile.split') }));
    await screen.findByRole('button', { name: i18n.t('videoStudio.timeline.clip', { index: 3 }) });
    fireEvent.click(within(bar).getByRole('button', { name: i18n.t('videoStudio.mobile.delete') }));
    await waitFor(() => {
      expect(
        screen.queryByRole('button', { name: i18n.t('videoStudio.timeline.clip', { index: 3 }) }),
      ).toBeNull();
    });
  });
});

describe('VideoStudio, the rail', () => {
  it('hides the properties panel on Properties, and brings it back with focus inside', async () => {
    await mount();
    const rail = screen.getByRole('toolbar', { name: i18n.t('videoStudio.commands') });
    const properties = within(rail).getByRole('button', {
      name: i18n.t('videoStudio.inspector.label'),
    });
    const shell = document.querySelector<HTMLElement>('.video-studio-shell');
    const panel = document.querySelector<HTMLElement>('.video-studio-properties');
    expect(properties.getAttribute('aria-pressed')).toBe('true');
    expect(shell?.dataset.properties).toBe('shown');

    // Pressed with the panel open (operator, 2026-09-28: "property button do nothing"): it goes,
    // and the stage takes its width.
    fireEvent.click(properties);
    expect(properties.getAttribute('aria-pressed')).toBe('false');
    expect(shell?.dataset.properties).toBe('hidden');

    fireEvent.click(properties);
    expect(properties.getAttribute('aria-pressed')).toBe('true');
    expect(shell?.dataset.properties).toBe('shown');
    expect(document.activeElement).toBe(panel);
  });
});
