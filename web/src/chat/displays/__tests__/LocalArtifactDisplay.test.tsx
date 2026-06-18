import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { LocalArtifactDisplay } from '../LocalArtifactDisplay';
import type { DisplayArtifact, DisplayPayload } from '../types';

// LocalArtifactDisplay: filename + human byte size + mono path chip.

function payload(artifact: DisplayArtifact): DisplayPayload {
  return { type: 'local_artifact', tool_call_id: 'call-1', artifact };
}

describe('LocalArtifactDisplay', () => {
  it('renders the filename and a mono path chip', () => {
    render(
      <LocalArtifactDisplay
        payload={payload({ filename: 'report.csv', size_bytes: 2048, path: '/run/out/report.csv' })}
      />,
    );
    expect(screen.getByText('report.csv')).toBeTruthy();
    expect(screen.getByText('/run/out/report.csv')).toBeTruthy();
    expect(screen.getByText('Path')).toBeTruthy();
  });

  it('formats the byte size in KB for a 2048-byte file', () => {
    render(<LocalArtifactDisplay payload={payload({ filename: 'a.bin', size_bytes: 2048 })} />);
    expect(screen.getByText('2.0 KB')).toBeTruthy();
  });

  it('formats a sub-KB size in bytes', () => {
    render(<LocalArtifactDisplay payload={payload({ filename: 'tiny', size_bytes: 512 })} />);
    expect(screen.getByText('512 B')).toBeTruthy();
  });

  it('formats an MB-scale size', () => {
    render(
      <LocalArtifactDisplay payload={payload({ filename: 'big', size_bytes: 5 * 1024 * 1024 })} />,
    );
    expect(screen.getByText('5.0 MB')).toBeTruthy();
  });

  it('omits the path chip when no path is present', () => {
    render(<LocalArtifactDisplay payload={payload({ filename: 'no-path.txt' })} />);
    expect(screen.getByText('no-path.txt')).toBeTruthy();
    expect(screen.queryByText('Path')).toBeNull();
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
