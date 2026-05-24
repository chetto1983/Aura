import { useState } from 'react';
import { useNavigate, NavLink } from 'react-router-dom';
import { toast } from 'sonner';
import { LayoutDashboard, BookText, Network, Inbox, Calendar, Bot, Sparkles, Plug, ShieldAlert, ShieldCheck, MessagesSquare, FileCheck, Wrench, Archive, FolderTree, MessageCircle, Settings as SettingsIcon, Sun, Moon, Contrast, LogOut } from 'lucide-react';
import { useAppTheme, type AppTheme } from '@/hooks/useAppTheme';
import { useLocale } from '@/hooks/useLocale';
import { api } from '@/api';
import { clearToken } from '@/lib/auth';

const ITEMS = [
  { to: '/', icon: LayoutDashboard },
  { to: '/wiki', icon: BookText },
  { to: '/graph', icon: Network },
  { to: '/sources', icon: Inbox },
  { to: '/tasks', icon: Calendar },
  { to: '/swarm', icon: Bot },
  { to: '/skills', icon: Sparkles },
  { to: '/mcp', icon: Plug },
  { to: '/pending', icon: ShieldCheck },
  { to: '/conversations', icon: MessagesSquare },
  { to: '/summaries', icon: FileCheck },
  { to: '/quarantine', icon: ShieldAlert },
  { to: '/maintenance', icon: Wrench },
  { to: '/backups', icon: Archive },
  { to: '/files', icon: FolderTree },
  { to: '/chat', icon: MessageCircle },
  { to: '/settings', icon: SettingsIcon },
];

const ROUTE_LABELS: Record<string, string> = {
  '/': 'sidebar.home',
  '/wiki': 'sidebar.wiki',
  '/graph': 'sidebar.graph',
  '/sources': 'sidebar.sources',
  '/tasks': 'sidebar.tasks',
  '/swarm': 'sidebar.swarm',
  '/skills': 'sidebar.skills',
  '/mcp': 'sidebar.mcp',
  '/pending': 'sidebar.pending',
  '/conversations': 'sidebar.conversations',
  '/summaries': 'sidebar.summaries',
  '/quarantine': 'sidebar.quarantine',
  '/maintenance': 'sidebar.maintenance',
  '/backups': 'sidebar.backups',
  '/files': 'sidebar.files',
  '/chat': 'sidebar.chat',
  '/settings': 'sidebar.settings',
};

const THEME_ICON: Record<AppTheme, typeof Sun> = {
  light: Sun,
  dark: Moon,
  contrast: Contrast,
};
const THEME_LABEL: Record<AppTheme, string> = {
  light: 'sidebar.lightTheme',
  dark: 'sidebar.darkTheme',
  contrast: 'sidebar.highContrast',
};

// BrandMark renders the Aura mark inline: a glowing disc cradling a
// filled A whose left half tints with --primary (theme cyan) and right
// half stays violet, wrapped by an asymmetric swirl + scattered sparkles.
// Mirrors Logo/Logo.png at any DPI — SVG so it scales crisply at 16/24/36px.
function BrandMark() {
  return (
    <svg
      width="36"
      height="36"
      viewBox="0 0 40 40"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true"
      className="shrink-0"
    >
      <defs>
        <radialGradient id="aura-orb" cx="50%" cy="40%" r="60%">
          <stop offset="0%" stopColor="var(--primary)" stopOpacity="0.85" />
          <stop offset="55%" stopColor="var(--primary)" stopOpacity="0.2" />
          <stop offset="100%" stopColor="var(--primary)" stopOpacity="0" />
        </radialGradient>
        <clipPath id="aura-a-left">
          <rect x="0" y="0" width="20" height="40" />
        </clipPath>
        <clipPath id="aura-a-right">
          <rect x="20" y="0" width="20" height="40" />
        </clipPath>
      </defs>
      <circle cx="20" cy="20" r="18" fill="url(#aura-orb)" />
      {/* asymmetric swirl — suggests the logo's dynamic halo */}
      <path
        d="M6 24 C 6 12, 20 6, 32 12 C 38 16, 36 28, 28 33"
        stroke="var(--primary)"
        strokeOpacity="0.55"
        strokeWidth="0.9"
        strokeLinecap="round"
        fill="none"
      />
      <path
        d="M11 9 C 16 7, 28 9, 34 18"
        stroke="#c084fc"
        strokeOpacity="0.45"
        strokeWidth="0.7"
        strokeLinecap="round"
        fill="none"
      />
      {/* A — filled triangle with inner cutout, split cyan/violet via clipPath */}
      <path
        d="M10 31 L20 9 L30 31 L25 31 L23 25.5 L17 25.5 L15 31 Z M18 21.5 L20 16 L22 21.5 Z"
        fill="var(--primary)"
        clipPath="url(#aura-a-left)"
        fillRule="evenodd"
      />
      <path
        d="M10 31 L20 9 L30 31 L25 31 L23 25.5 L17 25.5 L15 31 Z M18 21.5 L20 16 L22 21.5 Z"
        fill="#a855f7"
        clipPath="url(#aura-a-right)"
        fillRule="evenodd"
      />
      {/* scattered sparkle pricks — keep the energy/aura feel at small sizes */}
      <circle cx="7.5" cy="14" r="0.9" fill="var(--primary)" opacity="0.9" />
      <circle cx="33" cy="10" r="0.7" fill="#c084fc" opacity="0.85" />
      <circle cx="34" cy="26" r="0.8" fill="#a855f7" opacity="0.8" />
      <circle cx="9" cy="31" r="0.6" fill="var(--primary)" opacity="0.7" />
    </svg>
  );
}

