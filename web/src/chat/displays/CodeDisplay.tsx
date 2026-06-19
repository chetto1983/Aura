import { useEffect, useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { DisplayCode } from './types';
import { DisplayCardShell } from './DisplayCardShell';
import { useCopyAction } from './useCopyAction';
import { highlightCode, resolveLang } from './shiki';

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
}

export function CodeDisplay({ payload }: CodeDisplayProps) {
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
    <button
      type="button"
      onClick={() => {
        copy(body);
      }}
      aria-label={t('display.code.copyAria')}
      className="flex min-h-11 items-center rounded-[var(--radius-md)] border border-border px-2 text-[0.75rem] font-medium text-text-muted hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
    >
      {copied ? t('display.code.copied') : t('display.code.copy')}
    </button>
  );

  return (
    <DisplayCardShell label={label} meta={langMeta} actions={actions}>
      <div
        className={`overflow-hidden rounded-[var(--radius-md)] border border-border bg-surface ${
          showFull ? '' : 'max-h-80'
        }`}
      >
        {html !== null ? (
          // Shiki output: escaped-span HTML (HARDEN-08-safe — codeToHtml escapes the
          // code content). We override Shiki's inline pre background to our surface.
          <div
            id={bodyId}
            data-testid="code-highlighted"
            className="overflow-x-auto p-3 font-mono text-xs leading-relaxed [&_pre]:!bg-transparent [&_pre]:m-0 [&_pre]:whitespace-pre"
            dangerouslySetInnerHTML={{ __html: html }}
          />
        ) : (
          // Plain escaped fallback (pre-resolve OR unsupported lang). React escapes
          // the text — a <script> body is inert text, never an element.
          <pre
            id={bodyId}
            data-testid="code-plain"
            className="overflow-x-auto p-3 font-mono text-xs leading-relaxed text-text-muted"
          >
            {body}
          </pre>
        )}
      </div>

      {collapsible ? (
        <button
          type="button"
          onClick={() => {
            setExpanded((v) => !v);
          }}
          aria-expanded={expanded}
          aria-controls={bodyId}
          aria-label={expanded ? t('display.code.collapseAria') : t('display.code.expandAria')}
          className="mt-2 flex min-h-11 items-center rounded-[var(--radius-md)] border border-border px-2 text-[0.75rem] font-medium text-text-muted hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        >
          {expanded ? t('display.code.collapse') : t('display.code.expand')}
        </button>
      ) : null}
    </DisplayCardShell>
  );
}
