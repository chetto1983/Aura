import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { fetchDocumentEvents, type IngestionEvent } from './documentApi';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';

type EventStatus = 'loading' | 'ready' | 'error';

export function DocumentEventsPanel({ documentId }: { readonly documentId: string }) {
  const { t } = useTranslation();
  const [events, setEvents] = useState<readonly IngestionEvent[]>([]);
  const [status, setStatus] = useState<EventStatus>('loading');

  useEffect(() => {
    let cancelled = false;
    void fetchDocumentEvents(documentId)
      .then((next) => {
        if (cancelled) return;
        setEvents(next);
        setStatus('ready');
      })
      .catch(() => {
        if (cancelled) return;
        setEvents([]);
        setStatus('error');
      });
    return () => {
      cancelled = true;
    };
  }, [documentId]);

  return (
    <section aria-labelledby="document-events" className="flex flex-col gap-3">
      <h2 id="document-events" className="text-[18px] font-semibold text-text">
        {t('documents.detail.events')}
      </h2>
      {status === 'loading' ? (
        <div role="status" className="text-[13px] text-text-muted">
          {t('documents.events.loading')}
        </div>
      ) : status === 'error' ? (
        <Alert variant="destructive">
          <AlertDescription>{t('documents.events.error')}</AlertDescription>
        </Alert>
      ) : events.length === 0 ? (
        <div className="text-[13px] text-text-muted">{t('documents.events.empty')}</div>
      ) : (
        <div className="grid gap-2">
          {events.map((event) => (
            <EventRow key={event.id} event={event} />
          ))}
        </div>
      )}
    </section>
  );
}

function EventRow({ event }: { readonly event: IngestionEvent }) {
  return (
    <div className="grid gap-2 rounded-md border border-border bg-surface px-3 py-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="break-all font-mono text-[12px] font-semibold text-text">
          {event.event_type}
        </span>
        {event.to_status ? <Badge variant="secondary">{event.to_status}</Badge> : null}
      </div>
      {event.message ? <p className="text-[13px] text-text-muted">{event.message}</p> : null}
      <div className="flex flex-wrap gap-2 text-[12px] text-text-faint">
        {event.from_status ? <span>{event.from_status}</span> : null}
        {event.created_at ? <span>{new Date(event.created_at).toLocaleString()}</span> : null}
        {event.trace_id ? <span>{event.trace_id}</span> : null}
      </div>
    </div>
  );
}
