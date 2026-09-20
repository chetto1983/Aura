import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { Inspector } from '../Inspector';
import type { OverlayItem, VideoProject } from '../project';

// Task 8 writes the sentences; asserting on keys keeps these tests about what a field commits
// rather than about copy that does not exist yet.
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

function project(overlay?: OverlayItem): VideoProject {
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
        fps: 25,
      },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false },
      { id: 'clip-2', sourceId: 'src-a', duration: 4, sourceStart: 4, muted: false },
    ],
    overlays: overlay === undefined ? [] : [{ id: 'lane-1', items: [overlay] }],
  };
}

function text(props: Readonly<Record<string, unknown>> = {}): OverlayItem {
  return {
    id: 'title',
    kind: 'text',
    anchor: { clipId: 'clip-1', offset: 1 },
    duration: 2,
    props: { text: 'AURA', fontSize: 4, color: '#FFFFFF', ...props },
  };
}

function image(props: Readonly<Record<string, unknown>> = {}): OverlayItem {
  return {
    id: 'badge',
    kind: 'image',
    anchor: { clipId: 'clip-1', offset: 1 },
    duration: 2,
    props: { assetId: 'photo', scale: 0.5, ...props },
  };
}

interface Mounted {
  readonly commands: ((current: VideoProject) => VideoProject)[];
  /** The project as the one command this test emitted leaves it. */
  readonly applied: (current?: VideoProject) => VideoProject;
}

function mount(current: VideoProject, selectedId: string | undefined): Mounted {
  const commands: ((project: VideoProject) => VideoProject)[] = [];
  render(
    <Inspector
      project={current}
      selectedId={selectedId}
      onCommand={(edit) => commands.push(edit)}
    />,
  );
  return {
    commands,
    applied: (from = current) => {
      const edit = commands[0];
      if (edit === undefined) throw new Error('no command was emitted');
      return edit(from);
    },
  };
}

function commit(field: HTMLElement, value: string): void {
  fireEvent.change(field, { target: { value } });
  fireEvent.keyDown(field, { key: 'Enter' });
  fireEvent.blur(field);
}

describe('Inspector, on a clip', () => {
  it('shows where the clip sits in its source', () => {
    mount(project(), 'clip-2');

    expect(screen.getByLabelText('videoStudio.inspector.start')).toHaveProperty('value', '00:04.0');
    expect(screen.getByLabelText('videoStudio.inspector.end')).toHaveProperty('value', '00:08.0');
  });

  // The one arithmetic this panel can get wrong: `trimClip` counts from where the clip already
  // starts in its source, so a field showing source time commits the DIFFERENCE. Adding it back
  // would move the clip twice — here, to 6 s instead of 2 s.
  it('commits a start measured from the clip, never from the source zero', () => {
    const view = mount(project(), 'clip-2');

    commit(screen.getByLabelText('videoStudio.inspector.start'), '00:02.0');

    expect(view.applied().video[1]).toMatchObject({ sourceStart: 2, duration: 6 });
  });

  it('commits an end measured the same way', () => {
    const view = mount(project(), 'clip-2');

    commit(screen.getByLabelText('videoStudio.inspector.end'), '00:06.0');

    expect(view.applied().video[1]).toMatchObject({ sourceStart: 4, duration: 2 });
  });

  it('restores a time it cannot read and commits nothing', () => {
    const view = mount(project(), 'clip-2');
    const field = screen.getByLabelText('videoStudio.inspector.start');

    commit(field, 'later');

    expect(view.commands).toHaveLength(0);
    expect(field).toHaveProperty('value', '00:04.0');
  });

  it('mutes the clip', () => {
    const view = mount(project(), 'clip-2');

    fireEvent.click(screen.getByRole('switch', { name: 'videoStudio.inspector.mute' }));

    expect(view.applied().video[1]).toMatchObject({ muted: true });
  });
});

