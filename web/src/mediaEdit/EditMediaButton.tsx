import { Pencil } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAssetSource } from '../chat/artifacts/renderers/assetSourceContext';
import { editableKind, type EditKind } from './editRules';
import { useOpenEditor } from './mediaEditorContext';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

// The only thing a surface adds to offer editing. It renders nothing on a non-editable source
// (the share pages), outside the shell's provider, or for a type the editors do not take.

interface EditMediaButtonProps {
  readonly assetId: string;
  readonly kind: EditKind;
  /** When the surface knows it; the Studio stage does not, and the editor resolves it. */
  readonly mimeType?: string;
  readonly fileName?: string;
  readonly compact?: boolean;
  readonly className?: string;
  /** Runs before the editor opens: a modal closes itself here. */
  readonly onOpen?: () => void;
}

export function EditMediaButton({
  assetId,
  kind,
  mimeType,
  fileName,
  compact = false,
  className,
  onOpen,
}: EditMediaButtonProps) {
  const { t } = useTranslation();
  const { editable } = useAssetSource();
  const open = useOpenEditor();
  if (editable !== true || open === undefined) return null;
  if (mimeType !== undefined && mimeType !== '' && editableKind(mimeType) !== kind) return null;
  const label =
    fileName === undefined ? t('mediaEdit.edit') : t('mediaEdit.editName', { name: fileName });
  return (
    <Button
      type="button"
      size="sm"
      variant="ghost"
      aria-label={label}
      data-required-touch-target
      onClick={() => {
        onOpen?.();
        open({ assetId, kind });
      }}
      // No min-h here: size="sm" carries the 44px floor data-required-touch-target promises.
      className={cn('gap-1.5 text-xs', compact && 'min-w-[44px]', className)}
    >
      <Pencil aria-hidden="true" className="size-3.5" />
      {compact ? null : t('mediaEdit.edit')}
    </Button>
  );
}
