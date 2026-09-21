import { IDENTITY_SCOPED } from '../chat/artifacts/renderers/assetSourceContext';
import { finalizeAsset, getAsset, presignAsset } from '../chat/attachments/api';
import { isTerminalAsset, putWithProgress } from '../chat/attachments/upload';
import { assetIsGone } from './assetStatus';
import type { OverlayItem, OverlayTrack, ProjectSource, VideoItem, VideoProject } from './project';

// projectStore.ts — the project as a file. It is a `.json` DOCUMENT uploaded through the same
// presign the chat attachments use, which is what puts it under `chat/`: the server files by
// modality (internal/assets/service.go `folderFor`), and `media/` is where the sources live.
// No table, no route and no migration of its own — a project is bytes with an asset id.
//
// A load answers with the project AND with the sources whose bytes are gone, because a saved
// project outlives the clips it names: the library sweeps, and an asset id in a file is a claim
// about the past. Only a source that is really gone is reported gone — an expired session or a
// 500 is a different sentence, and telling an operator their file was permanently deleted
// because a proxy hiccuped is the worse of the two wrong answers.

/** The asset route a load reads through — the `useAssetSource` seam, as a value. */
export interface ProjectAssetSource {
  readonly assetUrl: (assetId: string) => string;
  readonly credentials: RequestCredentials;
}

export interface LoadedProject {
  readonly project: VideoProject;
  /** `ProjectSource.id` of every source whose asset the library no longer holds. */
  readonly missing: readonly string[];
}

const PROJECT_MIME = 'application/json';
/** Long enough to recognise the project, short enough that no store has to think about it. */
const NAME_MAX = 60;

/**
 * What the project is called on disk, saved or exported. Its name is operator input — a Studio
 * prompt, in the commonest case — so it is reduced to a slug rather than used: the server builds
 * its object key from the asset id, but the name also travels through headers, file cards and a
 * download attribute, and `../` in any of them is nobody's idea of a title.
 *
 * `name` is passed separately because an unnamed project reads as `videoStudio.untitled` on
 * screen and must read the same on disk: the fallback is a translated string the CALLER resolves,
 * never a word written here. A name that survives slugging as nothing at all falls back to the
 * project's own id, which is an identifier rather than prose and so needs no language.
 */
export function projectFileName(
  project: VideoProject,
  extension: string,
  name = project.name,
): string {
  const slug = name
    .normalize('NFKD')
    .replace(/[^a-zA-Z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, NAME_MAX);
  return `${slug === '' ? project.id : slug}.${extension}`;
}

/** Save the project and answer with the asset id it now lives at. */
export async function saveProject(project: VideoProject, name = project.name): Promise<string> {
  const file = new File([JSON.stringify(project)], projectFileName(project, 'json', name), {
    type: PROJECT_MIME,
  });
  const presign = await presignAsset({
    // No thread: a project belongs to the identity, the way a Studio frame does.
    thread_id: '',
    file_name: file.name,
    mime_type: PROJECT_MIME,
    size_bytes: file.size,
    modality_hint: 'document',
  });
  await putWithProgress(
    presign.upload.upload_url,
    file,
    presign.upload.required_headers,
    // The file is a few kilobytes: there is no progress worth drawing, and inventing a bar for
    // it would be a lie about how long the save takes.
    () => undefined,
  );
  const saved = await finalizeAsset(presign.asset.id);
  return saved.id;
}

/** Where the last project saved on this browser lives, so the Studio can offer it back after a
 *  reload. A listing of every saved project needs the library door, which is cycle 2's. */
const LAST_SAVED_KEY = 'aura.videoStudio.lastSavedProject';

export function rememberSavedProject(assetId: string): void {
  try {
    localStorage.setItem(LAST_SAVED_KEY, assetId);
  } catch {
    // A browser refusing storage is one where the entrance simply does not appear after a
    // reload. Explicitly silenced: it must never fail the save it followed.
  }
}

export function lastSavedProject(): string | undefined {
  try {
    return localStorage.getItem(LAST_SAVED_KEY) ?? undefined;
  } catch {
    return undefined;
  }
}

type Bag = Record<string, unknown>;
const CLIP_TRANSITIONS = new Set([
  'none',
  'fade',
  'blurResolve',
  'zoom',
  'slideUp',
  'slideDown',
  'slideLeft',
  'slideRight',
  'overshootPop',
  'glitchResolve',
  'wipeReveal',
  'lightSweepReveal',
]);

function bagOf(value: unknown): Bag | undefined {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Bag)
    : undefined;
}