describe('Inspector, on a text overlay', () => {
  it('commits the text, the size and the colour', () => {
    const view = mount(project(text()), 'title');

    commit(screen.getByLabelText('videoStudio.inspector.text'), 'HELLO');
    expect(view.applied().overlays[0]?.items[0]?.props.text).toBe('HELLO');

    commit(screen.getByLabelText('videoStudio.inspector.size'), '6');
    expect(view.commands[1]?.(project(text())).overlays[0]?.items[0]?.props.fontSize).toBe(6);

    commit(screen.getByLabelText('videoStudio.inspector.color'), '#ff0000');
    expect(view.commands[2]?.(project(text())).overlays[0]?.items[0]?.props.color).toBe('#ff0000');
  });

  it('restores a size it cannot read and commits nothing', () => {
    const view = mount(project(text()), 'title');
    const field = screen.getByLabelText('videoStudio.inspector.size');

    commit(field, 'big');

    expect(view.commands).toHaveLength(0);
    expect(field).toHaveProperty('value', '4');
  });

  // A property whose value is a list of keyframes compiles to an animation (BaseLayer.toJSON), so
  // the fade the operator picks IS the opacity property — nothing else has to carry it.
  it('writes the chosen fade as keyframes over the overlay window', () => {
    const view = mount(project(text()), 'title');

    fireEvent.change(screen.getByRole('combobox', { name: 'videoStudio.inspector.animation' }), {
      target: { value: 'fadeIn' },
    });

    expect(view.applied().overlays[0]?.items[0]?.props.opacity).toEqual([
      { time: 0, value: 0 },
      { time: 0.5, value: 1 },
    ]);
  });

  it('reads the fade back off the keyframes it wrote', () => {
    const faded = text({
      opacity: [
        { time: 1.5, value: 1 },
        { time: 2, value: 0 },
      ],
    });
    mount(project(faded), 'title');

    expect(
      screen.getByRole('combobox', { name: 'videoStudio.inspector.animation' }),
    ).toHaveProperty('value', 'fadeOut');
  });

  it('puts a full-strength layer back when the fade is taken off', () => {
    const faded = text({
      opacity: [
        { time: 0, value: 0 },
        { time: 0.5, value: 1 },
      ],
    });
    const view = mount(project(faded), 'title');

    fireEvent.change(screen.getByRole('combobox', { name: 'videoStudio.inspector.animation' }), {
      target: { value: 'none' },
    });

    expect(view.applied().overlays[0]?.items[0]?.props.opacity).toBe(1);
  });
});

describe('Inspector, on an image overlay', () => {
  it('commits the scale, and offers no text of its own', () => {
    const view = mount(project(image()), 'badge');

    expect(screen.queryByLabelText('videoStudio.inspector.text')).toBeNull();
    expect(screen.queryByLabelText('videoStudio.inspector.color')).toBeNull();

    commit(screen.getByLabelText('videoStudio.inspector.scale'), '1.25');

    expect(view.applied().overlays[0]?.items[0]?.props.scale).toBe(1.25);
  });

  it('animates like a text overlay does', () => {
    const view = mount(project(image()), 'badge');

    fireEvent.change(screen.getByRole('combobox', { name: 'videoStudio.inspector.animation' }), {
      target: { value: 'fadeOut' },
    });

    expect(view.applied().overlays[0]?.items[0]?.props.opacity).toEqual([
      { time: 1.5, value: 1 },
      { time: 2, value: 0 },
    ]);
  });
});

describe('Inspector, with nothing to show', () => {
  it('says so rather than showing an empty panel', () => {
    mount(project(), undefined);

    expect(screen.getByRole('status').textContent).toBe('videoStudio.inspector.empty');
  });

  it('says so for a selection the project no longer has', () => {
    mount(project(), 'gone');

    expect(screen.getByRole('status').textContent).toBe('videoStudio.inspector.empty');
  });
});
