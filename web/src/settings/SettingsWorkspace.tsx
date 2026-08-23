import { useTranslation } from 'react-i18next';
import { ShieldAlert } from 'lucide-react';
import { useCapabilities } from '../admin/useAdmin';
import { IdentityAccessPanel } from './IdentityAccessPanel';
import { ModelSettingsPanel } from './ModelSettingsPanel';
import { ProfilePanel } from './ProfilePanel';
import { SettingsRail } from './SettingsRail';
import { SharedLinksSection } from './SharedLinksSection';
import { TelegramSettingsPanel } from './TelegramSettingsPanel';
import { visibleSections, type SettingsSectionId } from './settingsSections';
import { useSettingsSection } from './useSettingsSection';

interface SettingsWorkspaceProps {
  readonly onCreateIdentity: () => void;
}

// SettingsWorkspace is the admin surface (MUSR-01 / D-03). It used to stack every panel on one
// scroll — nine headings, seventeen action buttons, and every panel's queries firing at once.
// It is a rail + pane now: the rail is the map, and only the selected pane mounts, so opening
// Settings no longer costs a roster fetch, a Telegram availability probe and a settings read
// the operator did not ask for. The whole surface stays gated on governance.write — a
// non-admin sees only the profile pane (defense-in-depth behind the server-side
// RequireCapability gate on every route).
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

  // The profile is every operator's, the runtime settings are the admin's. A non-admin gets
  // the editor and the explanation, not a bare "not authorized" page that hides the one thing
  // on this page they DO own — and no rail, because a one-item rail is a label, not a choice.
  if (!isAdmin) {
    return (
      <div className="h-full min-h-0 overflow-y-auto bg-bg">
        <div className="mx-auto flex w-full max-w-3xl flex-col items-start gap-6 px-4 py-10 sm:px-6">
          <ProfilePanel />
          <div className="flex w-full flex-col items-start gap-3 border-t border-border pt-6">
            <span className="grid size-12 place-items-center rounded-full border border-border bg-surface text-accent-text">
              <ShieldAlert aria-hidden="true" />
            </span>
            <h2 className="font-display text-[22px] font-semibold text-text">
              {t('admin.notAuthorized.heading')}
            </h2>
            <p className="text-[15.5px] leading-relaxed text-text-muted">
              {t('admin.notAuthorized.body')}
            </p>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col bg-bg lg:flex-row">
      <SettingsRail sections={sections} activeId={section} onSelect={setSection} />
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-5xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
          <SettingsPane section={section} onCreateIdentity={onCreateIdentity} />
        </div>
      </div>
    </div>
  );
}

function SettingsPane({
  section,
  onCreateIdentity,
}: {
  readonly section: SettingsSectionId;
  readonly onCreateIdentity: () => void;
}) {
  switch (section) {
    case 'profile':
      return <ProfilePanel />;
    case 'model':
      return <ModelSettingsPanel groups={['routing']} />;
    case 'budget':
      return <ModelSettingsPanel groups={['tokens']} />;
    case 'backends':
      return <ModelSettingsPanel groups={['backends']} />;
    case 'identities':
      return <IdentityAccessPanel onCreateIdentity={onCreateIdentity} />;
    case 'channels':
      return <TelegramSettingsPanel />;
    case 'sharing':
      return <SharedLinksSection />;
  }
}
