import { useId, useState } from 'react';
import { formatTimecode, parseTimecode } from './timecode';
import { Input } from '@/components/ui/input';

// The Start/End field. It keeps its own text while the operator types and commits on blur or
// Enter; a caller that changes the value from outside remounts it with key={formatTimecode(v)}.
export function TimeField({
  label,
  value,
  onCommit,
}: {
  readonly label: string;
  readonly value: number;
  readonly onCommit: (seconds: number) => void;
}) {
  const id = useId();
  const [text, setText] = useState(formatTimecode(value));
  const parsed = parseTimecode(text);
  return (
    <div className="flex flex-col gap-1 text-xs text-text-muted">
      <label htmlFor={id}>{label}</label>
      <Input
        id={id}
        value={text}
        inputMode="decimal"
        aria-invalid={parsed === undefined}
        onChange={(event) => {
          setText(event.target.value);
        }}
        onKeyDown={(event) => {
          if (event.key === 'Enter') event.currentTarget.blur();
        }}
        onBlur={() => {
          if (parsed === undefined) setText(formatTimecode(value));
          else onCommit(parsed);
        }}
        className="w-24 font-mono tabular-nums"
      />
    </div>
  );
}
