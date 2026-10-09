import { useState } from 'react';
import { ArrowDown, ArrowUp, Plus, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { BoardColumn } from './boardApi';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';

interface BoardColumnsDialogProps {
  readonly columns: readonly BoardColumn[];
  readonly onClose: () => void;
  readonly onSave: (columns: BoardColumn[]) => Promise<void>;
  readonly error: string;
  readonly saving: boolean;
}

interface DraftColumn {
  readonly id: string;
  readonly label: string;
  readonly limit: string;
}

/**
 * The column editor. The free widget can collapse a column but not add, remove, rename or
 * limit one, so the whole array is edited here and saved in one PUT. Mounted only while open,
 * so every opening starts from the board as it is.
 */
export function BoardColumnsDialog({
  columns,
  onClose,
  onSave,
  error,
  saving,
}: BoardColumnsDialogProps) {
  const { t } = useTranslation();
  const [draft, setDraft] = useState<DraftColumn[]>(() =>
    columns.map((c) => ({
      id: c.id,
      label: c.label,
      limit: c.cardLimit ? String(c.cardLimit) : '',
    })),
  );

  function update(index: number, patch: Partial<DraftColumn>) {
    setDraft((rows) => rows.map((row, i) => (i === index ? { ...row, ...patch } : row)));
  }

  function move(index: number, by: -1 | 1) {
    setDraft((rows) => {
      const next = [...rows];
      const [row] = next.splice(index, 1);
      if (row !== undefined) next.splice(index + by, 0, row);
      return next;
    });
  }

  function save() {
    void onSave(
      draft.map((row) => {
        const limit = Number.parseInt(row.limit, 10);
        return limit > 0
          ? { id: row.id, label: row.label.trim(), cardLimit: limit }
          : { id: row.id, label: row.label.trim() };
      }),
    );
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="max-h-[calc(100vh-2rem)] w-[calc(100vw-2rem)] max-w-lg overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t('board.columns.title')}</DialogTitle>
          <DialogDescription>{t('board.columns.description')}</DialogDescription>
        </DialogHeader>
        <ol className="flex flex-col gap-2">
          {draft.map((row, index) => (
            <li key={row.id} className="flex flex-wrap items-center gap-1.5 sm:flex-nowrap">
              <Input
                className="basis-full sm:basis-auto sm:flex-1"
                aria-label={t('board.columns.label', { position: index + 1 })}
                value={row.label}
                maxLength={80}
                onChange={(event) => {
                  update(index, { label: event.target.value });
                }}
              />
              <Input
                aria-label={t('board.columns.limit', { position: index + 1 })}
                placeholder={t('board.columns.noLimit')}
                inputMode="numeric"
                className="w-24 shrink-0"
                value={row.limit}
                onChange={(event) => {
                  update(index, { limit: event.target.value.replace(/\D/g, '') });
                }}
              />
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={t('board.columns.up', { position: index + 1 })}
                disabled={index === 0}
                onClick={() => {
                  move(index, -1);
                }}
              >
                <ArrowUp aria-hidden="true" />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={t('board.columns.down', { position: index + 1 })}
                disabled={index === draft.length - 1}
                onClick={() => {
                  move(index, 1);
                }}
              >
                <ArrowDown aria-hidden="true" />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={t('board.columns.remove', { position: index + 1 })}
                disabled={draft.length === 1}
                onClick={() => {
                  setDraft((rows) => rows.filter((_, i) => i !== index));
                }}
              >
                <Trash2 aria-hidden="true" />
              </Button>
            </li>
          ))}
        </ol>
        <Button
          type="button"
          variant="ghost"
          className="self-start"
          onClick={() => {
            setDraft((rows) => [
              ...rows,
              {
                id: crypto.randomUUID().slice(0, 8),
                label: t('board.columns.newColumn'),
                limit: '',
              },
            ]);
          }}
        >
          <Plus data-icon="inline-start" aria-hidden="true" />
          {t('board.columns.add')}
        </Button>
        {error !== '' && (
          <p role="alert" className="text-sm text-danger">
            {error}
          </p>
        )}
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>
            {t('board.columns.cancel')}
          </Button>
          <Button
            type="button"
            disabled={saving || draft.some((row) => row.label.trim() === '')}
            aria-busy={saving}
            onClick={save}
          >
            {t('board.columns.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
