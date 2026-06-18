import { describe, expect, it } from 'vitest';
import type { Element, Root, Text } from 'hast';
import {
  CITATION_NUMBER_ATTR,
  CITATION_REF_ATTR,
  indexSources,
  rehypeCitations,
} from '../rehypeCitations';
import type { DisplaySource } from '../types';

// rehypeCitations (DISP-03 / D-04): splices the `[n]` chip element INLINE at the
// claim position (NOT end-appended), backed by the code-owned source registry; an
// `[n]` whose number is not in the registry is left as literal text (T-26-18).

function source(index: number, refId: string, cited = true): DisplaySource {
  return { ref_id: refId, index, title: `Source ${String(index)}`, cited };
}

/** A hast Root holding a single paragraph with one text child = `value`. */
function paragraph(value: string): Root {
  const text: Text = { type: 'text', value };
  const p: Element = { type: 'element', tagName: 'p', properties: {}, children: [text] };
  return { type: 'root', children: [p] };
}

function runPlugin(tree: Root, sources: readonly DisplaySource[]): Root {
  const transform = rehypeCitations({ byIndex: indexSources(sources) });
  transform(tree);
  return tree;
}

function paragraphChildren(tree: Root): (Element | Text)[] {
  const p = tree.children[0] as Element;
  return p.children as (Element | Text)[];
}

describe('rehypeCitations (DISP-03 / D-04)', () => {
  it('splices the chip INLINE at the mid-sentence claim position (not end-appended)', () => {
    const tree = paragraph('The sky is blue [2] on a clear day.');
    runPlugin(tree, [source(2, 'src-sky')]);
    const kids = paragraphChildren(tree);

    // [text "The sky is blue "][span chip][text " on a clear day."]
    expect(kids).toHaveLength(3);
    expect(kids[0]).toMatchObject({ type: 'text', value: 'The sky is blue ' });

    const chip = kids[1] as Element;
    expect(chip.type).toBe('element');
    expect(chip.tagName).toBe('span');
    expect(chip.properties?.[CITATION_REF_ATTR]).toBe('src-sky');
    expect(chip.properties?.[CITATION_NUMBER_ATTR]).toBe('2');

    // The chip is BEFORE the trailing text — i.e. it is NOT appended at the end.
    expect(kids[2]).toMatchObject({ type: 'text', value: ' on a clear day.' });
  });

  it('resolves multiple inline markers each at its own claim position', () => {
    const tree = paragraph('A [1] then B [3] end.');
    runPlugin(tree, [source(1, 'a'), source(3, 'b')]);
    const kids = paragraphChildren(tree);
    const chips = kids.filter((k): k is Element => k.type === 'element');
    expect(chips).toHaveLength(2);
    const [firstChip, secondChip] = chips;
    expect(firstChip?.properties?.[CITATION_REF_ATTR]).toBe('a');
    expect(secondChip?.properties?.[CITATION_REF_ATTR]).toBe('b');
    // The first chip precedes the "then B" text — inline order is preserved.
    const firstChipPos = firstChip !== undefined ? kids.indexOf(firstChip) : -1;
    const hasMiddleText = kids.some(
      (k, i) => i > firstChipPos && k.type === 'text' && k.value.includes('then B'),
    );
    expect(hasMiddleText).toBe(true);
  });

  it('leaves an unknown `[n]` (not in the registry) as literal text — no live chip (T-26-18)', () => {
    const tree = paragraph('Claim [9] without a source.');
    runPlugin(tree, [source(1, 'a')]);
    const kids = paragraphChildren(tree);
    // No span element was spliced; the original text is untouched.
    expect(kids.some((k) => k.type === 'element')).toBe(false);
    const merged = kids.map((k) => (k.type === 'text' ? k.value : '')).join('');
    expect(merged).toContain('[9]');
  });

  it('splices known markers but keeps an interleaved unknown marker as literal text', () => {
    const tree = paragraph('Known [1] and unknown [9].');
    runPlugin(tree, [source(1, 'a')]);
    const kids = paragraphChildren(tree);
    const chips = kids.filter((k): k is Element => k.type === 'element');
    expect(chips).toHaveLength(1);
    expect(chips[0]?.properties?.[CITATION_REF_ATTR]).toBe('a');
    // The unknown [9] survives as text.
    const merged = kids.map((k) => (k.type === 'text' ? k.value : '')).join('');
    expect(merged).toContain('[9]');
    expect(merged).not.toContain('[1]'); // [1] was replaced by the chip
  });

  it('resolves a MULTI-digit citation number (e.g. [12])', () => {
    const tree = paragraph('Deep cite [12] here.');
    runPlugin(tree, [source(12, 'twelve')]);
    const kids = paragraphChildren(tree);
    const chips = kids.filter((k): k is Element => k.type === 'element');
    expect(chips).toHaveLength(1);
    expect(chips[0]?.properties?.[CITATION_REF_ATTR]).toBe('twelve');
    expect(chips[0]?.properties?.[CITATION_NUMBER_ATTR]).toBe('12');
    // The raw "[12]" marker is consumed, not left as text.
    const merged = kids.map((k) => (k.type === 'text' ? k.value : '')).join('');
    expect(merged).not.toContain('[12]');
  });

  it('keeps the trailing text after a citation that ends the string boundary', () => {
    const tree = paragraph('Tail [1]X');
    runPlugin(tree, [source(1, 'a')]);
    const kids = paragraphChildren(tree);
    const merged = kids.map((k) => (k.type === 'text' ? k.value : '')).join('');
    // The single trailing char after the marker survives (kills the `<`/`<=` boundary mutant).
    expect(merged).toBe('Tail X');
  });

  it('is a no-op for text with no citation markers', () => {
    const tree = paragraph('Plain prose, nothing to cite.');
    runPlugin(tree, [source(1, 'a')]);
    const kids = paragraphChildren(tree);
    expect(kids).toHaveLength(1);
    expect(kids[0]).toMatchObject({ type: 'text', value: 'Plain prose, nothing to cite.' });
  });

  it('indexSources keys the registry by the 1-based citation number', () => {
    const map = indexSources([source(1, 'a'), source(5, 'e')]);
    expect(map.get(1)?.ref_id).toBe('a');
    expect(map.get(5)?.ref_id).toBe('e');
    expect(map.get(2)).toBeUndefined();
    expect(indexSources(undefined).size).toBe(0);
  });
});
