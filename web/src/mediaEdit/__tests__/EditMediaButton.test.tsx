import { fireEvent, render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import { describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import {
  AssetSourceContext,
  useAssetSource,
} from '../../chat/artifacts/renderers/assetSourceContext';
import { EditMediaButton } from '../EditMediaButton';
import { OpenEditorContext } from '../mediaEditorContext';

function Harness({ editable, children }: { editable: boolean; children: ReactNode }) {
  const source = useAssetSource();
  const { editable: _drop, ...rest } = source;
  return (
    <AssetSourceContext.Provider value={editable ? source : rest}>
      {children}
    </AssetSourceContext.Provider>
  );
}

function mount(node: ReactNode, { editable = true, open = vi.fn() } = {}) {
  render(
    <OpenEditorContext.Provider value={open}>
      <Harness editable={editable}>{node}</Harness>
    </OpenEditorContext.Provider>,
  );
  return open;
}

describe('EditMediaButton', () => {
  it('hands the asset to the editor and lets the surface close first', () => {
    const onOpen = vi.fn();
    const open = mount(
      <EditMediaButton assetId="a1" kind="video" fileName="clip.mp4" onOpen={onOpen} />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Edit clip.mp4' }));
    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(open).toHaveBeenCalledWith({ assetId: 'a1', kind: 'video' });
  });

  it('shows nothing on a source that is not editable', () => {
    mount(<EditMediaButton assetId="a1" kind="image" />, { editable: false });
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('shows nothing without an editor to open', () => {
    render(<EditMediaButton assetId="a1" kind="image" />);
    expect(screen.queryByRole('button')).toBeNull();
  });

  it.each(['image/gif', 'image/svg+xml', 'video/quicktime'])('shows nothing for %s', (mimeType) => {
    mount(<EditMediaButton assetId="a1" kind="image" mimeType={mimeType} />);
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('shows an icon-only button when compact', () => {
    mount(<EditMediaButton assetId="a1" kind="image" mimeType="image/png" compact />);
    expect(screen.getByRole('button', { name: 'Edit' }).textContent).not.toContain('Edit');
  });
});