function isSize(value: unknown): boolean {
  const size = bagOf(value);
  return size !== undefined && typeof size.width === 'number' && typeof size.height === 'number';
}

function isSource(value: unknown): value is ProjectSource {
  const source = bagOf(value);
  return (
    source !== undefined &&
    typeof source.id === 'string' &&
    typeof source.assetId === 'string' &&
    (source.kind === 'video' || source.kind === 'image') &&
    typeof source.duration === 'number' &&
    (source.hasAudio === undefined || typeof source.hasAudio === 'boolean') &&
    // No `fps`: nothing ever measured a source's frame rate — `probeVideo` does not report one —
    // and a file saved while the field existed still loads, with the number simply ignored.
    isSize(source.size)
  );
}

function isClip(value: unknown): value is VideoItem {
  const clip = bagOf(value);
  return (
    clip !== undefined &&
    typeof clip.id === 'string' &&
    typeof clip.sourceId === 'string' &&
    typeof clip.duration === 'number' &&
    typeof clip.sourceStart === 'number' &&
    typeof clip.muted === 'boolean' &&
    (clip.volume === undefined || typeof clip.volume === 'number') &&
    (clip.rotation === undefined || [0, 90, 180, 270].includes(clip.rotation as number)) &&
    (clip.fit === undefined || clip.fit === 'contain' || clip.fit === 'cover') &&
    (clip.flipX === undefined || typeof clip.flipX === 'boolean') &&
    (clip.flipY === undefined || typeof clip.flipY === 'boolean') &&
    (clip.brightness === undefined || typeof clip.brightness === 'number') &&
    (clip.contrast === undefined || typeof clip.contrast === 'number') &&
    (clip.saturation === undefined || typeof clip.saturation === 'number') &&
    (clip.hue === undefined || typeof clip.hue === 'number') &&
    (clip.blur === undefined || typeof clip.blur === 'number') &&
    (clip.opacity === undefined || typeof clip.opacity === 'number') &&
    (clip.animation === undefined ||
      clip.animation === 'none' ||
      clip.animation === 'fadeIn' ||
      clip.animation === 'fadeOut') &&
    (clip.fadeIn === undefined || typeof clip.fadeIn === 'boolean') &&
    (clip.fadeOut === undefined || typeof clip.fadeOut === 'boolean') &&
    (clip.speed === undefined || typeof clip.speed === 'number') &&
    (clip.transitionIn === undefined ||
      (typeof clip.transitionIn === 'string' && CLIP_TRANSITIONS.has(clip.transitionIn))) &&
    (clip.transitionOut === undefined ||
      (typeof clip.transitionOut === 'string' && CLIP_TRANSITIONS.has(clip.transitionOut))) &&
    (clip.transitionInDuration === undefined || typeof clip.transitionInDuration === 'number') &&
    (clip.transitionOutDuration === undefined || typeof clip.transitionOutDuration === 'number')
  );
}

function isOverlayItem(value: unknown): value is OverlayItem {
  const item = bagOf(value);
  if (item === undefined) return false;
  const anchor = bagOf(item.anchor);
  return (
    typeof item.id === 'string' &&
    (item.kind === 'text' || item.kind === 'image') &&
    typeof item.duration === 'number' &&
    anchor !== undefined &&
    typeof anchor.clipId === 'string' &&
    typeof anchor.offset === 'number' &&
    // `props` is VideoFlow's own untyped bag by design — what is IN it is checked where it is
    // read (Stage clamps a position, Inspector refuses a size it cannot parse). That it is a bag
    // and not an array or a string is the part this guard can answer.
    bagOf(item.props) !== undefined
  );
}

