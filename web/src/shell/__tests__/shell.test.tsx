import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import '../../i18n/i18n';
import { Drawer } from '../Drawer';
import { ModeSwitcher } from '../ModeSwitcher';
import { ModeTabBar } from '../ModeTabBar';
import { useEdgeSwipe } from '../useEdgeSwipe';
import { useSurfaceIntent } from '../useSurfaceIntent';
import type { SurfaceIntent } from '../modes';

function EdgeProbe({
  onLeft,
  onRight,
}: {
  readonly onLeft: () => void;
  readonly onRight: () => void;
}) {
  const handlers = useEdgeSwipe({ onLeftEdge: onLeft, onRightEdge: onRight });
  return (
    <div data-testid="edge-probe" {...handlers}>
      edge
    </div>
  );
}

function SurfaceProbe() {
  const { surface, setSurface } = useSurfaceIntent();
  return (
    <>
      <p>{surface}</p>
      <button
        type="button"
        onClick={() => {
          setSurface('tree');
        }}
      >
        tree
      </button>
    </>
  );
}

describe('shell utilities', () => {
  afterEach(() => {
    localStorage.clear();
    document.body.style.overflow = '';
    vi.restoreAllMocks();
  });

  it('Drawer traps focus, closes on Escape, and restores scroll lock on unmount', () => {
    const onClose = vi.fn();
    const { unmount } = render(
      <Drawer open title="Navigation" side="left" onClose={onClose}>
        <button type="button">First</button>
        <button type="button">Last</button>
      </Drawer>,
    );

    const dialog = screen.getByRole('dialog', { name: 'Navigation' });
    const closeButton = within(dialog).getByRole('button', { name: 'Close panel' });
    expect(dialog).toBeTruthy();
    expect(document.body.style.overflow).toBe('hidden');
    expect(document.activeElement).toBe(closeButton);

    screen.getByRole('button', { name: 'Last' }).focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    expect(document.activeElement).toBe(closeButton);

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalled();

    unmount();
    expect(document.body.style.overflow).toBe('');
  });

  it('Drawer renders nothing when closed', () => {
    render(
      <Drawer open={false} title="Navigation" side="right" onClose={() => undefined}>
        <button type="button">Hidden</button>
      </Drawer>,
    );
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('useEdgeSwipe opens left and right drawers from touch edges only', () => {
    const onLeft = vi.fn();
    const onRight = vi.fn();
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 400 });
    render(<EdgeProbe onLeft={onLeft} onRight={onRight} />);
    const probe = screen.getByTestId('edge-probe');

    fireEvent.pointerDown(probe, { pointerType: 'touch', clientX: 2, clientY: 10 });
    fireEvent.pointerUp(probe, { pointerType: 'touch', clientX: 90, clientY: 12 });
    expect(onLeft).toHaveBeenCalledTimes(1);

    fireEvent.pointerDown(probe, { pointerType: 'touch', clientX: 398, clientY: 10 });
    fireEvent.pointerUp(probe, { pointerType: 'touch', clientX: 310, clientY: 12 });
    expect(onRight).toHaveBeenCalledTimes(1);

    fireEvent.pointerDown(probe, { pointerType: 'mouse', clientX: 2, clientY: 10 });
    fireEvent.pointerUp(probe, { pointerType: 'mouse', clientX: 90, clientY: 12 });
    expect(onLeft).toHaveBeenCalledTimes(1);
  });

  it('useSurfaceIntent restores valid stored surfaces and persists changes', () => {
    localStorage.setItem('aura.shell.surface', 'graph');
    render(<SurfaceProbe />);
    expect(screen.getByText('graph')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'tree' }));
    expect(screen.getAllByText('tree').length).toBeGreaterThan(0);
    expect(localStorage.getItem('aura.shell.surface')).toBe('tree');
  });

  it('mode controls expose desktop and mobile selection callbacks', () => {
    const selected: SurfaceIntent[] = [];
    render(
      <>
        <ModeSwitcher
          active="chat"
          onSelect={(mode) => {
            selected.push(mode);
          }}
        />
        <ModeTabBar
          active="chat"
          onSelect={(mode) => {
            selected.push(mode);
          }}
        />
      </>,
    );

    const treeButtons = screen.getAllByRole('button', { name: 'Tree' });
    const graphButtons = screen.getAllByRole('button', { name: 'Graph' });
    const treeButton = treeButtons[0];
    const graphButton = graphButtons[1];
    if (!treeButton || !graphButton) throw new Error('Expected desktop and mobile mode controls');
    fireEvent.click(treeButton);
    fireEvent.click(graphButton);
    expect(selected).toEqual(['tree', 'graph']);
  });
});
