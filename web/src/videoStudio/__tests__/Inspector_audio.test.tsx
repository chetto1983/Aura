import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { Inspector } from '../Inspector';
import type { AudioItem, VideoProject } from '../project';

// A sound in the inspector, and the audio controls a clip now shares with it. Asserting on keys
// keeps these about what a control commits rather than about copy.
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

function project(item: Partial<AudioItem> = {}): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 320, height: 180 },
    fps: 25,
    sources: [
      {
        id: 'src-a',
        assetId: 'a',
        kind: 'video',
        duration: 20,
        size: { width: 320, height: 180 },
        hasAudio: true,
      },
      { id: 'src-m', assetId: 'm', kind: 'audio', duration: 8, size: { width: 0, height: 0 } },
    ],
    video: [{ id: 'clip-1', sourceId: 'src-a', duration: 8, sourceStart: 0, muted: false }],
    overlays: [],
    audio: [
      {
        id: 'lane',
        items: [
          {
            id: 'bed',
            sourceId: 'src-m',
            anchor: { clipId: 'clip-1', offset: 1 },
            sourceStart: 2,
            duration: 4,
            volume: 1,
            muted: false,
            ...item,
          },
        ],
      },
    ],
  };
}

type Edit = (project: VideoProject) => VideoProject;

function mount(selectedId: string) {
  const commands: Edit[] = [];
  render(
    <Inspector
      project={project()}
      selectedId={selectedId}
      onCommand={(edit) => commands.push(edit)}
    />,
  );
  /** The n-th command the panel emitted, applied to the project it was emitted against. */
  return (index = 0): VideoProject => {
    const edit = commands[index];
    if (edit === undefined) throw new Error(`no command number ${String(index)} was emitted`);
    return edit(project());
  };
}

function bed(next: VideoProject): AudioItem | undefined {
  return next.audio?.[0]?.items[0];
}

/** A slider's thumb. Radix names the ROOT with `aria-label`, not the thumb, so the thumb is found
 *  inside the element the label names — the shape the cockpit's other slider tests use. */
function slider(name: string): HTMLElement {
  return within(screen.getByLabelText(name)).getByRole('slider');
}

function commit(field: HTMLElement, value: string): void {
  fireEvent.change(field, { target: { value } });
  fireEvent.keyDown(field, { key: 'Enter' });
  fireEvent.blur(field);
}

function openTab(name: string): void {
  const tab = screen.getByRole('tab', { name });
  fireEvent.mouseDown(tab, { button: 0, ctrlKey: false });
  fireEvent.click(tab);
}

describe('Inspector, on a sound', () => {
  it('offers the sound its three tabs, and falls back to Audio from a tab it does not have', () => {
    render(
      <Inspector project={project()} selectedId="bed" onCommand={vi.fn()} activeClipTab="adjust" />,
    );
    expect(screen.getAllByRole('tab').map((tab) => tab.textContent)).toEqual([
      'videoStudio.inspector.tabs.audio',
      'videoStudio.inspector.tabs.speed',
      'videoStudio.inspector.tabs.time',
    ]);
    expect(slider('videoStudio.inspector.volume')).toBeTruthy();
  });

  it('reports the tab it moves to', () => {
    const onClipTabChange = vi.fn();
    render(
      <Inspector
        project={project()}
        selectedId="bed"
        onCommand={vi.fn()}
        onClipTabChange={onClipTabChange}
      />,
    );
    openTab('videoStudio.inspector.tabs.time');
    expect(onClipTabChange).toHaveBeenCalledWith('time');
  });

  it('commits volume, mute and both fades', () => {
    const applied = mount('bed');
    fireEvent.keyDown(slider('videoStudio.inspector.volume'), {
      key: 'PageDown',
    });
    expect(bed(applied(0))?.volume).toBeCloseTo(0.9, 9);
    fireEvent.click(screen.getByRole('switch', { name: 'videoStudio.inspector.mute' }));
    expect(bed(applied(1))?.muted).toBe(true);
    fireEvent.keyDown(slider('videoStudio.audio.fadeIn'), {
      key: 'PageUp',
    });
    expect(bed(applied(2))?.fadeIn).toBeCloseTo(1, 9);
    fireEvent.keyDown(slider('videoStudio.audio.fadeOut'), {
      key: 'End',
    });
    expect(bed(applied(3))?.fadeOut).toBe(4);
  });

  it('sets the speed from a preset', () => {
    const applied = mount('bed');
    openTab('videoStudio.inspector.tabs.speed');
    fireEvent.click(screen.getByRole('button', { name: '2×' }));
    expect(bed(applied())?.speed).toBe(2);
  });

  it('moves the sound from its start field and trims it from In and Out, in source seconds', () => {
    const applied = mount('bed');
    openTab('videoStudio.inspector.tabs.time');
    expect(screen.getByLabelText('videoStudio.audio.startsAt')).toHaveProperty('value', '00:01.0');
    commit(screen.getByLabelText('videoStudio.audio.startsAt'), '00:03.0');
    expect(bed(applied(0))?.anchor.offset).toBe(3);
    commit(screen.getByLabelText('videoStudio.audio.in'), '00:03.0');
    expect(bed(applied(1))).toMatchObject({ sourceStart: 3, duration: 3 });
    commit(screen.getByLabelText('videoStudio.audio.out'), '00:05.0');
    expect(bed(applied(2))).toMatchObject({ sourceStart: 2, duration: 3 });
  });
});

describe('Inspector, on a clip Audio tab', () => {
  it('still sets the clip volume and mute from the controls it now shares', () => {
    const applied = mount('clip-1');
    openTab('videoStudio.inspector.tabs.audio');
    fireEvent.keyDown(slider('videoStudio.inspector.volume'), {
      key: 'Home',
    });
    expect(applied(0).video[0]?.volume).toBe(0);
    fireEvent.click(screen.getByRole('switch', { name: 'videoStudio.inspector.mute' }));
    expect(applied(1).video[0]?.muted).toBe(true);
  });

  it('extracts the clip sound from its Audio tab', () => {
    const applied = mount('clip-1');
    openTab('videoStudio.inspector.tabs.audio');
    fireEvent.click(screen.getByRole('button', { name: 'videoStudio.audio.extract' }));
    const next = applied();
    expect(next.video[0]?.muted).toBe(true);
    expect(
      next.audio?.flatMap((lane) => lane.items).some((item) => item.extractedFrom === 'clip-1'),
    ).toBe(true);
  });

  it('still sets the clip speed from the shared control', () => {
    const applied = mount('clip-1');
    openTab('videoStudio.inspector.tabs.speed');
    fireEvent.click(screen.getByRole('button', { name: '0.5×' }));
    expect(applied().video[0]?.speed).toBe(0.5);
  });
});
