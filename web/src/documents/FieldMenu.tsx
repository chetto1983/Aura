import { useState } from 'react';
import { ChevronDown } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  withField,
  type DocumentsFilter,
  type Facets,
  type FilterField,
  type Label,
} from './documentsApi';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { Button } from '@/components/ui/button';

interface FieldMenuProps {
  readonly field: FilterField;
  readonly label: string;
  readonly labels: readonly Label[];
  readonly facets: Facets;
  readonly filter: DocumentsFilter;
  readonly onChange: (filter: DocumentsFilter) => void;
}

/**
 * One list's dropdown in the filter bar (mockup 1): every value with its count in the current
 * result, Include or Exclude, and "Non assegnata"; tags add Any or All.
 */
export function FieldMenu({ field, label, labels, facets, filter, onChange }: FieldMenuProps) {
  const { t } = useTranslation();
  const [side, setSide] = useState<'include' | 'exclude'>('include');
  const [narrow, setNarrow] = useState('');
  const counts = facets[field] ?? {};
  const shown = labels.filter((l) => l.name.toLowerCase().includes(narrow.trim().toLowerCase()));
  const unassigned = filter.unassigned.includes(field);
  const active = filter.include[field].length + filter.exclude[field].length > 0 || unassigned;

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          size="sm"
          aria-label={label}
          className={active ? 'border-info/60 bg-info/10' : ''}
        >
          {label}
          <ChevronDown aria-hidden="true" data-icon="inline-end" />
        </Button>
      </PopoverTrigger>
      <PopoverContent aria-label={label} align="start" className="flex w-80 flex-col gap-2 p-3">
        <Input
          aria-label={t('documents.filters.narrow')}
          placeholder={t('documents.filters.narrow')}
          value={narrow}
          onChange={(e) => {
            setNarrow(e.target.value);
          }}
        />
        <ToggleGroup
          type="single"
          variant="outline"
          className="w-full"
          value={side}
          onValueChange={(next) => {
            if (next === 'include' || next === 'exclude') setSide(next);
          }}
        >
          <ToggleGroupItem value="include" className="flex-1">
            {t('documents.filters.include')}
          </ToggleGroupItem>
          <ToggleGroupItem value="exclude" className="flex-1">
            {t('documents.filters.exclude')}
          </ToggleGroupItem>
        </ToggleGroup>
        {field === 'tags' && side === 'include' && (
          <ToggleGroup
            type="single"
            variant="outline"
            className="w-full"
            aria-label={t('documents.columns.tags')}
            value={filter.tagsMode}
            onValueChange={(next) => {
              if (next === 'any' || next === 'all') onChange({ ...filter, tagsMode: next });
            }}
          >
            <ToggleGroupItem value="any" className="flex-1">
              {t('documents.filters.any')}
            </ToggleGroupItem>
            <ToggleGroupItem value="all" className="flex-1">
              {t('documents.filters.all')}
            </ToggleGroupItem>
          </ToggleGroup>
        )}
        <ul className="flex max-h-72 flex-col overflow-y-auto">
          <MenuRow
            id={`${field}-unassigned`}
            name={t('documents.filters.unassigned')}
            count={facets.unassigned?.[field] ?? 0}
            checked={unassigned}
            muted
            onToggle={() => {
              onChange({
                ...filter,
                unassigned: unassigned
                  ? filter.unassigned.filter((f) => f !== field)
                  : [...filter.unassigned, field],
              });
            }}
          />
          {shown.map((l) => (
            <MenuRow
              key={l.id}
              id={`${field}-${l.id}`}
              name={l.name}
              count={counts[l.id] ?? 0}
              checked={filter[side][field].includes(l.id)}
              onToggle={() => {
                onChange(withField(filter, side, field, l.id));
              }}
            />
          ))}
        </ul>
        <p className="border-t border-border pt-2 text-xs text-text-muted">
          {t('documents.filters.counts')}
        </p>
      </PopoverContent>
    </Popover>
  );
}

function MenuRow(props: {
  readonly id: string;
  readonly name: string;
  readonly count: number;
  readonly checked: boolean;
  readonly muted?: boolean;
  readonly onToggle: () => void;
}) {
  const { id, name, count, checked, muted, onToggle } = props;
  return (
    <li>
      <label
        htmlFor={id}
        className={`flex min-h-[44px] cursor-pointer items-center gap-2 rounded-md px-1 hover:bg-surface-3 ${
          count === 0 && !checked ? 'text-text-muted' : muted ? 'text-text-muted' : 'text-text'
        }`}
      >
        <Checkbox id={id} checked={checked} onCheckedChange={onToggle} />
        <span className={`flex-1 truncate ${checked ? 'font-semibold' : ''}`}>{name}</span>
        <span className="tabular-nums">{count}</span>
      </label>
    </li>
  );
}
