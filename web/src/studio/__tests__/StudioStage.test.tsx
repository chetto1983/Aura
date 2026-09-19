import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { OpenEditorContext } from '../../mediaEdit/mediaEditorContext';
import { StudioStage } from '../StudioStage';
import type { StudioRecord } from '../studioApi';

// The finished artefact's own renderer fetches its bytes; the stage's actions are what is
// under test here.
vi.mock('../../chat/artifacts/renderers/previewDispatch', () => ({ PreviewByKind: () => null }));

const COMPLETED: StudioRecord = {
  id: 'r1',
  kind: 'image',
  status: 'completed',
  model: 'test/model',
  prompt: 'a boat',
  used: {},
  asset_id: 'img-1',
  created_at: '2026-09-19T10:00:00Z',
};

function stage(record: StudioRecord) {
  const open = vi.fn();
  render(
    <OpenEditorContext.Provider value={open}>
      <StudioStage record={record} onReuse={vi.fn()} reuseState="ready" />
    </OpenEditorContext.Provider>,
  );
  return open;
}

describe('StudioStage editing', () => {
  it('offers Edit beside Download on a finished result', () => {
    const open = stage(COMPLETED);
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    expect(open).toHaveBeenCalledWith({ assetId: 'img-1', kind: 'image' });
  });

  it('offers nothing to edit on a failed generation', () => {
    const { asset_id: _none, ...failed } = COMPLETED;
    stage({ ...failed, status: 'failed', error: { code: 'provider_error', message: 'boom' } });
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();
  });
});
