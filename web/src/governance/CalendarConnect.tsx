import { useId } from 'react';
import { Settings } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useCapabilities } from '../admin/useAdmin';
import { PimProviderAppsPanel } from './PimProviderAppsPanel';
import { Button } from '@/components/ui/button';

// CalendarConnect — what the calendar MCP server's detail (aura-pim-mcp) says about accounts. An
// account is the identity's own, so it is listed, added and repaired in Settings → «Posta e
// calendario», which this section links to; Governance keeps only the administrator's provider
// OAuth apps (PimProviderAppsPanel), which every account's Google or Microsoft sign-in rides on.

export function CalendarConnect() {
  const { t } = useTranslation();
  const headingId = useId();
  const { isAdmin } = useCapabilities();

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-3">
      <h4
        id={headingId}
        className="text-[13px] font-semibold uppercase tracking-wide text-text-muted"
      >
        {t('governance.mcp.calendar.heading')}
      </h4>
      <Button asChild variant="outline" className="self-start whitespace-normal">
        <a href="/?settings=mail">
          <Settings data-icon aria-hidden="true" />
          {t('governance.mcp.calendar.accountsInSettings')}
        </a>
      </Button>
      {isAdmin ? <PimProviderAppsPanel /> : null}
    </section>
  );
}
