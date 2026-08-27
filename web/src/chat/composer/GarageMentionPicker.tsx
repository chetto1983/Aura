import { useMemo } from 'react';
import { ComposerPrimitive, type Unstable_TriggerItem } from '@assistant-ui/react';
import { FileText, Folder } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  GARAGE_FILE_ITEM_TYPE,
  GARAGE_TRIGGER_CHAR,
  createGarageDirectiveFormatter,
  createGarageMentionFetcher,
} from './garageMentions';
import { useGarageCompletionAdapter } from './useGarageCompletionAdapter';
import { createFileManagerProvider } from '@/files/filesApi';
import { cn } from '@/lib/utils';

export interface GarageMentionPickerProps {
  readonly disabled?: boolean;
  readonly onSelect?: (() => void) | undefined;
}

/**
 * Garage's @ picker is only the data/presentation half. assistant-ui's existing trigger
 * root owns detection, debounce lifecycle, keyboard navigation, highlight and listbox ARIA.
 */
export function GarageMentionPicker({ disabled = false, onSelect }: GarageMentionPickerProps) {
  const { t } = useTranslation();
  const provider = useMemo(() => createFileManagerProvider(), []);
  const fetcher = useMemo(
    () => createGarageMentionFetcher((path) => provider.loadFiles(path)),
    [provider],
  );
  const completion = useGarageCompletionAdapter(fetcher, !disabled);
  const formatter = useMemo(() => createGarageDirectiveFormatter(), []);

  if (disabled) return null;

  return (
    <ComposerPrimitive.Unstable_TriggerPopover
      char={GARAGE_TRIGGER_CHAR}
      adapter={completion.adapter}
      isLoading={completion.isLoading}
      aria-label={t('chat.garagePicker.ariaLabel')}
      className="absolute bottom-full left-0 z-50 mb-2 w-full min-w-[16rem] max-w-md overflow-hidden rounded-xl border border-border bg-surface-2 shadow-[var(--shadow-popover)]"
    >
      <ComposerPrimitive.Unstable_TriggerPopover.Directive
        formatter={formatter}
        onInserted={onSelect}
      />
      <ComposerPrimitive.Unstable_TriggerPopoverItems className="max-h-72 overflow-y-auto p-1.5">
        {(items) =>
          items.map((item: Unstable_TriggerItem, index) => {
            const isFile = item.type === GARAGE_FILE_ITEM_TYPE;
            const Icon = isFile ? FileText : Folder;
            return (
              <ComposerPrimitive.Unstable_TriggerPopoverItem
                key={item.id}
                item={item}
                index={index}
                className={cn(
                  'flex w-full items-start gap-2.5 rounded-lg px-2 py-1.5 text-left transition-colors',
                  'hover:bg-surface-3/60 data-[highlighted]:bg-surface-3',
                )}
              >
                <Icon
                  data-icon
                  aria-hidden="true"
                  className="mt-0.5 size-4 shrink-0 text-text-faint"
                />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13px] font-medium text-text">
                    {item.label}
                  </span>
                  <span className="block truncate text-[11px] text-text-muted">
                    {t(isFile ? 'chat.garagePicker.file' : 'chat.garagePicker.folder')} ·{' '}
                    {item.description}
                  </span>
                </span>
              </ComposerPrimitive.Unstable_TriggerPopoverItem>
            );
          })
        }
      </ComposerPrimitive.Unstable_TriggerPopoverItems>
    </ComposerPrimitive.Unstable_TriggerPopover>
  );
}
