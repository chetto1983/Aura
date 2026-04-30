import { useNavigate, NavLink } from 'react-router-dom';
import { toast } from 'sonner';
import { LayoutDashboard, BookText, Network, Inbox, Calendar, Sparkles, Plug, ShieldCheck, Sun, Moon, Contrast, LogOut } from 'lucide-react';
import { useAppTheme, type AppTheme } from '@/hooks/useAppTheme';
import { api } from '@/api';
import { clearToken } from '@/lib/auth';

const ITEMS = [
  { to: '/', label: 'Home', icon: LayoutDashboard },
  { to: '/wiki', label: 'Wiki', icon: BookText },
  { to: '/graph', label: 'Graph', icon: Network },
  { to: '/sources', label: 'Sources', icon: Inbox },
  { to: '/tasks', label: 'Tasks', icon: Calendar },
  { to: '/skills', label: 'Skills', icon: Sparkles },
  { to: '/mcp', label: 'MCP', icon: Plug },
  { to: '/pending', label: 'Pending', icon: ShieldCheck },
];

const THEME_ICON: Record<AppTheme, typeof Sun> = {
  light: Sun,
  dark: Moon,
  contrast: Contrast,
};
const THEME_LABEL: Record<AppTheme, string> = {
  light: 'Light theme',
  dark: 'Dark theme',
  contrast: 'High contrast',
};

// BrandMark is a stylized rendition of the Aura orb: a glowing disc
// with the cyan-blue arrow-A from the logo. Stays as inline SVG so it
// inherits color and tints with theme variables instead of needing a
// PNG per palette.
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
          <stop offset="0%" stopColor="var(--primary)" stopOpacity="0.8" />
          <stop offset="55%" stopColor="var(--primary)" stopOpacity="0.18" />
          <stop offset="100%" stopColor="var(--primary)" stopOpacity="0" />
        </radialGradient>
      </defs>
      <circle cx="20" cy="20" r="18" fill="url(#aura-orb)" />
      <circle cx="20" cy="20" r="14" stroke="var(--primary)" strokeOpacity="0.45" strokeWidth="1.2" />
      {/* arrow-A glyph from the logo */}
      <path
        d="M12 28 L20 11 L28 28 M16.5 22 L24 22 M24 11 L28 11 L28 15"
        stroke="var(--primary)"
        strokeWidth="2.2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

// Sidebar renders the same content for desktop (always-on aside) and the
// mobile drawer (rendered inside a SheetContent). When `onNavigate` is
// passed it fires after each NavLink click + after Sign out, so the
// mobile shell can close the drawer once the user picks a destination.
export function Sidebar({ onNavigate }: { onNavigate?: () => void } = {}) {
  const { theme, cycleTheme } = useAppTheme();
  const ThemeIcon = THEME_ICON[theme];
  const navigate = useNavigate();

  const handleLogout = async () => {
    // Best-effort revoke. If the API call fails, the token is still
    // cleared client-side so the user can't keep using the dashboard
    // — server-side revoke is a hardening, not a correctness gate.
    try {
      await api.logout();
    } catch {
      // ignore — fall through to client-side cleanup
    }
    clearToken();
    toast.success('Signed out.');
    onNavigate?.();
    navigate('/login', { replace: true });
  };

  return (
    <aside className="w-60 h-full shrink-0 border-r bg-sidebar flex flex-col">
      <div className="p-4 border-b flex items-center gap-3">
        <BrandMark />
        <div>
          <h1 className="text-lg font-semibold leading-none tracking-tight">Aura</h1>
          <p className="mt-1 text-[10px] uppercase tracking-[0.18em] text-muted-foreground">Second brain</p>
        </div>
      </div>
      <nav className="flex-1 p-2 space-y-1">
        {ITEMS.map(({ to, label, icon: Icon }) => (
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
            {label}
          </NavLink>
        ))}
      </nav>
      <div className="p-2 border-t space-y-1">
        <button
          type="button"
          onClick={cycleTheme}
          className="w-full flex items-center gap-3 rounded-md px-3 py-2 text-sm hover:bg-accent/50 text-muted-foreground"
          title="Cycle light → dark → contrast"
        >
          <ThemeIcon size={16} />
          {THEME_LABEL[theme]}
        </button>
        <button
          type="button"
          onClick={() => void handleLogout()}
          className="w-full flex items-center gap-3 rounded-md px-3 py-2 text-sm hover:bg-accent/50 text-muted-foreground"
          title="Revoke this token and return to login"
        >
          <LogOut size={16} />
          Sign out
        </button>
      </div>
    </aside>
  );
}
