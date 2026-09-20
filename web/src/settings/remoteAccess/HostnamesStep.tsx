import { useTranslation } from 'react-i18next';
import type { RemoteAccessStatusDTO } from './remoteAccessApi';

export function HostnamesStep({ status }: { readonly status: RemoteAccessStatusDTO }) {
  const { t } = useTranslation();
  return (
    <div className="flex max-w-xl flex-col gap-3 rounded-md border border-border bg-surface p-4">
      <h3 className="font-semibold text-text">{t('remoteAccess.nameservers.heading')}</h3>
      <p className="text-sm leading-relaxed text-text-muted">
        {t('remoteAccess.nameservers.body')}
      </p>
      <ul className="grid gap-2 sm:grid-cols-2">
        {(status.nameservers ?? []).map((name) => (
          <li
            key={name}
            className="break-all rounded-md bg-surface-2 px-3 py-2 font-mono text-sm text-text"
          >
            {name}
          </li>
        ))}
      </ul>
      <p role="status" className="text-sm text-text-muted">
        {t('remoteAccess.nameservers.waiting')}
      </p>
    </div>
  );
}
