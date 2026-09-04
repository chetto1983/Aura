import { useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import type { LLMCatalogModel } from './settingsApi';
import type { ModelCatalogState } from './useModelCatalog';
import { modelMeta } from './modelCatalogFormat';
import {
  ModelSelectorContent,
  ModelSelectorEmpty,
  ModelSelectorGroup,
  ModelSelectorItem,
  ModelSelectorList,
  ModelSelectorRoot,
  ModelSelectorSearch,
  ModelSelectorTrigger,
  type ModelOption,
} from '@/components/model-selector';
import { CommandItem } from '@/components/ui/command';

// ModelPicker is assistant-ui's model-selector element over the catalogue the daemon
// probes from the configured endpoint. The list is grouped by vendor (OpenRouter ids are
// `vendor/name`; llama.cpp and Ollama publish flat ids and land in one group), each row
// carrying the two numbers a routing decision actually turns on: the context window and,
// where the provider charges per token, its input/output rate.
//
// Free text survives: llama.cpp serves aliases its catalogue does not list and an
// unreachable endpoint must not block a save, so a typed id that matches nothing is
// offered as its own row instead of being swallowed.

interface ModelPickerProps {
  readonly id: string;
  readonly value: string;
  readonly catalog: ModelCatalogState;
  readonly onChange: (value: string) => void;
}

interface ModelGroup {
  readonly name: string;
  readonly models: readonly ModelOption[];
}

// OpenRouter ids are `vendor/name`; anything without a slash is served by the endpoint
// itself and needs no vendor header.
function vendorOf(id: string): string {
  const [vendor, rest] = id.split('/');
  return rest === undefined || vendor === undefined ? '' : vendor;
}

function toModelOptions(
  models: readonly LLMCatalogModel[],
  freeLabel: string,
): readonly ModelOption[] {
  return models.map((model) => {
    const vendor = vendorOf(model.id);
    return {
      id: model.id,
      name: model.id,
      description: modelMeta(model, freeLabel),
      ...(vendor === '' ? {} : { keywords: [vendor] }),
    };
  });
}

function groupByVendor(options: readonly ModelOption[]): readonly ModelGroup[] {
  const groups = new Map<string, ModelOption[]>();
  for (const option of options) {
    const vendor = vendorOf(option.id);
    const bucket = groups.get(vendor);
    if (bucket === undefined) groups.set(vendor, [option]);
    else bucket.push(option);
  }
  return [...groups].map(([name, models]) => ({ name, models }));
}

export function ModelPicker({ id, value, catalog, onChange }: ModelPickerProps) {
  const { t } = useTranslation();
  const [query, setQuery] = useState('');
  const options = toModelOptions(catalog.models, t('settings.models.noCharge'));
  // The saved model may not be in the catalogue (an alias, or a route the endpoint no
  // longer serves). It still has to render as the current value rather than disappear.
  const known = options.some((option) => option.id === value);
  // "not published" is a claim about the endpoint, and it is only true once the endpoint
  // has answered: while the probe is in flight the catalogue is empty for every model,
  // and saying so about the one that is actually running would be a lie on every load.
  const unpublished = catalog.status === 'ready' ? t('settings.models.notPublished') : '';
  const selectable =
    known || value.trim() === ''
      ? options
      : [
          { id: value, name: value, ...(unpublished === '' ? {} : { description: unpublished }) },
          ...options,
        ];
  const typed = query.trim();
  const custom = typed !== '' && !selectable.some((option) => option.id === typed);

  return (
    <div className="flex flex-col gap-1.5">
      <ModelSelectorRoot models={selectable} value={value} onValueChange={onChange}>
        <ModelSelectorTrigger id={id} className="w-full font-mono text-[13px]" />
        <ModelSelectorContent searchable className="w-(--radix-popover-trigger-width)">
          <ModelSelectorSearch
            placeholder={t('settings.models.search')}
            value={query}
            onValueChange={setQuery}
          />
          <ModelSelectorList>
            <ModelSelectorEmpty>{t('settings.models.empty')}</ModelSelectorEmpty>
            {custom ? (
              <ModelSelectorGroup>
                <CommandItem
                  forceMount
                  value={typed}
                  onSelect={() => {
                    onChange(typed);
                  }}
                  className="font-mono text-[13px]"
                >
                  {t('settings.models.useTyped', { model: typed })}
                </CommandItem>
              </ModelSelectorGroup>
            ) : null}
            {groupByVendor(selectable).map((group) => (
              <ModelSelectorGroup
                key={group.name}
                heading={group.name === '' ? undefined : group.name}
              >
                {group.models.map((model) => (
                  <ModelSelectorItem
                    key={model.id}
                    model={model}
                    className="font-mono text-[13px]"
                  />
                ))}
              </ModelSelectorGroup>
            ))}
          </ModelSelectorList>
        </ModelSelectorContent>
      </ModelSelectorRoot>

      <div className="flex items-center justify-between gap-2 text-[12px]">
        <span className={catalog.error === undefined ? 'text-text-faint' : 'text-warning'}>
          {catalog.status === 'loading'
            ? t('settings.models.loading')
            : catalog.error !== undefined
              ? t('settings.models.error', { message: catalog.error })
              : t('settings.models.count', { count: catalog.models.length })}
        </span>
        <button
          type="button"
          onClick={catalog.reload}
          disabled={catalog.status === 'loading'}
          className="inline-flex shrink-0 items-center gap-1 text-text-muted underline-offset-2 hover:underline disabled:opacity-60"
        >
          {catalog.status === 'loading' ? (
            <Spinner />
          ) : (
            <RefreshCw aria-hidden="true" className="size-3" />
          )}
          {t('settings.models.refresh')}
        </button>
      </div>
    </div>
  );
}
