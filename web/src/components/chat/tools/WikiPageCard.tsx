import type { ToolCallMessagePartProps } from '@assistant-ui/react';
import { FileText, LoaderCircle } from 'lucide-react';
import { Link } from 'react-router-dom';
import { useLocale } from '@/hooks/useLocale';
import { ToolCallComponent } from '../ToolCallComponent';
import { extractWikiPageSummary, safeJSONStringify } from '../ToolCallComponent.helpers';

export function WikiPageCard(props: ToolCallMessagePartProps) {
  const { t } = useLocale();
  const summary = extractWikiPageSummary(props.result);

  if (!summary && props.status.type !== 'running') {
    return <ToolCallComponent {...props} />;
  }

  return (
    <div
      data-testid="wiki-page-tool-card"
      data-status={props.status.type}
      className="my-2 overflow-hidden rounded-md border bg-muted/25 text-sm"
    >
      <div className="flex items-center gap-2 border-b px-3 py-2">
        {props.status.type === 'running' ? (
          <LoaderCircle className="size-4 animate-spin text-sky-600" />
        ) : (
          <FileText className="size-4 text-primary" />
        )}
        <span className="font-medium">{t('chat.toolCall.wiki.title')}</span>
        <span className="ml-auto rounded-md bg-emerald-500/10 px-1.5 py-0.5 text-xs text-emerald-700">
          {props.status.type === 'running'
            ? t('chat.toolCall.status.running')
            : t('chat.toolCall.status.done')}
        </span>
      </div>
      <div className="px-3 py-3">
        {summary ? (
          <>
            <Link
              to={`/wiki/${encodeURIComponent(summary.slug)}`}
              className="font-medium text-primary underline-offset-4 hover:underline"
            >
              {summary.title}
            </Link>
            <p className="mt-1 text-xs text-muted-foreground">[[{summary.slug}]]</p>
            {summary.preview && (
              <p className="mt-2 line-clamp-3 text-sm text-muted-foreground">{summary.preview}</p>
            )}
          </>
        ) : (
          <p className="text-sm text-muted-foreground">{t('chat.toolCall.wiki.running')}</p>
        )}
      </div>
      <details className="border-t border-dashed px-3 py-2 text-xs">
        <summary className="cursor-pointer font-medium text-muted-foreground">
          {t('chat.toolCall.details')}
        </summary>
        <pre className="mt-2 max-h-52 overflow-auto rounded-md bg-background p-2 leading-5">
          {safeJSONStringify(props.result ?? {})}
        </pre>
      </details>
    </div>
  );
}
