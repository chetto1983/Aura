import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { JunctionTransitionInspector } from '../Inspector_transition';
import { projectDuration, type VideoProject } from '../project';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

function project(): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 1920, height: 1080 },
    fps: 25,
    sources: [
      {
        id: 'source',
        assetId: 'asset',
        kind: 'video',
        duration: 10,
        size: { width: 1920, height: 1080 },
      },
    ],
    video: [
      { id: 'left', sourceId: 'source', duration: 4, sourceStart: 0, muted: false },
      { id: 'right', sourceId: 'source', duration: 4, sourceStart: 4, muted: false },
    ],
    overlays: [],
  };
}

describe('JunctionTransitionInspector', () => {
  it('applies a real overlap to the selected clip boundary', () => {
    const edits: ((current: VideoProject) => VideoProject)[] = [];
    const base = project();
    const left = base.video[0];
    const right = base.video[1];
    if (left === undefined || right === undefined) throw new Error('project fixture lost a clip');
    render(
      <JunctionTransitionInspector
        project={base}
        junction={{ fromClipId: 'left', toClipId: 'right' }}
        onCommand={(edit) => edits.push(edit)}
      />,
    );

    fireEvent.click(
      screen.getByRole('button', {
        name: 'videoStudio.inspector.transition.presets.crossfade',
      }),
    );
    const edit = edits[0];
    if (edit === undefined) throw new Error('transition emitted no edit');
    const next = edit(base);
    expect(next.video[1]).toMatchObject({
      junctionFromClipId: 'left',
      junctionTransition: 'crossfade',
      junctionDuration: 1,
    });
    expect(projectDuration(next)).toBe(7);
  });

  it('shows the duration only for an active transition', () => {
    const base = project();
    const left = base.video[0];
    const right = base.video[1];
    if (left === undefined || right === undefined) throw new Error('project fixture lost a clip');
    const active: VideoProject = {
      ...base,
      video: [
        left,
        {
          ...right,
          junctionFromClipId: 'left',
          junctionTransition: 'fadeWhite',
          junctionDuration: 1.5,
        },
      ],
    };
    const { rerender } = render(
      <JunctionTransitionInspector
        project={base}
        junction={{ fromClipId: 'left', toClipId: 'right' }}
        onCommand={vi.fn()}
      />,
    );
    expect(
      screen.queryByRole('group', { name: 'videoStudio.inspector.transition.duration' }),
    ).toBeNull();
    rerender(
      <JunctionTransitionInspector
        project={active}
        junction={{ fromClipId: 'left', toClipId: 'right' }}
        onCommand={vi.fn()}
      />,
    );
    expect(
      screen.getByRole('group', { name: 'videoStudio.inspector.transition.duration' }),
    ).toBeTruthy();
  });
});
