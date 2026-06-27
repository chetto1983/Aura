import { LogOut, PanelLeft } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { ApprovalBadge } from '../approvals/ApprovalBadge';
import { ApprovalList } from '../approvals/ApprovalList';
import { LanguageSwitcher } from '../i18n/LanguageSwitcher';
import { ThemeSwitcher } from '../theme/ThemeSwitcher';
import { ModeSwitcher } from './ModeSwitcher';
import { RuntimeStatusChip } from './RuntimeStatusChip';
import type { SurfaceIntent } from './modes';
import { Button } from '@/components/ui/button';

export function ShellHeader({
  activeMode,
  approvalsOpen,
  onModeSelect,
  onApprovalsToggle,
  onApprovalOpen,
  onNavigationOpen,
  onRuntimeOpen,
  logoutPending,
  onLogout,
}: {
  readonly activeMode: SurfaceIntent;
  readonly approvalsOpen: boolean;
  readonly onModeSelect: (mode: SurfaceIntent) => void;
  readonly onApprovalsToggle: () => void;
  readonly onApprovalOpen: (id: string) => void;
  readonly onNavigationOpen: () => void;
  readonly onRuntimeOpen: () => void;
  readonly logoutPending: boolean;
  readonly onLogout: () => void;
}) {
  const { t } = useTranslation();
  const logoutLabel = t('shell.logout');

  return (
    <header className="shell-header grid min-h-16 grid-cols-[auto_auto_minmax(0,1fr)_auto] items-center gap-2 border-b border-border bg-surface px-2 py-2 sm:px-3">
      <Button
        type="button"
        variant="ghost"
        size="icon"
        onClick={onNavigationOpen}
        aria-label={t('shell.openNavigation')}
        className="shell-nav-trigger text-text-muted hover:bg-surface-2 hover:text-text lg:hidden"
      >
        <PanelLeft data-icon="icon" aria-hidden="true" focusable="false" />
      </Button>
      <div className="flex min-w-0 items-center gap-2">
        <img
          src="/logo.png"
          alt="Aura"
          width={44}
          height={44}
          className="h-10 w-10 rounded-[var(--radius-md)] object-cover shadow-[0_8px_28px_rgb(26_115_232_/_0.18)] sm:h-11 sm:w-11"
        />
        <div className="min-w-0">
          <p className="font-display text-lg leading-none text-text">Aura</p>
          <p className="hidden truncate text-[0.75rem] uppercase tracking-[0.18em] text-text-faint sm:block">
            {t('shell.workspace')}
          </p>
        </div>
      </div>
      <ModeSwitcher active={activeMode} onSelect={onModeSelect} />
      <div className="flex min-w-0 items-center justify-end gap-2">
        <RuntimeStatusChip onOpen={onRuntimeOpen} />
        <div className="relative">
          <ApprovalBadge expanded={approvalsOpen} onToggle={onApprovalsToggle} />
          {approvalsOpen ? (
            <div className="absolute right-0 top-full z-20 mt-2 shadow-[var(--shadow-popover)]">
              <ApprovalList onOpen={onApprovalOpen} />
            </div>
          ) : null}
        </div>
        <ThemeSwitcher className="shell-header__wide-only hidden sm:flex" />
        <LanguageSwitcher className="shell-header__wide-only hidden sm:flex" />
        <Button
          type="button"
          variant="outline"
          size="icon"
          aria-label={logoutLabel}
          aria-busy={logoutPending}
          title={logoutLabel}
          disabled={logoutPending}
          onClick={onLogout}
          className="text-text-muted hover:border-border-strong hover:text-text disabled:cursor-wait"
        >
          <LogOut data-icon="icon" aria-hidden="true" focusable="false" />
        </Button>
      </div>
    </header>
  );
}
