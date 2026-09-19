import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { MediaEditorProvider } from '../MediaEditorProvider';
import { useOpenEditor } from '../mediaEditorContext';

vi.mock('../MediaEditorHost', () => ({
  default: ({ assetId, kind, onClose }: { assetId: string; kind: string; onClose: () => void }) => (
    <button type="button" onClick={onClose}>
      host {assetId} {kind}
    </button>
  ),
}));

function Opener() {
  const open = useOpenEditor();
  return (
    <button
      type="button"
      onClick={() => {
        open?.({ assetId: 'v1', kind: 'video' });
      }}
    >
      open
    </button>
  );
}

describe('MediaEditorProvider', () => {
  it('opens the one editor for the target a surface hands it, and closes it', async () => {
    render(
      <MediaEditorProvider>
        <Opener />
      </MediaEditorProvider>,
    );
    expect(screen.queryByText(/^host/)).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'open' }));
    fireEvent.click(await screen.findByRole('button', { name: 'host v1 video' }));
    expect(screen.queryByText(/^host/)).toBeNull();
  });
});
