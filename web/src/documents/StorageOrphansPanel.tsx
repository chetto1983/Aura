import { useState } from 'react';
import type { FormEvent } from 'react';
import { Search, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  cleanupStorageOrphans,
  fetchStorageOrphans,
  type StorageObjectInfo,
  type StorageOrphanReport,
} from './documentApi';
import { formatBytes } from './documentFormat';
import { Spinner } from '../components/Spinner';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

type PanelStatus = 'idle' | 'loading' | 'ready' | 'error';

export function StorageOrphansPanel() {
  const { t } = useTranslation();
  const [bucket, setBucket] = useState('');
  const [prefix, setPrefix] = useState('');
  const [confirm, setConfirm] = useState('');
  const [report, setReport] = useState<StorageOrphanReport | undefined>(undefined);
  const [status, setStatus] = useState<PanelStatus>('idle');
  const [cleaning, setCleaning] = useState(false);

  async function dryRun(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setStatus('loading');
    setConfirm('');
    try {
      const next = await fetchStorageOrphans({ bucket, prefix });
      setReport(next);
      setStatus('ready');
    } catch {
      setReport(undefined);
      setStatus('error');
    }
  }

  async function cleanup() {
    if (
      report === undefined ||
      report.orphans.length === 0 ||
      confirm !== 'DELETE ORPHAN OBJECTS'
    ) {
      return;
    }
    setCleaning(true);
    try {
      const next = await cleanupStorageOrphans({
        bucket: report.bucket,
        prefix: report.prefix,
        dry_run_token: report.dry_run_token,
        confirm: 'DELETE ORPHAN OBJECTS',
      });
      setReport(next);
    } finally {
      setCleaning(false);
    }
  }

  const orphanCount = report?.orphans.length ?? 0;
  const canDelete = report !== undefined && orphanCount > 0 && confirm === 'DELETE ORPHAN OBJECTS';

  return (
    <section
      aria-labelledby="storage-orphans"
      className="flex flex-col gap-4 border-t border-border pt-5"
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 id="storage-orphans" className="text-[18px] font-semibold text-text">
          {t('documents.storage.title')}
        </h2>
        {report !== undefined ? (
          <Badge variant={orphanCount > 0 ? 'warning' : 'success'}>
            {t('documents.storage.count', { count: orphanCount })}
          </Badge>
        ) : null}
      </div>

      <form onSubmit={dryRun} className="grid gap-3 md:grid-cols-[1fr_1fr_auto] md:items-end">
        <div className="grid gap-1.5">
          <Label htmlFor="storage-bucket">{t('documents.storage.bucket')}</Label>
          <Input
            id="storage-bucket"
            value={bucket}
            onChange={(event) => {
              setBucket(event.target.value);
            }}
          />
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor="storage-prefix">{t('documents.storage.prefix')}</Label>
          <Input
            id="storage-prefix"
            value={prefix}
            onChange={(event) => {
              setPrefix(event.target.value);
            }}
          />
        </div>
        <Button type="submit" disabled={bucket.trim() === '' || status === 'loading'}>
          {status === 'loading' ? <Spinner /> : <Search aria-hidden="true" />}
          {t('documents.storage.dryRun')}
        </Button>
      </form>

      {status === 'error' ? (
        <Alert variant="destructive">
          <AlertDescription>{t('documents.storage.error')}</AlertDescription>
        </Alert>
      ) : null}

      {report !== undefined ? (
        <div className="flex flex-col gap-3">
          {report.orphans.length === 0 ? (
            <p className="text-[13px] text-text-muted">{t('documents.storage.none')}</p>
          ) : (
            <div className="grid gap-2">
              {report.orphans.map((orphan) => (
                <OrphanRow key={`${objectBucket(orphan)}/${objectKey(orphan)}`} orphan={orphan} />
              ))}
            </div>
          )}

          {report.deleted > 0 ? (
            <div role="status" className="text-[13px] font-semibold text-success">
              {t('documents.storage.deleted', { count: report.deleted })}
            </div>
          ) : null}

          <div className="grid gap-3 border-t border-border pt-3 md:grid-cols-[1fr_auto] md:items-end">
            <div className="grid gap-1.5">
              <Label htmlFor="storage-orphan-confirm">{t('documents.storage.confirmLabel')}</Label>
              <Input
                id="storage-orphan-confirm"
                value={confirm}
                onChange={(event) => {
                  setConfirm(event.target.value);
                }}
              />
            </div>
            <Button
              type="button"
              variant="destructive"
              disabled={!canDelete || cleaning}
              onClick={() => void cleanup()}
            >
              {cleaning ? <Spinner /> : <Trash2 aria-hidden="true" />}
              {t('documents.storage.delete')}
            </Button>
          </div>
        </div>
      ) : null}
    </section>
  );
}

function OrphanRow({ orphan }: { readonly orphan: StorageObjectInfo }) {
  return (
    <div className="grid gap-2 rounded-md border border-border bg-surface px-3 py-3">
      <div className="min-w-0 break-all font-mono text-[12px] text-text">{objectKey(orphan)}</div>
      <div className="flex flex-wrap gap-2 text-[12px] text-text-muted">
        <span>{objectBucket(orphan)}</span>
        <span>{formatBytes(orphan.Attrs?.SizeBytes ?? 0)}</span>
        {orphan.Attrs?.MIMEType ? <span>{orphan.Attrs.MIMEType}</span> : null}
      </div>
    </div>
  );
}

function objectBucket(orphan: StorageObjectInfo): string {
  return orphan.Ref?.Bucket ?? '';
}

function objectKey(orphan: StorageObjectInfo): string {
  return orphan.Ref?.Key ?? '';
}
