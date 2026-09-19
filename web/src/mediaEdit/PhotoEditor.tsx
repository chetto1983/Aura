import { useQueryClient } from '@tanstack/react-query';
import { Download, Library, X } from 'lucide-react';
import { useMemo, useRef, useState } from 'react';
import FilerobotImageEditor, {
  TABS,
  TOOLS,
  type getCurrentImgDataFunction,
} from 'react-filerobot-image-editor';
import { useTranslation } from 'react-i18next';
import { StyleSheetManager } from 'styled-components';
import { uploadStudioFrame } from '../studio/frameUpload';
import { StudioError } from '../studio/studioApi';
import { studioKeys, useStudioLibrary } from '../studio/useStudio';
import { downloadBlob } from './download';
import { editedBase, imageExtension } from './editRules';
import { filerobotTheme, filerobotTranslations, forwardDomProp } from './filerobotSetup';
import { MediaEditorLayer } from './MediaEditorLayer';
import { useObjectUrl } from './useObjectUrl';
import type { EditorProps } from './VideoEditor';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';

// PhotoEditor — Filerobot inside Aura's layer, with Aura's own header: Download and Save to
// library are both available from the start (spec, adversarial review finding 2), and the Studio's
// availability is read from the library query before anything is uploaded.

type Action = 'download' | 'library';

type SaveState =
  | { readonly kind: 'idle' }
  | { readonly kind: 'saving'; readonly progress: number }
  | { readonly kind: 'saved' }
  | { readonly kind: 'failed'; readonly reason: string; readonly action: Action };

const QUALITY = 0.92;

function reason(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export default function PhotoEditor({ asset, source, onClose }: EditorProps) {
  const { t, i18n } = useTranslation();
  const client = useQueryClient();
  const url = useObjectUrl(source);
  const exportRef = useRef<getCurrentImgDataFunction | undefined>(undefined);
  const theme = useMemo(() => filerobotTheme(), []);
  const library = useStudioLibrary(true);
  const [dirty, setDirty] = useState(false);
  const [state, setState] = useState<SaveState>({ kind: 'idle' });
  const [confirmClose, setConfirmClose] = useState(false);

  const studioOff = library.error instanceof StudioError && library.error.status === 503;
  // Cached rows say nothing about the Studio now: upload only after a fresh answer that is not a
  // refusal, so a background refetch that ends in 503 never races an orphan upload.
  const studioReady = library.isSuccess && !library.isFetching;
  const extension = imageExtension(asset.mime_type);
  const base = editedBase(asset.file_name, t('mediaEdit.suffix.image'));
  const fileName = `${base}.${extension}`;
  const translations = filerobotTranslations(i18n.language);

  async function editedImage(): Promise<Blob> {
    const exportImage = exportRef.current;
    if (exportImage === undefined) throw new Error(t('mediaEdit.photo.notReady'));
    const { imageData } = exportImage({ name: base, extension, quality: QUALITY }, 1);
    if (imageData.imageBase64 === undefined) throw new Error(t('mediaEdit.photo.noImage'));
    return (await fetch(imageData.imageBase64)).blob();
  }

  async function download() {
    try {
      downloadBlob(await editedImage(), fileName);
    } catch (error) {
      setState({ kind: 'failed', reason: reason(error), action: 'download' });
    }
  }

  async function saveToLibrary() {
    setState({ kind: 'saving', progress: 0 });
    try {
      const blob = await editedImage();
      await uploadStudioFrame(new File([blob], fileName, { type: blob.type }), (progress) => {
        setState({ kind: 'saving', progress });
      });
      await client.invalidateQueries({ queryKey: studioKeys.library() });
      setDirty(false);
      setState({ kind: 'saved' });
    } catch (error) {
      setState({ kind: 'failed', reason: reason(error), action: 'library' });
    }
  }

  function requestClose() {
    if (dirty) setConfirmClose(true);
    else onClose();
  }

  const saving = state.kind === 'saving';

  return (
    <MediaEditorLayer
      label={t('mediaEdit.editName', { name: asset.file_name })}
      onEscape={requestClose}
    >
      <header className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">
        <h2 className="min-w-0 flex-1 truncate font-mono text-sm">{asset.file_name}</h2>
        <div role="status" className="text-xs text-text-muted tabular-nums">
          {state.kind === 'saving'
            ? t('mediaEdit.photo.saving', { percent: Math.round(state.progress * 100) })
            : null}
          {state.kind === 'saved' ? t('mediaEdit.photo.saved') : null}
          {studioOff ? t('mediaEdit.photo.studioOff') : null}
        </div>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          disabled={saving}
          onClick={() => void download()}
        >
          <Download aria-hidden="true" className="size-4" />
          {t('mediaEdit.photo.download')}
        </Button>
        <Button
          type="button"
          size="sm"
          disabled={saving || !studioReady}
          onClick={() => void saveToLibrary()}
        >
          <Library aria-hidden="true" className="size-4" />
          {t('mediaEdit.photo.saveLibrary')}
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-label={t('mediaEdit.close')}
          onClick={requestClose}
        >
          <X aria-hidden="true" className="size-4" />
        </Button>
      </header>
      {state.kind === 'failed' ? (
        <div
          role="alert"
          className="flex items-center gap-2 border-b border-danger/40 px-4 py-2 text-sm text-danger"
        >
          {t('mediaEdit.photo.failed', { reason: state.reason })}
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={state.action === 'library' && !studioReady}
            onClick={() => void (state.action === 'download' ? download() : saveToLibrary())}
          >
            {t('mediaEdit.photo.retry')}
          </Button>
        </div>
      ) : null}
      <div className="min-h-0 flex-1">
        {url === undefined ? (
          <p role="status" className="p-4 text-sm text-text-muted">
            {t('mediaEdit.loading')}
          </p>
        ) : (
          <StyleSheetManager shouldForwardProp={forwardDomProp}>
            <FilerobotImageEditor
              source={url}
              savingPixelRatio={1}
              previewPixelRatio={window.devicePixelRatio || 1}
              useBackendTranslations={false}
              language={translations === undefined ? 'en' : 'it'}
              {...(translations === undefined ? {} : { translations })}
              theme={theme}
              tabsIds={[TABS.ADJUST, TABS.FINETUNE, TABS.FILTERS, TABS.ANNOTATE, TABS.RESIZE]}
              defaultTabId={TABS.ADJUST}
              defaultToolId={TOOLS.CROP}
              Rotate={{ angle: 90, componentType: 'buttons' }}
              removeSaveButton
              // Its default registers a beforeunload prompt; Aura's ConfirmDialog is the only one.
              avoidChangesNotSavedAlertOnLeave
              getCurrentImgDataFnRef={exportRef}
              onModify={() => {
                setDirty(true);
                setState((current) => (current.kind === 'saved' ? { kind: 'idle' } : current));
              }}
            />
          </StyleSheetManager>
        )}
      </div>
      <ConfirmDialog
        open={confirmClose}
        onOpenChange={setConfirmClose}
        title={t('mediaEdit.photo.discardTitle')}
        description={t('mediaEdit.photo.discardBody')}
        cancelLabel={t('mediaEdit.photo.keep')}
        confirmLabel={t('mediaEdit.photo.discard')}
        confirmVariant="destructive"
        onConfirm={onClose}
      />
    </MediaEditorLayer>
  );
}
