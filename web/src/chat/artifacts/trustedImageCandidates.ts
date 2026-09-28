import { isDisplayPayload, type DisplayPayload } from '../displays/types';
import { previewKind } from './artifactMeta';

/** Collect image artifacts only from the backend-attached display field of this message. */
export function trustedImageDisplays(content: readonly unknown[]): readonly DisplayPayload[] {
  return content.flatMap((raw): DisplayPayload[] => {
    if (typeof raw !== 'object' || raw === null || !('display' in raw)) return [];
    const display = raw.display;
    if (!isDisplayPayload(display) || display.type !== 'local_artifact') return [];
    const artifact = display.artifact;
    if (
      artifact === undefined ||
      typeof artifact.filename !== 'string' ||
      typeof artifact.mime_type !== 'string' ||
      previewKind(artifact.mime_type, artifact.filename) !== 'image'
    )
      return [];
    return [display];
  });
}
