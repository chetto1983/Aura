import { useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { useCapabilities } from '../admin/useAdmin';
import { IdentityAccessPanel } from './IdentityAccessPanel';
import { ModelSettingsPanel } from './ModelSettingsPanel';
import { ChatGPTPlanConnection } from './ChatGPTPlanConnection';
import { ProfilePanel } from './ProfilePanel';
import { SettingsRail } from './SettingsRail';
import { SharedLinksSection } from './SharedLinksSection';
import { TelegramLinkedAccounts } from './TelegramLinkedAccounts';
import { TelegramSettingsPanel } from './TelegramSettingsPanel';
import { TELEGRAM_ACCOUNTS_KEY } from './settingsApi';
import { MailAccountsSection } from './mail/MailAccountsSection';
import { RemoteAccessPanel } from './remoteAccess/RemoteAccessPanel';
import { visibleSections, type SettingsSectionId } from './settingsSections';
import { useSettingsSection } from './useSettingsSection';

interface SettingsWorkspaceProps {
  readonly onCreateIdentity: () => void;
}

// SettingsWorkspace is every identity's settings surface (spec A1, 2026-11-20). It used to stack
// every panel on one scroll — nine headings, seventeen action buttons, and every panel's queries
// firing at once. It is a rail + pane now: the rail is the map, and only the selected pane mounts,
// so opening Settings no longer costs a roster fetch, a Telegram availability probe and a settings
// read nobody asked for. A member's rail carries only the personal sections (profile, mail and
// calendar); the runtime and access panes are the administrator's, dropped from the rail here and
// gated server-side by RequireCapability on every route.
export default function SettingsWorkspace({ onCreateIdentity }: SettingsWorkspaceProps) {
  const { t } = useTranslation();
  const { isAdmin, isLoading } = useCapabilities();
  const sections = visibleSections(isAdmin);
  const { section, setSection } = useSettingsSection(sections);

  if (isLoading) {
    return (
      <div role="status" className="grid min-h-40 place-items-center bg-bg text-sm text-text-muted">
        {t('settings.loading')}
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col bg-bg lg:flex-row">
      <SettingsRail sections={sections} activeId={section} onSelect={setSection} />
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-5xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
          <SettingsPane section={section} isAdmin={isAdmin} onCreateIdentity={onCreateIdentity} />
        </div>
      </div>
    </div>
  );
}

function SettingsPane({
  section,
  isAdmin,
  onCreateIdentity,
}: {
  readonly section: SettingsSectionId;
  readonly isAdmin: boolean;
  readonly onCreateIdentity: () => void;
}) {
  const client = useQueryClient();
  switch (section) {
    case 'profile':
      return isAdmin ? <ProfilePanel /> : <MemberProfile />;
    case 'mail':
      return <MailAccountsSection />;
    case 'model':
      return <ModelSettingsPanel groups={['routing']} />;
    case 'budget':
      return <ModelSettingsPanel groups={['tokens']} />;
    case 'backends':
      return <ModelSettingsPanel groups={['backends']} />;
    case 'identities':
      return <IdentityAccessPanel onCreateIdentity={onCreateIdentity} />;
    case 'channels':
      return (
        <div className="flex flex-col gap-8">
          <TelegramSettingsPanel
            onLinked={() => void client.invalidateQueries({ queryKey: TELEGRAM_ACCOUNTS_KEY })}
          />
          <TelegramLinkedAccounts />
        </div>
      );
    case 'sharing':
      return <SharedLinksSection />;
    case 'remote-access':
      return <RemoteAccessPanel />;
  }
}

// A member's ChatGPT account sits on their profile: an administrator connects it in model
// routing, the pane a member does not have.
function MemberProfile() {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col items-start gap-6">
      <ProfilePanel />
      <section aria-labelledby="own-chatgpt-account" className="flex w-full flex-col gap-3">
        <h2 id="own-chatgpt-account" className="text-[20px] font-semibold text-text">
          {t('settings.chatgpt.accountHeading')}
        </h2>
        <p className="text-[13px] text-text-muted">{t('settings.chatgpt.accountBody')}</p>
        <ChatGPTPlanConnection />
      </section>
    </div>
  );
}