// Sidebar renders the same content for desktop (always-on aside) and the
// mobile drawer (rendered inside a SheetContent). When `onNavigate` is
// passed it fires after each NavLink click + after Sign out, so the
// mobile shell can close the drawer once the user picks a destination.
export function Sidebar({ onNavigate }: { onNavigate?: () => void } = {}) {
  const { t } = useLocale();
  const { theme, cycleTheme } = useAppTheme();
  const ThemeIcon = THEME_ICON[theme];
  const navigate = useNavigate();
  const [loggingOut, setLoggingOut] = useState(false);

  const handleLogout = async () => {
    if (loggingOut) return;
    setLoggingOut(true);
    // Best-effort revoke. If the API call fails, the token is still
    // cleared client-side so the user can't keep using the dashboard
    // — server-side revoke is a hardening, not a correctness gate.
    try {
      await api.logout();
    } catch {
      // ignore — fall through to client-side cleanup
    }
    clearToken();
    toast.success(t('sidebar.signedOut'));
    onNavigate?.();
    navigate('/login', { replace: true });
  };

  return (
    <aside className="w-60 h-full shrink-0 border-r bg-sidebar flex flex-col">
      <div className="p-4 border-b flex items-center gap-3">
        <BrandMark />
        <div>
          <h1 className="text-lg font-semibold leading-none tracking-tight">Aura</h1>
          <p className="mt-1 text-[10px] uppercase tracking-[0.18em] text-muted-foreground">{t('sidebar.tagline')}</p>
        </div>
      </div>
      <nav className="flex-1 p-2 space-y-1">
        {ITEMS.map(({ to, icon: Icon }) => (
          <NavLink
            key={to}
            to={to}
            end={to === '/'}
            onClick={() => onNavigate?.()}
            className={({ isActive }) =>
              `relative flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors ${
                isActive
                  ? 'bg-primary/10 text-primary font-medium ring-1 ring-primary/20 shadow-[0_0_20px_-8px_var(--primary)]'
                  : 'hover:bg-accent/60 text-muted-foreground hover:text-foreground'
              }`
            }
          >
            <Icon size={16} />
            {t(ROUTE_LABELS[to])}
          </NavLink>
        ))}
      </nav>
      <div className="p-2 border-t space-y-1">
        <button
          type="button"
          onClick={cycleTheme}
          className="w-full flex items-center gap-3 rounded-md px-3 py-2 text-sm hover:bg-accent/50 text-muted-foreground"
          title={t('sidebar.cycleTheme')}
        >
          <ThemeIcon size={16} />
          {t(THEME_LABEL[theme])}
        </button>
        <button
          type="button"
          disabled={loggingOut}
          onClick={() => void handleLogout()}
          className="w-full flex items-center gap-3 rounded-md px-3 py-2 text-sm hover:bg-accent/50 text-muted-foreground disabled:opacity-50 disabled:cursor-wait"
          title={t('sidebar.revokeToken')}
        >
          <LogOut size={16} />
          {loggingOut ? t('sidebar.signingOut') : t('sidebar.signOut')}
        </button>
      </div>
    </aside>
  );
}
