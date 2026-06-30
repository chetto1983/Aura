import { Upload } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import { uploadLibraryDocument } from './documentUpload';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

interface DocumentUploadDialogProps {
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly onUploaded: () => void;
}

export function DocumentUploadDialog({
  open,
  onOpenChange,
  onUploaded,
}: DocumentUploadDialogProps) {
  const { t } = useTranslation();
  const [file, setFile] = useState<File | undefined>(undefined);
  const [progress, setProgress] = useState(0);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState('');

  async function upload() {
    if (file === undefined || uploading) return;
    setUploading(true);
    setError('');
    try {
      await uploadLibraryDocument(file, setProgress);
      onOpenChange(false);
      setFile(undefined);
      setProgress(0);
      onUploaded();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'upload failed');
    } finally {
      setUploading(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('documents.upload.title')}</DialogTitle>
          <DialogDescription>{t('documents.upload.body')}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-2">
          <Label htmlFor="document-upload-file">{t('documents.upload.file')}</Label>
          <Input
            id="document-upload-file"
            type="file"
            onChange={(event) => { setFile(event.target.files?.[0]); }}
          />
          {uploading ? (
            <div role="status" className="text-[13px] text-text-muted">
              {t('documents.upload.progress', { progress: Math.round(progress * 100) })}
            </div>
          ) : null}
          {error.length > 0 ? (
            <div role="alert" className="text-[13px] text-danger">
              {error}
            </div>
          ) : null}
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => { onOpenChange(false); }}>
            {t('documents.actions.cancel')}
          </Button>
          <Button type="button" disabled={file === undefined || uploading} onClick={() => void upload()}>
            {uploading ? <Spinner /> : <Upload aria-hidden="true" />}
            {t('documents.actions.upload')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
