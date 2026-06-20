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
      <button
        type="button"
        onClick={() => {
          setSurface('graph');
        }}
      >
        graph
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

  it('Drawer passes the explicit intent on close button, Escape, and backdrop tap (§3.1c)', () => {
    const onClose = vi.fn();
    render(
      <Drawer open title="Navigation" side="left" onClose={onClose}>
        <button type="button">First</button>
      </Drawer>,
    );
    const dialog = screen.getByRole('dialog', { name: 'Navigation' });

    fireEvent.click(within(dialog).getByRole('button', { name: 'Close panel' }));
    fireEvent.keyDown(document, { key: 'Escape' });
    // Two buttons share the label; the first is the full-bleed backdrop scrim.
    const [scrim] = screen.getAllByRole('button', { name: 'Close panel' });
    if (!scrim) throw new Error('expected a backdrop scrim button');
    fireEvent.click(scrim);

    expect(onClose).toHaveBeenCalledTimes(3);
    for (const call of onClose.mock.calls) {
      expect(call[0]).toBe('explicit');
    }
  });

  it('Drawer renders nothing when closed', () => {
    render(
      <Drawer open={false} title="Navigation" side="right" onClose={() => undefined}>
        <button type="button">Hidden</button>
      </Drawer>,
    );
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('useEdgeSwipe wires the ref to the rendered element and opens drawers from edge swipes', () => {
    // Full §3.1b gesture matrix lives in useEdgeSwipe.test.ts; this asserts the
    // {...handlers}→ref spread reaches a React-rendered host and the gesture fires.
    const onLeft = vi.fn();
    const onRight = vi.fn();
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 400 });
    render(<EdgeProbe onLeft={onLeft} onRight={onRight} />);
    const probe = screen.getByTestId('edge-probe');

    function touch(type: string, x: number, y: number): void {
      const event = new Event(type, { bubbles: true, cancelable: true });
      const list = [{ clientX: x, clientY: y, target: probe }];
      Object.defineProperty(event, 'touches', { value: list, configurable: true });
      Object.defineProperty(event, 'changedTouches', { value: list, configurable: true });
      Object.defineProperty(event, 'target', { value: probe, configurable: true });
      probe.dispatchEvent(event);
    }

    touch('touchstart', 4, 100);
    touch('touchmove', 220, 104); // >10px, horizontal, past 50%
    touch('touchend', 220, 104);
    expect(onLeft).toHaveBeenCalledTimes(1);

    touch('touchstart', 396, 100);
    touch('touchmove', 150, 104); // right edge, right→left, past 50%
    touch('touchend', 150, 104);
    expect(onRight).toHaveBeenCalledTimes(1);
  });

  it('useSurfaceIntent ignores stored future surfaces and only persists live surfaces', () => {
    // 'displays' is still a future (non-live) surface — a stored future surface is ignored
    // and clicking another future surface ('tree') does not switch. ('graph' is live as of
    // Phase 27, covered by the live-switch case below.)
    localStorage.setItem('aura.shell.surface', 'displays');
    render(<SurfaceProbe />);
    expect(screen.getByText('chat')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'tree' }));
    expect(screen.getByText('chat')).toBeTruthy();
    expect(localStorage.getItem('aura.shell.surface')).toBe('displays');
  });

  it('useSurfaceIntent switches to + persists the live graph surface (Phase 27)', () => {
    render(<SurfaceProbe />);
    expect(screen.getByText('chat')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'graph' }));
    // The surface <p> now reads 'graph' (the button shares the text, so query the paragraph).
    expect(screen.getByText('graph', { selector: 'p' })).toBeTruthy();
    expect(localStorage.getItem('aura.shell.surface')).toBe('graph');
  });

  it('desktop mode control exposes future modes disabled, while the mobile dock shows live modes only', () => {
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

    const primary = within(screen.getByRole('navigation', { name: 'Primary' }));
    const mobile = within(screen.getByRole('navigation', { name: 'Modes' }));
    const treeButton = primary.getByRole('button', { name: 'Tree' });
    const graphButton = primary.getByRole('button', { name: 'Graph' });
    expect(primary.getByRole('button', { name: 'Chat' }).getAttribute('aria-current')).toBe('page');
    expect(mobile.getByRole('button', { name: 'Chat' }).getAttribute('aria-current')).toBe('page');
    expect(treeButton.getAttribute('aria-disabled')).toBe('true');
    expect(mobile.queryByRole('button', { name: 'Tree' })).toBeNull();
    expect(mobile.queryByRole('button', { name: 'Displays' })).toBeNull();
    expect(mobile.queryByRole('button', { name: 'Settings' })).toBeNull();
    expect(graphButton.getAttribute('aria-disabled')).toBeNull();
    expect(mobile.getByRole('button', { name: 'Graph' }).getAttribute('aria-disabled')).toBeNull();
    fireEvent.click(treeButton);
    fireEvent.click(graphButton);
    expect(selected).toEqual(['graph']);
  });
});
