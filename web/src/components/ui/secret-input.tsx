import { useId, useState, type ComponentProps } from 'react';
import { Eye, EyeOff } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { cn } from '@/lib/utils';

interface SecretInputProps extends Omit<ComponentProps<'input'>, 'type'> {
  readonly showLabel: string;
  readonly hideLabel: string;
}

export function SecretInput({
  className,
  disabled,
  hideLabel,
  id,
  showLabel,
  ...props
}: SecretInputProps) {
  const generatedId = useId();
  const inputId = id ?? generatedId;
  const [revealed, setRevealed] = useState(false);
  const toggleLabel = revealed ? hideLabel : showLabel;

  return (
    <div className="relative">
      <Input
        id={inputId}
        type={revealed ? 'text' : 'password'}
        disabled={disabled}
        className={cn('pr-11', className)}
        {...props}
      />
      <Button
        type="button"
        variant="ghost"
        size="icon"
        disabled={disabled}
        onClick={() => {
          setRevealed((value) => !value);
        }}
        aria-controls={inputId}
        aria-pressed={revealed}
        aria-label={toggleLabel}
        className="absolute inset-y-0 right-0 h-auto min-h-[var(--row-h)] w-11 rounded-l-none text-text-muted hover:text-text"
      >
        {revealed ? (
          <EyeOff data-icon="icon" aria-hidden="true" focusable="false" />
        ) : (
          <Eye data-icon="icon" aria-hidden="true" focusable="false" />
        )}
      </Button>
    </div>
  );
}
