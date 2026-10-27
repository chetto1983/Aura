import { lazy, Suspense, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Pencil } from 'lucide-react';
import { useAssetSource } from './assetSourceContext';
import { PreviewLoading, type RendererProps } from './PreviewStatus';
import type { CraftApp } from './CraftFrame';
import { PreviewAction } from './PreviewAction';
import { previewActionClass } from './previewActionsContext';

const CraftFrame = lazy(() => import('./CraftFrame'));

// A file a light viewer shows and a Craft app edits (docs/superpowers/specs/2026-10-19-craft-web-apps-design.md):
// the viewer stays the preview, so reading costs no app download, and Edit swaps the app in. It
// is offered only where the source can store what the app saves: the file manager's tier writes
// it beside the file, the chat's attaches it to the next message (composerSource). An edit made
// anywhere else could only be lost. Edit sits in the host's header, beside its download.
export default function EditInCraft({
  app,
  asset,
  children,
}: {
  readonly app: CraftApp;
  readonly asset: RendererProps;
  readonly children: ReactNode;
}) {
  const { t } = useTranslation();
  const { save } = useAssetSource();
  const [editing, setEditing] = useState(false);
  if (editing) {
    return (
      <Suspense fallback={<PreviewLoading />}>
        <CraftFrame app={app} {...asset} />
      </Suspense>
    );
  }
  if (save === undefined) return children;
  return (
    <>
      {children}
      <PreviewAction>
        <button
          type="button"
          className={previewActionClass}
          onClick={() => {
            setEditing(true);
          }}
        >
          <Pencil aria-hidden="true" className="size-4" />
          {t('artifacts.preview.edit')}
        </button>
      </PreviewAction>
    </>
  );
}
