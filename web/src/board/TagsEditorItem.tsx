import { useState } from 'react';
import { Text } from '@svar-ui/react-core';
import { parseTags } from './boardModel';

const NO_TAGS: readonly string[] = [];

interface TagsEditorItemProps {
  readonly value?: readonly string[];
  readonly onChange: (ev: { value: string[] }) => void;
}

/**
 * The tags field of the card editor, handed to it as the item's `comp`. The stock multicombo
 * only picks among the options it is given and cannot make a new one, so a tag nobody has used
 * yet could never be typed. This is the editor's own text field holding a comma-separated
 * line: SVAR's Text takes the field's id, so the label names it, and it keeps the text as
 * typed, so a trailing comma survives the keystroke, while the widget gets the parsed list.
 */
export function TagsEditorItem({ value = NO_TAGS, onChange }: TagsEditorItemProps) {
  const [text, setText] = useState(() => value.join(', '));
  // The editor hands the card's values over after mounting its fields, and reuses them for the
  // next card, so a new list replaces the line; the list this field just emitted does not,
  // which is what keeps a trailing comma alive.
  const [shown, setShown] = useState(value);
  if (shown !== value) {
    setShown(value);
    if (parseTags(text).join('\n') !== value.join('\n')) setText(value.join(', '));
  }
  return (
    <Text
      value={text}
      onChange={({ value: next }) => {
        setText(String(next));
        onChange({ value: parseTags(String(next)) });
      }}
    />
  );
}
