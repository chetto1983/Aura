import { useId, useRef, useState } from 'react';
import { ImagePlus, Images, Plus, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { assetDownloadUrl, type StudioImageRef } from './studioApi';
import { uploadStudioFrame } from './frameUpload';
import { useStudioLibrary } from './useStudio';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Popover, PopoverAnchor, PopoverContent } from '@/components/ui/popover';

// ImageTile — one slot of the composer's image row: the start frame, the end frame, or one
// reference. It is the only place in the Studio that reaches an asset route, and it owns the
// whole attach lifecycle so the bar above it deals in StudioImageRefs and nothing else.

interface ImageTileProps {
  /** What this slot is — the start frame, the end frame, a reference. Names the tile. */
  readonly label: string;
  readonly image: StudioImageRef | undefined;
  /** The slot's new contents: an image when one is attached, undefined when it is emptied.
   *  One callback rather than two, so every state of the tile has exactly one it can call. */
  readonly onChange: (image: StudioImageRef | undefined) => void;
}

const TILE =
  'studio-frame-tile size-14 shrink-0 rounded-[var(--radius-md)] border border-dashed border-border';

function reasonOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/** A slot the model cannot use. It stays on screen and says why: a tile that does nothing and
 *  says nothing is a bug report waiting to happen. It is its own component because it accepts
 *  nothing, so it needs no change handler to leave unused. */
export function DisabledTile({
  label,
  reason,
}: {
  readonly label: string;
  readonly reason: string;
}) {
  const reasonId = useId();
  return (
    <span
      role="button"
      aria-disabled="true"
      aria-label={label}
      aria-describedby={reasonId}
      className={`${TILE} grid cursor-not-allowed place-items-center text-text-disabled`}
    >
      <Plus aria-hidden="true" className="size-4" />
      <span id={reasonId} className="sr-only">
        {reason}
      </span>
    </span>
  );
}

export function ImageTile({ label, image, onChange }: ImageTileProps) {
  const { t } = useTranslation();
  const fileInput = useRef<HTMLInputElement>(null);
  const openingLibrary = useRef(false);
  const [libraryOpen, setLibraryOpen] = useState(false);
  const [uploading, setUploading] = useState<string>();
  const [failure, setFailure] = useState<string>();
  const library = useStudioLibrary(libraryOpen);

  if (image !== undefined) {
    return (
      <button
        type="button"
        onClick={() => {
          onChange(undefined);
        }}
        aria-label={t('studio.frames.remove', { name: image.file_name })}
        className={`${TILE} group relative overflow-hidden border-solid bg-surface-2 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none`}
      >
        <img
          src={assetDownloadUrl(image.id)}
          alt={image.file_name}
          className="size-full object-cover"
        />
        <span className="absolute inset-0 grid place-items-center bg-bg/70 opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100">
          <X aria-hidden="true" className="size-4 text-text" />
        </span>
      </button>
    );
  }

  async function attach(file: File) {
    setFailure(undefined);
    setUploading(file.name);
    try {
      onChange(await uploadStudioFrame(file, () => undefined));
    } catch (error) {
      setFailure(t('studio.frames.uploadFailed', { reason: reasonOf(error) }));
    } finally {
      setUploading(undefined);
    }
  }

  return (
    <div className="flex flex-col items-start gap-1">
      <Popover open={libraryOpen} onOpenChange={setLibraryOpen}>
        <DropdownMenu>
          <PopoverAnchor asChild>
            <DropdownMenuTrigger
              aria-label={label}
              disabled={uploading !== undefined}
              // The reference's tile sits at a slight angle; it is the one ornament the bar
              // keeps, and it straightens on hover so the target reads as a button.
              className={`${TILE} grid -rotate-3 place-items-center bg-surface-2 text-text-faint transition-transform hover:rotate-0 hover:text-text focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none disabled:opacity-60`}
            >
              <Plus aria-hidden="true" className="size-5" />
            </DropdownMenuTrigger>
          </PopoverAnchor>
          <DropdownMenuContent
            align="start"
            className="w-48"
            onCloseAutoFocus={(event) => {
              // Opening the picker hands focus to the popover; Radix would then return it to
              // this trigger, which the popover reads as focus leaving it and dismisses
              // itself the same tick it appeared.
              if (openingLibrary.current) event.preventDefault();
              openingLibrary.current = false;
            }}
          >
            <DropdownMenuItem
              onSelect={() => {
                fileInput.current?.click();
              }}
            >
              <ImagePlus aria-hidden="true" />
              {t('studio.frames.upload')}
            </DropdownMenuItem>
            <DropdownMenuItem
              onSelect={() => {
                openingLibrary.current = true;
                setLibraryOpen(true);
              }}
            >
              <Images aria-hidden="true" />
              {t('studio.frames.library')}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <PopoverContent
          align="start"
          className="w-64 rounded-[var(--radius-md)] border-border bg-surface p-2 shadow-[var(--shadow-popover)]"
        >
          <h2 className="mb-2 px-1 text-[11px] tracking-wide text-text-faint uppercase">
            {t('studio.frames.library')}
          </h2>
          {library.isPending ? (
            <p role="status" className="px-1 py-2 text-xs text-text-muted">
              {t('studio.frames.libraryLoading')}
            </p>
          ) : library.isError ? (
            <p role="alert" className="px-1 py-2 text-xs text-danger">
              {t('studio.error.generic')}
            </p>
          ) : library.data.length === 0 ? (
            // Empty-because-loading is not empty-because-absent, so this sentence is written
            // only once the route has actually answered.
            <p className="px-1 py-2 text-xs text-text-muted">{t('studio.frames.libraryEmpty')}</p>
          ) : (
            <ul className="grid max-h-56 grid-cols-3 gap-1.5 overflow-y-auto">
              {library.data.map((asset) => (
                <li key={asset.id}>
                  <button
                    type="button"
                    aria-label={asset.file_name}
                    onClick={() => {
                      setLibraryOpen(false);
                      onChange(asset);
                    }}
                    className="block aspect-square w-full overflow-hidden rounded-[var(--radius-sm)] border border-border focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                  >
                    <img
                      src={assetDownloadUrl(asset.id)}
                      alt={asset.file_name}
                      className="size-full object-cover"
                    />
                  </button>
                </li>
              ))}
            </ul>
          )}
        </PopoverContent>
      </Popover>

      <input
        ref={fileInput}
        type="file"
        accept="image/*"
        className="sr-only"
        aria-label={t('studio.frames.upload')}
        onChange={(event) => {
          const file = event.target.files?.[0];
          // The same file picked twice in a row fires no change event unless the input is
          // cleared, and a re-upload after a failure is exactly that case.
          event.target.value = '';
          if (file !== undefined) void attach(file);
        }}
      />

      {uploading === undefined ? null : (
        <span role="status" className="max-w-28 truncate text-[11px] text-text-faint">
          {t('studio.frames.uploading', { name: uploading })}
        </span>
      )}
      {failure === undefined ? null : (
        <span role="alert" className="max-w-40 text-[11px] text-danger">
          {failure}
        </span>
      )}
    </div>
  );
}
