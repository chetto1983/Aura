// videoflow_transitions.ts — what a clip's layers play across their edges: the one transition a
// junction gives both clips where they overlap, else the clip's own. The picture takes them, and so
// does a cleaned clip's sound layer (videoflow_audio.ts): VideoFlow's `fade` lowers volume as well as
// opacity, and its mixer does so only for a layer that carries the transition itself
// (renderer-browser dist/audio/mixer.js applyAudioKeyframes, dist/transitions/presets.js `fade`).

import {
  clipTimelineDuration,
  junctionDurationAt,
  type ClipTransition,
  type VideoProject,
} from './project';

interface EdgeTransition {
  readonly transition: ClipTransition;
  readonly duration: number;
}

export interface ClipTransitions {
  readonly transitionIn?: EdgeTransition;
  readonly transitionOut?: EdgeTransition;
}

function junctionEdge(project: VideoProject, toIndex: number): EdgeTransition | undefined {
  const incoming = project.video[toIndex];
  const duration = junctionDurationAt(project, toIndex);
  if (incoming === undefined || duration <= 0) return undefined;
  const transition =
    incoming.junctionTransition === 'zoom'
      ? 'zoom'
      : incoming.junctionTransition === 'blur'
        ? 'blurResolve'
        : 'fade';
  return { transition, duration };
}

/** A clip's own transition, no longer than half of it. */
function ownEdge(
  transition: ClipTransition | undefined,
  duration: number | undefined,
  length: number,
): EdgeTransition | undefined {
  return transition === undefined || transition === 'none'
    ? undefined
    : { transition, duration: Math.min(duration ?? 1, length / 2) };
}

/** The transitions of the clip at `index`; a junction shared with a neighbour wins over its own. */
export function clipTransitions(project: VideoProject, index: number): ClipTransitions {
  const clip = project.video[index];
  if (clip === undefined) return {};
  const length = clipTimelineDuration(clip);
  const edgeIn =
    junctionEdge(project, index) ?? ownEdge(clip.transitionIn, clip.transitionInDuration, length);
  const edgeOut =
    junctionEdge(project, index + 1) ??
    ownEdge(clip.transitionOut, clip.transitionOutDuration, length);
  return {
    ...(edgeIn === undefined ? {} : { transitionIn: edgeIn }),
    ...(edgeOut === undefined ? {} : { transitionOut: edgeOut }),
  };
}
