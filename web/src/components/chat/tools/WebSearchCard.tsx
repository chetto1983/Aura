import type { ToolCallMessagePartProps } from '@assistant-ui/react';
import { ExternalLink, LoaderCircle, Search } from 'lucide-react';
import { useLocale } from '@/hooks/useLocale';
import { ToolCallComponent } from '../ToolCallComponent';
import { extractWebSearchResults, resultPreview, safeJSONStringify } from '../ToolCallComponent.helpers';

const MAX_VISIBLE_RESULTS = 3;

export function WebSearchCard(props: ToolCallMessagePartProps) {
  const { t } = useLocale();
  const results = extractWebSearchResults(props.result).slice(0, MAX_VISIBLE_RESULTS);
  const preview = resultPreview(props.result);

  if (props.status.type !== 'running' && results.length === 0 && !preview) {
    return <ToolCallComponent {...props} />;
  }

  return (
    <div
      data-testid="web-search-tool-card"
      data-status={props.status.type}
      className="my-2 overflow-hidden rounded-md border bg-muted/25 text-sm"
    >
      <div className="flex items-center gap-2 border-b px-3 py-2">
        {props.status.type === 'running' ? (
          <LoaderCircle className="size-4 animate-spin text-sky-600" />
        ) : (
          <Search className="size-4 text-primary" />
        )}
        <span className="font-medium">{t('chat.toolCall.webSearch.title')}</span>
        <span className="ml-auto rounded-md bg-sky-500/10 px-1.5 py-0.5 text-xs text-sky-700">
          {props.status.type === 'running'
            ? t('chat.toolCall.status.running')
            : t('chat.toolCall.webSearch.resultCount', { count: results.length })}
        </span>
      </div>

      <div className="space-y-2 px-3 py-3">
        {results.length > 0 ? (
          results.map((item, index) => (
            <div
              key={`${item.url ?? item.title}-${index}`}
              data-testid="web-search-result"
              className="rounded-md border bg-background px-3 py-2"
            >
              {item.url ? (
                <a
                  href={item.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex min-w-0 items-center gap-1 font-medium text-primary underline-offset-4 hover:underline"
                >
                  <span className="truncate">{item.title}</span>
                  <ExternalLink className="size-3 shrink-0" />
                </a>
              ) : (
                <p className="font-medium">{item.title}</p>
              )}
              {item.snippet && <p className="mt-1 text-xs text-muted-foreground">{item.snippet}</p>}
            </div>
          ))
        ) : (
          <p className="text-sm text-muted-foreground">
            {props.status.type === 'running'
              ? t('chat.toolCall.webSearch.running')
              : t('chat.toolCall.webSearch.noStructuredResults')}
          </p>
        )}
        {preview && results.length === 0 && (
          <p className="rounded-md bg-background px-3 py-2 text-xs text-muted-foreground">{preview}</p>
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
