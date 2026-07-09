import { Sparkles, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';

// SkillPill (D-06): the pinned-skill pill rendered above the composer input, mirroring the
// AttachmentChip shape so a pinned skill and an attachment share one visual language in the
// same row. A leading accent Sparkles glyph + accent-tinted border distinguish a skill from
// a file chip; the ghost X unpins it. The server-provided name renders as an auto-escaped
// React text node — no HTML-injection sink (T-37D-05).

interface SkillPillProps {
  readonly name: string;
  readonly onRemove: () => void;
}

export function SkillPill({ name, onRemove }: SkillPillProps) {
  const { t } = useTranslation();
  return (
    <span className="inline-flex min-h-10 max-w-full items-center gap-2 rounded-md border border-accent/40 bg-surface-2 px-2 py-1 text-xs text-text">
      <Sparkles data-icon aria-hidden="true" className="size-3.5 shrink-0 text-accent-text" />
      <span className="min-w-0 truncate">{name}</span>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        aria-label={t('chat.skillPicker.pinnedRemove', { name })}
        onClick={onRemove}
        className="h-8 min-h-8 w-8 rounded-full text-text-muted hover:bg-surface-3 hover:text-text"
      >
        <X data-icon aria-hidden="true" className="size-3.5" />
      </Button>
    </span>
  );
}
