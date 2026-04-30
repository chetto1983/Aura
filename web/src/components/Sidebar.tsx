/** @ts-nocheck */
import { MessageSquare, Puzzle, Network } from 'lucide-react';
import type { Session } from '../hooks/useSessions';

interface Props {
  sessions: Session[];
  activeId: string;
  onNewSession: () => void;
  onClearSession: () => void;
  onSwitchSession: (id: string) => void;
  threadId: string;
  userId: string;
  onUpdateThread: () => void;
  onUpdateUser: () => void;
  isMobileOpen?: boolean;
  onMobileClose?: () => void;
  onOpenChat: () => void;
  onOpenSkills: () => void;
  onOpenGraph?: () => void;
  onOpenCommand?: () => void;
  themeControl?: React.ReactNode;
}

export function Sidebar(props: Props) {
  const {
    sessions, activeId, onNewSession, onClearSession, onSwitchSession,
    threadId, userId, isMobileOpen, onMobileClose, onOpenChat, onOpenSkills, onOpenGraph, themeControl,
  } = props;

  return (
    <aside
      aria-label="Sidebar"
      className={`sacchi-sidebar${isMobileOpen ? ' sacchi-sidebar--open' : ''}`}
    >
      <header className="sacchi-sidebar__header">
        <a
          href="/"
          aria-label="Sacchi — home"
          className="sacchi-sidebar__brand"
        >
          <img
            src="/logo.svg"
            alt="Sacchi Automazione Sicura"
            height={32}
            className="sacchi-sidebar__logo"
          />
        </a>
        {onMobileClose && (
          <button
            type="button"
            onClick={onMobileClose}
            aria-label="Chiudi menu"
            className="sacchi-sidebar__close"
          >
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <line x1="18" y1="6" x2="6" y2="18" />
              <line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        )}
      </header>
      <p className="sacchi-sidebar__tagline">
        AG-UI Console
      </p>
      {themeControl && (
        <div className="sacchi-sidebar__theme-wrap">
          {themeControl}
        </div>
      )}

      <button
        type="button"
        onClick={onNewSession}
        className="sacchi-sidebar__new"
      >
        <span className="sacchi-sidebar__new-plus">+</span>
        Nuova conversazione
      </button>

      <SectionLabel>Conversazioni</SectionLabel>
      <div className="sacchi-sidebar__sessions">
        {sessions.map(sess => {
          const active = sess.id === activeId;
          const userMsgCount = sess.messages.filter(m => m.role === 'user').length;
          return (
            <button
              key={sess.id}
              type="button"
              onClick={() => onSwitchSession(sess.id)}
              className={`sacchi-sidebar__session${active ? ' sacchi-sidebar__session--active' : ''}`}
            >
              <span className="sacchi-sidebar__session-title">
                {sess.title || 'Sessione'}
              </span>
              <span className="sacchi-sidebar__session-meta">
                {userMsgCount} msg · {new Date(sess.updatedAt).toLocaleDateString('it-IT', { day: 'numeric', month: 'short' })}
              </span>
            </button>
          );
        })}
      </div>

      <button
        type="button"
        onClick={onClearSession}
        className="sacchi-sidebar__clear"
      >
        Cancella conversazione corrente
      </button>

      <div className="sacchi-sidebar__spacer" />

      <div>
        <SectionLabel>Sessione</SectionLabel>
        <div className="sacchi-sidebar__fields">
          <Field label="Thread" value={threadId} />
          <Field label="Utente" value={userId} />
        </div>
      </div>

      <div className="sacchi-sidebar__nav">
        <SidebarNavButton
          onClick={onOpenChat}
          aria-label="Torna alla chat"
          data-testid="sidebar-chat-trigger"
          icon={<MessageSquare size={16} />}
          label="Chat"
        />
        <SidebarNavButton
          onClick={onOpenSkills}
          aria-label="Gestisci skills"
          data-testid="sidebar-puzzle-trigger"
          icon={<Puzzle size={16} />}
          label="Skills"
        />
        {onOpenGraph && (
          <SidebarNavButton
            onClick={onOpenGraph}
            aria-label="Apri Wiki Graph"
            data-testid="sidebar-graph-trigger"
            icon={<Network size={16} />}
            label="Grafo wiki"
          />
        )}
      </div>

      <div className="sacchi-sidebar__badge">
        <span className="sacchi-sidebar__badge-dot" />
        Backend Agno · AG-UI Protocol
      </div>
    </aside>
  );
}

function SidebarNavButton({ onClick, icon, label, ...rest }: {
  onClick: () => void;
  icon: React.ReactNode;
  label: string;
  [k: string]: unknown;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="sacchi-sidebar__nav-btn"
      {...rest}
    >
      {icon}
      <span>{label}</span>
    </button>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="sacchi-sidebar__section">{children}</div>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <label className="sacchi-sidebar__field">
      <span className="sacchi-sidebar__field-label">{label}</span>
      <input
        readOnly
        value={value}
        className="sacchi-sidebar__field-input"
      />
    </label>
  );
}

export default Sidebar;
