import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useAui } from '@assistant-ui/react';
import { useTranslation } from 'react-i18next';
import { DisplayCardShell } from '../displays/DisplayCardShell';
import {
  createHostBridge,
  type HostContext,
  type McpViewDescriptor,
  type McpViewDocument,
  type SandboxEnvelope,
} from './hostProtocol';
import { fetchViewDocument } from './viewDocuments';

// McpViewFrame renders one MCP Apps view (SEP-1865).
//
// The frame it creates points at the SANDBOX ORIGIN, never at the cockpit's. That
// is the whole security posture in one line: a view needs `allow-same-origin` to
// run, so the origin it gets must be one where a session does not exist. The
// backend refuses to serve a document at all when that origin is unset or has
// collapsed onto the cockpit's, so this component can only ever be handed a
// usable pair.
//
// It owns the DOM and the effects; the conversation itself lives in
// ./hostProtocol, which knows nothing about React or iframes.

export interface McpViewFrameProps {
  readonly descriptor: McpViewDescriptor;
}

type Status = 'loading' | 'ready' | 'unavailable';

interface TextSink {
  setText: (text: string) => void;
}

/**
 * Resolve the composer a view's draft belongs in: the thread's, never the
 * message-scoped edit composer the card happens to render inside. The accessor
 * is probed rather than pinned because assistant-ui exposes the thread client at
 * more than one path across versions, and a wrong pin here is a runtime throw in
 * the one place a throw is most expensive (see onSendToChat).
 */
function threadComposer(aui: unknown): TextSink {
  const client = aui as {
    thread?: { composer?: TextSink };
    threads?: { main?: { composer?: TextSink } };
    composer?: TextSink;
  };
  const sink = client.thread?.composer ?? client.threads?.main?.composer ?? client.composer;
  if (!sink?.setText) throw new Error('no thread composer to draft into');
  return sink;
}

const INLINE_HEIGHT = '26rem';
const FULLSCREEN_HEIGHT = '80vh';

