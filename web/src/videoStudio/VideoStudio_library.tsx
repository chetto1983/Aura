import { Film, Music } from 'lucide-react';
import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { assetDownloadUrl, type LibraryModality, type StudioAssetRef } from '../studio/studioApi';
import { useStudioLibrary } from '../studio/useStudio';

// VideoStudio_library.tsx — the identity's library inside the Studio's panels: the sounds "Add a
// sound" offers and the videos and pictures "Add a clip" offers, as stored in Garage, newest first
// (/api/studio/library, the same listing the image Studio's pickers read). Picking one hands its
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
  return (
    <section aria-labelledby={id} className="grid gap-2">
      <h3 id={id} className="text-xs font-medium">
        {t('videoStudio.library.title')}
      </h3>
      {library.isPending ? (
        <p role="status" className="text-xs text-text-muted">
          {t('videoStudio.library.loading')}
        </p>
      ) : library.isError ? (
        <p role="alert" className="text-xs text-danger">
          {t('videoStudio.library.failed')}
        </p>
      ) : library.data.length === 0 ? (
        // Written only once the route has answered: empty-because-loading is not empty.
        <p className="text-xs text-text-muted">{t(empty)}</p>
      ) : (
        <ul className="grid max-h-48 gap-1 overflow-y-auto">
          {library.data.map((asset) => (
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
      )}
    </section>
  );
}
