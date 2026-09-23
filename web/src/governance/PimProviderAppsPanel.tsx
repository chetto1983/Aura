import { useId, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { HttpError } from '../api/json';
import { Spinner } from '../components/Spinner';
import { Field } from './CalendarConnectFields';
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
// the admin sees first; it opens by itself while a provider still needs its app.

const MANAGED_PROVIDERS = PIM_PROVIDERS.filter(pimIsManaged);

export function PimProviderAppsPanel() {
  const { t } = useTranslation();
  const [toggledOpen, setToggledOpen] = useState<boolean>();
  const apps = useQuery({
    queryKey: PIM_PROVIDERS_KEY,
    queryFn: listPimProviderApps,
    retry: false,
  });
  if (!apps.isSuccess) return null;
  const configured = MANAGED_PROVIDERS.filter(
    (def) => apps.data.find((a) => a.provider === def.id)?.configured,
  ).length;
  return (
    <details
      open={toggledOpen ?? configured < MANAGED_PROVIDERS.length}
      onToggle={(event) => {
        setToggledOpen(event.currentTarget.open);
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
        {MANAGED_PROVIDERS.map((def) => (
          <ProviderAppForm
            key={def.id}
            def={def}
            app={apps.data.find((a) => a.provider === def.id)}
          />
        ))}
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
      <p className="text-[13px] font-semibold text-text">
        {t(def.labelKey)} ·{' '}
        {app?.configured
          ? t('governance.mcp.calendar.apps.configured')
          : t('governance.mcp.calendar.apps.notConfigured')}
      </p>
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
