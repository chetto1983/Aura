export type AssetStatus =
  | 'created'
  | 'presigned'
  | 'uploaded'
  | 'accepted'
  | 'processing'
  | 'searchable'
  | 'embedding'
  | 'complete'
  | 'failed'
  | 'refused'
  | 'deleted'
  | 'canceled'
  // Written by the retention sweeper (internal/retention/store.go), not by the upload
  // pipeline -- which is why they were missing here: the union only listed what the
  // upload path produces, so a swept asset fell through every switch to the default and
  // a vanishing file reported itself as still processing.
  | 'deleting'
  | 'indexed'
  | 'recovered';

export type AssetModality = 'document' | 'image' | 'audio' | 'unknown';

export interface Asset {
  readonly id: string;
  readonly source_kind?: 'web' | 'telegram' | 'cli' | 'agent';
  readonly thread_id?: string;
  readonly scope?: 'thread' | 'library';
  readonly status: AssetStatus;
  readonly modality: AssetModality;
  readonly file_name: string;
  readonly mime_type: string;
  readonly declared_size_bytes: number;
  readonly size_bytes: number;
  readonly document_id?: string;
  readonly summary?: string;
  readonly error_code?: string;
  readonly error_message?: string;
  readonly created_at?: string;
  readonly updated_at?: string;
}

export interface PresignResponse {
  readonly asset: Asset;
  readonly upload: {
    readonly upload_url: string;
    readonly method: string;
    readonly required_headers: Record<string, string>;
    readonly expires_at: string;
  };
}

export interface PresignAssetRequest {
  readonly thread_id: string;
  readonly scope?: 'thread' | 'library';
  readonly file_name: string;
  readonly mime_type: string;
  readonly size_bytes: number;
  readonly modality_hint: AssetModality;
}

export interface UploadItem {
  readonly localId: string;
  readonly file: File;
  readonly asset?: Asset;
  readonly progress: number;
  readonly status: 'queued' | 'uploading' | 'processing' | 'ready' | 'failed' | 'refused';
  readonly error?: string;
}
