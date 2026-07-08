import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { LocalArtifactDisplay } from '../LocalArtifactDisplay';
import type { DisplayArtifact, DisplayPayload } from '../types';

// LocalArtifactDisplay (37A-04): an authenticated download button when the
// descriptor carries an asset_id, else a render-only filename + size + "delivery
// unavailable" card. A raw host/container path is NEVER rendered in either branch.

const HOST_PATH = '/run/out/report.csv';

function payload(artifact: DisplayArtifact): DisplayPayload {
  return { type: 'local_artifact', tool_call_id: 'call-1', artifact };
}

describe('LocalArtifactDisplay', () => {
  it('renders an authenticated same-origin download anchor when asset_id is present', () => {
    render(
      <LocalArtifactDisplay
        payload={payload({ filename: 'report.csv', size_bytes: 2048, asset_id: 'asset-123' })}
      />,
    );
    const link = screen.getByRole('link', { name: /report\.csv/i });
    expect(link.getAttribute('href')).toBe('/api/assets/asset-123/download');
    expect(link.getAttribute('download')).toBe('report.csv');
    expect(screen.getByText('report.csv')).toBeTruthy();
    expect(screen.getByText('2.0 KB')).toBeTruthy();
  });

  it('never renders a raw host/container path even if one leaks onto the artifact (asset_id present)', () => {
    // Defense-in-depth: the reducer already omits path, but the card itself must
    // not render it even if a path field rides alongside asset_id.
    const { container } = render(
      <LocalArtifactDisplay
        payload={payload({ filename: 'report.csv', asset_id: 'asset-123', path: HOST_PATH })}
      />,
    );
    expect(screen.queryByText(HOST_PATH)).toBeNull();
    expect(container.textContent).not.toContain(HOST_PATH);
    expect(screen.queryByText('Path')).toBeNull();
  });

  it('degrades to filename + size + a "delivery unavailable" note when asset_id is absent', () => {
    render(
      <LocalArtifactDisplay payload={payload({ filename: 'report.csv', size_bytes: 2048 })} />,
    );
    expect(screen.getByText('report.csv')).toBeTruthy();
    expect(screen.getByText('2.0 KB')).toBeTruthy();
    expect(screen.getByText('Delivery unavailable')).toBeTruthy();
    // No download affordance on the degraded card.
    expect(screen.queryByRole('link')).toBeNull();
  });

  it('never renders a raw host/container path on the degraded card (asset_id absent, path present)', () => {
    const { container } = render(
      <LocalArtifactDisplay
        payload={payload({ filename: 'report.csv', size_bytes: 2048, path: HOST_PATH })}
      />,
    );
    expect(screen.queryByText(HOST_PATH)).toBeNull();
    expect(container.textContent).not.toContain(HOST_PATH);
    expect(screen.queryByText('Path')).toBeNull();
    expect(screen.getByText('Delivery unavailable')).toBeTruthy();
  });

  it('formats the byte size in KB for a 2048-byte file', () => {
    render(
      <LocalArtifactDisplay
        payload={payload({ filename: 'a.bin', size_bytes: 2048, asset_id: 'a' })}
      />,
    );
    expect(screen.getByText('2.0 KB')).toBeTruthy();
  });

  it('formats a sub-KB size in bytes', () => {
    render(<LocalArtifactDisplay payload={payload({ filename: 'tiny', size_bytes: 512 })} />);
    expect(screen.getByText('512 B')).toBeTruthy();
  });

  it('formats an MB-scale size', () => {
    render(
      <LocalArtifactDisplay
        payload={payload({ filename: 'big', size_bytes: 5 * 1024 * 1024, asset_id: 'a' })}
      />,
    );
    expect(screen.getByText('5.0 MB')).toBeTruthy();
  });

  it('formats a GB-scale size', () => {
    render(
      <LocalArtifactDisplay
        payload={payload({ filename: 'huge', size_bytes: 3 * 1024 * 1024 * 1024, asset_id: 'a' })}
      />,
    );
    expect(screen.getByText('3.0 GB')).toBeTruthy();
  });

  it('falls to a safe name when the filename is missing', () => {
    render(
      <LocalArtifactDisplay
        payload={{ type: 'local_artifact', tool_call_id: 'call-x' } as DisplayPayload}
      />,
    );
    expect(screen.getByText('Untitled file')).toBeTruthy();
  });
});
