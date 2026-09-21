import { FolderOpen, X } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { IParsedEntity } from '@svar-ui/react-filemanager';
import FilesWorkspace from '../files/FilesWorkspace';
import { editableKindForFileName } from './editRules';
import { MediaEditorLayer } from './MediaEditorLayer';
import { useOpenEditor } from './mediaEditorContext';
import { Button } from '@/components/ui/button';

export function GarageMediaPicker({ className }: { readonly className?: string }) {
  const { t } = useTranslation();
  const openEditor = useOpenEditor();
  const [open, setOpen] = useState(false);
  const [problem, setProblem] = useState<string>();

  if (openEditor === undefined) return null;
  const launchEditor = openEditor;

  function select(file: IParsedEntity) {
    const kind = editableKindForFileName(file.name);
    if (kind === undefined) {
      setProblem(t('mediaEdit.picker.unsupported'));
      return;
    }
    launchEditor({
      garageObjectId: file.id,
      fileName: file.name,
      sizeBytes: file.size ?? 0,
      kind,
    });
    setProblem(undefined);
    setOpen(false);
  }

  return (
    <div className={className}>
      <Button
        type="button"
        size="sm"
        variant="ghost"
        className="min-h-8 gap-1.5 py-1 text-xs"
        onClick={() => {
          setProblem(undefined);
          setOpen(true);
        }}
      >
        <FolderOpen aria-hidden="true" className="size-3.5" />
        {t('mediaEdit.picker.action')}
      </Button>
      {open ? (
        <MediaEditorLayer
          label={t('mediaEdit.picker.title')}
          onEscape={() => {
            setOpen(false);
          }}
        >
          <header className="flex flex-none items-center gap-3 border-b border-border bg-surface px-3 py-2 sm:px-4">
            <div className="min-w-0 flex-1">
              <h2 className="truncate font-display text-base font-semibold text-text">
                {t('mediaEdit.picker.title')}
              </h2>
              <p className="truncate text-xs text-text-muted">
                {t('mediaEdit.picker.description')}
              </p>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              aria-label={t('mediaEdit.close')}
              onClick={() => {
                setOpen(false);
              }}
            >
              <X aria-hidden="true" className="size-4" />
            </Button>
          </header>
          {problem === undefined ? null : (
            <p role="alert" className="border-b border-danger/30 px-4 py-2 text-sm text-danger">
              {problem}
            </p>
          )}
          <div className="min-h-0 flex-1">
            <FilesWorkspace onOpenFile={select} />
          </div>
        </MediaEditorLayer>
      ) : null}
    </div>
  );
}
