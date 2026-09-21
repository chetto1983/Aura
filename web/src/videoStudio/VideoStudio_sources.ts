import { finalizeAsset, presignAsset } from '../chat/attachments/api';
import { putWithProgress } from '../chat/attachments/upload';
import { probeVideo, type VideoInfo } from '../mediaEdit/videoMedia';
import { assetIsGone } from './assetStatus';
import { addClip, CommandRefusal } from './commands';
import { emptyProject, type ProjectSource, type VideoProject } from './project';
import { loadProject, type LoadedProject, type ProjectAssetSource } from './projectStore';

// VideoStudio_sources.ts — the door a source comes in through, and the three ways a project is
// opened. Nothing here draws anything: it is the async half of the workspace, kept out of the
// component so the probe, the upload and the load stay testable without a renderer.
//
// The door is `probeVideo`, the single-clip editor's own, and it runs BEFORE the bytes become a
// layer — and, for a picked file, before they are uploaded. VideoFlow answers a source it cannot
// decode by disabling the layer: the export then succeeds and the frames are black. A refusal at
// the door is the only place that failure is still legible.

/** The two refusals the SHELL raises. `commands.ts` owns the other five, one per decision a
 *  command declines; these two are about the bytes, which no pure command ever sees. */
export const REFUSAL_UNDECODABLE = 'videoStudio.refusal.sourceUndecodable';
export const REFUSAL_MISSING_ASSET = 'videoStudio.refusal.sourceMissingAsset';

/**
 * What the file picker takes. The clips are exactly what the asset route accepts as a video
 * (internal/assets/limits.go `videoExts`), so the server has nothing left to refuse. The stills
 * are OURS to choose: `ModalityImage` has no extension allowlist at all, so the gate is the list
 * below plus the decode probe — and it holds the three raster formats every browser this cockpit
 * targets decodes. GIF is left out on purpose: the video lane would show one frame of it and
 * say nothing about the rest, and a silent loss is the defect class this cycle keeps refusing.
 */
export const SOURCE_ACCEPT = 'video/mp4,video/webm,image/png,image/jpeg,image/webp';

/**
 * How long a still is on screen when it is added. A number this module CHOOSES rather than
 * measures — an image has no length of its own — and the item is what carries it, so a trim
 * handle or the inspector changes it like any other clip's.
 */
const IMAGE_SECONDS = 5;

/** What a project is opened on. Three shapes because there are three doors: the quick editor
 *  hands over a project it built from the clip it was trimming, the Studio hands over one
 *  generated asset, and a saved project is a file to read back. */
export type StudioOpen =
  | { readonly kind: 'project'; readonly project: VideoProject }
  | { readonly kind: 'source'; readonly assetId: string; readonly name: string }
  | { readonly kind: 'saved'; readonly assetId: string };

/** A frame before there is a clip to measure. A project still wearing EXACTLY this, with no
 *  source in it, is one nobody has chosen a frame for — which is the only project `sourceEdit`
 *  is allowed to re-frame. */
const STARTING_SIZE = { width: 1920, height: 1080 };
const STARTING_FPS = 30;

/** Any pure project-to-project function — `history.ts`'s `Edit`, restated so this module does
 *  not depend on the history to describe what it returns. */
type Edit = (project: VideoProject) => VideoProject;

/** What the probe found: what kind of source it is, the numbers it needs, and nothing about the
 *  file. A still's `duration` is 0 — the model's own convention for a source with no length. */
export interface ProbedSource {
  readonly kind: ProjectSource['kind'];
  readonly duration: number;
  readonly width: number;
  readonly height: number;
  readonly hasAudio?: boolean;
}

/**
 * Read a still, or refuse it. `createImageBitmap` IS the decode — it is to an image what
 * `canDecode` is to a video track — so a file the browser cannot turn into pixels is refused at
 * the same door and with the same sentence, rather than becoming a layer VideoFlow disables.
 */
async function probeImage(bytes: Blob): Promise<ProbedSource> {
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(bytes);
  } catch {
    throw new CommandRefusal(REFUSAL_UNDECODABLE);
  }
  try {
    return { kind: 'image', duration: 0, width: bitmap.width, height: bitmap.height };
  } finally {
    // The pixels were wanted for their size only, and a bitmap left open holds them all.
    bitmap.close();
  }
}

/**
 * Read the bytes, or refuse them. Separate from `sourceEdit` so a PICKED file is probed before
 * it is uploaded: a clip this browser cannot decode is refused without paying for the transfer,
 * and the refusal names the browser rather than the server.
 *
 * Two different failures, one refusal. A file that will not parse throws on the way in. A file
 * that parses and has no decoder here — MPEG-4 Part 2, HEVC in a browser without it — is the
 * commoner one and the only one the refusal's own sentence describes: it arrives with a size
 * and a duration, and nothing but `decodable` distinguishes it from a clip that would play.
 * The single-clip editor still opens such a file, because a copy-trim never decodes a frame;
 * a COMPOSITION always does, and VideoFlow answers a layer it cannot decode with black.
 *
 * Which probe runs is the bytes' own declared type — a picked file's from the browser, a fetched
 * asset's from the route's Content-Type. It is a routing question, not a verdict: whatever it
 * says, the probe it picks is the one that really decodes, and refuses when it cannot.
 */
export async function probeSource(bytes: Blob): Promise<ProbedSource> {
  if (bytes.type.startsWith('image/')) return probeImage(bytes);
  let probed;
  try {
    probed = await probeVideo(bytes);
  } catch {
    throw new CommandRefusal(REFUSAL_UNDECODABLE);
  }
  if (!probed.decodable) throw new CommandRefusal(REFUSAL_UNDECODABLE);
  return {
    kind: 'video',
    duration: probed.duration,
    width: probed.width,
    height: probed.height,
    hasAudio: probed.hasAudio,
  };
}

