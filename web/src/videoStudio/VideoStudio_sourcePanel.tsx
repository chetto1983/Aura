import { Upload } from 'lucide-react';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import type { LibraryModality, StudioAssetRef } from '../studio/studioApi';
import { LibraryPicker } from './VideoStudio_library';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';

// VideoStudio_sourcePanel.tsx — the dialog an Add action opens: a file from the device, an asset
// already in the library, and whatever else that kind of source can come from (the audio panel
// adds the microphone and Aura's voice as children). Either door closes the panel first.

interface SourcePanelProps {
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly title: string;
  readonly description: string;
  readonly upload: string;
  readonly modalities: readonly LibraryModality[];
  /** What the library says when it holds none of `modalities`. */
  readonly empty: string;
  readonly onUpload: () => void;
  readonly onLibrary: (asset: StudioAssetRef) => void;
  readonly children?: ReactNode;
}

export function SourcePanel({
  open,
  onOpenChange,
  title,
  description,
  upload,
  modalities,
  empty,
  onUpload,
  onLibrary,
  children,
}: SourcePanelProps) {
  const { t } = useTranslation();
  const close = () => {
    onOpenChange(false);
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(title)}</DialogTitle>
          <DialogDescription>{t(description)}</DialogDescription>
        </DialogHeader>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => {
            close();
            onUpload();
          }}
        >
          <Upload aria-hidden="true" />
          {t(upload)}
        </Button>
        <LibraryPicker
          modalities={modalities}
          empty={empty}
          onPick={(asset) => {
            close();
            onLibrary(asset);
          }}
        />
        {children}
      </DialogContent>
    </Dialog>
  );
}
