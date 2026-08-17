// hostProtocol — the cockpit's half of the MCP Apps view protocol (SEP-1865).
//
// A view is a document a mounted MCP server wrote. It runs in a frame on a
// SECOND origin behind the sandbox relay (internal/agui/mcp_sandbox.html), and it
// speaks JSON-RPC 2.0 over postMessage. This module is that conversation and
// NOTHING else: it never touches the DOM, never opens a window, never fetches.
// Everything with an effect arrives as a callback, so the whole protocol is
// testable with a fake `post` and a list of messages.
//
// The relay envelope (`kind`) and the protocol (`jsonrpc`) are deliberately two
// layers. The relay knows no MCP; this module knows no iframes.

/** The `aura.mcp_view` CUSTOM descriptor: which view, and what it renders. */
export interface McpViewDescriptor {
  readonly server: string;
  readonly resource_uri: string;
  readonly tool_call_id: string;
  readonly tool_name?: string;
  readonly arguments?: unknown;
  readonly structured_content?: unknown;
  /** Set when the server's payload was over the host cap and was not forwarded. */
  readonly payload_dropped_bytes?: number;
}

/** GET /api/mcp/view — the document plus what the server asked for. */
export interface McpViewDocument {
  readonly server: string;
  readonly resource_uri: string;
  readonly html: string;
  readonly sandbox_origin: string;
  readonly applied_csp: string;
  readonly declared: {
    readonly sealed: boolean;
    readonly declared_csp: boolean;
    readonly connect_domains?: readonly string[];
    readonly resource_domains?: readonly string[];
    readonly frame_domains?: readonly string[];
    readonly base_uri_domains?: readonly string[];
    readonly domain?: string;
    readonly prefers_border: boolean;
  };
  readonly permissions?: Record<string, unknown>;
}

export function isMcpViewDescriptor(value: unknown): value is McpViewDescriptor {
  if (typeof value !== 'object' || value === null) return false;
  const v = value as Record<string, unknown>;
  return (
    typeof v.server === 'string' &&
    typeof v.resource_uri === 'string' &&
    typeof v.tool_call_id === 'string'
  );
}

/** What the host tells a view about the surface it is sitting in. */
export interface HostContext {
  readonly theme?: 'light' | 'dark';
  readonly styles?: { readonly variables?: Record<string, string> };
}

interface JsonRpcMessage {
  readonly jsonrpc?: string;
  readonly id?: number | string;
  readonly method?: string;
  readonly params?: Record<string, unknown>;
}

/** One envelope arriving from the sandbox relay. */
export type SandboxEnvelope =
  | { readonly kind: 'ready' }
  | { readonly kind: 'mounted' }
  // Nullable on purpose: this arrives from postMessage, where the sender decides
  // the shape. Typing it non-null would only move the check out of the compiler's
  // sight, not out of the runtime.
  | { readonly kind: 'from_view'; readonly message: JsonRpcMessage | null };

export interface HostBridgeOptions {
  readonly descriptor: McpViewDescriptor;
  /** The armed document to hand the relay once it says it is ready. */
  readonly html: string;
  /** Send one envelope to the relay. */
  readonly post: (envelope: unknown) => void;
  /** Run a tool the view asked for. Rejects when the host refuses it. */
  readonly callTool: (tool: string, args: Record<string, unknown>) => Promise<unknown>;
  readonly hostContext: () => HostContext;
  /** The view asked to put text in the conversation. */
  readonly onSendToChat?: (text: string) => void;
  /** The view asked to open a link; the URL is already http(s)-checked. */
  readonly onOpenLink?: (url: string) => void;
  /** The view asked for a display mode ('inline' | 'fullscreen'). */
  readonly onDisplayMode?: (mode: string) => void;
  /** A view-side log line (notifications/message). */
  readonly onLog?: (level: string, data: unknown) => void;
}

export interface HostBridge {
  /** Feed one relay envelope. */
  readonly onEnvelope: (envelope: SandboxEnvelope) => void;
  /** Ask the view to tear down (a request the view MUST answer). */
  readonly teardown: () => void;
  /** Push a fresh result into a mounted view (a re-run of the same tool). */
  readonly pushResult: (descriptor: McpViewDescriptor) => void;
}

const PROTOCOL_VERSION = '2026-01-26';

