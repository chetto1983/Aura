import { useTranslation } from 'react-i18next';
import type { StudioModel } from './studioApi';
import { modelPrice } from './studioPrice';
import {
  ModelSelectorContent,
  ModelSelectorItem,
  ModelSelectorList,
  ModelSelectorRoot,
  ModelSelectorTrigger,
  type ModelOption,
} from '@/components/model-selector';

// StudioModelPill — the owned model-selector over the Studio catalog. The catalog carries a
// name and a one-line description that the rest of Aura drops; here they are the row, because
// choosing between two ids that differ by a suffix is not a choice anybody can make.

interface StudioModelPillProps {
  readonly models: readonly StudioModel[];
  readonly value: string;
  readonly onChange: (modelId: string) => void;
}

function nameOf(model: StudioModel): string {
  return model.name === undefined || model.name === '' ? model.id : model.name;
}

export function StudioModelPill({ models, value, onChange }: StudioModelPillProps) {
  const { t } = useTranslation();
  const options: readonly ModelOption[] = models.map((model) => ({
    id: model.id,
    name: nameOf(model),
    // The id is not on the row, so it has to stay findable by search.
    keywords: [model.id],
  }));

  return (
    <ModelSelectorRoot models={options} value={value} onValueChange={onChange}>
      <ModelSelectorTrigger
        size="sm"
        variant="ghost"
        aria-label={t('studio.model.label')}
        className="studio-pill max-w-44"
      />
      <ModelSelectorContent align="start" className="w-80">
        <ModelSelectorList>
          {models.map((model) => (
            <ModelSelectorItem
              key={model.id}
              model={{ id: model.id, name: nameOf(model), keywords: [model.id] }}
            >
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="truncate text-[13px] font-medium text-text">{nameOf(model)}</span>
                {model.description === undefined || model.description === '' ? null : (
                  <span className="truncate text-xs text-text-muted">{model.description}</span>
                )}
                <ModelPriceLine model={model} />
              </span>
            </ModelSelectorItem>
          ))}
        </ModelSelectorList>
      </ModelSelectorContent>
    </ModelSelectorRoot>
  );
}

/** The row's price, or nothing. A model the catalog never priced says nothing rather than
 *  showing a zero the bill will not match. */
function ModelPriceLine({ model }: { readonly model: StudioModel }) {
  const { t } = useTranslation();
  const price = modelPrice(model);
  if (price === undefined) return null;
  const label =
    price.unit === 'second'
      ? t('studio.model.perSecond', { price: price.amount })
      : price.unit === 'image'
        ? t('studio.model.perImage', { price: price.amount })
        : t('studio.model.perTokens', { price: price.amount });
  return <span className="text-[11px] text-text-faint tabular-nums">{label}</span>;
}
