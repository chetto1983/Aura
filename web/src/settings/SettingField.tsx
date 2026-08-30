import type { ReactNode } from 'react';
import { RotateCcw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import type { SettingItem } from './settingsApi';
import type { SettingDef, SettingsKey } from './modelSettingsDefs';
import { settingRow, type LoadedState } from './modelSettingsState';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { SecretInput } from '@/components/ui/secret-input';
import { cn } from '@/lib/utils';

function SettingsGrid({ children }: { readonly children: ReactNode }) {
  return <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">{children}</div>;
}

export function SettingsFields({
  defs,
  loaded,
  onReset,
  onValueChange,
  resetting,
}: {
  readonly defs: readonly SettingDef[];
  readonly loaded: LoadedState;
  readonly resetting: string | undefined;
  readonly onValueChange: (key: SettingsKey, value: string) => void;
  readonly onReset: (key: SettingsKey) => void;
}) {
  return (
    <SettingsGrid>
      {defs.map((def) => (
        <SettingField
          key={def.key}
          def={def}
          item={settingRow(loaded, def)}
          value={loaded.values[def.key] ?? ''}
          onChange={(value) => {
            onValueChange(def.key, value);
          }}
          onReset={() => {
            onReset(def.key);
          }}
          resetting={resetting === def.key}
        />
      ))}
    </SettingsGrid>
  );
}

function SettingField({
  def,
  item,
  onChange,
  onReset,
  resetting,
  value,
}: {
  readonly def: SettingDef;
  readonly item: SettingItem;
  readonly value: string;
  readonly onChange: (value: string) => void;
  readonly onReset: () => void;
  readonly resetting: boolean;
}) {
  const { t } = useTranslation();
  const inputId = `setting-${def.key}`;
  const label = t(def.labelKey);
  const isBooleanActive = def.kind === 'bool' && value === 'true';
  const isConfigured = def.kind === 'bool' ? isBooleanActive : item.has_value;
  const status =
    def.kind === 'bool'
      ? t(isBooleanActive ? 'settings.status.active' : 'settings.status.inactive')
      : item.has_value
        ? t('settings.status.configured')
        : t('settings.status.notConfigured');

  return (
    <div className="flex min-h-32 flex-col gap-2 rounded-md border border-border bg-surface px-3 py-3">
      <div className="flex min-w-0 items-start justify-between gap-2">
        <Label htmlFor={inputId} className="min-w-0 break-words text-[13px]">
          {label}
        </Label>
        <Badge variant={isConfigured ? 'success' : 'secondary'}>{status}</Badge>
      </div>
      {def.kind === 'bool' ? (
        <label className="flex min-h-[44px] items-center gap-3 rounded-md border border-input bg-surface-3 px-3 text-[13px] text-text">
          <input
            id={inputId}
            type="checkbox"
            checked={value === 'true'}
            onChange={(event) => {
              onChange(event.target.checked ? 'true' : 'false');
            }}
            className="size-4 accent-accent"
          />
          {t('settings.fields.enabled')}
        </label>
      ) : def.secret ? (
        <SecretInput
          id={inputId}
          value={value}
          placeholder={def.placeholder ?? t('settings.secretPlaceholder')}
          showLabel={t('secret.show', { label })}
          hideLabel={t('secret.hide', { label })}
          onChange={(event) => {
            onChange(event.target.value);
          }}
          className="font-mono text-[13px]"
        />
      ) : (
        <Input
          id={inputId}
          type={def.kind === 'int' ? 'number' : 'text'}
          inputMode={def.kind === 'int' ? 'numeric' : undefined}
          value={value}
          placeholder={def.placeholder}
          onChange={(event) => {
            onChange(event.target.value);
          }}
          className="font-mono text-[13px]"
        />
      )}
      {def.helpKey === undefined ? null : (
        <p className="text-[12px] leading-snug text-text-muted">{t(def.helpKey)}</p>
      )}
      <div className="mt-auto flex items-center justify-between gap-2">
        <div className="flex min-w-0 flex-col gap-1">
          <code className="min-w-0 break-all text-[12px] text-text-faint">{def.key}</code>
          {/* Amendment #188: each field says how it reaches the running daemon, so a
              pending restart is attributed to THIS row instead of a pane-level banner. */}
          <span
            data-applied={item.applied}
            className={cn(
              'text-[12px]',
              item.applied === 'restart' ? 'font-medium text-warning' : 'text-text-faint',
            )}
          >
            {t(`settings.applied.${item.applied}`)}
          </span>
        </div>
        {item.overridden ? (
          <Button type="button" variant="ghost" size="sm" disabled={resetting} onClick={onReset}>
            {resetting ? <Spinner /> : <RotateCcw aria-hidden="true" />}
            {t('settings.actions.reset')}
          </Button>
        ) : null}
      </div>
    </div>
  );
}
