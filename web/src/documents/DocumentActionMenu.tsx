import { MessageSquareText, MoreHorizontal, Pencil, RefreshCw, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { DocumentItem } from './documentApi';
import { failedStatuses } from './documentViewModel';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';

interface DocumentActionMenuProps {
  readonly document: DocumentItem;
  readonly onOpen?: () => void;
  readonly onAsk: (document: DocumentItem) => void;
  readonly onEditTags: (document: DocumentItem) => void;
  readonly onRetry: (document: DocumentItem) => void;
  readonly onDelete: (document: DocumentItem) => void;
}

export function DocumentActionMenu({
  document,
  onOpen,
  onAsk,
  onEditTags,
  onRetry,
  onDelete,
}: DocumentActionMenuProps) {
  const { t } = useTranslation();
  const menuLabel = `Actions for ${document.title}`;
  return (
    <DropdownMenu
      onOpenChange={(nextOpen) => {
        if (nextOpen) onOpen?.();
      }}
    >
      <DropdownMenuTrigger asChild>
        <Button type="button" size="icon" variant="ghost" aria-label={menuLabel}>
          <MoreHorizontal aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent aria-label={menuLabel}>
        <DropdownMenuLabel className="max-w-52 truncate">{document.title}</DropdownMenuLabel>
        <DropdownMenuGroup>
          <DropdownMenuItem
            onSelect={() => {
              onAsk(document);
            }}
          >
            <MessageSquareText aria-hidden="true" />
            {t('documents.actions.askDocument')}
          </DropdownMenuItem>
          <DropdownMenuItem
            onSelect={() => {
              onEditTags(document);
            }}
          >
            <Pencil aria-hidden="true" />
            {t('documents.actions.editTags')}
          </DropdownMenuItem>
        </DropdownMenuGroup>
        {failedStatuses.has(document.status) ? (
          <DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onSelect={() => {
                onRetry(document);
              }}
            >
              <RefreshCw aria-hidden="true" />
              {t('documents.actions.retryProcessing')}
            </DropdownMenuItem>
          </DropdownMenuGroup>
        ) : null}
        <DropdownMenuSeparator />
        <DropdownMenuItem
          variant="destructive"
          onSelect={() => {
            onDelete(document);
          }}
        >
          <Trash2 aria-hidden="true" />
          {t('documents.actions.delete')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
