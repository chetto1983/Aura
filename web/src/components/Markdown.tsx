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
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

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

export function Markdown({ content, variant = 'default' }: Props) {
  const safe = content || '';
  const fallback = (
    <span className="sacchi-md-fallback">{safe}</span>
  );
  return (
    <div className={`md ${variant === 'user' ? 'md--user' : ''}`}>
      <MarkdownErrorBoundary fallback={fallback} resetKey={String(safe.length)}>
        <ReactMarkdown
          remarkPlugins={[remarkGfm]}
          skipHtml
          components={{
            a: ({ node, ...props }) => {
              void node;
              return <a {...props} target="_blank" rel="noopener noreferrer" />;
            },
          }}
        >
          {safe}
        </ReactMarkdown>
      </MarkdownErrorBoundary>
    </div>
  );
}

export default Markdown;
