import { MessageSquareText, Pencil, RefreshCw, Trash2, X } from 'lucide-react';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import type { DocumentItem } from './documentApi';

interface DocumentActionMenuProps {
  readonly document: DocumentItem | undefined;
  readonly open: boolean;
  readonly onClose: () => void;
  readonly onAsk: () => void;
  readonly onEditTags: () => void;
  readonly onRetry: () => void;
  readonly onDelete: () => void;
}

export function DocumentActionMenu({
  document,
  open,
  onClose,
  onAsk,
  onEditTags,
  onRetry,
  onDelete,
}: DocumentActionMenuProps) {
  const { t } = useTranslation();
  if (!open || document === undefined) return null;
  const menuLabel = `Actions for ${document.title}`;
  return (
    <div className="absolute right-6 top-32 z-20 w-56 rounded-md border border-border bg-surface p-1 shadow-popover">
      <div className="flex items-center justify-between px-2 py-1 text-[12px] font-semibold text-text-muted">
        <span className="truncate">{document.title}</span>
        <Button type="button" size="icon" variant="ghost" aria-label={t('documents.actions.close')} onClick={onClose}>
          <X aria-hidden="true" />
        </Button>
      </div>
      <div role="menu" aria-label={menuLabel} className="grid gap-1">
        <MenuButton
          icon={<MessageSquareText aria-hidden="true" />}
          label={t('documents.actions.askDocument')}
          onClick={onAsk}
        />
        <MenuButton
          icon={<Pencil aria-hidden="true" />}
          label={t('documents.actions.editTags')}
          onClick={onEditTags}
        />
        {document.status === 'failed' ? (
          <MenuButton
            icon={<RefreshCw aria-hidden="true" />}
            label={t('documents.actions.retryProcessing')}
            onClick={onRetry}
          />
        ) : null}
        <MenuButton
          danger
          icon={<Trash2 aria-hidden="true" />}
          label={t('documents.actions.delete')}
          onClick={onDelete}
        />
      </div>
    </div>
  );
}

function MenuButton({
  icon,
  label,
  danger = false,
  onClick,
}: {
  readonly icon: ReactNode;
  readonly label: string;
  readonly danger?: boolean;
  readonly onClick: () => void;
}) {
  return (
    <button
      type="button"
      role="menuitem"
      className={`flex min-h-10 items-center gap-2 rounded-sm px-3 text-left text-[14px] outline-none hover:bg-surface-2 focus-visible:ring-2 focus-visible:ring-ring ${
        danger ? 'text-danger' : 'text-text'
      }`}
      onClick={onClick}
    >
      {icon}
      <span>{label}</span>
    </button>
  );
}