/**
 * Whether this project's frame is still the one nobody picked: no source in it, and the default
 * size untouched. A project built from a clip already carries that clip's frame, and a saved one
 * carries whatever it was saved with — neither is re-framed by what is added to it next.
 */
function framedByDefault(project: VideoProject): boolean {
  return (
    project.sources.length === 0 &&
    project.size.width === STARTING_SIZE.width &&
    project.size.height === STARTING_SIZE.height
  );
}

/**
 * The edit that puts a probed source in the project, with a clip of its whole length.
 *
 * The first source of an UNFRAMED project also sets its frame. VideoFlow does not letterbox a
 * clip that does not fit, it crops it (`fit: 'cover'`), so a portrait clip in a project still
 * wearing the 1920×1080 default would lose its sides with nothing said — and silent cropping is
 * the defect class this cycle keeps refusing. The narrowing is what keeps the cure from becoming
 * the same disease: a SECOND source never re-frames the project, and neither does the first
 * source of a project whose frame came from somewhere (a clip, a saved file). The workspace
 * watches the size across the commit and says so when it changes.
 */
export function sourceEdit(probed: ProbedSource, assetId: string): Edit {
  const size = { width: probed.width, height: probed.height };
  return (project) => {
    const source: ProjectSource = {
      id: crypto.randomUUID(),
      assetId,
      kind: probed.kind,
      duration: probed.duration,
      size,
      ...(probed.hasAudio === undefined ? {} : { hasAudio: probed.hasAudio }),
    };
    return addClip(
      {
        ...project,
        size: framedByDefault(project) ? size : project.size,
        sources: [...project.sources, source],
      },
      // A still lasts as long as its ITEM says: the source's own duration is zero, and `addClip`
      // refuses a clip of no length.
      { sourceId: source.id, duration: probed.kind === 'image' ? IMAGE_SECONDS : probed.duration },
    );
  };
}

/** Presign, PUT, finalize — the attachments' own path — and answer with the asset id. */
export async function uploadSource(file: File): Promise<string> {
  const presign = await presignAsset({
    // No thread: a source belongs to the identity, the way a Studio frame does.
    thread_id: '',
    file_name: file.name,
    mime_type: file.type,
    // Named rather than guessed: the server files an .mp4 under media/, and a hint reading
    // 'unknown' is how a clip ends up beside the documents. A still is filed as one.
    modality_hint: file.type.startsWith('image/') ? 'image' : 'video',
    size_bytes: file.size,
  });
  await putWithProgress(presign.upload.upload_url, file, presign.upload.required_headers, () => {
    // The sentence beside the picker names the file, not a percentage: a bar here would
    // re-render the editor — and recompile the preview — on every chunk.
  });
  const finalized = await finalizeAsset(presign.asset.id);
  return finalized.id;
}

/**
 * The bytes of a source the editor was pointed at. `assetIsGone` is what reads the status — the
 * same reading a load uses — and this is what the editor DOES with it: a gone asset is a refusal
 * the operator can act on, anything else is the failure it already is.
 */
async function fetchSource(assetId: string, source: ProjectAssetSource): Promise<Blob> {
  const response = await fetch(source.assetUrl(assetId), { credentials: source.credentials });
  if (assetIsGone(assetId, response)) throw new CommandRefusal(REFUSAL_MISSING_ASSET);
  return response.blob();
}

/**
 * The project the editor opens on, and the sources it already knows are gone. A seeded asset
 * goes through the SAME probe as a picked file, so a generated clip this browser cannot decode
 * is refused at the entrance instead of exporting black.
 */
export async function openedProject(
  open: StudioOpen,
  source: ProjectAssetSource,
): Promise<LoadedProject> {
  if (open.kind === 'project') return { project: open.project, missing: [] };
  if (open.kind === 'saved') return loadProject(open.assetId, source);
  const probed = await probeSource(await fetchSource(open.assetId, source));
  const edit = sourceEdit(probed, open.assetId);
  return {
    project: edit(emptyProject(open.name.trim(), STARTING_SIZE, STARTING_FPS)),
    missing: [],
  };
}

/**
 * A new project holding one stretch of one clip: the quick editor's way forward, where the bytes
 * have already been probed and only the trim has to survive the crossing.
 *
 * It takes the quick editor's OWN probe — a `VideoInfo`, not a `ProbedSource` — because that probe
 * is the only place the answer lives, and this is the one door that can arrive with a no. The
 * single-clip editor welcomes a file this browser cannot decode: a copy-trim never decodes a
 * frame. A composition always does, and VideoFlow answers a layer it cannot decode by disabling
 * it and rendering black, so the third door refuses exactly what the other two refuse.
 */
export function projectFromClip(
  name: string,
  assetId: string,
  probed: VideoInfo,
  range: { readonly start: number; readonly end: number },
): VideoProject {
  if (!probed.decodable) throw new CommandRefusal(REFUSAL_UNDECODABLE);
  const size = { width: probed.width, height: probed.height };
  const source: ProjectSource = {
    id: crypto.randomUUID(),
    assetId,
    kind: 'video',
    duration: probed.duration,
    size,
    hasAudio: probed.hasAudio,
  };
  return addClip(
    { ...emptyProject(name, size, STARTING_FPS), sources: [source] },
    { sourceId: source.id, sourceStart: range.start, duration: range.end - range.start },
  );
}
