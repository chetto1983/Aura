import { finalizeAsset, getAsset, presignAsset } from '../chat/attachments/api';
import { isTerminalAsset, putWithProgress } from '../chat/attachments/upload';
import type { VideoProject } from './project';

// projectStore.ts — the project as a file. It is a `.json` DOCUMENT uploaded through the same
// presign the chat attachments use, which is what puts it under `chat/`: the server files by
// modality (internal/assets/service.go `folderFor`), and `media/` is where the sources live.
// No table, no route and no migration of its own — a project is bytes with an asset id.
//
// A load answers with the project AND with the sources whose bytes are gone, because a saved
// project outlives the clips it names: the library sweeps, and an asset id in a file is a claim
// about the past. The editor shows those sources as missing rather than handing VideoFlow a URL
// that answers 404 and letting the layer render black.

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
 */
export function projectFileName(project: VideoProject, extension: string): string {
  const slug = project.name
    .normalize('NFKD')
    .replace(/[^a-zA-Z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, NAME_MAX);
  return `${slug === '' ? 'project' : slug}.${extension}`;
}

/** Save the project and answer with the asset id it now lives at. */
export async function saveProject(project: VideoProject): Promise<string> {
  const file = new File([JSON.stringify(project)], projectFileName(project, 'json'), {
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

function isSized(value: unknown): boolean {
  if (typeof value !== 'object' || value === null) return false;
  const size = value as Record<string, unknown>;
  return typeof size.width === 'number' && typeof size.height === 'number';
}

/**
 * Whether the bytes are a project at all. A saved project is a file, and a file is not a
 * promise: this is the shallow shape the editor needs to hold something without throwing
 * through React. What is INSIDE a clip or an overlay's props is checked where it is read —
 * `Stage.tsx` clamps a position, `Inspector.tsx` refuses a size it cannot parse.
 */
function isProject(value: unknown): value is VideoProject {
  if (typeof value !== 'object' || value === null) return false;
  const project = value as Record<string, unknown>;
  return (
    typeof project.id === 'string' &&
    typeof project.name === 'string' &&
    typeof project.fps === 'number' &&
    isSized(project.size) &&
    Array.isArray(project.sources) &&
    Array.isArray(project.video) &&
    Array.isArray(project.overlays)
  );
}

/** Whether the library still holds this source's bytes. A read that fails and a row the
 *  retention sweeper marked terminal are the same answer to the editor: it cannot be played. */
async function stillThere(assetId: string): Promise<boolean> {
  try {
    return !isTerminalAsset(await getAsset(assetId));
  } catch {
    return false;
  }
}

export async function loadProject(
  assetId: string,
  source: ProjectAssetSource,
): Promise<LoadedProject> {
  const response = await fetch(source.assetUrl(assetId), { credentials: source.credentials });
  if (!response.ok)
    throw new Error(`videoStudio: the project file answered ${String(response.status)}`);
  const value: unknown = await response.json();
  if (!isProject(value)) throw new Error('videoStudio: those bytes are not a project');
  const checked = await Promise.all(
    value.sources.map(async (item) => ((await stillThere(item.assetId)) ? undefined : item.id)),
  );
  return { project: value, missing: checked.filter((id) => id !== undefined) };
}
