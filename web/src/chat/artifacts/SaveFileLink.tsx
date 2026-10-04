import type { ComponentProps } from 'react';
import { useTranslation } from 'react-i18next';
import { Download, Loader2, Share } from 'lucide-react';
import { useAssetSource } from './renderers/assetSourceContext';
import { useSaveFile, useSaveLabel, type HeldSave, type SaveState } from './useSaveFile';
import { cn } from '@/lib/utils';

// The cockpit's download links, in one place so the installed app's share-sheet path
// (useSaveFile) cannot be missing from one of them: the chat card, the preview header and the
// preview's download card. The artifacts panel's icon-only row draws SaveIcon itself, and the
// file manager, whose several-file download is a menu entry, shows SaveFilesBar.

export function SaveIcon({ state, className }: { state: SaveState; className?: string }) {
  if (state === 'preparing') {
    return <Loader2 aria-hidden="true" className={cn(className, 'animate-spin')} />;
  }
  if (state === 'ready') return <Share aria-hidden="true" className={className} />;
  return <Download aria-hidden="true" className={className} />;
}

interface SaveFileLinkProps extends Omit<
  ComponentProps<'a'>,
  'href' | 'download' | 'onClick' | 'children'
> {
  readonly href: string;
  readonly fileName: string;
  readonly mimeType: string;
  /** The idle text: what the link has always said. */
  readonly label: string;
}

export function SaveFileLink({
  href,
  fileName,
  mimeType,
  label,
  'aria-label': ariaLabel,
  ...rest
}: SaveFileLinkProps) {
  const { credentials } = useAssetSource();
  const save = useSaveFile(href, fileName, mimeType, credentials);
  const stateLabel = useSaveLabel(save.state);
  return (
    <a
      {...rest}
      href={href}
      download={fileName}
      onClick={save.onClick}
      aria-label={stateLabel ?? ariaLabel}
      aria-busy={save.state === 'preparing'}
    >
      <SaveIcon state={save.state} className="size-4 shrink-0" />
      {stateLabel ?? label}
    </a>
  );
}

/**
 * Where several files stand on their way to one share sheet, for a download started from a menu
 * that closes as soon as it is used: the "Save" a lapsed tap needs has to live somewhere.
 * Renders nothing while idle.
 */
export function SaveFilesBar({ held, onSave }: { held: HeldSave; onSave: () => void }) {
  const { t } = useTranslation();
  const label = useSaveLabel(held.state);
  if (label === undefined) return null;
  return (
    <div
      role="status"
      className="absolute inset-x-0 bottom-4 z-10 mx-auto flex w-fit items-center gap-3 rounded-full border border-border bg-surface px-4 py-2 text-sm text-text shadow-lg"
    >
      <span>{t('files.saveCount', { count: held.links.length })}</span>
      <button
        type="button"
        onClick={onSave}
        disabled={held.state === 'preparing'}
        aria-busy={held.state === 'preparing'}
        className="inline-flex min-h-[44px] items-center gap-2 rounded-full border border-accent/40 bg-surface-2 px-3 font-medium text-accent-text transition-colors hover:border-accent disabled:opacity-60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
      >
        <SaveIcon state={held.state} className="size-4 shrink-0" />
        {label}
      </button>
    </div>
  );
}
