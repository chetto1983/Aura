import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { highlightCode } from './displays/shiki';
import { useCopyAction } from './displays/useCopyAction';
import { Button } from '@/components/ui/button';

// ToolResultPanel (compact-chat spec §3.5) — the structured raw panel behind a
// collapsed tool row when NO typed display payload exists (replaces the bare
// unbounded <pre>, C1). Two labeled sections: Request (streamed argsText,
// pretty-printed when parseable) and Result (pretty-printed + lazy-Shiki
// highlighted when JSON, escaped text otherwise — the CodeDisplay degrade).
//
// SECURITY (HARDEN-08 / D-FALLBACK): the body is untrusted tool output. The
// plain path renders as React TEXT inside <pre>; the highlighted path is
// Shiki's escaped-span HTML (codeToHtml tokenizes-as-text). NEVER markdown.

function isDarkTheme(): boolean {
  if (typeof document === 'undefined') return true;
  return document.documentElement.getAttribute('data-theme') !== 'light';
}

/** Pretty-print a JSON body (2-space); null when the text is not valid JSON. */
function prettyJSON(text: string): string | null {
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return null;
  }
}

export interface ToolResultPanelProps {
  /** Raw streamed argument text (partial JSON during streaming). */
  readonly argsText?: string | undefined;
  /** Raw tool-result preview; absent while the call is still running. */
  readonly result?: string | undefined;
}

function Section({
  label,
  body,
  html,
  actions,
}: {
  readonly label: string;
  readonly body: string;
  readonly html?: string | null;
  readonly actions?: React.ReactNode;
}) {
  return (
    <section className="min-w-0">
      <div className="flex items-center justify-between gap-2">
        <h4 className="text-[0.75rem] font-medium uppercase tracking-wider text-text-faint">
          {label}
        </h4>
        {actions}
      </div>
      {html !== null && html !== undefined ? (
        // Shiki escaped-span HTML (tokenized-as-text, HARDEN-08-safe).
        <div
          data-testid="tool-result-highlighted"
          className="overflow-x-auto rounded-[var(--radius-sm)] border border-border bg-surface p-2 font-mono text-xs leading-relaxed [&_pre]:m-0 [&_pre]:whitespace-pre [&_pre]:!bg-transparent"
          dangerouslySetInnerHTML={{ __html: html }}
        />
      ) : (
        <pre className="overflow-x-auto whitespace-pre-wrap rounded-[var(--radius-sm)] border border-border bg-surface p-2 font-mono text-xs leading-relaxed text-text-muted">
          {body}
        </pre>
      )}
    </section>
  );
}

export function ToolResultPanel({ argsText, result }: ToolResultPanelProps) {
  const { t } = useTranslation();
  const { copied, copy } = useCopyAction();
  const request = argsText ?? '';
  const prettyRequest = prettyJSON(request);
  const prettyResult = result === undefined ? null : prettyJSON(result);
  const [highlighted, setHighlighted] = useState<{
    readonly source: string;
    readonly html: string;
  } | null>(null);

  // Lazy-highlight a JSON result; the plain escaped <pre> shows until/unless
  // the Shiki chunk resolves (the exact CodeDisplay degrade — never an error).
  // The html is keyed to its source so a stale highlight never renders for a
  // changed (or no-longer-JSON) result — no synchronous state reset needed.
  useEffect(() => {
    if (prettyResult === null) return;
    let cancelled = false;
    void highlightCode(prettyResult, 'json', isDarkTheme()).then(
      (html) => {
        if (!cancelled && html !== null) setHighlighted({ source: prettyResult, html });
      },
      () => {
        // Highlight failure is non-fatal — keep the plain escaped <pre>.
      },
    );
    return () => {
      cancelled = true;
    };
  }, [prettyResult]);
  const resultHtml =
    highlighted !== null && highlighted.source === prettyResult ? highlighted.html : null;

  return (
    <div className="flex max-h-80 min-w-0 flex-col gap-2 overflow-y-auto px-3 py-2">
      {request.length > 0 ? (
        <Section label={t('chat.tool.request')} body={prettyRequest ?? request} />
      ) : null}
      {result !== undefined ? (
        <Section
          label={t('chat.tool.result')}
          body={prettyResult ?? result}
          html={resultHtml}
          actions={
            <Button
              type="button"
              variant="ghost"
              size="sm"
              data-testid="tool-copy"
              aria-label={t('chat.tool.copyAria')}
              onClick={() => {
                copy(result);
              }}
              className="px-2 text-[0.75rem] text-text-muted hover:text-text"
            >
              {copied ? t('chat.tool.copied') : t('chat.tool.copy')}
            </Button>
          }
        />
      ) : null}
    </div>
  );
}