export function createHostBridge(opts: HostBridgeOptions): HostBridge {
  let descriptor = opts.descriptor;
  let nextRequestId = 1;

  const toView = (message: unknown) => {
    opts.post({ kind: 'to_view', message });
  };
  const notify = (method: string, params: Record<string, unknown>) => {
    toView({ jsonrpc: '2.0', method, params });
  };
  const reply = (id: number | string, result: unknown) => {
    toView({ jsonrpc: '2.0', id, result });
  };
  const replyError = (id: number | string, message: string) => {
    toView({ jsonrpc: '2.0', id, error: { code: -32000, message } });
  };

  /**
   * The two notifications that carry the turn. They go out right after
   * ui/initialize resolves — the point the spec names, and the point a view's
   * handlers are guaranteed to be registered (they are registered BEFORE the
   * handshake precisely so this is safe).
   */
  const sendTurn = () => {
    notify('ui/notifications/tool-input', {
      arguments: descriptor.arguments ?? {},
    });
    notify('ui/notifications/tool-result', {
      structuredContent: descriptor.structured_content ?? null,
    });
  };

  /**
   * Every request is answered — including one whose handler throws.
   *
   * A view's request is a promise on the other side of a postMessage; an
   * unanswered one never rejects and never resolves, so the view hangs with no
   * error to show. Measured 2026-08-17: a host callback threw ("Composer is not
   * available") and the view's send button simply stopped responding forever.
   */
  const handleRequest = async (
    id: number | string,
    method: string,
    params: Record<string, unknown>,
  ) => {
    try {
      await dispatchRequest(id, method, params);
    } catch (err) {
      replyError(id, err instanceof Error ? err.message : String(err));
    }
  };

  const dispatchRequest = async (
    id: number | string,
    method: string,
    params: Record<string, unknown>,
  ) => {
    switch (method) {
      case 'ui/initialize':
        reply(id, {
          protocolVersion: PROTOCOL_VERSION,
          hostInfo: { name: 'aura-cockpit', version: '1.0.0' },
          hostContext: opts.hostContext(),
        });
        sendTurn();
        return;
      case 'tools/call': {
        const tool = typeof params.name === 'string' ? params.name : '';
        const args = (params.arguments ?? {}) as Record<string, unknown>;
        try {
          // The view reads result.structuredContent (that is what the official
          // ext-apps client and the spec's own sample both do), so the host must
          // answer in that shape rather than in its own.
          reply(id, { structuredContent: await opts.callTool(tool, args) });
        } catch (err) {
          replyError(id, err instanceof Error ? err.message : String(err));
        }
        return;
      }
      case 'ui/message': {
        const text = readMessageText(params);
        if (text) opts.onSendToChat?.(text);
        reply(id, {});
        return;
      }
      case 'ui/open-link': {
        const url = safeExternalURL(params.url);
        if (!url) {
          replyError(id, 'only http(s) links can be opened');
          return;
        }
        opts.onOpenLink?.(url);
        reply(id, {});
        return;
      }
      case 'ui/request-display-mode': {
        const mode = typeof params.mode === 'string' ? params.mode : 'inline';
        opts.onDisplayMode?.(mode);
        reply(id, { mode });
        return;
      }
      default:
        // An unimplemented request is answered, never dropped: a view that waits
        // forever on a method this host does not have would simply hang.
        replyError(id, `unsupported method ${method}`);
    }
  };

  const onEnvelope = (envelope: SandboxEnvelope) => {
    if (envelope.kind === 'ready') {
      opts.post({ kind: 'mount', html: opts.html });
      return;
    }
    if (envelope.kind !== 'from_view') return;
    const message = envelope.message;
    if (message?.jsonrpc !== '2.0') return;
    const params = message.params ?? {};
    if (typeof message.method !== 'string') return; // a response to us; nothing pending
    if (message.id !== undefined) {
      void handleRequest(message.id, message.method, params);
      return;
    }
    if (message.method === 'notifications/message') {
      opts.onLog?.(typeof params.level === 'string' ? params.level : 'info', params.data);
    }
  };

  return {
    onEnvelope,
    teardown: () => {
      toView({
        jsonrpc: '2.0',
        id: `host-${String(nextRequestId++)}`,
        method: 'ui/resource-teardown',
        params: {},
      });
    },
    pushResult: (next: McpViewDescriptor) => {
      descriptor = next;
      sendTurn();
    },
  };
}

/** Read the text out of a ui/message payload, tolerating both shapes in the wild. */
function readMessageText(params: Record<string, unknown>): string {
  const content = params.content;
  if (typeof content === 'string') return content;
  if (typeof content === 'object' && content !== null) {
    const text = (content as { text?: unknown }).text;
    if (typeof text === 'string') return text;
  }
  return '';
}

/**
 * A view can ask the host to open any string. Only http(s) survives — a
 * javascript:, data: or file: URL handed to window.open would run in the
 * COCKPIT's origin, which is the one thing the whole sandbox exists to prevent.
 */
export function safeExternalURL(value: unknown): string | null {
  if (typeof value !== 'string') return null;
  try {
    const url = new URL(value);
    return url.protocol === 'https:' || url.protocol === 'http:' ? url.href : null;
  } catch {
    return null;
  }
}
