import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const css = readFileSync(resolve(process.cwd(), 'src/styles/responsive-containers.css'), 'utf8');

describe('responsive shell containers', () => {
  it('keeps the mobile Documents surface inside the shell with the bottom dock available', () => {
    expect(css).toContain(".aura-shell[data-surface='documents']");
    expect(css).toContain('grid-template-rows: minmax(0, 1fr) auto !important;');
    expect(css).toContain(".aura-shell[data-surface='documents'] .shell-header");
    expect(css).not.toContain(
      ".aura-shell[data-surface='documents'] .shell-header,\n  .aura-shell[data-surface='documents'] .shell-bottom-dock",
    );
    expect(css).not.toMatch(/\.aura-shell\[data-surface='documents'\]\s+\.shell-bottom-dock\s*\{\s*display:\s*none/i);
  });

  it('keeps the mobile Graph canvas and evidence strip inside the shell row', () => {
    expect(css).toContain('@container aura-graph (max-width: 44rem)');
    expect(css).toContain('overflow: hidden !important;');
    expect(css).toContain('min-height: clamp(10rem, 34svh, 18rem) !important;');
    expect(css).toContain('min-height: 7rem !important;');
    expect(css).toContain('max-height: none !important;');
    expect(css).toContain('flex: 1 1 0% !important;');
  });
});
