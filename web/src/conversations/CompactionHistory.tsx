import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  type CompactionEntry,
  useCompactionHistory,
  usePreviewCompaction,
  useRestoreCompaction,
} from './useCompactions';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';

export interface CompactionHistoryProps {
  readonly conversationId: string;
}

export function CompactionHistory({ conversationId }: CompactionHistoryProps) {
  const { t, i18n } = useTranslation();
  const history = useCompactionHistory(conversationId);
  const preview = usePreviewCompaction(conversationId);
  const restore = useRestoreCompaction(conversationId);
  const [selected, setSelected] = useState<CompactionEntry | null>(null);
  const previewButton = useRef<HTMLButtonElement | null>(null);
  const entries = history.data?.entries ?? [];
  const lossy = entries.some((entry) => entry.kind === 'l2_5_loss');
  const damaged = entries.some(
    (entry) => entry.quality === 'corrupt' || entry.quality === 'quarantined',
  );
  const number = new Intl.NumberFormat(i18n.resolvedLanguage ?? i18n.language);
  function openPreview(entry: CompactionEntry, button: HTMLButtonElement) {
    previewButton.current = button;
    setSelected(entry);
    preview.mutate(entry.checkpoint_id);
  }
  return (
    <section aria-labelledby="compaction-history-heading" className="flex flex-col gap-3">
      <div>
        <h2 id="compaction-history-heading" className="text-lg font-semibold">
          {t('compaction.heading')}
        </h2>
        <p className="text-sm text-muted-foreground">{t('compaction.description')}</p>
      </div>
      {history.isPending ? <p role="status">{t('compaction.loading')}</p> : null}
      {history.isError ? (
        <Alert variant="destructive">
          <strong>{t('compaction.unavailable')}</strong>
          <AlertDescription>{t('compaction.retry')}</AlertDescription>
        </Alert>
      ) : null}
      {lossy ? (
        <Alert variant="destructive">
          <strong>{t('compaction.lossTitle')}</strong>
          <AlertDescription>{t('compaction.lossBody')}</AlertDescription>
        </Alert>
      ) : null}
      {damaged ? (
        <Alert variant="destructive">
          <strong>{t('compaction.recoveryTitle')}</strong>
          <AlertDescription>{t('compaction.recoveryBody')}</AlertDescription>
        </Alert>
      ) : null}
      <ul className="flex flex-col gap-2" aria-label={t('compaction.checkpoints')}>
        {entries.map((entry) => (
          <li key={entry.checkpoint_id}>
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-base">
                  {t(`compaction.kinds.${entry.kind}`)}
                  <Badge variant={entry.quality === 'passed' ? 'secondary' : 'danger'}>
                    {entry.quality}
                  </Badge>
                </CardTitle>
                <CardDescription>
                  {t('compaction.metadata', {
                    trigger: entry.trigger,
                    reason: entry.reason,
                    delta: number.format(entry.token_delta),
                  })}
                </CardDescription>
              </CardHeader>
              <CardContent>
                <Button
                  ref={entry === selected ? previewButton : undefined}
                  type="button"
                  variant="outline"
                  aria-label={t('compaction.previewCheckpoint', { id: entry.checkpoint_id })}
                  onClick={(event) => {
                    openPreview(entry, event.currentTarget);
                  }}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter' || event.key === ' ') {
                      event.preventDefault();
                      openPreview(entry, event.currentTarget);
                    }
                  }}
                  className="motion-reduce:transition-none"
                >
                  {t('compaction.preview')}
                </Button>
              </CardContent>
            </Card>
          </li>
        ))}
      </ul>
      {selected ? (
        <Card role="region" aria-label={t('compaction.previewRegion')}>
          <CardHeader>
            <CardTitle>{t('compaction.checkpoint', { id: selected.checkpoint_id })}</CardTitle>
            <CardDescription>
              {preview.data?.summary ?? t('compaction.metadataPreview')}
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-wrap gap-2">
            <Button
              type="button"
              aria-label={t('compaction.restoreCheckpoint', { id: selected.checkpoint_id })}
              disabled={restore.isPending}
              onClick={() => {
                restore.mutate(selected.checkpoint_id);
              }}
            >
              {t('compaction.restore')}
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                setSelected(null);
                previewButton.current?.focus();
              }}
            >
              {t('compaction.close')}
            </Button>
          </CardContent>
        </Card>
      ) : null}
      {restore.isSuccess ? <p role="status">{t('compaction.restored')}</p> : null}
      {restore.isError ? (
        <Alert variant="destructive">
          <strong>{t('compaction.restoreFailed')}</strong>
          <AlertDescription>{t('compaction.unchanged')}</AlertDescription>
        </Alert>
      ) : null}
    </section>
  );
}
