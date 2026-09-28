// Adapted from Tool UI CodeBlock at assistant-ui/tool-ui commit 49a870286facdbf28160cd647f0d337ebdc9b275:
// apps/www/components/tool-ui/code-block/code-block.tsx, SHA-256 E225AA2FC57BC5D235AB0259174201A8D91EAB6BC5E80696B1FA8113D4E7E340.
// Aura supplies its existing lazy Shiki result, copy action and disclosure state to avoid a second highlighter and clipboard owner.
// MIT license and copyright notice: see LICENSE.tool-ui in this directory.
'use client';

export function CodeBlock({
  body,
  html,
  bodyId,
  showFull,
  filename,
  firstLine,
  notice,
  extractedLabel,
}: {
  readonly body: string;
  readonly html: string | null;
  readonly bodyId: string;
  readonly showFull: boolean;
  readonly filename?: string;
  readonly firstLine?: number;
  readonly notice?: string;
  readonly extractedLabel?: string;
}) {
  return (
    <div
      data-slot="code-block"
      className="w-full min-w-0 overflow-hidden rounded-[var(--radius-md)] border border-border bg-surface"
    >
      {filename || firstLine || extractedLabel ? (
        <div className="flex min-w-0 flex-wrap items-center gap-2 border-b border-border px-3 py-2 text-xs text-text-muted">
          {filename ? (
            <span className="max-w-full break-all font-medium text-text">{filename}</span>
          ) : null}
          {firstLine ? <span className="font-mono tabular-nums">{firstLine}</span> : null}
          {extractedLabel ? <span>{extractedLabel}</span> : null}
        </div>
      ) : null}
      <div className={showFull ? '' : 'max-h-80 overflow-hidden'}>
        {html !== null ? (
          <div
            id={bodyId}
            data-testid="code-highlighted"
            className="overflow-x-auto p-3 font-mono text-xs leading-relaxed [&_pre]:!bg-transparent [&_pre]:m-0 [&_pre]:whitespace-pre"
            dangerouslySetInnerHTML={{ __html: html }}
          />
        ) : (
          <pre
            id={bodyId}
            data-testid="code-plain"
            className="overflow-x-auto p-3 font-mono text-xs leading-relaxed text-text-muted"
          >
            {body}
          </pre>
        )}
      </div>
      {notice ? (
        <p className="border-t border-border px-3 py-2 text-xs text-text-faint">{notice}</p>
      ) : null}
    </div>
  );
}
