import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';
import { documentsQuery, errorMessage } from './documentsApi';
import { deleteView, saveView, type SavedView } from './workspaceApi';
import { savedViewHref, type DocumentQuery } from './documentRoute';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Checkbox } from '@/components/ui/checkbox';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';

function useViewWrite(identity: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: async (work: () => Promise<unknown>) => {
      await work();
    },
    onSuccess: () => client.invalidateQueries({ queryKey: ['documents-views', identity] }),
  });
}

export function SaveViewButton({
  identity,
  query,
}: {
  readonly identity: string;
  readonly query: DocumentQuery;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState('');
  const [open, setOpen] = useState(false);
  const write = useViewWrite(identity);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button size="sm" variant="outline">
          {t('documents.workspace.saveView')}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-72" align="end">
        <form
          className="space-y-3"
          onSubmit={(event) => {
            event.preventDefault();
            if (write.isPending || !name.trim()) return;
            write.mutate(
              () =>
                saveView({
                  name: name.trim(),
                  version: 1,
                  query: documentsQuery(query.filter, { ...query.page, offset: 0 }),
                  display: query.display,
                  pinned: false,
                  dashboard: false,
                }),
              {
                onSuccess: () => {
                  setOpen(false);
                  setName('');
                },
              },
            );
          }}
        >
          <label className="text-sm">
            {t('documents.workspace.viewName')}
            <Input
              aria-label={t('documents.workspace.viewName')}
              maxLength={80}
              required
              value={name}
              onChange={(event) => {
                setName(event.target.value);
              }}
            />
          </label>
          <Button type="submit" disabled={write.isPending || !name.trim()}>
            {t('documents.workspace.save')}
          </Button>
          {write.isError && (
            <p role="alert" className="text-sm text-danger">
              {errorMessage(write.error)}
            </p>
          )}
        </form>
      </PopoverContent>
    </Popover>
  );
}

export function SavedViews({
  identity,
  views,
  error,
  onRetry,
}: {
  readonly identity: string;
  readonly views: readonly SavedView[];
  readonly error: boolean;
  readonly onRetry: () => void;
}) {
  const { t } = useTranslation();
  return (
    <section className="h-full overflow-y-auto p-4 md:p-6">
      <h1 className="mb-5 font-display text-2xl font-semibold">{t('documents.workspace.views')}</h1>
      {error ? (
        <p role="alert">
          {t('documents.loadFailed')}{' '}
          <Button onClick={onRetry}>{t('documents.workspace.retry')}</Button>
        </p>
      ) : views.length === 0 ? (
        <p className="text-sm text-text-muted">{t('documents.workspace.noViews')}</p>
      ) : (
        <ul className="max-w-3xl space-y-3">
          {views.map((view) => (
            <ViewEditor key={view.id} identity={identity} view={view} />
          ))}
        </ul>
      )}
    </section>
  );
}
function ViewEditor({ identity, view }: { readonly identity: string; readonly view: SavedView }) {
  const { t } = useTranslation();
  const [draft, setDraft] = useState(view);
  const [confirm, setConfirm] = useState(false);
  const write = useViewWrite(identity);
  return (
    <li className="rounded-xl border border-border bg-surface p-4">
      <form
        className="flex flex-wrap items-center gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          if (!write.isPending) write.mutate(() => saveView(draft, view.id));
        }}
      >
        <label className="min-w-0 flex-[1_1_16rem] text-sm">
          {t('documents.workspace.viewName')}
          <Input
            required
            maxLength={80}
            aria-label={t('documents.workspace.viewName')}
            value={draft.name}
            onChange={(event) => {
              setDraft({ ...draft, name: event.target.value });
            }}
          />
        </label>
        <Link to={savedViewHref(view)} className="text-sm text-info underline">
          {t('documents.workspace.openView')}
        </Link>
        <label className="flex min-h-11 items-center gap-2 text-sm">
          <Checkbox
            checked={draft.pinned}
            onCheckedChange={(value) => {
              setDraft({ ...draft, pinned: value === true });
            }}
          />
          {t('documents.workspace.pin')}
        </label>
        <label className="flex min-h-11 items-center gap-2 text-sm">
          <Checkbox
            checked={draft.dashboard}
            onCheckedChange={(value) => {
              setDraft({ ...draft, dashboard: value === true });
            }}
          />
          {t('documents.workspace.dashboard')}
        </label>
        <Button type="submit" disabled={write.isPending || !draft.name.trim()}>
          {t('documents.workspace.save')}
        </Button>
        <Button
          type="button"
          variant="ghost"
          disabled={write.isPending}
          onClick={() => {
            setConfirm(true);
          }}
        >
          {t('documents.workspace.delete')}
        </Button>
      </form>
      {write.isError && (
        <p role="alert" className="text-sm text-danger">
          {errorMessage(write.error)}
        </p>
      )}
      {write.isSuccess && (
        <p role="status" className="text-xs text-text-muted">
          {t('documents.workspace.viewSaved')}
        </p>
      )}
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title={t('documents.workspace.deleteTitle')}
        cancelLabel={t('documents.workspace.cancel')}
        confirmLabel={t('documents.bulk.confirm')}
        confirmPending={write.isPending}
        onConfirm={() => {
          write.mutate(() => deleteView(view.id), {
            onSuccess: () => {
              setConfirm(false);
            },
          });
        }}
      />
    </li>
  );
}
