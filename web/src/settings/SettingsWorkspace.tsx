import { useTranslation } from 'react-i18next';
import { UserPlus } from 'lucide-react';
import { ModelSettingsPanel } from './ModelSettingsPanel';
import { TelegramSettingsPanel } from './TelegramSettingsPanel';
import { Button } from '@/components/ui/button';

interface SettingsWorkspaceProps {
  readonly onCreateIdentity: () => void;
}

export default function SettingsWorkspace({ onCreateIdentity }: SettingsWorkspaceProps) {
  const { t } = useTranslation();
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
        <TelegramSettingsPanel />
        <ModelSettingsPanel />
      </div>
    </div>
  );
}
