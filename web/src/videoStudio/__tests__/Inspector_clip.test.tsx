import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { Inspector } from '../Inspector';
import type { VideoItem, VideoProject } from '../project';

// The clip panels Inspector.test.tsx does not reach: how a clip is framed, flipped and turned, its
// look, and its entrance and exit. Each control is judged by the edit it emits.
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

function project(clip: Partial<VideoItem> = {}): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 1920, height: 1080 },
    fps: 25,
    sources: [
      {
        id: 'src-a',
        assetId: 'a',
        kind: 'video',
        duration: 20,
        size: { width: 1920, height: 1080 },
      },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 8, sourceStart: 0, muted: false, ...clip },
    ],
    overlays: [],
  };
}

type Edit = (project: VideoProject) => VideoProject;

/** The panel on one clip, and the n-th edit it emitted applied to the project it was shown. */
function mount(shown: VideoProject = project()) {
  const edits: Edit[] = [];
  render(<Inspector project={shown} selectedId="clip-1" onCommand={(edit) => edits.push(edit)} />);
  return (index = 0): VideoItem | undefined => {
    const edit = edits[index];
    if (edit === undefined) throw new Error(`no edit number ${String(index)} was emitted`);
    return edit(shown).video[0];
  };
}

function openTab(name: string): void {
  const tab = screen.getByRole('tab', { name });
  fireEvent.mouseDown(tab, { button: 0, ctrlKey: false });
  fireEvent.click(tab);
}

/** Radix names a slider's root with `aria-label`; the thumb the keys move is inside it. */
function slider(name: string): HTMLElement {
  return within(screen.getByLabelText(name)).getByRole('slider');
}

describe('Inspector, framing a clip', () => {
  it('fits and fills the frame', () => {
    const applied = mount();
    fireEvent.click(screen.getByRole('radio', { name: 'videoStudio.inspector.transform.fit' }));
    expect(applied(0)?.fit).toBe('contain');
    fireEvent.click(screen.getByRole('radio', { name: 'videoStudio.inspector.transform.fill' }));
    expect(applied(1)?.fit).toBe('cover');
  });

  it('flips both ways and turns a quarter left', () => {
    const applied = mount(project({ flipX: true }));
    fireEvent.click(
      screen.getByRole('button', { name: 'videoStudio.inspector.transform.flipHorizontal' }),
    );
    expect(applied(0)?.flipX).toBe(false);
    fireEvent.click(
      screen.getByRole('button', { name: 'videoStudio.inspector.transform.flipVertical' }),
    );
    expect(applied(1)?.flipY).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: 'mediaEdit.video.rotateLeft' }));
    expect(applied(2)?.rotation).toBe(270);
  });
});

describe('Inspector, the look of a clip', () => {
  it('writes each adjustment in the unit the renderer reads', () => {
    const applied = mount();
    openTab('videoStudio.inspector.tabs.adjust');
    fireEvent.keyDown(slider('videoStudio.inspector.adjust.brightness'), { key: 'End' });
    expect(applied(0)?.brightness).toBe(2);
    fireEvent.keyDown(slider('videoStudio.inspector.adjust.hue'), { key: 'Home' });
    expect(applied(1)?.hue).toBe(-180);
    fireEvent.keyDown(slider('videoStudio.inspector.adjust.blur'), { key: 'End' });
    expect(applied(2)?.blur).toBe(2);
    fireEvent.keyDown(slider('videoStudio.inspector.adjust.opacity'), { key: 'Home' });
    expect(applied(3)?.opacity).toBe(0);
  });
});

describe('Inspector, the entrance and exit of a clip', () => {
  it('sets a transition on each edge and its duration', () => {
    const applied = mount(project({ transitionOut: 'zoom' }));
    openTab('videoStudio.inspector.tabs.animation');
    fireEvent.click(
      screen.getByRole('button', { name: 'videoStudio.inspector.animations.presets.fade' }),
    );
    expect(applied(0)).toMatchObject({ transitionIn: 'fade', animation: 'none', fadeIn: false });

    fireEvent.click(screen.getByRole('radio', { name: 'videoStudio.inspector.animations.out' }));
    const zoom = screen.getByRole('button', {
      name: 'videoStudio.inspector.animations.presets.zoom',
    });
    expect(zoom.getAttribute('aria-pressed')).toBe('true');
    // Pressing the transition already chosen opens its duration instead of choosing it again.
    fireEvent.click(zoom);
    const duration = screen.getByRole('group', {
      name: 'videoStudio.inspector.transitionDuration',
    });
    fireEvent.keyDown(within(duration).getByRole('slider'), { key: 'End' });
    expect(applied(1)?.transitionOutDuration).toBe(4);
    // And pressing it once more closes it again.
    fireEvent.click(zoom);
    expect(
      screen.queryByRole('group', { name: 'videoStudio.inspector.transitionDuration' }),
    ).toBeNull();
  });
});
