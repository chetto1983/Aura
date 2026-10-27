import { attachToComposer } from '../composer/composerAttach';
import { extOf } from './artifactMeta';
import { IDENTITY_SCOPED, type AssetSource } from './renderers/assetSourceContext';
import type { PreviewTarget } from './PreviewModal';

/**
 * The chat's tier for a preview that may be edited: it reads like the identity tier, and every
 * save a Craft app makes goes into the composer's next message as a file, beside the artifact,
 * which stays as it was. The edit keeps the artifact's type while it keeps its extension; a
 * file of another kind (FilmCraft's project) goes untyped, as an upload of it would.
 */
export function composerSource(
  target: Pick<PreviewTarget, 'file_name' | 'mime_type'>,
): AssetSource {
  return {
    ...IDENTITY_SCOPED,
    savesInto: 'message',
    save: (_assetId, bytes, saveAs) => {
      const name = saveAs ?? target.file_name;
      const type = extOf(name) === extOf(target.file_name) ? target.mime_type : '';
      return attachToComposer(new File([bytes], name, { type }));
    },
  };
}
