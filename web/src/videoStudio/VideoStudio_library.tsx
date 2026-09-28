import { Film, Music } from 'lucide-react';
import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { assetDownloadUrl, type LibraryModality, type StudioAssetRef } from '../studio/studioApi';
import { useStudioLibrary } from '../studio/useStudio';
import { takesAsSource } from './VideoStudio_sources';

// VideoStudio_library.tsx — the identity's library inside the Studio's panels: the sounds "Add a
// sound" offers and the videos and pictures "Add a clip" offers, as stored in Garage, newest first
// and a page at a time (/api/studio/library, the same listing the image Studio's pickers read;
// "Show more" reads the next page, older than every asset shown). Picking one hands its
// asset over; nothing is uploaded again — the workspace probes it where it already is.

interface LibraryPickerProps {
  readonly modalities: readonly LibraryModality[];
  /** What the list says when the library holds none of its kinds. */
  readonly empty: string;
  readonly onPick: (asset: StudioAssetRef) => void;
}

/** A picture shows itself; a video or a sound is named, and marked by its kind. */
function AssetFace({ asset }: { readonly asset: StudioAssetRef }) {
  if (asset.mime_type.startsWith('image/')) {
    return (
      <img
        src={assetDownloadUrl(asset.id)}
        alt=""
        className="size-8 shrink-0 rounded-[var(--radius-sm)] object-cover"
      />
    );
  }
  const Kind = asset.mime_type.startsWith('audio/') ? Music : Film;
  return <Kind aria-hidden="true" className="size-4 shrink-0 text-text-muted" />;
}

export function LibraryPicker({ modalities, empty, onPick }: LibraryPickerProps) {
  const { t } = useTranslation();
  const id = useId();
  const library = useStudioLibrary(true, modalities);
  if (library.data === undefined) {
    return (
      <section aria-labelledby={id} className="grid gap-2">
        <LibraryTitle id={id} />
        {library.isPending ? (
          <p role="status" className="text-xs text-text-muted">
            {t('videoStudio.library.loading')}
          </p>
        ) : (
          <p role="alert" className="text-xs text-danger">
            {t('videoStudio.library.failed')}
          </p>
        )}
      </section>
    );
  }
  // Offered only what the door takes: the picture the probe would refuse is not listed.
  const offered = library.data.filter((asset) => takesAsSource(asset.mime_type));
  return (
    <section aria-labelledby={id} className="grid gap-2">
      <LibraryTitle id={id} />
      {offered.length > 0 ? (
        <ul className="grid max-h-48 gap-1 overflow-y-auto">
          {offered.map((asset) => (
            <li key={asset.id}>
              <button
                type="button"
                onClick={() => {
                  onPick(asset);
                }}
                className="flex min-h-11 w-full items-center gap-2 rounded-[var(--radius-sm)] border border-border px-2 py-1 text-left text-xs hover:bg-surface-2 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
              >
                <AssetFace asset={asset} />
                <span className="truncate">{asset.file_name}</span>
              </button>
            </li>
          ))}
        </ul>
      ) : library.hasNextPage ? null : (
        // Written only once the route has answered, and only when nothing older is left to read:
        // a page of assets this panel does not offer is not an empty library.
        <p className="text-xs text-text-muted">{t(empty)}</p>
      )}
      {library.isFetchNextPageError ? (
        <p role="alert" className="text-xs text-danger">
          {t('videoStudio.library.moreFailed')}
        </p>
      ) : null}
      {library.hasNextPage ? (
        <button
          type="button"
          disabled={library.isFetchingNextPage}
          onClick={() => {
            void library.fetchNextPage();
          }}
          className="min-h-11 rounded-[var(--radius-sm)] border border-border px-2 text-xs hover:bg-surface-2 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none disabled:opacity-60"
        >
          {library.isFetchingNextPage
            ? t('videoStudio.library.loadingMore')
            : t('videoStudio.library.more')}
        </button>
      ) : null}
    </section>
  );
}

function LibraryTitle({ id }: { readonly id: string }) {
  const { t } = useTranslation();
  return (
    <h3 id={id} className="text-xs font-medium">
      {t('videoStudio.library.title')}
    </h3>
  );
}
