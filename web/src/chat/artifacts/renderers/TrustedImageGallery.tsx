// Adapted from Tool UI Image Gallery's registry grid and lightbox at revision
// 49a870286facdbf28160cd647f0d337ebdc9b275 (image-gallery.json SHA-256
// 56305605e86fdc6b9a24ff6b29be394e9ce98fbe254161ad8dc8f4a2941f3a8d).
// MIT notice: see components/LICENSE.tool-ui.
// Aura owns the authorized blobs, decoded dimensions, downloads and dialog focus.

import { useCallback, useEffect, useRef, useState } from 'react';
import { Download, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { LocalArtifactDisplay } from '../../displays/LocalArtifactDisplay';
import type { DisplayArtifact } from '../../displays/types';
import { useBlobPreview } from '../useBlobPreview';
import { useAssetSource } from './assetSourceContext';
import {
  groupTrustedImages,
  MAX_GALLERY_IMAGES,
  type GalleryCandidate,
  type GalleryItem,
} from './galleryGrouping';
import { PreviewLoading } from './PreviewStatus';
import { Dialog, DialogClose, DialogContent, DialogTitle } from '@/components/ui/dialog';

export function ToolImageGallery({
  id,
  images,
}: {
  readonly id: string;
  readonly images: readonly GalleryItem[];
}) {
  const { t } = useTranslation();
  const { assetUrl } = useAssetSource();
  const [active, setActive] = useState<number | null>(null);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const selected = active === null ? undefined : images[active];
  return (
    <article
      data-slot="image-gallery"
      data-tool-ui-id={id}
      className="w-full max-w-[768px] rounded-[var(--radius-md)] border border-border bg-surface p-3"
    >
      <h3 className="mb-3 text-sm font-medium text-text">{t('artifacts.gallery.title')}</h3>
      <div role="list" className="columns-2 gap-2 sm:columns-3">
        {images.map((image, index) => (
          <div key={image.assetId} role="listitem" className="mb-2 break-inside-avoid">
            <button
              type="button"
              data-required-touch-target
              aria-label={t('artifacts.gallery.open', { name: image.alt })}
              onClick={(event) => {
                triggerRef.current = event.currentTarget;
                setActive(index);
              }}
              className="block min-h-11 w-full overflow-hidden rounded-[var(--radius-sm)] border border-border bg-surface-2 text-left focus-visible:outline-2 focus-visible:outline-accent"
            >
              <img
                src={image.src}
                alt=""
                width={image.width}
                height={image.height}
                className="block h-auto w-full object-contain"
              />
              <span className="block truncate px-2 py-1 text-xs text-text-muted">{image.alt}</span>
            </button>
          </div>
        ))}
      </div>
      <Dialog
        open={selected !== undefined}
        onOpenChange={(open) => {
          if (!open) setActive(null);
        }}
      >
        {selected !== undefined ? (
          <DialogContent
            showCloseButton={false}
            aria-describedby={undefined}
            onCloseAutoFocus={(event) => {
              event.preventDefault();
              triggerRef.current?.focus();
            }}
            className="flex h-[96dvh] w-[96vw] max-w-[96vw] flex-col items-center justify-center gap-3 border-0 bg-bg/95 p-4"
          >
            <DialogTitle className="sr-only">{selected.alt}</DialogTitle>
            <img
              src={selected.src}
              alt={selected.alt}
              className="max-h-[80dvh] h-auto max-w-full w-auto object-contain"
            />
            <div className="flex items-center gap-3 text-sm text-text">
              <span>{selected.alt}</span>
              <a
                href={assetUrl(selected.assetId)}
                download={selected.alt}
                data-required-touch-target
                aria-label={t('artifacts.gallery.download', { name: selected.alt })}
                className="inline-flex min-h-11 min-w-11 items-center justify-center rounded-[var(--radius-sm)] bg-surface px-3 focus-visible:outline-2 focus-visible:outline-accent"
              >
                <Download aria-hidden className="size-4" />
              </a>
            </div>
            <DialogClose
              aria-label={t('artifacts.gallery.close')}
              className="absolute end-3 top-3 grid size-11 place-items-center rounded-full bg-surface/80 text-text focus-visible:outline-2 focus-visible:outline-accent"
            >
              <X aria-hidden className="size-5" />
            </DialogClose>
          </DialogContent>
        ) : null}
      </Dialog>
    </article>
  );
}

interface ArtifactImage {
  readonly artifact: DisplayArtifact;
  readonly assetId: string;
}

function GalleryProbe({
  item,
  onDecoded,
  onFailed,
}: {
  readonly item: ArtifactImage;
  readonly onDecoded: (item: GalleryItem) => void;
  readonly onFailed: (assetId: string) => void;
}) {
  const { url, error } = useBlobPreview(item.assetId, item.artifact.mime_type);
  // A failed authenticated fetch cannot fire an <img> error, so signal it from an effect.
  // The callback is stable in GalleryPayload and state updates remain outside render.
  useEffect(() => {
    if (error === undefined) return;
    let current = true;
    queueMicrotask(() => {
      if (current) onFailed(item.assetId);
    });
    return () => {
      current = false;
    };
  }, [error, item.assetId, onFailed]);
  if (url === undefined) return null;
  return (
    <img
      src={url}
      alt=""
      aria-hidden="true"
      className="pointer-events-none absolute size-px opacity-0"
      onLoad={(event) => {
        const { naturalWidth, naturalHeight } = event.currentTarget;
        if (naturalWidth === 0 || naturalHeight === 0) {
          onFailed(item.assetId);
          return;
        }
        onDecoded({
          assetId: item.assetId,
          src: url,
          width: naturalWidth,
          height: naturalHeight,
          alt: item.artifact.filename,
        });
      }}
      onError={() => {
        onFailed(item.assetId);
      }}
    />
  );
}

function GalleryPayload({
  turnId,
  items,
}: {
  readonly turnId: string;
  readonly items: readonly ArtifactImage[];
}) {
  const [decoded, setDecoded] = useState<Record<string, GalleryItem | null>>({});
  const onDecoded = useCallback((item: GalleryItem) => {
    setDecoded((current) => ({ ...current, [item.assetId]: item }));
  }, []);
  const onFailed = useCallback((assetId: string) => {
    setDecoded((current) => ({ ...current, [assetId]: null }));
  }, []);
  const complete = items.every((item) => decoded[item.assetId] !== undefined);
  const candidates: GalleryCandidate[] = items.flatMap((item) => {
    const record = decoded[item.assetId];
    return record === undefined || record === null ? [] : [{ ...record, turnId }];
  });
  const groups =
    complete && candidates.length === items.length ? groupTrustedImages(candidates) : [];
  return (
    <div className="relative">
      {items.map((item) => (
        <GalleryProbe key={item.assetId} item={item} onDecoded={onDecoded} onFailed={onFailed} />
      ))}
      {groups[0] !== undefined ? (
        <ToolImageGallery id={turnId} images={groups[0]} />
      ) : complete ? (
        <div className="space-y-2">
          {items.map((item) => (
            <LocalArtifactDisplay key={item.assetId} payload={{ artifact: item.artifact }} />
          ))}
        </div>
      ) : (
        <PreviewLoading />
      )}
    </div>
  );
}

export function TrustedImageGallery({
  turnId,
  artifacts,
}: {
  readonly turnId: string;
  readonly artifacts: readonly DisplayArtifact[];
}) {
  const { assetUrl } = useAssetSource();
  const items = artifacts.flatMap((artifact): ArtifactImage[] =>
    artifact.asset_id === undefined ? [] : [{ artifact, assetId: artifact.asset_id }],
  );
  if (
    items.length !== artifacts.length ||
    items.length < 2 ||
    items.length > MAX_GALLERY_IMAGES ||
    new Set(items.map((item) => item.assetId)).size !== items.length
  ) {
    return (
      <div className="space-y-2">
        {artifacts.map((artifact, index) => (
          <LocalArtifactDisplay
            key={`${artifact.asset_id ?? 'missing'}-${String(index)}`}
            payload={{ artifact }}
          />
        ))}
      </div>
    );
  }
  const sourceKey = items.map((item) => assetUrl(item.assetId)).join('|');
  return <GalleryPayload key={sourceKey} turnId={turnId} items={items} />;
}
