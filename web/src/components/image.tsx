import { useState, type ComponentProps, type ReactNode } from 'react';
import { CheckIcon, CopyIcon, DownloadIcon, ImageIcon, ImageOffIcon, XIcon } from 'lucide-react';
import { cn } from '@/lib/utils';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog';

// Owned copy of the @assistant-ui/image registry item, kept as its compound elements (Root,
// Preview, Filename, Zoom, Actions) on the cockpit tokens. Changes from the installed source:
// - Zoom opens the cockpit's Radix Dialog from a native button, which already owns the focus
//   trap, Escape, focus return and scroll lock the registry wrote by hand.
// - Actions download through a caller-supplied href (the asset route of the active tier)
//   instead of rebuilding the file from the image source, and report a clipboard failure.
// - Every visible or accessible string is a prop, so the cockpit localizes it.
// - The message-part renderer, the generating and content-filter states, and the data-URI
//   decoding are gone: the cockpit renders images from asset IDs only, has its own generation
//   frame, and no provider result ever reports a content-filter block (spec §5).

const ACTION_CLASS =
  'inline-flex min-h-[44px] min-w-[44px] items-center justify-center rounded-[var(--radius-sm)] text-text-muted transition-colors hover:bg-surface-2 hover:text-text focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-50';

export function ImageRoot({ className, ...props }: ComponentProps<'div'>) {
  return (
    <div
      data-slot="image-root"
      className={cn(
        'relative w-full max-w-[512px] overflow-hidden rounded-[var(--radius-md)] border border-border bg-surface',
        className,
      )}
      {...props}
    />
  );
}

export function ImagePreview({ src, alt }: { readonly src: string; readonly alt: string }) {
  const [loadedSrc, setLoadedSrc] = useState<string>();
  const [errorSrc, setErrorSrc] = useState<string>();
  const loaded = loadedSrc === src;
  if (errorSrc === src) {
    return (
      <span
        data-slot="image-preview-error"
        role="img"
        aria-label={alt}
        className="flex min-h-32 items-center justify-center bg-surface-2 p-4"
      >
        <ImageOffIcon aria-hidden="true" className="size-8 text-text-faint" />
      </span>
    );
  }
  return (
    <span data-slot="image-preview" className="relative block min-h-32">
      {loaded ? null : (
        <span
          data-slot="image-preview-loading"
          className="absolute inset-0 flex items-center justify-center bg-surface-2"
        >
          <ImageIcon
            aria-hidden="true"
            className="size-8 animate-pulse text-text-faint motion-reduce:animate-none"
          />
        </span>
      )}
      <img
        src={src}
        alt={alt}
        className={cn('block h-auto w-full object-contain', !loaded && 'invisible')}
        onLoad={() => {
          setLoadedSrc(src);
        }}
        onError={() => {
          setErrorSrc(src);
        }}
      />
    </span>
  );
}

export function ImageFilename({ children }: { readonly children: ReactNode }) {
  return (
    <span data-slot="image-filename" className="block min-w-0 truncate text-xs text-text-muted">
      {children}
    </span>
  );
}

export interface ImageZoomLabels {
  /** Accessible name of the button that opens the full screen view. */
  readonly open: string;
  readonly close: string;
}

export function ImageZoom({
  src,
  alt,
  labels,
  children,
}: {
  readonly src: string;
  readonly alt: string;
  readonly labels: ImageZoomLabels;
  readonly children: ReactNode;
}) {
  return (
    <Dialog>
      <DialogTrigger asChild>
        <button
          type="button"
          aria-label={labels.open}
          className="block w-full cursor-zoom-in text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent"
        >
          {children}
        </button>
      </DialogTrigger>
      <DialogContent
        showCloseButton={false}
        aria-describedby={undefined}
        className="h-[96dvh] w-[96vw] max-w-[96vw] items-center justify-center border-0 bg-bg/95 p-4"
      >
        <DialogTitle className="sr-only">{alt}</DialogTitle>
        <img src={src} alt={alt} className="max-h-full max-w-full object-contain" />
        <DialogClose
          aria-label={labels.close}
          className="absolute end-3 top-3 grid size-11 place-items-center rounded-full bg-surface/80 text-text-muted transition-colors hover:text-text focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <XIcon aria-hidden="true" className="size-5" />
        </DialogClose>
      </DialogContent>
    </Dialog>
  );
}

export interface ImageActionsLabels {
  readonly download: string;
  readonly copy: string;
  readonly copied: string;
  readonly copyFailed: string;
}

// Copies the image already on screen: `src` is the preview's own object URL, so this reads
// local bytes and asks no server for anything.
async function copyImage(src: string): Promise<void> {
  const image = await (await fetch(src)).blob();
  await navigator.clipboard.write([new ClipboardItem({ [image.type]: image })]);
}

export function ImageActions({
  src,
  downloadHref,
  fileName,
  labels,
  className,
}: {
  readonly src: string;
  readonly downloadHref: string;
  readonly fileName: string;
  readonly labels: ImageActionsLabels;
  readonly className?: string;
}) {
  const [copyState, setCopyState] = useState<'idle' | 'pending' | 'copied' | 'error'>('idle');
  async function copy() {
    setCopyState('pending');
    try {
      await copyImage(src);
      setCopyState('copied');
    } catch {
      setCopyState('error');
    }
  }
  return (
    <div data-slot="image-actions" className={cn('flex items-center gap-1', className)}>
      {copyState === 'error' ? (
        <span role="alert" className="text-xs text-danger">
          {labels.copyFailed}
        </span>
      ) : null}
      <a
        href={downloadHref}
        download={fileName}
        data-slot="image-download"
        data-required-touch-target
        aria-label={labels.download}
        className={ACTION_CLASS}
      >
        <DownloadIcon aria-hidden="true" className="size-4" />
      </a>
      <button
        type="button"
        data-slot="image-copy"
        data-required-touch-target
        disabled={copyState === 'pending'}
        aria-label={copyState === 'copied' ? labels.copied : labels.copy}
        onClick={() => {
          void copy();
        }}
        className={ACTION_CLASS}
      >
        {copyState === 'copied' ? (
          <CheckIcon aria-hidden="true" className="size-4" />
        ) : (
          <CopyIcon aria-hidden="true" className="size-4" />
        )}
      </button>
    </div>
  );
}
