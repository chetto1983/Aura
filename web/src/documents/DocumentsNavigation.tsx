import { NavLink, Link, useLocation } from 'react-router';
import { useTranslation } from 'react-i18next';
import { LayoutDashboard, Files, Inbox, LoaderCircle, Bookmark, Tags, Folder } from 'lucide-react';
import { DOCUMENT_DESTINATIONS, savedViewHref } from './documentRoute';
import type { SavedView } from './workspaceApi';
import { DeleteSavedViewButton } from './SavedViews';

const icons = [LayoutDashboard, Files, Inbox, LoaderCircle, Bookmark, Tags, Folder];
export function DocumentsNavigation({
  identity,
  views,
  compact = false,
  onNavigate,
}: {
  readonly identity: string;
  readonly views: readonly SavedView[];
  readonly compact?: boolean;
  readonly onNavigate: () => void;
}) {
  const { t } = useTranslation();
  const { pathname } = useLocation();
  return (
    <nav
      aria-label={t('documents.workspace.nav')}
      className="flex min-h-0 flex-1 flex-col gap-1 overflow-y-auto p-2"
    >
      {DOCUMENT_DESTINATIONS.map((destination, index) => {
        const Icon = icons[index] ?? Files;
        const label = t(`documents.workspace.${destination}`);
        return (
          <NavLink
            key={destination}
            to={`/documents/${destination}`}
            title={compact ? label : undefined}
            onClick={onNavigate}
            className={({ isActive }) =>
              `flex min-h-[44px] items-center gap-3 rounded-lg px-3 text-sm font-medium ${isActive || (destination === 'overview' && pathname === '/documents') ? 'bg-info/10 text-info' : 'text-text-muted hover:bg-surface-2 hover:text-text'}`
            }
          >
            <Icon aria-hidden="true" className="size-4 shrink-0" />
            <span className={compact ? 'sr-only' : ''}>{label}</span>
          </NavLink>
        );
      })}
      {!compact && views.some((view) => view.pinned) && (
        <div className="mt-4 border-t border-border pt-3">
          <p className="px-3 pb-2 text-xs text-text-muted">{t('documents.workspace.views')}</p>
          {views
            .filter((view) => view.pinned)
            .map((view) => (
              <div key={view.id} className="flex items-center rounded-lg hover:bg-surface-2">
                <Link
                  to={savedViewHref(view)}
                  onClick={onNavigate}
                  className="min-w-0 flex-1 break-words rounded-lg px-3 py-2 text-sm"
                >
                  {view.name}
                </Link>
                <DeleteSavedViewButton identity={identity} view={view} iconOnly />
              </div>
            ))}
        </div>
      )}
    </nav>
  );
}
