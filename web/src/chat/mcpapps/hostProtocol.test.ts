import { describe, expect, it, vi } from 'vitest';
import {
  createHostBridge,
  isMcpViewDescriptor,
  safeExternalURL,
  type McpViewDescriptor,
} from './hostProtocol';

const descriptor: McpViewDescriptor = {
  server: 'whatsapp',
  resource_uri: 'ui://whatsapp/thread.html',
  tool_call_id: 'tc1',
  tool_name: 'whatsapp__list_messages',
  arguments: { chat_jid: '39@s.whatsapp.net', page: 0 },
  structured_content: [{ id: 1, text: 'ciao' }],
};

function harness(overrides: Partial<Parameters<typeof createHostBridge>[0]> = {}) {
  const sent: unknown[] = [];
  const bridge = createHostBridge({
    descriptor,
    html: '<html>view</html>',
    post: (envelope) => sent.push(envelope),
    callTool: async () => ({ rows: [] }),
    hostContext: () => ({ theme: 'dark' }),
    ...overrides,
  });
  const toView = () =>
    sent
      .filter((e): e is { kind: string; message: Record<string, unknown> } =>
        typeof e === 'object' && e !== null && (e as { kind?: string }).kind === 'to_view',
      )
      .map((e) => e.message);
  return { bridge, sent, toView };
}

describe('the relay handshake', () => {
  it('hands the document over only once the relay says it is ready', () => {
    const { bridge, sent } = harness();
    expect(sent).toHaveLength(0);
    bridge.onEnvelope({ kind: 'ready' });
    expect(sent[0]).toEqual({ kind: 'mount', html: '<html>view</html>' });
  });

  it('ignores an envelope that is not the relay protocol', () => {
    const { bridge, sent } = harness();
    bridge.onEnvelope({ kind: 'mounted' });
    bridge.onEnvelope({ kind: 'from_view', message: { method: 'ui/initialize', id: 1 } });
    expect(sent).toHaveLength(0);
  });
});

describe('ui/initialize', () => {
  it('answers, then pushes the turn the view was rendered for', async () => {
    const { bridge, toView } = harness();
    bridge.onEnvelope({
      kind: 'from_view',
      message: { jsonrpc: '2.0', id: 1, method: 'ui/initialize', params: {} },
    });
    await Promise.resolve();

    const messages = toView();
    expect(messages).toHaveLength(3);
    const [init, input, result] = messages as [
      (typeof messages)[number],
      (typeof messages)[number],
      (typeof messages)[number],
    ];
    expect((init.result as { hostContext: unknown }).hostContext).toEqual({ theme: 'dark' });
    expect(input.method).toBe('ui/notifications/tool-input');
    expect(input.params).toEqual({ arguments: descriptor.arguments });
    // The view reads result.structuredContent — the shape the official ext-apps
    // client and the spec's own sample both key on.
    expect(result.method).toBe('ui/notifications/tool-result');
    expect(result.params).toEqual({ structuredContent: descriptor.structured_content });
  });
});

describe('tools/call', () => {
  it('answers in the structuredContent shape the view reads', async () => {
    const callTool = vi.fn().mockResolvedValue({ rows: [1, 2] });
    const { bridge, toView } = harness({ callTool });
    bridge.onEnvelope({
      kind: 'from_view',
      message: { jsonrpc: '2.0', id: 7, method: 'tools/call', params: { name: 'list_messages', arguments: { page: 1 } } },
    });
    await vi.waitFor(() => expect(toView()).toHaveLength(1));

    expect(callTool).toHaveBeenCalledWith('list_messages', { page: 1 });
    expect(toView()[0]).toMatchObject({ id: 7, result: { structuredContent: { rows: [1, 2] } } });
  });

  it('turns a host refusal into a JSON-RPC error, never a hang', async () => {
    const callTool = vi.fn().mockRejectedValue(new Error('refused'));
    const { bridge, toView } = harness({ callTool });
    bridge.onEnvelope({
      kind: 'from_view',
      message: { jsonrpc: '2.0', id: 8, method: 'tools/call', params: { name: 'send_message' } },
    });
    await vi.waitFor(() => expect(toView()).toHaveLength(1));
    expect(toView()[0]).toMatchObject({ id: 8, error: { message: 'refused' } });
  });
});

