import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { DocumentDisplay } from '../DocumentDisplay';
import type { DisplayPayload, DisplaySource } from '../types';

// DocumentDisplay (D-10 / D-04): renders web_fetch markdown through the sanitized
// shared pipeline, with inline citations spliced at the claim position and images
// rendered (elysia's prose-img:hidden dropped). Copy-text copies the raw markdown.

function source(index: number, refId: string, cited = true): DisplaySource {
  return {
    ref_id: refId,
    index,
    type: 'document',
    title: `Reference ${String(index)}`,
    snippet: `Snippet ${String(index)}`,
    url: `https://example.com/${refId}`,
    cited,
  };
}

function payload(over: Partial<DisplayPayload> = {}): DisplayPayload {
  return {
    type: 'document',
    tool_call_id: 'call-doc',
    document: { content_md: '# Title\n\nBody text.', title: 'Fetched page' },
    ...over,
  };
}

describe('DocumentDisplay (D-10 / D-04)', () => {
  it('renders the sanitized markdown body', async () => {
    render(<DocumentDisplay payload={payload()} />);
    await waitFor(() => {
      expect(screen.getByText('Body text.')).toBeTruthy();
    });
    // The display label + the document title.
    expect(screen.getByText('Document')).toBeTruthy();
    expect(screen.getByText('Fetched page')).toBeTruthy();
  });

  it('STRIPS a <script> tag — sanitize chokepoint intact (T-26-17)', async () => {
    const malicious = 'Safe text.\n\n<script>window.__pwned=1</script>';
    const { container } = render(
      <DocumentDisplay payload={payload({ document: { content_md: malicious } })} />,
    );
    await waitFor(() => {
      expect(screen.getByText('Safe text.')).toBeTruthy();
    });
    expect(container.querySelector('script')).toBeNull();
    expect((window as unknown as { __pwned?: number }).__pwned).toBeUndefined();
  });

  it('splices an INLINE citation chip at the claim position (not end-appended)', async () => {
    const md = 'The earth is round [1] per the source.';
    render(
      <DocumentDisplay
        payload={payload({
          document: { content_md: md },
          sources: [source(1, 'ref-earth')],
        })}
      />,
    );
    // The chip is a button labelled by its source — proves rehypeCitations + the
    // span renderer produced a live CitationBubble.
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Source 1: Reference 1' })).toBeTruthy();
    });
    // The surrounding prose remains; the raw `[1]` marker is gone (replaced by the chip).
    expect(screen.getByText(/The earth is round/)).toBeTruthy();
  });

  it('renders an image (drops elysia prose-img:hidden, D-04 fix 2)', async () => {
    const md = '![A chart](https://example.com/chart.png)';
    const { container } = render(
      <DocumentDisplay payload={payload({ document: { content_md: md } })} />,
    );
    await waitFor(() => {
      const img = container.querySelector('img');
      expect(img).not.toBeNull();
      expect(img?.getAttribute('src')).toBe('https://example.com/chart.png');
      expect(img?.getAttribute('alt')).toBe('A chart');
      expect(img?.getAttribute('loading')).toBe('lazy');
      expect(img?.getAttribute('referrerpolicy')).toBe('no-referrer');
    });
  });

  it('an unknown `[n]` (no registry entry) renders no live chip (T-26-18)', async () => {
    const md = 'Unsupported [9] claim.';
    render(
      <DocumentDisplay
        payload={payload({ document: { content_md: md }, sources: [source(1, 'a')] })}
      />,
    );
    await waitFor(() => {
      expect(screen.getByText(/Unsupported/)).toBeTruthy();
    });
    expect(screen.queryByRole('button', { name: /Source 9/ })).toBeNull();
  });

  it('copies the raw markdown via Copy text', () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    const md = '# Doc\n\nContent.';
    render(<DocumentDisplay payload={payload({ document: { content_md: md } })} />);
    fireEvent.click(screen.getByRole('button', { name: 'Copy document text' }));
    expect(writeText).toHaveBeenCalledWith(md);
    vi.unstubAllGlobals();
  });

  it('shows the empty state when the document has no content', () => {
    render(<DocumentDisplay payload={payload({ document: { content_md: '' } })} />);
    expect(screen.getByText('No content')).toBeTruthy();
    expect(screen.getByText('This document has no readable content.')).toBeTruthy();
  });
});
