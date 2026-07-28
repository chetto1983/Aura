import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import { SkillBodyEditor } from '../SkillBodyEditor';

// SkillBodyEditor test — the round trip is the whole risk. A WYSIWYG editor over a file
// format only works if what it hands back is what it was given: if the serializer drops or
// rewrites a construct, saving an untouched skill would silently rewrite it on disk.
//
// So the assertions here are about markdown IN → markdown OUT, and about the toolbar
// offering only constructs markdown can carry.

function renderEditor(value: string) {
  const onChange = vi.fn();
  render(<SkillBodyEditor value={value} onChange={onChange} label="Skill body" />);
  return onChange;
}

describe('SkillBodyEditor', () => {
  it('renders the seeded markdown as rich text', async () => {
    renderEditor('## When to use\n\nRun it when the operator asks for a **quote**.');

    await waitFor(() => {
      expect(screen.getByText('When to use')).toBeTruthy();
    });
    const heading = screen.getByText('When to use');
    expect(heading.tagName).toBe('H2');
    expect(screen.getByText('quote').tagName).toBe('STRONG');
  });

  it('hands back markdown, and an edit that cancels itself restores the original', async () => {
    // The round trip is the whole risk of putting a WYSIWYG editor over a file format: if
    // the serializer rewrites a construct, saving an untouched skill would silently
    // rewrite SKILL.md. Quoting a paragraph and unquoting it must land exactly where it
    // started.
    const source = '## When to use\n\nRun it when the operator asks for a quote.';
    const onChange = renderEditor(source);

    const quote = await screen.findByRole('button', { name: 'Quote' });
    fireEvent.click(quote);
    await waitFor(() => {
      expect(onChange).toHaveBeenCalled();
    });
    // The intermediate state is markdown too, not HTML.
    const quoted = onChange.mock.calls.at(-1)?.[0] as string;
    expect(quoted).toContain('>');
    expect(quoted).not.toContain('<blockquote');

    fireEvent.click(quote);
    await waitFor(() => {
      expect((onChange.mock.calls.at(-1)?.[0] as string).trim()).toBe(source);
    });
  });

  it('offers only formatting markdown can express', async () => {
    renderEditor('body');

    const toolbar = await screen.findByRole('toolbar', { name: 'Formatting' });
    expect(toolbar).toBeTruthy();
    // Every control maps to a markdown construct. Anything that cannot round-trip
    // (colour, font, alignment) must never appear here — it would be dropped on save.
    for (const name of [
      'Heading',
      'Bold',
      'Italic',
      'Bulleted list',
      'Numbered list',
      'Inline code',
      'Code block',
      'Quote',
    ]) {
      expect(screen.getByRole('button', { name })).toBeTruthy();
    }
    for (const forbidden of [/colou?r/i, /font/i, /align/i, /highlight/i]) {
      expect(screen.queryByRole('button', { name: forbidden })).toBeNull();
    }
  });

  it('states that what is written is what lands in the file', async () => {
    renderEditor('body');
    await waitFor(() => {
      expect(screen.getByText(/what you write is what lands in SKILL.md/)).toBeTruthy();
    });
  });

  it('exposes the body surface under an accessible name', async () => {
    renderEditor('body');
    await waitFor(() => {
      expect(screen.getByLabelText('Skill body')).toBeTruthy();
    });
  });
});
