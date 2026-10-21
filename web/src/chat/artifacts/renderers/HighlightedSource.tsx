import { useContext, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { highlightCode, canHighlight } from '../../displays/shiki';
import { useCopyAction } from '../../displays/useCopyAction';
import { useAssetContent } from './useAssetContent';
import { useAssetSource } from './assetSourceContext';
import { PreviewActionsContext } from './previewActionsContext';
import { PreviewAction } from './PreviewAction';
import { PreviewErrorWithDownload, PreviewLoading } from './PreviewStatus';
import { useThemeMode } from '@/theme/useThemeMode';
import { cn } from '@/lib/utils';

// An asset's text, shown React-escaped in a <pre> at once and upgraded to Shiki's escaped-span
// HTML when its lang is in the allow-list (displays/shiki.ts): Shiki tokenizes as text and never
// emits live markup from the source, so the bytes are shown, never parsed or run (T-37B-10). An
// unknown lang keeps the plain <pre>. Both the text preview (lang from the file extension) and an
// HTML artifact's source tab use it.

interface HighlightedSourceProps {
  readonly assetId: string;
  /** A Shiki lang or file extension; resolveLang decides whether it is highlighted. */
  readonly lang: string;
  /** Wrap long lines: a viewed file reads as a document, a page's markup as code. */
  readonly wrap?: boolean;
  readonly fileName?: string;
}

export function HighlightedSource({
  assetId,
  lang,
  wrap = false,
  fileName = 'source',
}: HighlightedSourceProps) {
  const { data, error, truncated, binary } = useAssetContent(assetId, 'text');
  const { t } = useTranslation();
  const { assetUrl } = useAssetSource();
  const { copy, copied } = useCopyAction();
  const actions = useContext(PreviewActionsContext);
  const dark = useThemeMode() === 'dark';
  const [highlighted, setHighlighted] = useState<{
    source: string;
    html: string;
    lang: string;
    dark: boolean;
  } | null>(null);
  useEffect(() => {
    if (data === undefined) return;
    let cancelled = false;
    void highlightCode(data, lang, dark).then(
      (html) => {
        if (!cancelled) setHighlighted(html === null ? null : { source: data, html, lang, dark });
      },
      () => undefined,
    );
    return () => {
      cancelled = true;
    };
  }, [data, lang, dark]);
  if (error !== undefined)
    return <PreviewErrorWithDownload assetId={assetId} fileName={fileName} detail={error} />;
  if (data === undefined) return <PreviewLoading />;
  const layout = cn(
    'min-h-0 flex-1 overflow-auto p-4 font-mono',
    wrap
      ? 'whitespace-pre-wrap break-words text-sm [&_pre]:whitespace-pre-wrap [&_pre]:break-words'
      : 'text-xs leading-relaxed',
  );
  const html =
    highlighted?.source === data && highlighted.lang === lang && highlighted.dark === dark
      ? highlighted.html
      : null;
  const copyButton =
    !binary && !truncated ? (
      <button
        type="button"
        className="min-h-[44px] text-sm text-info"
        onClick={() => {
          copy(data);
        }}
      >
        {t(copied ? 'artifacts.preview.sourceCopied' : 'artifacts.preview.copySource')}
      </button>
    ) : null;
  return (
    <div className="flex h-full min-h-0 flex-col">
      {actions ? (
        <PreviewAction>{copyButton}</PreviewAction>
      ) : (
        <div className="flex shrink-0 flex-wrap items-center gap-3 border-b border-border px-4 py-2 text-sm">
          {copyButton}
          <a
            className="inline-flex min-h-[44px] items-center text-info underline"
            href={assetUrl(assetId)}
            download={fileName}
          >
            {t('artifacts.preview.downloadOriginal')}
          </a>
        </div>
      )}
      {(binary === true || truncated === true || !canHighlight(data)) && (
        <p role="status" className="px-4 py-2 text-sm text-text-muted">
          {t(
            binary
              ? 'artifacts.preview.binarySource'
              : truncated
                ? 'artifacts.preview.truncatedSource'
                : 'artifacts.preview.plainSource',
          )}
        </p>
      )}
      {!binary &&
        (html !== null ? (
          <div
            className={cn(layout, '[&_pre]:m-0 [&_pre]:!bg-transparent')}
            dangerouslySetInnerHTML={{ __html: html }}
          />
        ) : (
          <pre className={cn(layout, 'text-text')}>{data}</pre>
        ))}
    </div>
  );
}
