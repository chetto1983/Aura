import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so the row's t() keys resolve
import type { Asset } from '../attachments/types';
import { ArtifactRow } from './ArtifactRow';

// ArtifactRow (D-05/D-12/D-18): the accepted-vs-degraded split. The accepted row
// exposes the id-only download anchor + the click-to-preview body; the degraded row
// shows a role="note" and is inert (no preview). The final test pins T-37B-16: no
// internal store key or host path is ever rendered, only the opaque asset id.

function asset(over: Partial<Asset>): Asset {
  return {
    id: 'a-1',
    status: 'accepted',
    modality: 'document',
    file_name: 'report.docx',
    mime_type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    declared_size_bytes: 2048,
    size_bytes: 2048,
    source_kind: 'agent',
    ...over,
  };
}

describe('ArtifactRow — accepted', () => {
  it('renders the id-only download anchor with the file name', () => {
    render(
      <ArtifactRow asset={asset({ id: 'xyz', file_name: 'report.docx' })} onPreview={vi.fn()} />,
    );
    const link = screen.getByRole('link', { name: /download report\.docx/i });
    expect(link.getAttribute('href')).toBe('/api/assets/xyz/download');
    expect(link.getAttribute('download')).toBe('report.docx');
  });

  it('renders the "Categoria · EXT" label and the formatted size', () => {
    render(<ArtifactRow asset={asset({})} onPreview={vi.fn()} />);
    // Documento · DOCX (category from ext) plus the 2 KB size on the same line.
    expect(screen.getByText(/DOCX/)).toBeTruthy();
    expect(screen.getByText(/2\.0 KB/)).toBeTruthy();
  });

  it('opens the preview when the row body is clicked', () => {
    const onPreview = vi.fn();
    const a = asset({});
    render(<ArtifactRow asset={a} onPreview={onPreview} />);
    fireEvent.click(screen.getByRole('button'));
    expect(onPreview).toHaveBeenCalledWith(a);
  });

  it('does NOT open the preview when the download anchor is clicked', () => {
    const onPreview = vi.fn();
    render(<ArtifactRow asset={asset({})} onPreview={onPreview} />);
    fireEvent.click(screen.getByRole('link'));
    expect(onPreview).not.toHaveBeenCalled();
  });
});

describe('ArtifactRow — degraded', () => {
  it('renders a role="note" and no download anchor for a non-accepted asset', () => {
    render(<ArtifactRow asset={asset({ status: 'processing' })} onPreview={vi.fn()} />);
    expect(screen.getByRole('note')).toBeTruthy();
    expect(screen.queryByRole('link')).toBeNull();
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('does not open a preview (no clickable body) when degraded', () => {
    const onPreview = vi.fn();
    render(<ArtifactRow asset={asset({ status: 'failed' })} onPreview={onPreview} />);
    // The filename still renders, but clicking it must not trigger a preview.
    fireEvent.click(screen.getByText('report.docx'));
    expect(onPreview).not.toHaveBeenCalled();
  });
});

describe('ArtifactRow — no path/key leakage (T-37B-16)', () => {
  it('renders only the id in the download URL, never object_key/object_bucket/host path', () => {
    const { container } = render(
      <ArtifactRow asset={asset({ id: 'safe-id', file_name: 'meteo.docx' })} onPreview={vi.fn()} />,
    );
    const html = container.innerHTML;
    expect(html).not.toMatch(/object_key/i);
    expect(html).not.toMatch(/object_bucket/i);
    // No absolute filesystem / bucket path — the only asset URL is the id-addressed route.
    expect(html).not.toMatch(/\/(var|home|mnt|root)\//);
    expect(html).toContain('/api/assets/safe-id/download');
  });
});
