import { useRef, useState } from 'react';
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

const KIND_LABEL: Record<CompactionEntry['kind'], string> = {
  compaction: 'Semantic compaction',
  l1_offload: 'L1 payload offload',
  l2_5_loss: 'Lossy emergency reduction',
};

export function CompactionHistory({ conversationId }: CompactionHistoryProps) {
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

  function openPreview(entry: CompactionEntry, button: HTMLButtonElement) {
    previewButton.current = button;
    setSelected(entry);
    preview.mutate(entry.checkpoint_id);
  }

  return (
    <section aria-labelledby="compaction-history-heading" className="flex flex-col gap-3">
      <div>
        <h2 id="compaction-history-heading" className="text-lg font-semibold">
          Compaction history
        </h2>
        <p className="text-sm text-muted-foreground">
          Working context changes; the canonical conversation remains intact unless loss is
          explicitly shown.
        </p>
      </div>

      {history.isPending ? <p role="status">Loading compaction history…</p> : null}
      {history.isError ? (
        <Alert variant="destructive">
          <strong>History unavailable</strong>
          <AlertDescription>Retry when the conversation service is available.</AlertDescription>
        </Alert>
      ) : null}
      {lossy ? (
        <Alert variant="destructive">
          <strong>Information was removed</strong>
          <AlertDescription>
            Lossy emergency reduction is different from reversible compaction.
          </AlertDescription>
        </Alert>
      ) : null}
      {damaged ? (
        <Alert variant="destructive">
          <strong>Recovery required</strong>
          <AlertDescription>
            The checkpoint is quarantined. Restore the last verified checkpoint.
          </AlertDescription>
        </Alert>
      ) : null}

      <ul className="flex flex-col gap-2" aria-label="Compaction checkpoints">
        {entries.map((entry) => (
          <li key={entry.checkpoint_id}>
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-base">
                  {KIND_LABEL[entry.kind]}
                  <Badge variant={entry.quality === 'passed' ? 'secondary' : 'danger'}>
                    {entry.quality}
                  </Badge>
                </CardTitle>
                <CardDescription>
                  {entry.trigger} · {entry.reason} · {entry.token_delta} tokens
                </CardDescription>
              </CardHeader>
              <CardContent>
                <Button
                  ref={entry === selected ? previewButton : undefined}
                  type="button"
                  variant="outline"
                  aria-label={`Preview checkpoint ${entry.checkpoint_id}`}
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
                  Preview
                </Button>
              </CardContent>
            </Card>
          </li>
        ))}
      </ul>

      {selected ? (
        <Card role="region" aria-label="Checkpoint preview">
          <CardHeader>
            <CardTitle>Checkpoint {selected.checkpoint_id}</CardTitle>
            <CardDescription>
              {preview.data?.summary ?? 'Validated metadata preview'}
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-wrap gap-2">
            <Button
              type="button"
              aria-label={`Restore checkpoint ${selected.checkpoint_id}`}
              disabled={restore.isPending}
              onClick={() => {
                restore.mutate(selected.checkpoint_id);
              }}
            >
              Restore checkpoint
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                setSelected(null);
                previewButton.current?.focus();
              }}
            >
              Close preview
            </Button>
          </CardContent>
        </Card>
      ) : null}

      {restore.isSuccess ? <p role="status">Checkpoint restored</p> : null}
      {restore.isError ? (
        <Alert variant="destructive">
          <strong>Restore failed</strong>
          <AlertDescription>The active context was not changed.</AlertDescription>
        </Alert>
      ) : null}
    </section>
  );
}