describe('what a view may ask the host to do', () => {
  it('drafts into the composer instead of sending', async () => {
    const onSendToChat = vi.fn();
    const { bridge } = harness({ onSendToChat });
    bridge.onEnvelope({
      kind: 'from_view',
      message: {
        jsonrpc: '2.0',
        id: 2,
        method: 'ui/message',
        params: { role: 'user', content: { type: 'text', text: 'riassumi questa chat' } },
      },
    });
    await Promise.resolve();
    expect(onSendToChat).toHaveBeenCalledWith('riassumi questa chat');
  });

  it('opens only http(s) links', async () => {
    const onOpenLink = vi.fn();
    const { bridge, toView } = harness({ onOpenLink });
    for (const url of ['javascript:alert(1)', 'data:text/html,<b>', 'file:///etc/passwd', 42]) {
      bridge.onEnvelope({
        kind: 'from_view',
        message: { jsonrpc: '2.0', id: 3, method: 'ui/open-link', params: { url } },
      });
    }
    await Promise.resolve();
    expect(onOpenLink).not.toHaveBeenCalled();
    // Refused, but ANSWERED: an unanswered request leaves the view waiting.
    expect(toView().every((m) => 'error' in m)).toBe(true);

    bridge.onEnvelope({
      kind: 'from_view',
      message: { jsonrpc: '2.0', id: 4, method: 'ui/open-link', params: { url: 'https://ok.test/x' } },
    });
    await Promise.resolve();
    expect(onOpenLink).toHaveBeenCalledWith('https://ok.test/x');
  });

  it('answers a method it does not implement rather than dropping it', async () => {
    const { bridge, toView } = harness();
    bridge.onEnvelope({
      kind: 'from_view',
      message: { jsonrpc: '2.0', id: 9, method: 'ui/does-not-exist', params: {} },
    });
    await Promise.resolve();
    expect(toView()[0]).toMatchObject({ id: 9, error: { message: expect.stringContaining('unsupported') } });
  });

  it('routes a display-mode request and echoes the granted mode', async () => {
    const onDisplayMode = vi.fn();
    const { bridge, toView } = harness({ onDisplayMode });
    bridge.onEnvelope({
      kind: 'from_view',
      message: { jsonrpc: '2.0', id: 5, method: 'ui/request-display-mode', params: { mode: 'fullscreen' } },
    });
    await Promise.resolve();
    expect(onDisplayMode).toHaveBeenCalledWith('fullscreen');
    expect(toView()[0]).toMatchObject({ id: 5, result: { mode: 'fullscreen' } });
  });
});

describe('teardown', () => {
  it('is a REQUEST, because the view has to be able to answer it', () => {
    const { bridge, toView } = harness();
    bridge.teardown();
    const messages = toView();
    expect(messages).toHaveLength(1);
    const message = messages[0] as (typeof messages)[number];
    expect(message.method).toBe('ui/resource-teardown');
    expect(message.id).toBeDefined();
  });
});

describe('safeExternalURL', () => {
  it.each([
    ['https://ok.test/a', 'https://ok.test/a'],
    ['http://ok.test/a', 'http://ok.test/a'],
    ['javascript:alert(1)', null],
    ['data:text/html,<b>', null],
    ['file:///etc/passwd', null],
    ['not a url', null],
    ['', null],
  ])('%s', (input, expected) => {
    expect(safeExternalURL(input)).toBe(expected);
  });
});

describe('isMcpViewDescriptor', () => {
  it('needs the correlation key, or the card would attach to the wrong call', () => {
    expect(isMcpViewDescriptor(descriptor)).toBe(true);
    expect(isMcpViewDescriptor({ server: 'x', resource_uri: 'ui://x/a.html' })).toBe(false);
    expect(isMcpViewDescriptor({ resource_uri: 'ui://x/a.html', tool_call_id: 't' })).toBe(false);
    expect(isMcpViewDescriptor(null)).toBe(false);
    expect(isMcpViewDescriptor('nope')).toBe(false);
  });
});
