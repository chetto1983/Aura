import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { DisplayRouter } from '../DisplayRouter';
import type { DisplayPayload } from '../types';

// DisplayRouter routes a trusted-normalizer payload to its per-type card; the
// SECURITY-critical contract is the `default:` — an unknown/foreign type must
// degrade to the escaped raw ToolActivityCard (never null, never a rich render).

// A payload with an unknown discriminant (e.g. a future/foreign type the cockpit
// doesn't know) — still degrades to the raw card, never thrown away.
const unknownType = {
  type: 'totally_unknown_type',
  tool_call_id: 'call-2',
} as unknown as DisplayPayload;

describe('DisplayRouter (DISP-02 / D-FALLBACK)', () => {
  it('routes a web_result payload to the rich WebResultDisplay (26-05 wired)', () => {
    const webResult: DisplayPayload = {
      type: 'web_result',
      tool_call_id: 'call-1',
      web_results: [{ title: 'Forecast', url: 'https://example.com', snippet: 'sunny' }],
    };
    render(<DisplayRouter payload={webResult} toolName="web_search" result="RAW BLOB" />);
    // The rich card renders (the snippet + the typed label), NOT the raw blob.
    expect(screen.getByText('sunny')).toBeTruthy();
    expect(screen.getByText('Web results')).toBeTruthy();
  });

  it('renders the raw card for an unknown payload type (default case, never null)', () => {
    const { container } = render(
      <DisplayRouter payload={unknownType} toolName="mystery_tool" result="OUTPUT" />,
    );
    expect(container.firstChild).not.toBeNull();
    expect(screen.getByText('mystery_tool')).toBeTruthy();
  });

  it('renders untrusted result as ESCAPED text, never markdown/HTML (HARDEN-08 / T-26-05)', () => {
    const payload = '<img src=x onerror="alert(1)"><b>bold</b>';
    render(<DisplayRouter payload={unknownType} toolName="evil_tool" result={payload} />);
    // Expand the raw blob via the card's expander.
    fireEvent.click(screen.getByRole('button', { name: 'Show raw result' }));
    const pre = screen.getByText(payload);
    // The markup is present as TEXT (escaped) inside a <pre>, not parsed into DOM.
    expect(pre.tagName.toLowerCase()).toBe('pre');
    expect(pre.textContent).toBe(payload);
    expect(pre.querySelector('img')).toBeNull();
    expect(pre.querySelector('b')).toBeNull();
  });

  it('passes argsText + isError through to the raw card', () => {
    render(
      <DisplayRouter
        payload={unknownType}
        toolName="failing_tool"
        argsText='{"q":"x"}'
        result="boom"
        isError
      />,
    );
    expect(screen.getByText('failing_tool')).toBeTruthy();
    expect(screen.getByText('Error')).toBeTruthy();
  });

  it('renders a running raw card when no result has arrived (D-15 progressive swap)', () => {
    render(<DisplayRouter payload={unknownType} toolName="slow_tool" argsText="{}" />);
    expect(screen.getByText('Running')).toBeTruthy();
  });

  // Each per-type case routes to its typed card (the per-card behavior is covered in
  // that card's own test; here we pin the ROUTING — the switch reaches each branch).
  it.each([
    [{ type: 'table', tool_call_id: 't', table: { columns: ['A'], rows: [['1']] } }, 'Table'],
    [{ type: 'chart', tool_call_id: 't', chart: { x_labels: ['a'], y_values: [1] } }, 'Chart'],
    [
      {
        type: 'system_event',
        tool_call_id: 't',
        system: { class: 'web_error', reason: 'timeout', severity: 'warning' },
      },
      'System',
    ],
    [
      {
        type: 'swarm_report',
        tool_call_id: 't',
        swarm: [{ goal_index: 0, child_id: 'c', status: 'ok' }],
      },
      'Workers',
    ],
    [
      {
        type: 'local_artifact',
        tool_call_id: 't',
        artifact: { filename: 'f.txt', size_bytes: 10 },
      },
      'Artifact',
    ],
    [{ type: 'document', tool_call_id: 't', document: { content_md: 'hello doc' } }, 'Document'],
    [{ type: 'code', tool_call_id: 't', code: { body: 'print(1)', lang: 'python' } }, 'Code'],
  ] as [DisplayPayload, string][])(
    'routes the %s payload to its typed card label "%s"',
    (payload, label) => {
      render(<DisplayRouter payload={payload} toolName="tool" result="RAW" />);
      // The typed label appears (some cards also echo it in an sr-only caption).
      expect(screen.getAllByText(label).length).toBeGreaterThan(0);
    },
  );

  it('forwards onOpenSource to the document/web_result evidence cards (26-06)', () => {
    const onOpenSource = vi.fn();
    const doc: DisplayPayload = {
      type: 'document',
      tool_call_id: 'call-doc',
      document: { content_md: 'A claim [1].' },
      sources: [
        {
          ref_id: 'src-1',
          index: 1,
          type: 'document',
          title: 'Cited',
          url: 'https://x.test',
          cited: true,
        },
      ],
    };
    render(<DisplayRouter payload={doc} toolName="web_fetch" onOpenSource={onOpenSource} />);
    // The inline citation chip is rendered by DocumentDisplay; clicking it fires the
    // forwarded callback (the DisplayRouter threaded onOpenSource through).
    fireEvent.click(screen.getByRole('button', { name: /Source 1/ }));
    expect(onOpenSource).toHaveBeenCalledWith('src-1');
  });
});
