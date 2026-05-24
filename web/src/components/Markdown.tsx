/** @ts-nocheck */
/**
 * Markdown renderer using the upstream-supported version pair:
 *   react-markdown@8 + remark-gfm@3.0.1
 *
 * The previously-installed remark-gfm@4.x crashes with
 * `Cannot set properties of undefined (setting 'inTable')` because it depends
 * on remark-parse@11 while react-markdown@8 ships remark-parse@10. The fix is
 * to pin remark-gfm to 3.0.1 — confirmed in upstream issues
 * remarkjs/react-markdown#771 and remarkjs/remark-gfm#57.
 *
 * The error boundary stays as a defensive fallback; should anything still
 * throw, we render the raw text instead of crashing the whole bubble.
 */
import { Component } from 'react';
import { Link } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeHighlight from 'rehype-highlight';

interface Props {
  content: string;
  variant?: 'default' | 'user';
  streaming?: boolean;
}

class MarkdownErrorBoundary extends Component<
  { children: React.ReactNode; fallback: React.ReactNode; resetKey: string },
  { hasError: boolean; lastKey: string }
> {
  state = { hasError: false, lastKey: '' };
  static getDerivedStateFromError() { return { hasError: true, lastKey: '' }; }
  static getDerivedStateFromProps(props: { resetKey: string }, state: { hasError: boolean; lastKey: string }) {
    if (props.resetKey !== state.lastKey) return { hasError: false, lastKey: props.resetKey };
    return null;
  }
  render() {
    return this.state.hasError ? this.props.fallback : this.props.children;
  }
}

function escapeMarkdownLabel(value: string): string {
  return value.replace(/\\/g, '\\\\').replace(/\]/g, '\\]');
}

function markdownWithWikiLinks(content: string): string {
  let inFence = false;
  return content
    .split(/(\r?\n)/)
    .map((line) => {
      if (!line.includes('\n') && line.trimStart().startsWith('```')) {
        inFence = !inFence;
        return line;
      }
      if (inFence) return line;
      return line.replace(/\[\[([A-Za-z0-9][A-Za-z0-9._-]*)(?:\|([^\]\n]+))?\]\]/g, (match, rawSlug: string, rawLabel?: string) => {
        const slug = rawSlug.trim();
        const label = escapeMarkdownLabel((rawLabel ?? slug).trim());
        return `[${label}](/wiki/${encodeURIComponent(slug)})`;
      });
    })
    .join('');
}

export function Markdown({ content, variant = 'default' }: Props) {
  const safe = content || '';
  const rendered = markdownWithWikiLinks(safe);
  const fallback = (
    <span className="aura-md-fallback">{safe}</span>
  );
  return (
    <div className={`md ${variant === 'user' ? 'md--user' : ''}`}>
      <MarkdownErrorBoundary fallback={fallback} resetKey={String(safe.length)}>
        <ReactMarkdown
          remarkPlugins={[remarkGfm]}
          rehypePlugins={[[rehypeHighlight, { detect: false, ignoreMissing: true }]]}
          skipHtml
          components={{
            a: ({ node, href = '', ...props }) => {
              void node;
              if (href.startsWith('/wiki/')) {
                return <Link {...props} to={href} className="aura-wiki-link" />;
              }
              return <a {...props} href={href} target="_blank" rel="noopener noreferrer" />;
            },
          }}
        >
          {rendered}
        </ReactMarkdown>
      </MarkdownErrorBoundary>
    </div>
  );
}

export default Markdown;