function isOverlayTrack(value: unknown): value is OverlayTrack {
  const track = bagOf(value);
  return (
    track !== undefined &&
    typeof track.id === 'string' &&
    Array.isArray(track.items) &&
    track.items.every(isOverlayItem)
  );
}

/**
 * Whether every id the project points with has something to point at. A shape check cannot see
 * this, and both ways of failing it are already known: a clip naming a source the file does not
 * hold reaches `videoflow.ts`, which throws an untranslated internal sentence into the alert, and
 * an overlay anchored to a clip that is not there becomes a zero-length ghost on a lane. Neither
 * belongs on the far side of the load.
 */
function referencesHold(project: VideoProject): boolean {
  const sources = new Set(project.sources.map((source) => source.id));
  const clips = new Set(project.video.map((clip) => clip.id));
  return (
    project.video.every((clip) => sources.has(clip.sourceId)) &&
    project.overlays.every((lane) => lane.items.every((item) => clips.has(item.anchor.clipId)))
  );
}

function hasProjectShape(value: unknown): value is VideoProject {
  const project = bagOf(value);
  return (
    project !== undefined &&
    typeof project.id === 'string' &&
    typeof project.name === 'string' &&
    typeof project.fps === 'number' &&
    isSize(project.size) &&
    Array.isArray(project.sources) &&
    project.sources.every(isSource) &&
    Array.isArray(project.video) &&
    project.video.every(isClip) &&
    Array.isArray(project.overlays) &&
    project.overlays.every(isOverlayTrack)
  );
}

/**
 * Whether the bytes are a project. A saved project file is EXTERNAL INPUT — it round-trips
 * through a store anyone with the asset id can overwrite — so the whole persisted shape is
 * checked, not only the containers: a lane holding a string, or a clip with no `sourceStart`,
 * reaches `clipStarts` as `NaN` and takes the timeline with it. And the shape is only half of
 * it: the ids have to point somewhere too.
 */
function isProject(value: unknown): value is VideoProject {
  return hasProjectShape(value) && referencesHold(value);
}

/**
 * Whether the bytes are gone, asked of the route the renderer will really fetch. `HEAD` because
 * the answer wanted is the status and not the file; Go's ServeMux matches a `GET` pattern for
 * `HEAD` too, so this is the download route answering about itself. What the status MEANS is
 * `assetIsGone`'s, which is the same reading the editor's own fetch uses.
 */
async function bytesAreGone(assetId: string, source: ProjectAssetSource): Promise<boolean> {
  return assetIsGone(
    assetId,
    await fetch(source.assetUrl(assetId), { method: 'HEAD', credentials: source.credentials }),
  );
}

/**
 * Whether the library still holds this source. One metadata read on the happy path: a row the
 * retention sweeper marked terminal is gone even while its bytes linger.
 *
 * `getAsset` throws an Error that does not carry the status it came from, so a rejection is
 * ambiguous — 404, 401 and 500 arrive identically. The bytes route is asked to break the tie
 * rather than assuming the worst, which is what made every failure read as "deleted".
 */
async function sourceIsGone(assetId: string, source: ProjectAssetSource): Promise<boolean> {
  try {
    return isTerminalAsset(await getAsset(assetId));
  } catch {
    return await bytesAreGone(assetId, source);
  }
}

export async function loadProject(
  assetId: string,
  source: ProjectAssetSource = IDENTITY_SCOPED,
): Promise<LoadedProject> {
  const response = await fetch(source.assetUrl(assetId), { credentials: source.credentials });
  if (!response.ok)
    throw new Error(`videoStudio: the project file answered ${String(response.status)}`);
  const value: unknown = await response.json();
  if (!isProject(value)) throw new Error('videoStudio: those bytes are not a project');
  const checked = await Promise.all(
    value.sources.map(async (item) =>
      (await sourceIsGone(item.assetId, source)) ? item.id : undefined,
    ),
  );
  return { project: value, missing: checked.filter((id) => id !== undefined) };
}
