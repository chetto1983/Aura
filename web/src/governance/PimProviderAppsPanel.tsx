import { useId, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { HttpError } from '../api/json';
import { Spinner } from '../components/Spinner';
import { Field, ProviderSelect } from './CalendarConnectFields';
import {
  listPimProviderApps,
  PIM_PROVIDERS_KEY,
  savePimProviderApp,
  type PimManagedProviderId,
  type PimProviderApp,
  type PimProviderAppInput,
} from './pimApi';
import { PIM_PROVIDERS, pimIsManaged, type PimProviderDef } from './pimProviders';
import { Button } from '@/components/ui/button';

// PimProviderAppsPanel is the admin's half of calendar connect: one OAuth client per managed
// provider, set once, which Aura injects into every member's account create. The secret field is
// never pre-filled — the server never returns it — and an empty one keeps the stored secret.
// Once every provider is configured the panel starts collapsed, so connecting an account is what
// the admin sees first; it opens by itself, on the first provider still missing its app, while one
// is. Both are decided when the list first loads: a save that completes the set must not fold the
// panel or switch the form under the admin's "Saved.".

const MANAGED_PROVIDERS = PIM_PROVIDERS.filter(pimIsManaged);

export function PimProviderAppsPanel() {
  const apps = useQuery({
    queryKey: PIM_PROVIDERS_KEY,
    queryFn: listPimProviderApps,
    retry: false,
  });
  return apps.isSuccess ? <ProviderApps apps={apps.data} /> : null;
}

function ProviderApps({ apps }: { readonly apps: readonly PimProviderApp[] }) {
  const { t } = useTranslation();
  const selectId = useId();
  const appOf = (def: PimProviderDef) => apps.find((a) => a.provider === def.id);
  const configured = MANAGED_PROVIDERS.filter((def) => appOf(def)?.configured).length;
  const [open, setOpen] = useState(() => configured < MANAGED_PROVIDERS.length);
  const [selected, setSelected] = useState(
    () => (MANAGED_PROVIDERS.find((def) => !appOf(def)?.configured) ?? MANAGED_PROVIDERS[0])?.id,
  );
  const current = MANAGED_PROVIDERS.find((def) => def.id === selected);

  function statusOf(def: PimProviderDef): string {
    const status = appOf(def)?.configured ? 'configured' : 'notConfigured';
    return `${t(def.labelKey)} · ${t(`governance.mcp.calendar.apps.${status}`)}`;
  }

  return (
    <details
      open={open}
      onToggle={(event) => {
        setOpen(event.currentTarget.open);
      }}
      className="rounded-md border border-border bg-surface px-3 py-3"
    >
      <summary className="cursor-pointer text-[13px] font-semibold text-text-muted">
        {t('governance.mcp.calendar.apps.heading')} ·{' '}
        <span className="font-normal">
          {t('governance.mcp.calendar.apps.summary', {
            configured,
            total: MANAGED_PROVIDERS.length,
          })}
        </span>
      </summary>
      <div className="mt-3 flex flex-col gap-3">
        <p className="text-[13px] text-text-muted">{t('governance.mcp.calendar.apps.intro')}</p>
        {current ? (
          <>
            <ProviderSelect
              id={selectId}
              value={current.id}
              onChange={setSelected}
              providers={MANAGED_PROVIDERS}
              optionLabel={statusOf}
            />
            <ProviderAppForm key={current.id} def={current} app={appOf(current)} />
          </>
        ) : null}
      </div>
    </details>
  );
}

function ProviderAppForm({
  def,
  app,
}: {
  readonly def: PimProviderDef;
  readonly app: PimProviderApp | undefined;
}) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const idPrefix = useId();
  const [values, setValues] = useState<Record<string, string>>(() => ({
    clientId: app?.clientId ?? '',
    tenantId: app?.tenantId ?? '',
    clientSecret: '',
  }));

  const save = useMutation({
    mutationFn: () => {
      const secret = (values.clientSecret ?? '').trim();
      const input: PimProviderAppInput = {
        clientId: (values.clientId ?? '').trim(),
        ...(def.appFields.some((f) => f.key === 'tenantId')
          ? { tenantId: (values.tenantId ?? '').trim() }
          : {}),
        ...(secret !== '' ? { clientSecret: secret } : {}),
      };
      return savePimProviderApp(def.id as PimManagedProviderId, input);
    },
    onSuccess: () => {
      setValues((prev) => ({ ...prev, clientSecret: '' }));
      void queryClient.invalidateQueries({ queryKey: PIM_PROVIDERS_KEY });
    },
  });

  function hintFor(key: string, hintKey: string | undefined): string | undefined {
    if (key === 'clientSecret' && app?.secretSet)
      return t('governance.mcp.calendar.apps.secretStored');
    return hintKey !== undefined ? t(hintKey) : undefined;
  }

  return (
    <form
      className="flex flex-col gap-2 rounded-md border border-border-strong bg-surface-2 px-3 py-3"
      onSubmit={(event) => {
        event.preventDefault();
        save.mutate();
      }}
    >
      {def.appFields.map((field) => (
        <Field
          key={field.key}
          id={`${idPrefix}-${field.key}`}
          label={t(field.labelKey)}
          type={field.type === 'password' ? 'password' : 'text'}
          value={values[field.key] ?? ''}
          onChange={(v) => {
            setValues((prev) => ({ ...prev, [field.key]: v }));
          }}
          invalid={false}
          hint={hintFor(field.key, field.hintKey)}
        />
      ))}
      {app?.redirectUri ? (
        <>
          <p className="text-[13px] font-semibold text-text">
            {t('governance.mcp.calendar.redirectHeading')}
          </p>
          <p className="break-all font-mono text-[13px] text-text">{app.redirectUri}</p>
          <p className="text-[13px] text-text-muted">{t('governance.mcp.calendar.redirectHint')}</p>
        </>
      ) : null}
      <Button
        type="submit"
        disabled={save.isPending}
        aria-busy={save.isPending}
        className="self-start text-[13px]"
      >
        {save.isPending ? <Spinner /> : null}
        {t('governance.mcp.calendar.apps.save')}
      </Button>
      {save.isError ? (
        <p role="alert" className="text-[13px] text-danger">
          {save.error instanceof HttpError && save.error.reason !== ''
            ? save.error.reason
            : t('governance.error')}
        </p>
      ) : null}
      {save.isSuccess ? (
        <p role="status" className="text-[13px] text-success">
          {t('governance.mcp.calendar.apps.saved')}
        </p>
      ) : null}
    </form>
  );
}
