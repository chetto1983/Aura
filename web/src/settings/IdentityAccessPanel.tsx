import { UserPlus } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { CapabilityAdminPanel } from './CapabilityAdminPanel';
import { StandingApprovalsPanel } from './StandingApprovalsPanel';
import { Button } from '@/components/ui/button';

interface IdentityAccessPanelProps {
  readonly onCreateIdentity: () => void;
}

// Creating an identity, revoking the standing approvals the operator handed the agent, and
// deciding what another identity may do are one job. They are one pane: roster first, the
// operator's own standing consent second, per-identity capabilities last.
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
        <StandingApprovalsPanel />
      </div>
      <div className="border-t border-border pt-6">
        <CapabilityAdminPanel />
      </div>
    </div>
  );
}
