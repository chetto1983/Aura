import { UserPlus } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { CapabilityAdminPanel } from './CapabilityAdminPanel';
import { Button } from '@/components/ui/button';

interface IdentityAccessPanelProps {
  readonly onCreateIdentity: () => void;
}

// Creating an identity and deciding what it may do is one job, split across two sections by a
// divider on the old single-scroll page. They are one pane now: roster first, grants second.
export function IdentityAccessPanel({ onCreateIdentity }: IdentityAccessPanelProps) {
  const { t } = useTranslation();

  return (
    <div className="flex flex-col gap-7">
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
    </div>
  );
}
