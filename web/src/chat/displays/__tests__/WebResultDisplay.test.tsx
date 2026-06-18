import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { WebResultDisplay } from '../WebResultDisplay';
import type { DisplayPayload, DisplaySource, WebItem } from '../types';

// WebResultDisplay (DISP-02 / D-09): renders rich web_result cards — domain chip +
// snippet + relevance meter (info, not accent) + published_at + citation bubble +
// thumbnails routed through the SSRF-safe image-proxy (NEVER a raw external <img>),
// paginated in-card (default 3/page). Threat: client-side SSRF via external img (T-26-15).

function item(over: Partial<WebItem> = {}): WebItem {
  return {
    title: 'Forecast for Rome',
    url: 'https://weather.example.com/rome',
    snippet: 'Sunny, high of 24C.',
    domain: 'weather.example.com',
    score: 0.82,
    published_at: '2026-06-17',
    thumbnail: 'https://cdn.example.com/rome.jpg',
    ref_id: 'src-rome',
    ...over,
  };
}

function payload(items: WebItem[], sources?: DisplaySource[]): DisplayPayload {
  return {
    type: 'web_result',
    tool_call_id: 'call-web',
    web_results: items,
    ...(sources !== undefined ? { sources } : {}),
  };
}

describe('WebResultDisplay (DISP-02 / D-09)', () => {
  it('renders the domain chip, snippet, relevance hint and published date', () => {
    render(<WebResultDisplay payload={payload([item()])} />);
    expect(screen.getByText('weather.example.com')).toBeTruthy();
    expect(screen.getByText('Sunny, high of 24C.')).toBeTruthy();
    expect(screen.getByText('Relevance 0.82')).toBeTruthy();
    expect(screen.getByText('Published 2026-06-17')).toBeTruthy();
  });

  it('routes the thumbnail through /api/image-proxy with referrerpolicy + lazy (T-26-15)', () => {
    const { container } = render(<WebResultDisplay payload={payload([item()])} />);
    const img = container.querySelector('img');
    expect(img).not.toBeNull();
    const src = img?.getAttribute('src') ?? '';
    expect(src.startsWith('/api/image-proxy?url=')).toBe(true);
    expect(src).toContain(encodeURIComponent('https://cdn.example.com/rome.jpg'));
    expect(img?.getAttribute('referrerpolicy')).toBe('no-referrer');
    expect(img?.getAttribute('loading')).toBe('lazy');
    expect(img?.getAttribute('alt')).toBe('Forecast for Rome');
  });

  it('NEVER emits a raw external <img src="http…"> to an arbitrary host (T-26-15)', () => {
    const { container } = render(
      <WebResultDisplay payload={payload([item(), item({ thumbnail: 'https://evil.test/x.png' })])} />,
    );
    const imgs = [...container.querySelectorAll('img')];
    expect(imgs.length).toBeGreaterThan(0);
    for (const img of imgs) {
      const src = img.getAttribute('src') ?? '';
      // Every external image goes through the proxy — no direct http(s) host src.
      expect(src.startsWith('http://')).toBe(false);
      expect(src.startsWith('https://')).toBe(false);
      expect(src.startsWith('/api/image-proxy?url=')).toBe(true);
    }
  });

  it('degrades a broken image to alt + domain chip (no toast)', () => {
    const { container } = render(<WebResultDisplay payload={payload([item()])} />);
    const img = container.querySelector('img');
    expect(img).not.toBeNull();
    if (img === null) throw new Error('expected an img');
    fireEvent.error(img);
    // The image is removed; the domain chip + title remain (graceful degrade).
    expect(container.querySelector('img')).toBeNull();
    expect(screen.getByText('weather.example.com')).toBeTruthy();
    expect(screen.getByText('Forecast for Rome')).toBeTruthy();
  });

  it('renders a citation bubble when the item ref_id resolves in the registry', async () => {
    const source: DisplaySource = {
      ref_id: 'src-rome',
      index: 1,
      type: 'web_result',
      title: 'Forecast for Rome',
      snippet: 'Sunny.',
      cited: true,
    };
    render(<WebResultDisplay payload={payload([item()], [source])} />);
    const chip = screen.getByRole('button', { name: 'Source 1: Forecast for Rome' });
    expect(chip).toBeTruthy();
    fireEvent.focus(chip);
    await waitFor(() => {
      // The hovercard heading renders the source title.
      expect(screen.getAllByText('Forecast for Rome').length).toBeGreaterThan(0);
    });
  });

  it('the relevance meter is INFO-tinted, not accent (Color rule)', () => {
    const { container } = render(<WebResultDisplay payload={payload([item()])} />);
    const meterFill = container.querySelector('.bg-info');
    expect(meterFill).not.toBeNull();
    // No accent fill on the meter.
    expect(container.querySelector('.bg-accent')).toBeNull();
  });

  it('paginates result groups at the default 3 per page', () => {
    const many = Array.from({ length: 5 }, (_, i) =>
      item({ title: `Result ${String(i)}`, url: `https://e.test/${String(i)}`, ref_id: `r${String(i)}` }),
    );
    render(<WebResultDisplay payload={payload(many)} />);
    // Page 1 shows 3; "1–3 of 5" count is present.
    expect(screen.getByText('1–3 of 5')).toBeTruthy();
    expect(screen.getByText('Result 0')).toBeTruthy();
    expect(screen.getByText('Result 2')).toBeTruthy();
    expect(screen.queryByText('Result 3')).toBeNull();
    // Next page reveals the rest.
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(screen.getByText('Result 3')).toBeTruthy();
    expect(screen.getByText('Result 4')).toBeTruthy();
  });

  it('shows the empty state when there are no results', () => {
    render(<WebResultDisplay payload={payload([])} />);
    expect(screen.getByText('No results')).toBeTruthy();
    expect(screen.getByText('This search returned no web results.')).toBeTruthy();
  });

  it('links the title to the source url in a new tab', () => {
    render(<WebResultDisplay payload={payload([item()])} />);
    const link = screen.getByRole('link', { name: 'Open Forecast for Rome in a new tab' });
    expect(link.getAttribute('href')).toBe('https://weather.example.com/rome');
    expect(link.getAttribute('target')).toBe('_blank');
    expect(link.getAttribute('rel')).toContain('noreferrer');
  });

  it('renders without a thumbnail when none is provided (no img element)', () => {
    const noThumb: WebItem = {
      title: 'No image result',
      url: 'https://example.com/x',
      snippet: 'A result with no thumbnail.',
      domain: 'example.com',
    };
    const { container } = render(<WebResultDisplay payload={payload([noThumb])} />);
    expect(container.querySelector('img')).toBeNull();
    expect(screen.getByText('No image result')).toBeTruthy();
  });
});
