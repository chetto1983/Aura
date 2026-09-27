import { Music, Plus, Scissors, SlidersHorizontal, Trash2, Type } from 'lucide-react';
import type { RefObject } from 'react';
import { useTranslation } from 'react-i18next';

interface StudioRailProps {
  readonly canAddTitle: boolean;
  readonly canRemove: boolean;
  readonly onAddSource: () => void;
  readonly onAddAudio: () => void;
  readonly onAddTitle: () => void;
  readonly onSplit: () => void;
  readonly onShowProperties: () => void;
  readonly onRemove: () => void;
}

/** The desktop tool rail. The narrow layout has its own bar (VideoStudio_mobile.tsx); both drive
 *  the handlers the workspace owns. */
export function StudioRail({
  canAddTitle,
  canRemove,
  onAddSource,
  onAddAudio,
  onAddTitle,
  onSplit,
  onShowProperties,
  onRemove,
}: StudioRailProps) {
  const { t } = useTranslation();
  return (
    <div role="toolbar" aria-label={t('videoStudio.commands')} className="video-studio-rail">
      <button
        type="button"
        className="video-studio-rail-button"
        data-primary="true"
        onClick={onAddSource}
      >
        <Plus aria-hidden="true" />
        <span>{t('videoStudio.command.addSource')}</span>
      </button>
      <button type="button" className="video-studio-rail-button" onClick={onAddAudio}>
        <Music aria-hidden="true" />
        <span>{t('videoStudio.audio.add')}</span>
      </button>
      <button
        type="button"
        className="video-studio-rail-button"
        disabled={!canAddTitle}
        onClick={onAddTitle}
      >
        <Type aria-hidden="true" />
        <span>{t('videoStudio.command.addText')}</span>
      </button>
      <button type="button" className="video-studio-rail-button" onClick={onSplit}>
        <Scissors aria-hidden="true" />
        <span>{t('videoStudio.command.split')}</span>
      </button>
      <button type="button" className="video-studio-rail-button" onClick={onShowProperties}>
        <SlidersHorizontal aria-hidden="true" />
        <span>{t('videoStudio.inspector.label')}</span>
      </button>
      <button
        type="button"
        className="video-studio-rail-button"
        disabled={!canRemove}
        onClick={onRemove}
      >
        <Trash2 aria-hidden="true" />
        <span>{t('videoStudio.command.remove')}</span>
      </button>
    </div>
  );
}

interface FilePickerProps {
  readonly inputRef: RefObject<HTMLInputElement | null>;
  readonly accept: string;
  readonly label: string;
  readonly onFile: (file: File) => void;
}

/** A hidden file input the rail's buttons open. It is cleared on every pick: the same file picked
 *  twice in a row fires no change otherwise, and a retry after a refusal is exactly that case. */
export function FilePicker({ inputRef, accept, label, onFile }: FilePickerProps) {
  return (
    <input
      ref={inputRef}
      type="file"
      accept={accept}
      className="sr-only"
      aria-label={label}
      onChange={(event) => {
        const file = event.target.files?.[0];
        event.target.value = '';
        if (file !== undefined) onFile(file);
      }}
    />
  );
}
