import { useTranslation } from 'react-i18next';
import { ShieldAlert, UserPlus } from 'lucide-react';
import { AdminAuditView } from '../audit/AdminAuditView';
import { useCapabilities } from '../admin/useAdmin';
import { CapabilityAdminPanel } from './CapabilityAdminPanel';
import { ModelSettingsPanel } from './ModelSettingsPanel';
import { SharedLinksSection } from './SharedLinksSection';
import { TelegramSettingsPanel } from './TelegramSettingsPanel';
import { Button } from '@/components/ui/button';

interface SettingsWorkspaceProps {
  readonly onCreateIdentity: () => void;
}

// SettingsWorkspace is the admin surface (MUSR-01 / D-03): model settings, identity creation,
// capability grant/revoke (D-26), and the per-user audit view (D-28). The whole page is gated
// on governance.write — a non-admin session renders the not-authorized fallback instead of the
// controls (defense-in-depth behind the server-side RequireCapability gate on every route).
export default function SettingsWorkspace({ onCreateIdentity }: SettingsWorkspaceProps) {
  const { t } = useTranslation();
  const { isAdmin, isLoading } = useCapabilities();

  if (isLoading) {
    return (
      <div role="status" className="grid min-h-40 place-items-center bg-bg text-sm text-text-muted">
        {t('settings.loading')}
      </div>
    );
  }

  if (!isAdmin) {
    return (
      <div className="h-full min-h-0 overflow-y-auto bg-bg">
        <div className="mx-auto flex w-full max-w-2xl flex-col items-start gap-4 px-4 py-16 sm:px-6">
          <span className="grid size-12 place-items-center rounded-full border border-border bg-surface text-accent-text">
            <ShieldAlert aria-hidden="true" />
          </span>
          <h1 className="font-display text-[22px] font-semibold text-text">
            {t('admin.notAuthorized.heading')}
          </h1>
          <p className="text-[15.5px] leading-relaxed text-text-muted">
            {t('admin.notAuthorized.body')}
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="h-full min-h-0 overflow-y-auto bg-bg">
      <div className="mx-auto flex w-full max-w-7xl flex-col gap-7 px-4 py-6 sm:px-6 lg:px-8">
        <header className="flex flex-col gap-2 border-b border-border pb-5">
          <p className="text-xs font-semibold uppercase text-accent-text">{t('settings.kicker')}</p>
          <h1 className="font-display text-[26px] font-semibold text-text">
            {t('settings.heading')}
          </h1>
          <p className="max-w-3xl text-[15.5px] leading-relaxed text-text-muted">
            {t('settings.body')}
          </p>
        </header>
        <section aria-labelledby="settings-identity" className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <h2 id="settings-identity" className="text-[20px] font-semibold text-text">
              {t('settings.identity.heading')}
            </h2>
            <p className="max-w-3xl text-[15.5px] leading-relaxed text-text-muted">
              {t('settings.identity.body')}
            </p>
          </div>
          <div>
            <Button type="button" variant="outline" onClick={onCreateIdentity}>
              <UserPlus aria-hidden="true" />
              {t('onboarding.open')}
            </Button>
          </div>
        </section>
        <div className="border-t border-border pt-6">
          <CapabilityAdminPanel />
        </div>
        <div className="border-t border-border pt-6">
          <AdminAuditView />
        </div>
        <TelegramSettingsPanel />
        <ModelSettingsPanel />
        <SharedLinksSection />
      </div>
    </div>
  );
}
