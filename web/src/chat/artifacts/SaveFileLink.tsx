import type { ComponentProps } from 'react';
import { Download, Loader2, Share } from 'lucide-react';
import { useAssetSource } from './renderers/assetSourceContext';
import { useSaveFile, useSaveLabel, type SaveState } from './useSaveFile';
import { cn } from '@/lib/utils';

// The cockpit's download links, in one place so the installed app's share-sheet path
// (useSaveFile) cannot be missing from one of them: the chat card, the preview header and the
// preview's download card. The artifacts panel's icon-only row draws SaveIcon itself.

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
