import { Suspense, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { DocumentRow } from './documentsApi';
import { previewKind } from '@/chat/artifacts/artifactMeta';
import { AssetSourceContext } from '@/chat/artifacts/renderers/assetSourceContext';
import { PreviewLoading } from '@/chat/artifacts/renderers/PreviewStatus';
import { PreviewActionsContext } from '@/chat/artifacts/renderers/previewActionsContext';
import { PreviewByKind } from '@/chat/artifacts/renderers/previewDispatch';
import { directURL, FILE_MANAGER_SOURCE } from '@/files/filesApi';

export function DocumentPreview({ doc }: { readonly doc: DocumentRow }) {
  const { t } = useTranslation();
  const [actions, setActions] = useState<HTMLElement | null>(null);
  const id = `/${doc.source_key}`;
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex min-h-12 flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-2">
        <h2 className="text-xs font-semibold tracking-wider text-text-muted uppercase">
          {t('documents.detail.preview')}
        </h2>
        <div className="flex items-center gap-3">
          <span ref={setActions} className="flex items-center gap-3" />
          <a
            className="inline-flex min-h-[44px] items-center text-sm text-info underline"
            href={directURL(id, true)}
          >
            {t('artifacts.preview.downloadOriginal')}
          </a>
        </div>
      </div>
      <div className="min-h-0 flex-1">
        <AssetSourceContext.Provider value={FILE_MANAGER_SOURCE}>
          <PreviewActionsContext.Provider value={actions}>
            <Suspense fallback={<PreviewLoading />}>
              <PreviewByKind
                kind={previewKind('', doc.file_name, doc.file_type)}
                asset={{ assetId: id, mimeType: '', fileName: doc.file_name }}
                downloadFallback={
                  <a
                    href={directURL(id, true)}
                    className="grid h-full place-items-center text-sm text-info underline"
                  >
                    {t('artifacts.preview.downloadFallback')}
                  </a>
                }
              />
            </Suspense>
          </PreviewActionsContext.Provider>
        </AssetSourceContext.Provider>
      </div>
    </div>
  );
}
