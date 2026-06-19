import { describe, expect, it, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { CodeDisplay } from '../CodeDisplay';
import { highlightCode } from '../shiki';
import type { DisplayPayload } from '../types';

// CodeDisplay (DISP-02 / D-10 / HARDEN-08): renders the body in mono, shows a plain
// escaped <pre> before the lazy Shiki chunk resolves, upgrades to escaped-span HTML
// (never executes — T-26-16), copies the raw body, and collapses a long body.
//
// The shiki loader is mocked (vi.mock is hoisted) so the test controls the async
// resolve deterministically — the real module is dynamic-import-only, exercised by
// the build code-split + the resolveLang unit test.
vi.mock('../shiki', () => ({
  // The real loader returns null for an unsupported lang and escaped HTML otherwise.
  highlightCode: vi.fn(),
  resolveLang: (lang: string | undefined) => (lang === 'javascript' ? 'javascript' : null),
}));

const mockHighlight = vi.mocked(highlightCode);

function payload(body: string, lang?: string): DisplayPayload {
  return {
    type: 'code',
    tool_call_id: 'call-code',
    code: { body, ...(lang !== undefined ? { lang } : {}) },
  };
}

beforeEach(() => {
  mockHighlight.mockReset();
});

describe('CodeDisplay (DISP-02 / D-10 / HARDEN-08)', () => {
  it('renders a PLAIN escaped <pre> immediately (before/without the lazy chunk)', () => {
    // Never resolves → the plain fallback is what the operator sees first (A2).
    mockHighlight.mockReturnValue(new Promise(() => undefined));
    render(<CodeDisplay payload={payload('const x = 1;', 'javascript')} />);
    const pre = screen.getByTestId('code-plain');
    expect(pre.tagName.toLowerCase()).toBe('pre');
    expect(pre.textContent).toBe('const x = 1;');
  });

  it('a <script> in the body renders as ESCAPED text, never executes (T-26-16)', () => {
    mockHighlight.mockReturnValue(new Promise(() => undefined));
    const evil = '<script>window.__codePwned = 1</script>';
    const { container } = render(<CodeDisplay payload={payload(evil)} />);
    const pre = screen.getByTestId('code-plain');
    // The markup is text inside the <pre>, not parsed into a live <script> element.
    expect(pre.textContent).toBe(evil);
    expect(container.querySelector('script')).toBeNull();
    expect((window as unknown as { __codePwned?: number }).__codePwned).toBeUndefined();
  });

  it('upgrades to the lazy escaped-span Shiki HTML once the chunk resolves', async () => {
    // Shiki escapes the code content; here the resolved HTML is escaped spans.
    mockHighlight.mockResolvedValue(
      '<pre class="shiki"><code><span>const</span> x = 1;</code></pre>',
    );
    render(<CodeDisplay payload={payload('const x = 1;', 'javascript')} />);
    await waitFor(() => {
      expect(screen.getByTestId('code-highlighted')).toBeTruthy();
    });
    // The plain fallback is replaced by the highlighted view.
    expect(screen.queryByTestId('code-plain')).toBeNull();
  });

  it('keeps the plain fallback when the lang is unsupported (highlight returns null)', async () => {
    mockHighlight.mockResolvedValue(null);
    render(<CodeDisplay payload={payload('plain body', 'brainfuck')} />);
    // Give the resolved-null promise a tick; the plain <pre> stays.
    await waitFor(() => {
      expect(mockHighlight).toHaveBeenCalled();
    });
    expect(screen.getByTestId('code-plain')).toBeTruthy();
    expect(screen.queryByTestId('code-highlighted')).toBeNull();
  });

  it('calls the lazy highlighter (dynamic import) rather than highlighting inline (A2)', () => {
    mockHighlight.mockReturnValue(new Promise(() => undefined));
    render(<CodeDisplay payload={payload('const x = 1;', 'javascript')} />);
    expect(mockHighlight).toHaveBeenCalledWith('const x = 1;', 'javascript', expect.any(Boolean));
  });

  it('copies the raw code body via Copy code', () => {
    mockHighlight.mockReturnValue(new Promise(() => undefined));
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    const body = 'echo hello';
    render(<CodeDisplay payload={payload(body, 'bash')} />);
    fireEvent.click(screen.getByRole('button', { name: 'Copy code' }));
    expect(writeText).toHaveBeenCalledWith(body);
    vi.unstubAllGlobals();
  });

  it('collapses a long body and expands on toggle', () => {
    mockHighlight.mockReturnValue(new Promise(() => undefined));
    const longBody = Array.from({ length: 40 }, (_, i) => `line ${String(i)}`).join('\n');
    render(<CodeDisplay payload={payload(longBody, 'javascript')} />);
    const toggle = screen.getByRole('button', { name: 'Show the full code body' });
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    fireEvent.click(toggle);
    expect(
      screen.getByRole('button', { name: 'Collapse the code body' }).getAttribute('aria-expanded'),
    ).toBe('true');
  });

  it('does NOT show a collapse toggle for a short body', () => {
    mockHighlight.mockReturnValue(new Promise(() => undefined));
    render(<CodeDisplay payload={payload('short', 'javascript')} />);
    expect(screen.queryByRole('button', { name: 'Show the full code body' })).toBeNull();
  });

  it('shows the empty state for an empty body', () => {
    render(<CodeDisplay payload={payload('')} />);
    expect(screen.getByText('No content')).toBeTruthy();
  });
});
