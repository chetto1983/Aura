import { useTranslation } from 'react-i18next';
import { ApprovalBadge } from '../approvals/ApprovalBadge';
import { ApprovalList } from '../approvals/ApprovalList';
import { LanguageSwitcher } from '../i18n/LanguageSwitcher';
import { ThemeSwitcher } from '../theme/ThemeSwitcher';
import { ModeSwitcher } from './ModeSwitcher';
import { RuntimeStatusChip } from './RuntimeStatusChip';
import type { SurfaceIntent } from './modes';

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
    <header className="grid min-h-16 grid-cols-[auto_auto_minmax(0,1fr)_auto] items-center gap-2 border-b border-border bg-surface px-2 py-2 sm:px-3">
      <button
        type="button"
        onClick={onNavigationOpen}
        aria-label={t('shell.openNavigation')}
        className="flex min-h-10 min-w-10 items-center justify-center rounded-[var(--radius-md)] text-text-muted outline-none hover:bg-surface-2 hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent lg:hidden"
      >
        <span aria-hidden="true" className="font-mono text-base">
          =
        </span>
      </button>
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
        <ThemeSwitcher className="hidden sm:flex" />
        <LanguageSwitcher className="hidden sm:flex" />
        <button
          type="button"
          aria-label={logoutLabel}
          aria-busy={logoutPending}
          title={logoutLabel}
          disabled={logoutPending}
          onClick={onLogout}
          className="flex min-h-10 min-w-10 shrink-0 items-center justify-center rounded-[var(--radius-md)] border border-border bg-surface-2 text-text-muted outline-none transition hover:border-border-strong hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:cursor-wait disabled:opacity-70"
        >
          <LogoutIcon />
        </button>
      </div>
    </header>
  );
}

function LogoutIcon() {
  return (
    <svg
      width="17"
      height="17"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
      <path d="M16 17l5-5-5-5" />
      <path d="M21 12H9" />
    </svg>
  );
}
