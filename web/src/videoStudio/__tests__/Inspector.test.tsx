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

/** Two titles on one lane, so a selection can move from one to the other. */
function twoTitles(): VideoProject {
  const second: OverlayItem = {
    ...text({ text: 'SECOND' }),
    id: 'subtitle',
    anchor: { clipId: 'clip-1', offset: 3 },
  };
  return { ...project(text()), overlays: [{ id: 'lane-1', items: [text(), second] }] };
}

/** Two clips beginning at the same point of the same source — the pair a key made of the value
 *  alone cannot tell apart. */
function sameStart(): VideoProject {
  return {
    ...project(),
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false },
      { id: 'clip-2', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false },
    ],
  };
}

interface Mounted {
  readonly commands: ((current: VideoProject) => VideoProject)[];
  /** The project as the one command this test emitted leaves it. */
  readonly applied: (current?: VideoProject) => VideoProject;
  /** What the workspace does after an edit, an undo or a click on another item. */
  readonly show: (next: VideoProject, selectedId: string | undefined) => void;
}

function mount(current: VideoProject, selectedId: string | undefined): Mounted {
  const commands: ((project: VideoProject) => VideoProject)[] = [];
  function panel(shown: VideoProject, id: string | undefined) {
    return <Inspector project={shown} selectedId={id} onCommand={(edit) => commands.push(edit)} />;
  }
  const view = render(panel(current, selectedId));
  return {
    commands,
    applied: (from = current) => {
      const edit = commands[0];
      if (edit === undefined) throw new Error('no command was emitted');
      return edit(from);
    },
    show: (next, id) => {
      view.rerender(panel(next, id));
    },
  };
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

describe('Inspector, on a clip', () => {
  it('shows where the clip sits in its source', () => {
    mount(project(), 'clip-2');
    openTab('videoStudio.inspector.tabs.time');

    expect(screen.getByLabelText('videoStudio.inspector.start')).toHaveProperty('value', '00:04.0');
    expect(screen.getByLabelText('videoStudio.inspector.end')).toHaveProperty('value', '00:08.0');
  });

  it('crops the shared canvas from the same inspector', () => {
    const view = mount(project(), 'clip-1');
    fireEvent.click(screen.getByRole('radio', { name: 'videoStudio.inspector.transform.crop' }));
    fireEvent.click(screen.getByRole('button', { name: '1:1' }));
    expect(view.applied().size).toEqual({ width: 1080, height: 1080 });
  });

  it('rotates the selected clip from the same inspector', () => {
    const view = mount(project(), 'clip-1');
    fireEvent.click(screen.getByRole('button', { name: 'mediaEdit.video.rotateRight' }));
    expect(view.applied().video[0]).toMatchObject({ rotation: 90 });
  });

  it('changes playback speed from the shared controls', () => {
    const view = mount(project(), 'clip-1');
    openTab('videoStudio.inspector.tabs.speed');
    fireEvent.click(screen.getByRole('button', { name: '2×' }));
    expect(view.applied().video[0]).toMatchObject({ speed: 2 });
  });

  it('uses VideoFlow transition presets for clip animations', () => {
    const view = mount(project(), 'clip-1');
    openTab('videoStudio.inspector.tabs.animation');
    fireEvent.click(
      screen.getByRole('button', {
        name: 'videoStudio.inspector.animations.presets.blurResolve',
      }),
    );
    const updated = view.applied();
    expect(updated.video[0]).toMatchObject({ transitionIn: 'blurResolve' });
    view.show(updated, 'clip-1');
    const duration = screen.getByRole('group', {
      name: 'videoStudio.inspector.transitionDuration',
    });
    expect(duration.querySelector('[role="slider"]')?.getAttribute('aria-valuenow')).toBe('1');
  });

  // The one arithmetic this panel can get wrong: `trimClip` counts from where the clip already
  // starts in its source, so a field showing source time commits the DIFFERENCE. Adding it back
  // would move the clip twice — here, to 6 s instead of 2 s.
  it('commits a start measured from the clip, never from the source zero', () => {
    const view = mount(project(), 'clip-2');
    openTab('videoStudio.inspector.tabs.time');

    commit(screen.getByLabelText('videoStudio.inspector.start'), '00:02.0');

    expect(view.applied().video[1]).toMatchObject({ sourceStart: 2, duration: 6 });
  });

  it('commits an end measured the same way', () => {
    const view = mount(project(), 'clip-2');
    openTab('videoStudio.inspector.tabs.time');

    commit(screen.getByLabelText('videoStudio.inspector.end'), '00:06.0');

    expect(view.applied().video[1]).toMatchObject({ sourceStart: 4, duration: 2 });
  });

  it('restores a time it cannot read and commits nothing', () => {
    const view = mount(project(), 'clip-2');
    openTab('videoStudio.inspector.tabs.time');
    const field = screen.getByLabelText('videoStudio.inspector.start');

    commit(field, 'later');

    expect(view.commands).toHaveLength(0);
    expect(field).toHaveProperty('value', '00:04.0');
  });

  describe('removing a middle range', () => {
    // The spec's cycle-1 list names it beside trim, split, mute and reorder. Split-split-remove
    // reaches the same end state through three undo steps and is not the same gesture.
    it('offers the clip’s own bounds to narrow', () => {
      mount(project(), 'clip-2');
      openTab('videoStudio.inspector.tabs.time');

      expect(screen.getByLabelText('videoStudio.inspector.rangeFrom')).toHaveProperty(
        'value',
        '00:04.0',
      );
      expect(screen.getByLabelText('videoStudio.inspector.rangeTo')).toHaveProperty(
        'value',
        '00:08.0',
      );
    });

    it('takes a stretch out of the middle and closes the lane over it', () => {
      const view = mount(project(), 'clip-2');
      openTab('videoStudio.inspector.tabs.time');

      commit(screen.getByLabelText('videoStudio.inspector.rangeFrom'), '00:05.0');
      commit(screen.getByLabelText('videoStudio.inspector.rangeTo'), '00:06.0');
      expect(view.commands).toHaveLength(0);
      fireEvent.click(screen.getByRole('button', { name: 'videoStudio.inspector.removeRange' }));

      // The fields read SOURCE time, `removeRange` takes PROJECT time, and clip-2 starts at 4 in
      // the lane and at 4 in its source. The second of the two is what goes.
      const next = view.applied();
      expect(next.video).toHaveLength(3);
      expect(next.video[1]).toMatchObject({ sourceStart: 4, duration: 1 });
      expect(next.video[2]).toMatchObject({ sourceStart: 6, duration: 2 });
    });

    it('converts by the clip’s place in the lane, not by its place in the source', () => {
      // A clip whose source time and project time disagree: it starts at 4 in the lane and at 10
      // in the file. A conversion that forgot one of the two would remove the wrong second.
      const shifted: VideoProject = {
        ...project(),
        video: [
          { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false },
          { id: 'clip-2', sourceId: 'src-a', duration: 4, sourceStart: 10, muted: false },
        ],
      };
      const view = mount(shifted, 'clip-2');
      openTab('videoStudio.inspector.tabs.time');

      commit(screen.getByLabelText('videoStudio.inspector.rangeFrom'), '00:11.0');
      commit(screen.getByLabelText('videoStudio.inspector.rangeTo'), '00:12.0');
      fireEvent.click(screen.getByRole('button', { name: 'videoStudio.inspector.removeRange' }));

      const next = view.applied(shifted);
      expect(next.video).toHaveLength(3);
      expect(next.video[1]).toMatchObject({ sourceStart: 10, duration: 1 });
      expect(next.video[2]).toMatchObject({ sourceStart: 12, duration: 2 });
    });

    it('lets the command refuse a range that is not one', () => {
      const view = mount(project(), 'clip-2');
      openTab('videoStudio.inspector.tabs.time');

      commit(screen.getByLabelText('videoStudio.inspector.rangeTo'), '00:04.0');
      fireEvent.click(screen.getByRole('button', { name: 'videoStudio.inspector.removeRange' }));

      // The refusal belongs to the workspace, which words it: the panel only hands over the edit.
      expect(() => view.applied()).toThrow('videoStudio.refusal.emptyRange');
    });
  });

  it('mutes the clip', () => {
    const view = mount(project(), 'clip-2');
    openTab('videoStudio.inspector.tabs.audio');

    fireEvent.click(screen.getByRole('switch', { name: 'videoStudio.inspector.mute' }));

    expect(view.applied().video[1]).toMatchObject({ muted: true });
  });

  // A draft is state, and two clips can begin at the same point of the same source: keyed on the
  // value alone, the field would survive the switch and commit one clip's time onto the other.
  it('never carries a time from one clip into the next', () => {
    const twins = sameStart();
    const view = mount(twins, 'clip-1');
    openTab('videoStudio.inspector.tabs.time');
    fireEvent.change(screen.getByLabelText('videoStudio.inspector.start'), {
      target: { value: '00:03.0' },
    });

    view.show(twins, 'clip-2');
    openTab('videoStudio.inspector.tabs.time');

    expect(screen.getByLabelText('videoStudio.inspector.start')).toHaveProperty('value', '00:00.0');
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

  // The data loss this guards: type into one title, click the next one, and the words you typed
  // against the first are still on screen — and the next blur writes them into the second.
  it('never carries a draft from one overlay into the next', () => {
    const view = mount(twoTitles(), 'title');
    fireEvent.change(screen.getByLabelText('videoStudio.inspector.text'), {
      target: { value: 'HELLO' },
    });

    view.show(twoTitles(), 'subtitle');

    expect(screen.getByLabelText('videoStudio.inspector.text')).toHaveProperty('value', 'SECOND');

    commit(screen.getByLabelText('videoStudio.inspector.text'), 'THIRD');

    const edited = view.applied(twoTitles());
    expect(edited.overlays[0]?.items[0]?.props.text).toBe('AURA');
    expect(edited.overlays[0]?.items[1]?.props.text).toBe('THIRD');
  });

  it('follows the project when the value changes under it', () => {
    const view = mount(project(text()), 'title');
    fireEvent.change(screen.getByLabelText('videoStudio.inspector.text'), {
      target: { value: 'HELLO' },
    });

    view.show(project(text({ text: 'UNDONE' })), 'title');

    expect(screen.getByLabelText('videoStudio.inspector.text')).toHaveProperty('value', 'UNDONE');
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

  // `props.assetId` is the only thing in the model that can name an image, so without this field
  // an image overlay could never be given a source or have a wrong one corrected.
  it('names the asset it shows', () => {
    const view = mount(project(image()), 'badge');
    const field = screen.getByLabelText('videoStudio.inspector.asset');
    expect(field).toHaveProperty('value', 'photo');

    commit(field, 'other-photo');

    expect(view.applied().overlays[0]?.items[0]?.props.assetId).toBe('other-photo');
  });

  it('refuses a blank asset rather than pointing the layer at nothing', () => {
    const view = mount(project(image()), 'badge');
    const field = screen.getByLabelText('videoStudio.inspector.asset');

    commit(field, '   ');

    expect(view.commands).toHaveLength(0);
    expect(field).toHaveProperty('value', 'photo');
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
