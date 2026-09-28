import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { DisplayRouter } from '../DisplayRouter';
import type { DisplayDiff, DisplayPayload } from '../types';

// DisplayRouter routes a trusted-normalizer payload to its per-type card; the
// SECURITY-critical contract is the `default:` — an unknown/foreign type must
// degrade to the escaped ToolResultPanel (never null, never a rich render).
// The router is now hosted INSIDE the compact tool row's expanded body
// (compact-chat spec §3.5), so the default degrade is the panel, not a card.

// A payload with an unknown discriminant (e.g. a future/foreign type the cockpit
// doesn't know) — still degrades to the raw panel, never thrown away.
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

  it('renders the raw panel for an unknown payload type (default case, never null)', () => {
    const { container } = render(
      <DisplayRouter payload={unknownType} toolName="mystery_tool" result="OUTPUT" />,
    );
    expect(container.firstChild).not.toBeNull();
    expect(screen.getByText('Result')).toBeTruthy();
    expect(screen.getByText('OUTPUT')).toBeTruthy();
  });

  it('renders escaped raw text when a recognized payload lacks its required slot', () => {
    const malformed = { type: 'table', tool_call_id: 'missing-table' } as DisplayPayload;
    const raw = '<b>untrusted</b>';
    render(<DisplayRouter payload={malformed} toolName="search_files" result={raw} />);
    const pre = screen.getByText(raw);
    expect(pre.tagName.toLowerCase()).toBe('pre');
    expect(pre.querySelector('b')).toBeNull();
  });

  it('keeps malformed table fields in the raw panel', () => {
    const malformed = {
      type: 'table',
      tool_call_id: 'bad-table',
      table: { columns: 'not columns', rows: [] },
    } as unknown as DisplayPayload;
    render(<DisplayRouter payload={malformed} toolName="search_files" result="raw table" />);
    expect(screen.getByText('raw table').tagName.toLowerCase()).toBe('pre');
  });

  it('labels a verified native list table and keeps a mismatched row raw', () => {
    const payload: DisplayPayload = {
      type: 'table',
      title: 'native_tasks',
      tool_call_id: 'task-1',
      table: {
        columns: ['Task', 'Kind', 'Schedule', 'Next', 'State'],
        rows: [['task-1', 'reminder', 'every', '2026-10-01T09:00:00Z', 'active']],
      },
    };
    const { rerender } = render(
      <DisplayRouter payload={payload} toolName="task" result="raw task list" />,
    );
    expect(screen.getAllByText('Scheduled tasks').length).toBeGreaterThan(0);
    expect(screen.getAllByText('task-1').length).toBeGreaterThan(0);
    rerender(
      <DisplayRouter
        payload={{ ...payload, table: { columns: ['Task'], rows: [['forged']] } }}
        toolName="task"
        result="raw task list"
      />,
    );
    expect(screen.getByText('raw task list').tagName.toLowerCase()).toBe('pre');
  });

  it('routes a trusted todo payload to its checklist', () => {
    const payload = {
      type: 'todo',
      tool_call_id: 'todo-1',
      todo: { items: [{ content: 'Build', status: 'in_progress', active_form: 'Building' }] },
    } as DisplayPayload;
    render(<DisplayRouter payload={payload} toolName="todo_write" result="[~] Build" />);
    expect(screen.getByText('Build')).toBeTruthy();
    expect(screen.getByText('Building')).toBeTruthy();
  });

  it('keeps malformed todo payloads on the raw path', () => {
    const payload = { type: 'todo', tool_call_id: 'todo-2' } as DisplayPayload;
    render(<DisplayRouter payload={payload} toolName="todo_write" result="[~] Build" />);
    expect(screen.getByText('[~] Build').tagName.toLowerCase()).toBe('pre');
  });

  it('does not crash or mount a checklist for a malformed todo item', () => {
    const payload = {
      type: 'todo',
      tool_call_id: 'todo-3',
      todo: { items: [null] },
    } as unknown as DisplayPayload;
    render(<DisplayRouter payload={payload} toolName="todo_write" result="raw todo" />);
    expect(screen.getByText('raw todo').tagName.toLowerCase()).toBe('pre');
  });

  it('keeps oversized todo text in the raw panel', () => {
    const payload = {
      type: 'todo',
      tool_call_id: 'todo-4',
      todo: { items: [{ content: 'x'.repeat(513), status: 'pending' }] },
    } as DisplayPayload;
    render(<DisplayRouter payload={payload} toolName="todo_write" result="raw oversized todo" />);
    expect(screen.getByText('raw oversized todo').tagName.toLowerCase()).toBe('pre');
  });

  it('keeps a todo payload with two active items in the raw panel', () => {
    const payload = {
      type: 'todo',
      tool_call_id: 'todo-5',
      todo: {
        items: [
          { content: 'First', status: 'in_progress' },
          { content: 'Second', status: 'in_progress' },
        ],
      },
    } as DisplayPayload;
    render(<DisplayRouter payload={payload} toolName="todo_write" result="raw ambiguous todo" />);
    expect(screen.getByText('raw ambiguous todo').tagName.toLowerCase()).toBe('pre');
  });

  it('routes a completed terminal and keeps a missing exit code raw', () => {
    const complete = {
      type: 'terminal',
      tool_call_id: 'shell-1',
      terminal: { command: 'echo hello', output: 'hello', exit_code: 0 },
    } as DisplayPayload;
    const { rerender } = render(
      <DisplayRouter payload={complete} toolName="shell_exec" result="raw" />,
    );
    expect(screen.getByText('echo hello')).toBeTruthy();
    expect(screen.getByText('exit 0')).toBeTruthy();
    const malformed = {
      ...complete,
      terminal: { command: 'echo hello', output: 'hello' },
    } as DisplayPayload;
    rerender(<DisplayRouter payload={malformed} toolName="shell_exec" result="raw shell" />);
    expect(screen.getByText('raw shell').tagName.toLowerCase()).toBe('pre');
  });

  it('routes a verified diff and rejects inconsistent line counts', () => {
    const diff: DisplayDiff = {
      filename: 'a.txt',
      additions: 1,
      deletions: 1,
      lines: [
        { kind: 'removed', text: 'old' },
        { kind: 'added', text: 'new' },
      ],
    };
    const complete = { type: 'diff', tool_call_id: 'patch-1', diff } as DisplayPayload;
    const { rerender } = render(
      <DisplayRouter payload={complete} toolName="patch" result="raw patch" />,
    );
    expect(screen.getByText('a.txt')).toBeTruthy();
    rerender(
      <DisplayRouter
        payload={{ ...complete, diff: { ...diff, additions: 2 } }}
        toolName="patch"
        result="raw malformed patch"
      />,
    );
    expect(screen.getByText('raw malformed patch').tagName.toLowerCase()).toBe('pre');
  });

  it('keeps an oversized diff line in the raw panel', () => {
    const malformed = {
      type: 'diff',
      tool_call_id: 'patch-oversized',
      diff: {
        filename: 'a.txt',
        additions: 1,
        deletions: 0,
        lines: [{ kind: 'added', text: 'x'.repeat(65537) }],
      },
    } as DisplayPayload;
    render(<DisplayRouter payload={malformed} toolName="patch" result="raw long patch" />);
    expect(screen.getByText('raw long patch').tagName.toLowerCase()).toBe('pre');
  });

  it.each(['system_event', 'swarm_report', 'local_artifact', 'document', 'web_result', 'code'])(
    'keeps a %s payload with no data slot in the raw panel',
    (type) => {
      const malformed = { type, tool_call_id: 'missing-slot' } as DisplayPayload;
      render(<DisplayRouter payload={malformed} toolName="tool" result="untrusted value" />);
      expect(screen.getByText('untrusted value').tagName.toLowerCase()).toBe('pre');
    },
  );

  it('renders untrusted result as ESCAPED text, never markdown/HTML (HARDEN-08 / T-26-05)', () => {
    const payload = '<img src=x onerror="alert(1)"><b>bold</b>';
    render(<DisplayRouter payload={unknownType} toolName="evil_tool" result={payload} />);
    // The panel renders the (non-JSON) blob as a plain escaped <pre> directly.
    const pre = screen.getByText(payload);
    // The markup is present as TEXT (escaped) inside a <pre>, not parsed into DOM.
    expect(pre.tagName.toLowerCase()).toBe('pre');
    expect(pre.textContent).toBe(payload);
    expect(pre.querySelector('img')).toBeNull();
    expect(pre.querySelector('b')).toBeNull();
  });

  it('passes argsText + result through to the raw panel sections', () => {
    render(
      <DisplayRouter
        payload={unknownType}
        toolName="failing_tool"
        argsText='{"q":"x"}'
        result="boom"
        isError
      />,
    );
    expect(screen.getByText('Request')).toBeTruthy();
    expect(screen.getByText(/"q": "x"/)).toBeTruthy();
    expect(screen.getByText('boom')).toBeTruthy();
  });

  it('renders a Request-only panel when no result has arrived (D-15 progressive swap)', () => {
    render(<DisplayRouter payload={unknownType} toolName="slow_tool" argsText="{}" />);
    expect(screen.getByText('Request')).toBeTruthy();
    expect(screen.queryByText('Result')).toBeNull();
  });

  // Each per-type case routes to its typed card (the per-card behavior is covered in
  // that card's own test; here we pin the ROUTING — the switch reaches each branch).
  it.each([
    [{ type: 'table', tool_call_id: 't', table: { columns: ['A'], rows: [['1']] } }, 'Table'],
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
      'Agents',
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
