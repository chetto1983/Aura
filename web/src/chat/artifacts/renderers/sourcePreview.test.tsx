import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import '@/i18n/i18n';
import { previewKind } from '../artifactMeta';
import { HighlightedSource } from './HighlightedSource';
import TextPreview from './TextPreview';
import HtmlPreview from './HtmlPreview';
import { AssetSourceContext, IDENTITY_SCOPED } from './assetSourceContext';

afterEach(() => vi.unstubAllGlobals());
const asset = { assetId: 'source', fileName: 'source.rs', mimeType: '' };

it('routes verified Office imports, untyped audio and canonical unknown source safely', () => {
  for (const ext of ['doc', 'odt', 'rtf']) expect(previewKind('', `x.${ext}`)).toBe('docx');
  for (const ext of ['xls', 'ods', 'csv']) expect(previewKind('', `x.${ext}`)).toBe('xlsx');
  expect(previewKind('', 'x.wav')).toBe('audio');
  expect(previewKind('', 'x.iecst', 'code')).toBe('text');
  expect(previewKind('', 'x.html', 'code')).toBe('html');
  expect(previewKind('', 'x.svg', 'code')).toBe('download');
});

it('cancels the body reader at the preview bound and labels truncated plain source', async () => {
  const cancel = vi.fn();
  let reads = 0;
  vi.stubGlobal(
    'fetch',
    vi.fn(
      () =>
        new Response(
          new ReadableStream({
            pull(controller) {
              reads++;
              controller.enqueue(new TextEncoder().encode('x'.repeat(65536)));
              if (reads === 20) controller.close();
            },
            cancel,
          }),
        ),
    ),
  );
  render(<TextPreview {...asset} />);
  await waitFor(() => {
    expect(screen.getByRole('status').textContent).toMatch(/truncat|limitat/i);
  });
  expect(cancel).toHaveBeenCalledOnce();
  expect(reads).toBeLessThanOrEqual(6);
  expect(screen.getByRole('link', { name: /download|scarica/i }).getAttribute('href')).toContain(
    '/download',
  );
  expect(screen.queryByRole('button', { name: /copy|copia/i })).toBeNull();
});

it('identifies binary content without displaying or tokenizing bytes', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => new Response('SQLite format 3\0binary')),
  );
  const { container } = render(<TextPreview {...asset} fileName="project.db" />);
  await waitFor(() => {
    expect(screen.getByRole('status').textContent).toMatch(/binary|binari/i);
  });
  expect(container.querySelector('pre')).toBeNull();
});

it('does not render incomplete HTML through srcdoc', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => new Response('<p>'.repeat(100000))),
  );
  const { container } = render(
    <AssetSourceContext.Provider
      value={{
        assetUrl: IDENTITY_SCOPED.assetUrl,
        streamUrl: IDENTITY_SCOPED.streamUrl,
        credentials: 'omit',
      }}
    >
      <HtmlPreview {...asset} />
    </AssetSourceContext.Provider>,
  );
  expect((await screen.findByRole('alert')).textContent).toMatch(/truncat|limitat/i);
  expect(container.querySelector('iframe')).toBeNull();
});

it('resets highlighted HTML when the same source switches to an unsupported language', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => new Response('print("<script>bad</script>")')),
  );
  const { container, rerender } = render(<HighlightedSource assetId="source" lang="python" />);
  await waitFor(
    () => {
      expect(container.querySelector('.shiki')).not.toBeNull();
    },
    { timeout: 5000 },
  );
  rerender(<HighlightedSource assetId="source" lang="iecst" />);
  await act(() => Promise.resolve());
  expect(container.querySelector('.shiki')).toBeNull();
  expect(container.querySelector('script')).toBeNull();
});

it('copies the complete original source and offers its download', async () => {
  const writeText = vi.fn(() => Promise.resolve());
  vi.stubGlobal('navigator', { clipboard: { writeText } });
  vi.stubGlobal(
    'fetch',
    vi.fn(() => new Response('fn main() {}')),
  );
  render(<TextPreview {...asset} />);
  fireEvent.click(await screen.findByRole('button', { name: /copy|copia/i }));
  expect(writeText).toHaveBeenCalledWith('fn main() {}');
});

it('replaces the highlighted result when the theme changes with identical source', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve(new Response('const answer = 42;'))),
  );
  document.documentElement.setAttribute('data-theme', 'dark');
  const { container } = render(<HighlightedSource assetId="source" lang="typescript" />);
  await waitFor(() => {
    expect(container.querySelector('.github-dark-default')).not.toBeNull();
  });
  await act(async () => {
    document.documentElement.setAttribute('data-theme', 'light');
    await Promise.resolve();
  });
  await waitFor(() => {
    expect(container.querySelector('.github-light-default')).not.toBeNull();
  });
  expect(container.querySelector('.github-dark-default')).toBeNull();
  document.documentElement.removeAttribute('data-theme');
});
