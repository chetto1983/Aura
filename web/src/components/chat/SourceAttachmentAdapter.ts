import type {
  AttachmentAdapter,
  CompleteAttachment,
  PendingAttachment,
} from '@assistant-ui/react';
import { api } from '@/api';
import type { UploadResponse } from '@/types/api';

export interface SourceAttachmentReference {
  source_id: string;
  filename: string;
  mime_type?: string;
  size?: number;
}

export interface SourceAttachmentRunState {
  track(ref: SourceAttachmentReference): void;
  extraBody(): Record<string, unknown> | undefined;
  clear(): void;
}

type UploadSource = (file: File) => Promise<UploadResponse>;

const SOURCE_ATTACHMENT_ACCEPT = [
  'application/pdf',
  'text/plain',
  'text/markdown',
  'application/json',
  'text/csv',
  'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  '.pdf',
  '.txt',
  '.md',
  '.markdown',
  '.json',
  '.csv',
  '.xlsx',
  '.docx',
].join(',');

function sourceReference(file: File, upload: UploadResponse): SourceAttachmentReference {
  return {
    source_id: upload.id,
    filename: upload.filename || file.name,
    mime_type: file.type || undefined,
    size: file.size || undefined,
  };
}

function sourceContent(ref: SourceAttachmentReference): CompleteAttachment['content'] {
  return [{
    type: 'file',
    filename: ref.filename,
    data: `aura-source://${ref.source_id}`,
    mimeType: 'application/x-aura-source-ref',
  }];
}

export function createSourceAttachmentAdapter({
  upload = api.uploadSource,
  onSend,
}: {
  upload?: UploadSource;
  onSend?: (ref: SourceAttachmentReference) => void;
} = {}): AttachmentAdapter {
  return {
    accept: SOURCE_ATTACHMENT_ACCEPT,
    async add({ file }: { file: File }): Promise<PendingAttachment> {
      const uploaded = await upload(file);
      const ref = sourceReference(file, uploaded);
      return {
        id: ref.source_id,
        type: 'document',
        name: ref.filename,
        contentType: file.type || undefined,
        file,
        content: sourceContent(ref),
        status: { type: 'requires-action', reason: 'composer-send' },
      };
    },
    async send(attachment: PendingAttachment): Promise<CompleteAttachment> {
      const ref: SourceAttachmentReference = {
        source_id: attachment.id,
        filename: attachment.name,
        mime_type: attachment.contentType,
        size: attachment.file?.size,
      };
      onSend?.(ref);
      return {
        id: ref.source_id,
        type: 'document',
        name: ref.filename,
        contentType: ref.mime_type,
        content: sourceContent(ref),
        status: { type: 'complete' },
      };
    },
    async remove(): Promise<void> {
      // Uploaded sources are canonical artifacts. Removing the composer chip
      // only detaches the reference from this message.
    },
  };
}

export function createSourceAttachmentRunState(): SourceAttachmentRunState {
  let pending: SourceAttachmentReference[] = [];
  return {
    track(ref) {
      pending = [
        ...pending.filter((item) => item.source_id !== ref.source_id),
        ref,
      ];
    },
    extraBody() {
      if (pending.length === 0) return undefined;
      return {
        source_ids: pending.map((attachment) => attachment.source_id),
        attachments: pending,
      };
    },
    clear() {
      pending = [];
    },
  };
}
