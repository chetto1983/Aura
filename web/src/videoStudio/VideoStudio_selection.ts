// VideoStudio_selection.ts — what the workspace asks of a project around its selection: whether an
// id still names something after an edit, which item an edit added, and what Split means for what
// is selected. Pure, so the shell's own file keeps to drawing.

import { findAudioItem } from './audioLane';
import { splitAt } from './commands';
import { splitAudio } from './commands_audio';
import { audioTracks, type VideoProject } from './project';

type Edit = (project: VideoProject) => VideoProject;

export function overlayCount(project: VideoProject): number {
  return project.overlays.reduce((total, lane) => total + lane.items.length, 0);
}

/** Overlays and sounds: the items an edit can add and the workspace then selects. */
function itemIds(project: VideoProject): readonly string[] {
  return [
    ...project.overlays.flatMap((lane) => lane.items.map((item) => item.id)),
    ...audioTracks(project).flatMap((lane) => lane.items.map((item) => item.id)),
  ];
}

export function addedItem(before: VideoProject, after: VideoProject): string | undefined {
  const had = new Set(itemIds(before));
  return itemIds(after).find((id) => !had.has(id));
}

export function holds(project: VideoProject, id: string | undefined): boolean {
  if (id === undefined) return false;
  return project.video.some((clip) => clip.id === id) || itemIds(project).includes(id);
}

export function unplayableClips(
  project: VideoProject,
  missing: readonly string[],
): readonly string[] {
  return project.video.filter((clip) => missing.includes(clip.sourceId)).map((clip) => clip.id);
}

/** Split cuts what is selected when that is a sound, and the clip under the playhead otherwise. */
export function splitEdit(selectedId: string | undefined, time: number): Edit {
  return (project) =>
    selectedId !== undefined && findAudioItem(project, selectedId) !== undefined
      ? splitAudio(project, { itemId: selectedId, time })
      : splitAt(project, { time });
}
