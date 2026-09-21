import { Upload } from 'lucide-react';
import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { finalizeAsset, presignAsset } from '../chat/attachments/api';
import { putWithProgress } from '../chat/attachments/upload';
import { editableKind } from './editRules';
import { useOpenEditor } from './mediaEditorContext';
import { Button } from '@/components/ui/button';

const ACCEPTED_MEDIA = 'image/png,image/jpeg,image/webp,video/mp4,video/webm';

/** Uploads an editable file into the caller's library, then hands its owned asset to the editor. */
export function UploadMediaButton({ className }: { readonly className?: string }) {
  const { t } = useTranslation();
  const open = useOpenEditor();
  const input = useRef<HTMLInputElement>(null);
  const [state, setState] = useState<'idle' | 'uploading'>('idle');
  const [problem, setProblem] = useState<string>();

  if (open === undefined) return null;
  const openEditor = open;

  async function upload(file: File) {
    const kind = editableKind(file.type);
    if (kind === undefined) {
      setProblem(t('mediaEdit.upload.unsupported'));
      return;
    }
    setProblem(undefined);
    setState('uploading');
    try {
      const presigned = await presignAsset({
        thread_id: '',
        scope: 'library',
        file_name: file.name,
        mime_type: file.type,
        size_bytes: file.size,
        modality_hint: kind,
      });
      await putWithProgress(
        presigned.upload.upload_url,
        file,
        presigned.upload.required_headers,
        () => {
          setState('uploading');
        },
      );
      const asset = await finalizeAsset(presigned.asset.id);
      openEditor({ assetId: asset.id, kind });
    } catch (err) {
      setProblem(err instanceof Error ? err.message : t('mediaEdit.upload.failed'));
    } finally {
      setState('idle');
    }
  }

  return (
    <div className={className}>
      <input
        ref={input}
        type="file"
        accept={ACCEPTED_MEDIA}
        className="sr-only"
        aria-label={t('mediaEdit.upload.choose')}
        onChange={(event) => {
          const file = event.currentTarget.files?.[0];
          event.currentTarget.value = '';
          if (file !== undefined) void upload(file);
        }}
      />
      <Button
        type="button"
        size="sm"
        variant="ghost"
        disabled={state === 'uploading'}
        className="min-h-8 gap-1.5 py-1 text-xs"
        onClick={() => {
          input.current?.click();
        }}
      >
        <Upload aria-hidden="true" className="size-3.5" />
        {state === 'uploading' ? t('mediaEdit.upload.uploading') : t('mediaEdit.upload.action')}
      </Button>
      {problem === undefined ? null : (
        <p role="alert" className="mt-1 text-xs text-danger">
          {problem}
        </p>
      )}
    </div>
  );
}
