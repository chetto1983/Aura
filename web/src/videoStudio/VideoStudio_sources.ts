import { finalizeAsset, presignAsset } from '../chat/attachments/api';
import { putWithProgress } from '../chat/attachments/upload';
import { probeVideo } from '../mediaEdit/videoMedia';
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

/** What the file picker takes: exactly what the asset route accepts as a video
 *  (internal/assets/limits.go `videoExts`), so the server has nothing left to refuse. */
export const SOURCE_ACCEPT = 'video/mp4,video/webm';

/** What a project is opened on. Three shapes because there are three doors: the quick editor
 *  hands over a project it built from the clip it was trimming, the Studio hands over one
 *  generated asset, and a saved project is a file to read back. */
export type StudioOpen =
  | { readonly kind: 'project'; readonly project: VideoProject }
  | { readonly kind: 'source'; readonly assetId: string; readonly name: string }
  | { readonly kind: 'saved'; readonly assetId: string };

/** A frame before there is a clip to measure. The first source replaces it (`sourceEdit`). */
const STARTING_SIZE = { width: 1920, height: 1080 };
const STARTING_FPS = 30;

/** Any pure project-to-project function — `history.ts`'s `Edit`, restated so this module does
 *  not depend on the history to describe what it returns. */
type Edit = (project: VideoProject) => VideoProject;

/** What the probe found: the three numbers a source needs, and nothing about the file. */
export interface ProbedSource {
  readonly duration: number;
  readonly width: number;
  readonly height: number;
}

/**
 * Read the bytes, or refuse them. Separate from `sourceEdit` so a PICKED file is probed before
 * it is uploaded: a clip this browser cannot decode is refused without paying for the transfer,
 * and the refusal names the browser rather than the server.
 */
export async function probeSource(bytes: Blob): Promise<ProbedSource> {
  try {
    return await probeVideo(bytes);
  } catch {
    throw new CommandRefusal(REFUSAL_UNDECODABLE);
  }
}

/**
 * The edit that puts a probed source in the project, with a clip of its whole length.
 *
 * The FIRST source also sets the frame. A generated vertical clip in a 1920×1080 project is not
 * letterboxed by VideoFlow, it is cropped (`fit: 'cover'`), so a project that kept its default
 * would quietly throw away the sides of every portrait clip the Studio makes.
 */
export function sourceEdit(probed: ProbedSource, assetId: string): Edit {
  const size = { width: probed.width, height: probed.height };
  return (project) => {
    const source: ProjectSource = {
      id: crypto.randomUUID(),
      assetId,
      kind: 'video',
      duration: probed.duration,
      size,
      fps: project.fps,
    };
    const first = project.sources.length === 0;
    return addClip(
      { ...project, size: first ? size : project.size, sources: [...project.sources, source] },
      { sourceId: source.id, duration: probed.duration },
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
    // 'unknown' is how a clip ends up beside the documents.
    modality_hint: 'video',
    size_bytes: file.size,
  });
  await putWithProgress(presign.upload.upload_url, file, presign.upload.required_headers, () => {
    // The sentence beside the picker names the file, not a percentage: a bar here would
    // re-render the editor — and recompile the preview — on every chunk.
  });
  const finalized = await finalizeAsset(presign.asset.id);
  return finalized.id;
}

async function fetchSource(assetId: string, source: ProjectAssetSource): Promise<Blob> {
  const response = await fetch(source.assetUrl(assetId), { credentials: source.credentials });
  if (!response.ok) throw new CommandRefusal(REFUSAL_MISSING_ASSET);
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

/** A new project holding one stretch of one clip: the quick editor's way forward, where the
 *  bytes have already been probed and only the trim has to survive the crossing. */
export function projectFromClip(
  name: string,
  assetId: string,
  probed: ProbedSource,
  range: { readonly start: number; readonly end: number },
): VideoProject {
  const size = { width: probed.width, height: probed.height };
  const source: ProjectSource = {
    id: crypto.randomUUID(),
    assetId,
    kind: 'video',
    duration: probed.duration,
    size,
    fps: STARTING_FPS,
  };
  return addClip(
    { ...emptyProject(name, size, STARTING_FPS), sources: [source] },
    { sourceId: source.id, sourceStart: range.start, duration: range.end - range.start },
  );
}
