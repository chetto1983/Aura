import type { AttachmentAdapter, CompleteAttachment, PendingAttachment } from '@assistant-ui/core';
import { deleteAsset, finalizeAsset, presignAsset } from './api';
import type { Asset } from './types';
import { inferModality, isReadyAsset, pollUntilReady, putWithProgress } from './upload';

// auraAttachmentAdapter — Aura's four-step upload (presign → PUT → finalize → poll) expressed
// as the assistant-ui AttachmentAdapter the runtime already asks for, replacing the
// useAttachmentUploads hook that held the same lifecycle in component state beside it.
//
// `add` is an ASYNC GENERATOR on purpose: the contract lets it yield successive
// PendingAttachments, which is exactly a progress bar — the runtime re-renders on each yield,
// so no upload state has to live in React at all. The composer's own send gate reads
// `status.type === 'running'`, which is what used to be `hasBlockingUploads`.
//
// The attachment id is a CLIENT id held stable for the whole upload, not the Aura asset id.
// The runtime indexes attachments by id, so a generator that changed it mid-flight (file name
// first, asset id after presign) produced TWO chips for one file — one frozen at 0%, one at
// 100% — measured live 2026-08-17. assetFor() maps the stable id onto the asset the run
// envelope needs, which keeps the identity the UI keys on separate from the server's.

/** What the presigner accepts today; the browser file dialog filters on it. */
const ACCEPTED_TYPES =
  'image/*,audio/*,application/pdf,.docx,.pptx,.xlsx,.xlsm,.html,.htm,.csv,.md,.markdown,.txt,.json,.xml,.epub';

/** The adapter plus the one thing the AttachmentAdapter shape cannot carry: the Aura Asset
 * behind an attachment id. CompleteAttachment has no free-form metadata field, and the
 * optimistic user turn renders the asset (name, modality, status), so the adapter — which
 * uploaded it and therefore already holds it — hands it back by id rather than having the
 * caller re-fetch what it just uploaded. */
export interface AuraAttachmentAdapter extends AttachmentAdapter {
  readonly assetFor: (id: string) => Asset | undefined;
}

export interface AuraAttachmentAdapterOptions {
  readonly threadId: string;
  /** Fixed poll interval; omitted in production so the backoff in upload.ts applies. */
  readonly pollIntervalMs?: number | undefined;
}

export function createAuraAttachmentAdapter(
  options: AuraAttachmentAdapterOptions,
): AuraAttachmentAdapter {
  const uploaded = new Map<string, Asset>();
  return {
    accept: ACCEPTED_TYPES,

    assetFor: (id) => uploaded.get(id),

    async *add({ file }) {
      const attachmentId = crypto.randomUUID();
      // Queued: named and visible before a single byte moves, so a large file does not look
      // like a dropped one.
      yield pending(file, attachmentId, { type: 'running', reason: 'uploading', progress: 0 });

      let asset: Asset;
      try {
        const presigned = await presignAsset({
          thread_id: options.threadId,
          file_name: file.name,
          mime_type: file.type || 'application/octet-stream',
          size_bytes: file.size,
          modality_hint: inferModality(file),
        });
        asset = presigned.asset;
        // The PUT reports progress through a callback, but a generator cannot yield from
        // inside one; the fractions are collected and drained after it resolves. Progress
        // therefore lands in steps rather than continuously — the same information, without
        // an extra state container to keep in sync.
        let latest = 0;
        await putWithProgress(
          presigned.upload.upload_url,
          file,
          presigned.upload.required_headers,
          (progress) => {
            latest = progress;
          },
        );
        // Progress 1 marks the bytes as delivered; the chip reads that as "processing",
        // because what follows is the server indexing the document, not more upload.
        yield pending(file, attachmentId, {
          type: 'running',
          reason: 'uploading',
          progress: latest > 0 && latest < 1 ? latest : 1,
        });

        const finalized = await finalizeAsset(asset.id);
        asset = await pollUntilReady(finalized, { pollIntervalMs: options.pollIntervalMs });
      } catch (err) {
        yield pending(file, attachmentId, {
          type: 'incomplete',
          reason: 'error',
          message: errorMessage(err),
        });
        return;
      }

      if (!isReadyAsset(asset)) {
        // Refused and failed are different words for the operator but the same shape here:
        // the file is not attachable, and the server's reason is the only useful message.
        yield pending(file, attachmentId, {
          type: 'incomplete',
          reason: 'error',
          message: asset.error_message ?? `attachment ${asset.status}`,
        });
        return;
      }

      uploaded.set(attachmentId, asset);
      // 'requires-action / composer-send' is the library's own word for "uploaded, waiting
      // for the turn to be sent" — SimpleImageAttachmentAdapter returns exactly this — and
      // it is what stops a ready attachment from reading as a still-running upload.
      yield pending(file, attachmentId, { type: 'requires-action', reason: 'composer-send' });
    },

    // The bytes are already on the server by the time the turn is sent, so this only marks
    // the attachment complete. `content` stays empty deliberately: Aura references the asset
    // by id on the run envelope and never inlines it into the message.
    send(attachment): Promise<CompleteAttachment> {
      return Promise.resolve({
        ...attachment,
        status: { type: 'complete' },
        content: [],
      });
    },

    async remove(attachment) {
      // The DELETE needs the ASSET id, not the attachment's client id — deleting by the
      // latter silently orphaned the upload on the server (caught by the adapter test).
      const assetID = uploaded.get(attachment.id)?.id;
      uploaded.delete(attachment.id);
      if (assetID === undefined) return;
      // A failed upload has no asset behind it at all, so a 404 here is expected rather
      // than exceptional; the chip goes either way.
      await deleteAsset(assetID).catch(() => undefined);
    },
  };
}

function pending(file: File, id: string, status: PendingAttachment['status']): PendingAttachment {
  return {
    id,
    type: attachmentTypeFor(file),
    name: file.name,
    contentType: file.type || 'application/octet-stream',
    file,
    status,
  };
}

function attachmentTypeFor(file: File): PendingAttachment['type'] {
  const modality = inferModality(file);
  if (modality === 'image') return 'image';
  if (modality === 'document') return 'document';
  return 'file';
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : 'upload failed';
}
