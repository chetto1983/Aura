import { useEffect, useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { DisplayCode } from './types';
import { DisplayCardShell } from './DisplayCardShell';
import { useCopyAction } from './useCopyAction';
import { highlightCode, resolveLang } from './shiki';
import { Button } from '@/components/ui/button';
import { CodeBlock } from '@/components/code-block';

// CodeDisplay (D-10 / HARDEN-08): renders a code/shell body in mono. It shows a
// PLAIN escaped <pre> immediately (React escapes the text — a <script> in the body
// is text, never an element), then UPGRADES to the lazy-Shiki escaped-span highlight
// once the separate chunk resolves (A2: Shiki is dynamic-import()ed, off the
// critical-path bundle). `Copy code` copies the raw body; a long body collapses.
//
// SECURITY — T-26-16: both the plain fallback (React text node) and the Shiki HTML
// (codeToHtml HTML-escapes the content) tokenize-as-text and NEVER execute the code.

const COLLAPSE_LINES = 20;

function isDarkTheme(): boolean {
  if (typeof document === 'undefined') return true;
  return document.documentElement.getAttribute('data-theme') !== 'light';
}

export interface CodeDisplayProps {
  readonly payload: { readonly code?: DisplayCode };
  readonly rawResult?: string;
}

export function CodeDisplay({ payload, rawResult }: CodeDisplayProps) {
  const { t } = useTranslation();
  const { copied, copy } = useCopyAction();
  const bodyId = useId();
  const code = payload.code;
  const body = code?.body ?? '';
  const lang = code?.lang;
  const label = t('display.type.code');

  const lineCount = body.length === 0 ? 0 : body.split('\n').length;
  const collapsible = lineCount > COLLAPSE_LINES;
  const [expanded, setExpanded] = useState(false);
  const [showRaw, setShowRaw] = useState(false);
  const [html, setHtml] = useState<string | null>(null);

  // Lazy-highlight on mount / when the body or lang changes. The plain <pre> shows
  // until this resolves; if the lang isn't in the allow-list, html stays null and
  // the plain fallback remains (graceful degrade, never an error).
  useEffect(() => {
    if (body.length === 0) return;
    let cancelled = false;
    void highlightCode(body, lang, isDarkTheme()).then(
      (result) => {
        if (!cancelled) setHtml(result);
      },
      () => {
        // Highlight failure is non-fatal — keep the plain escaped <pre>.
      },
    );
    return () => {
      cancelled = true;
    };
  }, [body, lang]);

  if (body.length === 0) {
    return (
      <DisplayCardShell label={label}>
        <div className="flex flex-col items-center gap-1 py-8 text-center">
          <p className="text-sm font-medium text-text">{t('display.document.emptyHeading')}</p>
          <p className="text-[0.75rem] text-text-faint">{t('display.document.emptyBody')}</p>
        </div>
      </DisplayCardShell>
    );
  }

  const langMeta = resolveLang(lang) ?? t('display.code.plainText');
  const showFull = expanded || !collapsible;

  const actions = (
    <div className="flex flex-wrap gap-2">
      <Button
        type="button"
        onClick={() => {
          copy(body);
        }}
        aria-label={t('display.code.copyAria')}
        variant="outline"
        className="px-3 text-[0.75rem] text-text-muted hover:text-text"
      >
        {copied ? t('display.code.copied') : t('display.code.copy')}
      </Button>
      {rawResult !== undefined ? (
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            setShowRaw((value) => !value);
          }}
          aria-expanded={showRaw}
        >
          {showRaw ? t('display.code.hideRaw') : t('display.code.showRaw')}
        </Button>
      ) : null}
    </div>
  );

  return (
    <DisplayCardShell label={label} meta={langMeta} actions={actions}>
      <CodeBlock
        body={body}
        html={html}
        bodyId={bodyId}
        showFull={showFull}
        {...(code?.filename ? { filename: code.filename } : {})}
        {...(code?.first_line ? { firstLine: code.first_line } : {})}
        {...(code?.notice ? { notice: code.notice } : {})}
        {...(code?.extracted ? { extractedLabel: t('display.code.extracted') } : {})}
      />
      {showRaw && rawResult !== undefined ? (
        <pre className="mt-2 max-w-full overflow-x-auto whitespace-pre rounded-[var(--radius-md)] border border-border bg-surface p-3 text-xs text-text-muted">
          {rawResult}
        </pre>
      ) : null}

      {collapsible ? (
        <Button
          type="button"
          onClick={() => {
            setExpanded((v) => !v);
          }}
          aria-expanded={expanded}
          aria-controls={bodyId}
          aria-label={expanded ? t('display.code.collapseAria') : t('display.code.expandAria')}
          variant="outline"
          className="mt-2 px-3 text-[0.75rem] text-text-muted hover:text-text"
        >
          {expanded ? t('display.code.collapse') : t('display.code.expand')}
        </Button>
      ) : null}
    </DisplayCardShell>
  );
}
