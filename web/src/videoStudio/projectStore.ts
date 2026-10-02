import { IDENTITY_SCOPED } from '../chat/artifacts/renderers/assetSourceContext';
import { finalizeAsset, getAsset, presignAsset } from '../chat/attachments/api';
import { isTerminalAsset, putWithProgress } from '../chat/attachments/upload';
import { assetIsGone } from './assetStatus';
import { audioTracks } from './project';
import type {
  AudioItem,
  AudioTrack,
  OverlayItem,
  OverlayTrack,
  ProjectSource,
  VideoItem,
  VideoProject,
} from './project';

// projectStore.ts — the project as a file. It is a `.aura-video.json` DOCUMENT uploaded through
// the same presign the chat attachments use, which is what puts it under `chat/`: the server files
// by modality (internal/assets/service.go `folderFor`), and `media/` is where the sources live.
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

/**
 * What a project file's name ends in: the Studio's marker, never a separate format. It still ends
 * in `.json`, the extension the upload allowlist accepts (internal/assets/limits.go), and the
 * object key keeps it whole (internal/objectstore `StudioProjectSuffix`), which is what the
 * ingest skips (services/ingest/source.py `STUDIO_PROJECT_PATTERN`): a project is the editor's
 * state, not a document anyone searches. A project saved before the marker was plain `.json`: the
 * server moves it to the marker once, at boot (internal/assets/studio_project_rekey.go), and it
 * loads either way, because `loadProject` reads by asset id and never looks at a name.
 */
export const PROJECT_FILE_EXTENSION = 'aura-video.json';

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
  const file = new File(
    [JSON.stringify(project)],
    projectFileName(project, PROJECT_FILE_EXTENSION, name),
    { type: PROJECT_MIME },
  );
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
const JUNCTION_TRANSITIONS = new Set([
  'none',
  'crossfade',
  'fadeBlack',
  'fadeWhite',
  'zoom',
  'blur',
]);

function bagOf(value: unknown): Bag | undefined {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Bag)
    : undefined;
}

/**
 * A number inside the range the editor itself can produce. `typeof Infinity === 'number'`, JSON
 * reads `1e999` as exactly that, and a speed of 0 is finite but divided by: either reaches
 * `clipStarts` as NaN. The bounds are the commands' own (`setClipPresentation`,
 * `setAudioProperties`), so a file the editor wrote always loads.
 */
function between(value: unknown, min: number, max: number): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value >= min && value <= max;
}

function optionalBetween(value: unknown, min: number, max: number): boolean {
  return value === undefined || between(value, min, max);
}

/** Past zero: a length, a rate or a ramp that is 0 is a division waiting to happen. */
function positive(value: unknown): value is number {
  return between(value, Number.MIN_VALUE, Number.MAX_VALUE);
}

function nonNegative(value: unknown): value is number {
  return between(value, 0, Number.MAX_VALUE);
}

function optionalPositive(value: unknown): boolean {
  return value === undefined || positive(value);
}

const ANY = Number.MAX_VALUE;
const SPEED = [0.25, 4] as const;

function isSpeech(value: unknown): boolean {
  return (
    Array.isArray(value) &&
    value.every(
      (window) =>
        Array.isArray(window) &&
        window.length === 2 &&
        nonNegative(window[0]) &&
        nonNegative(window[1]) &&
        window[0] <= window[1],
    )
  );
}

/** A source's pixel size: 0 × 0 is a sound's. */
function isSize(value: unknown): boolean {
  const size = bagOf(value);
  return size !== undefined && nonNegative(size.width) && nonNegative(size.height);
}

/** The project's own frame, which a render divides by. */
function isFrame(value: unknown): boolean {
  const size = bagOf(value);
  return size !== undefined && positive(size.width) && positive(size.height);
}

function isSource(value: unknown): value is ProjectSource {
  const source = bagOf(value);
  return (
    source !== undefined &&
    typeof source.id === 'string' &&
    typeof source.assetId === 'string' &&
    (source.kind === 'video' || source.kind === 'image' || source.kind === 'audio') &&
    nonNegative(source.duration) &&
    (source.hasAudio === undefined || typeof source.hasAudio === 'boolean') &&
    (source.speech === undefined || isSpeech(source.speech)) &&
    (source.denoisedAssetId === undefined || typeof source.denoisedAssetId === 'string') &&
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
    positive(clip.duration) &&
    nonNegative(clip.sourceStart) &&
    typeof clip.muted === 'boolean' &&
    optionalBetween(clip.volume, 0, 2) &&
    (clip.denoise === undefined || typeof clip.denoise === 'boolean') &&
    (clip.rotation === undefined || [0, 90, 180, 270].includes(clip.rotation as number)) &&
    (clip.fit === undefined || clip.fit === 'contain' || clip.fit === 'cover') &&
    (clip.flipX === undefined || typeof clip.flipX === 'boolean') &&
    (clip.flipY === undefined || typeof clip.flipY === 'boolean') &&
    optionalBetween(clip.brightness, 0, ANY) &&
    optionalBetween(clip.contrast, 0, ANY) &&
    optionalBetween(clip.saturation, 0, ANY) &&
    optionalBetween(clip.hue, -ANY, ANY) &&
    optionalBetween(clip.blur, 0, ANY) &&
    optionalBetween(clip.opacity, 0, 1) &&
    (clip.animation === undefined ||
      clip.animation === 'none' ||
      clip.animation === 'fadeIn' ||
      clip.animation === 'fadeOut') &&
    (clip.fadeIn === undefined || typeof clip.fadeIn === 'boolean') &&
    (clip.fadeOut === undefined || typeof clip.fadeOut === 'boolean') &&
    optionalBetween(clip.speed, ...SPEED) &&
    (clip.transitionIn === undefined ||
      (typeof clip.transitionIn === 'string' && CLIP_TRANSITIONS.has(clip.transitionIn))) &&
    (clip.transitionOut === undefined ||
      (typeof clip.transitionOut === 'string' && CLIP_TRANSITIONS.has(clip.transitionOut))) &&
    optionalPositive(clip.transitionInDuration) &&
    optionalPositive(clip.transitionOutDuration) &&
    (clip.junctionFromClipId === undefined || typeof clip.junctionFromClipId === 'string') &&
    (clip.junctionTransition === undefined ||
      (typeof clip.junctionTransition === 'string' &&
        JUNCTION_TRANSITIONS.has(clip.junctionTransition))) &&
    optionalPositive(clip.junctionDuration)
  );
}

