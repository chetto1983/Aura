import { presignAsset } from '../chat/attachments/api';
import { inferModality, putWithProgress } from '../chat/attachments/upload';
import { finalizeStudioUpload, type StudioImageRef } from './studioApi';

// frameUpload.ts — the three steps that turn a file the operator dropped on the composer into
// an asset id a generation can name. The presign and the progress-reporting PUT are the ones
// the chat attachments already use; only the last step differs, because the Studio's finalize
// accepts an image and refuses anything else.

/** Presign, upload, finalize. `onProgress` receives the transfer fraction (0…1).
 *
 *  The presign names no thread: a Studio frame belongs to the identity, not to a conversation,
 *  and the library route lists the identity's images from any thread or none.
 *
 *  The modality hint is read from the file rather than asserted to be an image, so a file that
 *  is not one is refused by the server with a sentence about that file — instead of being
 *  mislabelled on the way in and failing later for a reason nobody can act on. */
export async function uploadStudioFrame(
  file: File,
  onProgress: (progress: number) => void,
): Promise<StudioImageRef> {
  const presigned = await presignAsset({
    thread_id: '',
    file_name: file.name,
    mime_type: file.type,
    size_bytes: file.size,
    modality_hint: inferModality(file),
  });
  await putWithProgress(
    presigned.upload.upload_url,
    file,
    presigned.upload.required_headers,
    onProgress,
  );
  return finalizeStudioUpload(presigned.asset.id);
}
