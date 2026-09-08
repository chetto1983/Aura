import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import HtmlArtifactViewer from './HtmlArtifactViewer';
import InlineHtmlArtifact from './InlineHtmlArtifact';
import { ArtifactWorkspace } from './ArtifactWorkspace';
import { ArtifactCopyButton } from './ArtifactCopyButton';
import { AssetSourceContext } from './renderers/assetSourceContext';

vi.mock('../displays/shiki', () => ({ highlightCode: () => Promise.resolve(null) }));

const artifact = { assetId: 'html-1', fileName: 'weather.html', mimeType: 'text/html' };
const source = '<html><script>parent.secret = true</script><h1>Weather</h1></html>';
function sourceFetch() {
  const fetcher = vi.fn(() => Promise.resolve(new Response(source)));
  vi.stubGlobal('fetch', fetcher);
  return fetcher;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('HTML artifact workflow', () => {
  it('shows the sealed preview inline and keeps its frame when switching to escaped source', async () => {
    sourceFetch();
    const { container } = render(<HtmlArtifactViewer {...artifact} />);
    const frame = container.querySelector('iframe');
    if (!frame) throw new Error('Preview iframe missing');
    expect(frame.getAttribute('sandbox')).toBe('allow-scripts');
    expect(frame.getAttribute('src')).toBe('/api/assets/html-1/render');
    expect(frame.hasAttribute('srcdoc')).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: 'Show code' }));
    await screen.findByText(source);
    expect(container.querySelector('script')).toBeNull();
    expect(container.querySelector('iframe')).toBe(frame);
    fireEvent.click(screen.getByRole('button', { name: 'Preview' }));
    expect(screen.queryByText(source)).toBeNull();
    expect(container.querySelector('iframe')).toBe(frame);
  });

  it('shows source beside the preview in the expanded workspace without restarting the frame', async () => {
    sourceFetch();
    const { container } = render(<HtmlArtifactViewer {...artifact} expanded />);
    const frame = container.querySelector('iframe');
    if (!frame) throw new Error('Preview iframe missing');
    fireEvent.click(screen.getByRole('button', { name: 'Show code' }));
    await screen.findByText(source);
    expect(container.querySelector('iframe')).toBe(frame);
    expect(screen.getByRole('separator', { name: 'Resize code and preview' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Hide code' }));
    expect(screen.queryByText(source)).toBeNull();
    expect(container.querySelector('iframe')).toBe(frame);
  });

  it('expands from chat, preserves the conversation, and restores trigger focus on Escape', async () => {
    const onExpand = vi.fn();
    render(
      <ArtifactWorkspace onExpand={onExpand}>
        <p>Conversation stays mounted</p>
        <InlineHtmlArtifact {...artifact} />
      </ArtifactWorkspace>,
    );
    const trigger = screen.getByRole('button', { name: 'Expand artifact' });
    trigger.focus();
    fireEvent.click(trigger);
    await screen.findByRole('button', { name: 'Back to conversation' });
    expect(onExpand).toHaveBeenCalledOnce();
    expect(screen.getByText('Conversation stays mounted')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Expand artifact' })).toBeNull();
    fireEvent.keyDown(document, { key: 'Escape' });
    await waitFor(() => {
      expect(document.activeElement).toBe(trigger);
    });
    expect(screen.queryByRole('button', { name: 'Back to conversation' })).toBeNull();
  });

  it('uses a dialog when a standalone shared artifact has no workspace', async () => {
    render(<InlineHtmlArtifact {...artifact} />);
    fireEvent.click(screen.getByRole('button', { name: 'Expand artifact' }));
    expect(await screen.findByRole('dialog')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Back to conversation' }));
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
  });

  it('clears selection on a thread change without remounting the conversation', async () => {
    const child = (
      <>
        <input aria-label="Draft" defaultValue="Keep my draft" />
        <InlineHtmlArtifact {...artifact} />
      </>
    );
    const onExpand = vi.fn();
    const { rerender } = render(
      <ArtifactWorkspace scopeKey="one" onExpand={onExpand}>
        {child}
      </ArtifactWorkspace>,
    );
    const draft = screen.getByRole('textbox', { name: 'Draft' });
    fireEvent.click(screen.getByRole('button', { name: 'Expand artifact' }));
    await screen.findByRole('button', { name: 'Back to conversation' });
    rerender(
      <ArtifactWorkspace scopeKey="two" onExpand={onExpand}>
        {child}
      </ArtifactWorkspace>,
    );
    expect(screen.queryByRole('button', { name: 'Back to conversation' })).toBeNull();
    expect(screen.getByRole('textbox', { name: 'Draft' })).toBe(draft);
  });

  it('copies original bytes through the active asset source, with truthful failure and retry', async () => {
    const fetcher = sourceFetch();
    const writeText = vi
      .fn()
      .mockRejectedValueOnce(new Error('Denied'))
      .mockResolvedValue(undefined);
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    render(
      <AssetSourceContext.Provider
        value={{ assetUrl: () => '/share/token/file', credentials: 'omit' }}
      >
        <ArtifactCopyButton assetId="html-1" />
      </AssetSourceContext.Provider>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Copy code' }));
    await screen.findByRole('alert');
    fireEvent.click(screen.getByRole('button', { name: 'Copy code' }));
    await screen.findByRole('button', { name: 'Copied' });
    expect(writeText).toHaveBeenLastCalledWith(source);
    expect(fetcher).toHaveBeenCalledWith('/share/token/file', { credentials: 'omit' });
    expect(screen.queryByRole('alert')).toBeNull();
  });
});