function isOverlayItem(value: unknown): value is OverlayItem {
  const item = bagOf(value);
  if (item === undefined) return false;
  const anchor = bagOf(item.anchor);
  return (
    typeof item.id === 'string' &&
    (item.kind === 'text' || item.kind === 'image') &&
    nonNegative(item.duration) &&
    anchor !== undefined &&
    typeof anchor.clipId === 'string' &&
    nonNegative(anchor.offset) &&
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

function isEnvelopePoint(value: unknown): boolean {
  const point = bagOf(value);
  return point !== undefined && nonNegative(point.time) && between(point.gain, 0, 1);
}

function isDucking(value: unknown): boolean {
  const ducking = bagOf(value);
  return (
    ducking !== undefined && between(ducking.amountDb, -24, -3) && between(ducking.ramp, 0.1, 2)
  );
}

function isAudioItem(value: unknown): value is AudioItem {
  const item = bagOf(value);
  if (item === undefined) return false;
  const anchor = bagOf(item.anchor);
  return (
    typeof item.id === 'string' &&
    typeof item.sourceId === 'string' &&
    anchor !== undefined &&
    typeof anchor.clipId === 'string' &&
    nonNegative(anchor.offset) &&
    nonNegative(item.sourceStart) &&
    positive(item.duration) &&
    between(item.volume, 0, 2) &&
    typeof item.muted === 'boolean' &&
    optionalBetween(item.fadeIn, 0, 5) &&
    optionalBetween(item.fadeOut, 0, 5) &&
    optionalBetween(item.speed, ...SPEED) &&
    (item.envelope === undefined ||
      (Array.isArray(item.envelope) && item.envelope.every(isEnvelopePoint))) &&
    (item.ducking === undefined || isDucking(item.ducking)) &&
    (item.denoise === undefined || typeof item.denoise === 'boolean') &&
    (item.extractedFrom === undefined || typeof item.extractedFrom === 'string') &&
    (item.label === undefined || typeof item.label === 'string')
  );
}

function isAudioTrack(value: unknown): value is AudioTrack {
  const track = bagOf(value);
  return (
    track !== undefined &&
    typeof track.id === 'string' &&
    Array.isArray(track.items) &&
    track.items.every(isAudioItem)
  );
}

/**
 * Whether every id the project points with has something to point at, and at the right kind of
 * thing. A shape check cannot see this, and every way of failing it is known: a clip naming a
 * source the file does not hold reaches `videoflow.ts`, which throws an untranslated internal
 * sentence into the alert; an overlay anchored to a clip that is not there becomes a zero-length
 * ghost on a lane; a lane playing a source of the wrong kind (a clip over a sound, a sound over a
 * still) hands the renderer media it cannot play; and a sound hanging off a missing clip has no
 * project time at all. None of it belongs on the far side of the load.
 */
function referencesHold(project: VideoProject): boolean {
  const kinds = new Map(project.sources.map((source) => [source.id, source.kind]));
  const clips = new Set(project.video.map((clip) => clip.id));
  const plays = (sourceId: string, allowed: readonly ProjectSource['kind'][]) => {
    const kind = kinds.get(sourceId);
    return kind !== undefined && allowed.includes(kind);
  };
  return (
    project.video.every((clip) => plays(clip.sourceId, ['video', 'image'])) &&
    project.video.every(
      (clip, index) =>
        clip.junctionFromClipId === undefined ||
        project.video[index - 1]?.id === clip.junctionFromClipId,
    ) &&
    project.overlays.every((lane) => lane.items.every((item) => clips.has(item.anchor.clipId))) &&
    audioTracks(project).every((lane) =>
      lane.items.every(
        (item) =>
          plays(item.sourceId, ['audio', 'video']) &&
          clips.has(item.anchor.clipId) &&
          (item.extractedFrom === undefined || clips.has(item.extractedFrom)),
      ),
    )
  );
}

function hasProjectShape(value: unknown): value is VideoProject {
  const project = bagOf(value);
  return (
    project !== undefined &&
    typeof project.id === 'string' &&
    typeof project.name === 'string' &&
    positive(project.fps) &&
    isFrame(project.size) &&
    Array.isArray(project.sources) &&
    project.sources.every(isSource) &&
    Array.isArray(project.video) &&
    project.video.every(isClip) &&
    Array.isArray(project.overlays) &&
    project.overlays.every(isOverlayTrack) &&
    (project.audio === undefined ||
      (Array.isArray(project.audio) && project.audio.every(isAudioTrack)))
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
