import { NavLink } from 'react-router-dom';
import { LayoutDashboard, BookText, Network, Inbox, Calendar, Sun, Moon, Contrast } from 'lucide-react';
import { useAppTheme, type AppTheme } from '@/hooks/useAppTheme';

const ITEMS = [
  { to: '/', label: 'Home', icon: LayoutDashboard },
  { to: '/wiki', label: 'Wiki', icon: BookText },
  { to: '/graph', label: 'Graph', icon: Network },
  { to: '/sources', label: 'Sources', icon: Inbox },
  { to: '/tasks', label: 'Tasks', icon: Calendar },
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

export function Sidebar() {
  const { theme, cycleTheme } = useAppTheme();
  const ThemeIcon = THEME_ICON[theme];
  return (
    <aside className="w-60 shrink-0 border-r bg-card flex flex-col">
      <div className="p-4 border-b">
        <h1 className="text-lg font-semibold">Aura</h1>
        <p className="text-xs text-muted-foreground">v3.0</p>
      </div>
      <nav className="flex-1 p-2 space-y-1">
        {ITEMS.map(({ to, label, icon: Icon }) => (
          <NavLink
            key={to}
            to={to}
            end={to === '/'}
            className={({ isActive }) =>
              `flex items-center gap-3 rounded-md px-3 py-2 text-sm ${
                isActive ? 'bg-accent text-accent-foreground' : 'hover:bg-accent/50 text-muted-foreground'
              }`
            }
          >
            <Icon size={16} />
            {label}
          </NavLink>
        ))}
      </nav>
      <div className="p-2 border-t">
        <button
          type="button"
          onClick={cycleTheme}
          className="w-full flex items-center gap-3 rounded-md px-3 py-2 text-sm hover:bg-accent/50 text-muted-foreground"
          title="Cycle light → dark → contrast"
        >
          <ThemeIcon size={16} />
          {THEME_LABEL[theme]}
        </button>
      </div>
    </aside>
  );
}
