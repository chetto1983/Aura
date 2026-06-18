import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { describe, expect, it } from 'vitest';
import tokens from '../../tokens/tokens.json';

interface ReadabilityTokens {
  readonly density: {
    readonly compact: { readonly 'font-base': string };
    readonly operator: { readonly 'font-base': string };
    readonly review: { readonly 'font-base': string };
  };
  readonly font: { readonly sans: string };
}

const readabilityTokens = tokens as ReadabilityTokens;
const OPERATOR_BASE_PX = 15.5;
const MIN_OPERATOR_TEXT_PX = 11;

function px(value: string): number {
  return Number.parseFloat(value.replace('px', ''));
}

function sourceFiles(root: string): string[] {
  const files: string[] = [];
  for (const entry of readdirSync(root)) {
    if (entry === '__tests__' || entry === 'test') continue;
    const path = join(root, entry);
    const stats = statSync(path);
    if (stats.isDirectory()) {
      files.push(...sourceFiles(path));
    } else if (/\.(tsx?|css)$/.test(entry)) {
      files.push(path);
    }
  }
  return files;
}

describe('readability design tokens', () => {
  it('uses Atkinson Hyperlegible Next for body and UI text', () => {
    expect(readabilityTokens.font.sans).toContain('Atkinson Hyperlegible Next');
  });

  it('uses readable density base sizes', () => {
    expect(px(readabilityTokens.density.compact['font-base'])).toBe(14);
    expect(px(readabilityTokens.density.operator['font-base'])).toBe(15.5);
    expect(px(readabilityTokens.density.review['font-base'])).toBe(16);
  });

  it('keeps arbitrary rem text utilities at 11px or larger in operator density', () => {
    const offenders: string[] = [];
    const textSizePattern = /text-\[(\d*\.?\d+)rem\]/g;

    for (const file of sourceFiles(join(process.cwd(), 'src'))) {
      const source = readFileSync(file, 'utf8');
      for (const match of source.matchAll(textSizePattern)) {
        const rem = Number.parseFloat(match[1] ?? '0');
        if (rem * OPERATOR_BASE_PX < MIN_OPERATOR_TEXT_PX) {
          offenders.push(`${relative(process.cwd(), file)}: ${match[0]}`);
        }
      }
    }

    expect(offenders).toEqual([]);
  });
});
