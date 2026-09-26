import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ComputerUse, type ComputerStep } from '../computer-use';

// The owned element draws the screen it is given, the address, and the last three steps: the
// newest as the cursor and the action line, the two before it as a fading trail.

function step(id: string, x: number, y: number): ComputerStep {
  return { id, action: 'click', target: `target ${id}`, x, y };
}

function dots(container: HTMLElement) {
  return [...container.querySelectorAll<HTMLElement>('span.rounded-full[aria-hidden]')].filter(
    (el) => el.style.left !== '',
  );
}

describe('ComputerUse', () => {
  it('shows the address and the screen, and no cursor or action line before a step', () => {
    const { container } = render(
      <ComputerUse url="https://portal.test/login" steps={[]}>
        <img alt="page" />
      </ComputerUse>,
    );
    expect(screen.getByText('https://portal.test/login')).toBeTruthy();
    expect(screen.getByRole('img', { name: 'page' })).toBeTruthy();
    expect(container.querySelector('svg')).toBeNull();
    expect(screen.queryByText('click')).toBeNull();
    expect(dots(container)).toHaveLength(0);
  });

  it('puts the cursor on the newest step and fades only the two before it', () => {
    const steps = [step('1', 5, 5), step('2', 10, 20), step('3', 30, 40), step('4', 62, 24)];
    const { container } = render(
      <ComputerUse url="u" steps={steps} className="extra">
        <div />
      </ComputerUse>,
    );
    const cursor = container.querySelector<SVGElement>('svg');
    expect(cursor?.style.left).toBe('62%');
    expect(cursor?.style.top).toBe('24%');
    expect(screen.getByText('target 4')).toBeTruthy();
    expect(screen.queryByText('target 3')).toBeNull();

    const trail = dots(container);
    expect(trail.map((d) => [d.style.left, d.style.top, d.style.opacity])).toEqual([
      ['10%', '20%', String(0.18)],
      ['30%', '40%', String(0.36)],
      ['62%', '24%', String(0.18 * 3)],
    ]);
    expect(container.firstElementChild?.classList.contains('extra')).toBe(true);
  });

  it('fits its screen, and keeps a long address from widening it', () => {
    const { container } = render(
      <ComputerUse url={'https://portal.test/'.padEnd(400, 'x')} steps={[step('1', 1, 1)]}>
        <div />
      </ComputerUse>,
    );
    const root = container.firstElementChild;
    expect(root?.getAttribute('data-slot')).toBe('computer-use');
    expect(root?.className).toContain('w-fit');
    expect(root?.className).toContain('max-w-full');
    const contained = container.querySelectorAll('.\\[contain\\:inline-size\\]');
    expect(contained).toHaveLength(2);
    const tints = [...container.querySelectorAll('span.size-2.rounded-full:not([style])')].map(
      (d) => d.className,
    );
    expect(tints).toEqual([
      expect.stringContaining('bg-danger/50'),
      expect.stringContaining('bg-warning/50'),
      expect.stringContaining('bg-success/50'),
    ]);
  });
});