export function McpViewFrame({ descriptor }: McpViewFrameProps) {
  const { t } = useTranslation();
  const aui = useAui();
  const frameRef = useRef<HTMLIFrameElement | null>(null);
  const [mode, setMode] = useState<'inline' | 'fullscreen'>('inline');
  const [viewError, setViewError] = useState<string | null>(null);

  // The fetch outcome is stored WITH the pair it belongs to, and the pair is
  // compared during render. Resetting it from inside the effect instead would
  // mean a setState in an effect body: one cascading render per card, and a
  // window where a new descriptor is shown the previous one's document.
  const key = `${descriptor.server}|${descriptor.resource_uri}`;
  const [fetched, setFetched] = useState<{
    key: string;
    doc: McpViewDocument | null;
    failed: boolean;
  }>({
    key,
    doc: null,
    failed: false,
  });
  const current = fetched.key === key ? fetched : { key, doc: null, failed: false };
  const doc = current.doc;
  const status: Status = current.failed ? 'unavailable' : doc ? 'ready' : 'loading';

  useEffect(() => {
    let live = true;
    fetchViewDocument(descriptor.server, descriptor.resource_uri)
      .then((document_) => {
        if (live) setFetched({ key, doc: document_, failed: false });
      })
      .catch(() => {
        if (live) setFetched({ key, doc: null, failed: true });
      });
    return () => {
      live = false;
    };
  }, [descriptor.server, descriptor.resource_uri, key]);

  const hostContext = useCallback((): HostContext => {
    const root = document.documentElement;
    const read = (name: string) => getComputedStyle(root).getPropertyValue(name).trim();
    return {
      theme: root.classList.contains('dark') ? 'dark' : 'light',
      styles: {
        variables: {
          '--wa-bg': read('--color-surface') || '',
          '--host-surface': read('--color-surface') || '',
          '--host-text': read('--color-text') || '',
          '--host-accent': read('--color-accent') || '',
        },
      },
    };
  }, []);

  // A tool call a view asks for goes to the host, which refuses anything that is
  // not read-only. The browser never reaches an MCP server directly.
  const callTool = useCallback(
    async (tool: string, args: Record<string, unknown>) => {
      const res = await fetch('/api/mcp/view/call', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify({ server: descriptor.server, tool, arguments: args }),
      });
      if (!res.ok) throw new Error(t('display.mcpView.callRefused'));
      const body = (await res.json()) as { structured_content?: unknown };
      return body.structured_content ?? null;
    },
    [descriptor.server, t],
  );

  const sandboxSrc = useMemo(() => {
    if (!doc) return '';
    return `${doc.sandbox_origin}/mcp-sandbox?host=${encodeURIComponent(window.location.origin)}`;
  }, [doc]);

  useEffect(() => {
    if (!doc || status !== 'ready') return;
    const frame = frameRef.current;
    if (!frame) return;

    const bridge = createHostBridge({
      descriptor,
      html: doc.html,
      post: (envelope) => frame.contentWindow?.postMessage(envelope, doc.sandbox_origin),
      callTool,
      hostContext,
      // A view drafts, it does not send: the text lands in the composer and the
      // human presses enter. Anything else would let a mounted server speak as
      // the operator.
      // The THREAD composer, explicitly. A card renders inside a message part,
      // where the ambient `aui.composer` is that message's EDIT composer — which
      // does not exist unless the human is editing, and throws "Composer is not
      // available" when it is not. Found by driving a real view (2026-08-17).
      onSendToChat: (text) => {
        threadComposer(aui).setText(text);
      },
      onOpenLink: (url) => window.open(url, '_blank', 'noopener,noreferrer'),
      onDisplayMode: (requested) => {
        setMode(requested === 'fullscreen' ? 'fullscreen' : 'inline');
      },
      onLog: (level, data) => {
        if (level === 'error') setViewError(String(data));
      },
    });

    const onMessage = (event: MessageEvent) => {
      // Identity is BOTH the origin and the window. Origin alone would admit any
      // other frame the sandbox origin ever serves; window alone would admit a
      // navigated frame.
      if (event.origin !== doc.sandbox_origin) return;
      if (event.source !== frame.contentWindow) return;
      const envelope = event.data as SandboxEnvelope | null;
      if (envelope && typeof envelope === 'object' && 'kind' in envelope)
        bridge.onEnvelope(envelope);
    };

    window.addEventListener('message', onMessage);
    return () => {
      bridge.teardown();
      window.removeEventListener('message', onMessage);
    };
  }, [aui, callTool, descriptor, doc, hostContext, status]);

  const label = t('display.type.mcp_view');

  if (status === 'unavailable') {
    return (
      <DisplayCardShell label={label} rule="warning">
        <p className="text-[0.8125rem] text-text-faint">{t('display.mcpView.unavailable')}</p>
      </DisplayCardShell>
    );
  }

  return (
    <DisplayCardShell
      label={label}
      meta={descriptor.server}
      rule="info"
      actions={
        <button
          type="button"
          onClick={() => {
            setMode(mode === 'inline' ? 'fullscreen' : 'inline');
          }}
          className="rounded-[var(--radius-sm)] border border-border px-2 py-1 text-[0.75rem] text-text-faint transition-colors hover:border-accent hover:text-accent-text focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          {mode === 'inline' ? t('display.expand') : t('display.mcpView.collapse')}
        </button>
      }
    >
      <div className="flex flex-col gap-2">
        {doc ? (
          <iframe
            ref={frameRef}
            src={sandboxSrc}
            title={`${descriptor.server} ${descriptor.resource_uri}`}
            sandbox="allow-scripts allow-same-origin"
            referrerPolicy="no-referrer"
            className="w-full rounded-[var(--radius-sm)] border border-border bg-surface"
            style={{ height: mode === 'inline' ? INLINE_HEIGHT : FULLSCREEN_HEIGHT }}
          />
        ) : (
          <div
            className="w-full animate-pulse rounded-[var(--radius-sm)] border border-border bg-surface-2"
            style={{ height: INLINE_HEIGHT }}
          />
        )}
        {descriptor.payload_dropped_bytes !== undefined ? (
          <p role="note" className="text-[0.75rem] text-warning">
            {t('display.mcpView.payloadDropped')}
          </p>
        ) : null}
        {viewError ? (
          <p role="note" className="text-[0.75rem] text-warning">
            {viewError}
          </p>
        ) : null}
      </div>
    </DisplayCardShell>
  );
}
