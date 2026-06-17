import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { ToolActivityCard } from '../ToolActivityCard';
import { toolStatus } from '../toolStatus';

describe('ToolActivityCard (D-02 — raw view only)', () => {
  it('renders the tool name + a running status when no result yet', () => {
    render(<ToolActivityCard toolName="web_search" argsText='{"query":"meteo"}' />);
    expect(screen.getByText('web_search')).toBeTruthy();
    expect(screen.getByText('Running')).toBeTruthy();
  });

  it('shows a done status once a result is present', () => {
    render(<ToolActivityCard toolName="web_search" result="the answer" />);
    expect(screen.getByText('Done')).toBeTruthy();
  });

  it('shows an error status when isError is set', () => {
    render(<ToolActivityCard toolName="web_search" result="boom" isError />);
    expect(screen.getByText('Error')).toBeTruthy();
  });

  it('the expander reveals the raw mono result and toggles aria-expanded', () => {
    render(<ToolActivityCard toolName="web_search" result="RAW RESULT BLOB" />);
    // Collapsed by default — the raw blob is not yet in the DOM.
    expect(screen.queryByText('RAW RESULT BLOB')).toBeNull();
    const expander = screen.getByRole('button', { name: 'Show raw result' });
    expect(expander.getAttribute('aria-expanded')).toBe('false');

    fireEvent.click(expander);
    expect(expander.getAttribute('aria-expanded')).toBe('true');
    const raw = screen.getByText('RAW RESULT BLOB');
    expect(raw).toBeTruthy();
    // It renders as a <pre> mono block — text only.
    expect(raw.tagName.toLowerCase()).toBe('pre');
    expect(raw.className).toContain('font-mono');

    fireEvent.click(screen.getByRole('button', { name: 'Hide raw result' }));
    expect(screen.queryByText('RAW RESULT BLOB')).toBeNull();
  });

  it('renders untrusted tool output as TEXT, never as HTML (XSS guard)', () => {
    const payload = '<img src=x onerror="alert(1)"><b>bold</b>';
    render(<ToolActivityCard toolName="evil_tool" result={payload} />);
    fireEvent.click(screen.getByRole('button', { name: 'Show raw result' }));
    const pre = screen.getByText(payload);
    // The literal markup is present as TEXT content (escaped), not parsed: no
    // <img>/<b> elements were injected into the DOM.
    expect(pre.textContent).toBe(payload);
    expect(pre.querySelector('img')).toBeNull();
    expect(pre.querySelector('b')).toBeNull();
  });

  it('does not render an expander when there is no raw content', () => {
    render(<ToolActivityCard toolName="noop_tool" />);
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('toolStatus derives the status from result/isError', () => {
    expect(toolStatus({})).toBe('running');
    expect(toolStatus({ result: 'x' })).toBe('done');
    expect(toolStatus({ result: 'x', isError: true })).toBe('error');
  });
});
