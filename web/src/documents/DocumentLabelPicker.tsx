import { useEffect, useId, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { saveLabel, errorMessage, type Label, type LabelKind } from './documentsApi';
import { useCapabilities } from '@/admin/useAdmin';
import { Button } from '@/components/ui/button';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import {
  Command,
  CommandInput,
  CommandList,
  CommandItem,
  CommandEmpty,
} from '@/components/ui/command';

interface Props {
  readonly kind: LabelKind;
  readonly labels: readonly Label[];
  readonly value: string;
  readonly name: string;
  readonly currentName?: string;
  readonly disabled: boolean;
  readonly onValueChange: (id: string) => void;
}
export function DocumentLabelPicker({
  kind,
  labels,
  value,
  name,
  currentName,
  disabled,
  onValueChange,
}: Props) {
  const { t } = useTranslation();
  const { identityId } = useCapabilities();
  const client = useQueryClient();
  const listId = useId();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const callback = useRef(onValueChange);
  const lifetime = useRef<AbortController | null>(null);
  useEffect(() => {
    callback.current = onValueChange;
  }, [onValueChange]);
  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    return () => {
      controller.abort();
    };
  }, [identityId]);
  const select = (id: string) => {
    callback.current(id);
    setOpen(false);
    setSearch('');
  };
  const create = async () => {
    const signal = lifetime.current?.signal;
    if (!signal || signal.aborted || busy || !search.trim()) return;
    const active = () => !signal.aborted;
    setBusy(true);
    setError('');
    try {
      const result = await saveLabel(
        {
          kind,
          name: search.trim(),
          match_algorithm: 'none',
          match_pattern: '',
          case_insensitive: true,
        },
        undefined,
        signal,
      );
      if (!active()) return;
      const key = ['documents-labels', kind, identityId];
      client.setQueryData<readonly Label[]>(key, (old) => [...(old ?? []), result.label]);
      select(result.label.id);
      void client.invalidateQueries({ queryKey: key });
    } catch (err) {
      if (active()) setError(errorMessage(err));
    } finally {
      if (active()) setBusy(false);
    }
  };
  const found = labels.filter((label) =>
    label.name.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()),
  );
  const exists = labels.some(
    (label) => label.name.toLocaleLowerCase() === search.trim().toLocaleLowerCase(),
  );
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          role="combobox"
          aria-controls={listId}
          aria-haspopup="dialog"
          aria-label={name}
          aria-expanded={open}
          variant="outline"
          disabled={disabled || busy}
          className="w-full justify-start overflow-hidden text-ellipsis"
        >
          {labels.find((label) => label.id === value)?.name ??
            (value ? currentName : t('documents.detail.none'))}
        </Button>
      </PopoverTrigger>
      <PopoverContent id={listId} aria-label={name} className="w-80 p-0" align="start">
        <Command shouldFilter={false} label={t('documents.detail.searchLabel')}>
          <CommandInput
            aria-label={t('documents.detail.searchLabel')}
            placeholder={t('documents.detail.searchLabel')}
            value={search}
            onValueChange={setSearch}
            disabled={busy}
          />
          <CommandList label={name}>
            <CommandEmpty>{t('documents.lists.empty')}</CommandEmpty>
            {kind !== 'tag' && (
              <CommandItem
                onSelect={() => {
                  select('');
                }}
              >
                {t('documents.detail.none')}
              </CommandItem>
            )}
            {found.map((label) => (
              <CommandItem
                key={label.id}
                value={label.id}
                disabled={busy}
                onSelect={() => {
                  select(label.id);
                }}
              >
                {label.name}
              </CommandItem>
            ))}
            {search.trim() && !exists && (
              <CommandItem
                value="create-new-label"
                disabled={busy}
                onSelect={() => {
                  void create();
                }}
              >
                {t('documents.detail.createLabel', { name: search.trim() })}
              </CommandItem>
            )}
          </CommandList>
        </Command>
        {error && (
          <p role="alert" className="p-3 text-sm text-danger">
            {error}
          </p>
        )}
      </PopoverContent>
    </Popover>
  );
}
