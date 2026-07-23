import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import '../../i18n/i18n';
import { highlightCode } from '../displays/shiki';
import { ToolResultPanel } from '../ToolResultPanel';

// ToolResultPanel (compact-chat spec §3.5): Request/Result sections, JSON
// pretty-print + lazy-Shiki highlight with the plain-escaped degrade, the copy
// affordance, the max-h-80 scroll cap, and the streaming (Request-only) state.

vi.mock('../displays/shiki', () => ({
  highlightCode: vi.fn(),
}));
const mockHighlight = vi.mocked(highlightCode);

afterEach(() => {
  vi.clearAllMocks();
});

describe('ToolResultPanel (§3.5)', () => {
  it('streaming state: renders the Request section only while no result exists', () => {
    mockHighlight.mockResolvedValue(null);
    render(<ToolResultPanel argsText='{"query":"meteo"}' />);
    expect(screen.getByText('Request')).toBeTruthy();
    expect(screen.queryByText('Result')).toBeNull();
    expect(screen.queryByTestId('tool-copy')).toBeNull();
  });

  it('pretty-prints parseable JSON in both sections and caps the container', () => {
    mockHighlight.mockResolvedValue(null);
    const { container } = render(
      <ToolResultPanel argsText='{"query":"meteo"}' result='{"answer":42}' />,
    );
    // 2-space pretty print (multi-line output).
    expect(screen.getByText(/"query": "meteo"/)).toBeTruthy();
    expect(screen.getByText(/"answer": 42/)).toBeTruthy();
    expect(container.querySelector('.max-h-80.overflow-y-auto')).not.toBeNull();
  });

  it('upgrades a JSON result to the Shiki escaped-span HTML when the chunk resolves', async () => {
    mockHighlight.mockResolvedValue('<pre class="shiki"><code><span>json</span></code></pre>');
    render(<ToolResultPanel result='{"a":1}' />);
    await waitFor(() => {
      expect(screen.getByTestId('tool-result-highlighted')).toBeTruthy();
    });
    expect(mockHighlight).toHaveBeenCalledWith(JSON.stringify({ a: 1 }, null, 2), 'json', true);
  });

  it('keeps the plain escaped <pre> for non-JSON results — XSS-inert', () => {
    mockHighlight.mockResolvedValue(null);
    const payload = '<script>alert(1)</script> plain text';
    render(<ToolResultPanel result={payload} />);
    const pre = screen.getByText(payload);
    expect(pre.tagName.toLowerCase()).toBe('pre');
    expect(pre.querySelector('script')).toBeNull();
    expect(mockHighlight).not.toHaveBeenCalled(); // non-JSON never highlights
  });

  it('keeps the plain fallback when the highlighter rejects', async () => {
    mockHighlight.mockRejectedValue(new Error('no chunk'));
    render(<ToolResultPanel result='{"a":1}' />);
    await waitFor(() => {
      expect(mockHighlight).toHaveBeenCalled();
    });
    expect(screen.queryByTestId('tool-result-highlighted')).toBeNull();
    expect(screen.getByText(/"a": 1/)).toBeTruthy();
  });

  it('copies the RAW untruncated result string (not the pretty-print)', async () => {
    mockHighlight.mockResolvedValue(null);
    const writeText = vi.fn(() => Promise.resolve());
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    try {
      render(<ToolResultPanel result='{"a":1}' />);
      fireEvent.click(screen.getByTestId('tool-copy'));
      await waitFor(() => {
        expect(writeText).toHaveBeenCalledWith('{"a":1}');
      });
      // Confirmation label swap (the shared useCopyAction pattern).
      await screen.findByText('Copied');
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it('renders nothing at all for an empty request with no result', () => {
    mockHighlight.mockResolvedValue(null);
    render(<ToolResultPanel argsText="" />);
    expect(screen.queryByText('Request')).toBeNull();
    expect(screen.queryByText('Result')).toBeNull();
  });
});
