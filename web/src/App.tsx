/** @ts-nocheck */
import { Suspense, lazy, useEffect, useMemo, useRef, useState, useCallback } from 'react';
import { Toaster } from 'sonner';
// CopilotKit removed: pointing it at /agui/run made its runtime parser try to
// JSON.parse raw AG-UI SSE chunks, producing a "Unexpected token 'd', \"data: {..."
// toast on every chat send. We were not actually using its runtime — agent
// streaming goes through useAgent against /agui/run directly. The two hook
// usages below (action + readable) are stubbed as no-ops so the action
// registration sites are preserved for a future re-introduction.
const useCopilotAction = (_def: unknown): void => { void _def; };
const useCopilotReadable = (_def: unknown): void => { void _def; };

import { useAgent } from './hooks/useAgent';
import { useSessions } from './hooks/useSessions';
import { Sidebar } from './components/Sidebar';
import { ChatPanel } from './components/ChatPanel';
import { EventStrip } from './components/EventStrip';
import { GraphSkeleton } from './components/common/AppSkeletons';
import { ThemeToggle } from './components/common/ThemeToggle';

import { useSkillsRegistry } from './hooks/useSkillsRegistry';
import { useSkillInstall } from './hooks/useSkillInstall';
import { useAppTheme } from './hooks/useAppTheme';
import { SkillsDialog } from './components/SkillsDialog';
import { SkillsCommand } from './components/SkillsCommand';
import { SkillDetailSheet } from './components/SkillDetailSheet';
import { StderrLogSheet } from './components/StderrLogSheet';
import { UninstallConfirmDialog } from './components/UninstallConfirmDialog';

// POL-03 / D-06: lazy boundary on WikiGraphView. The whole component
// (incl. react-force-graph-2d, d3-force-3d, ProductDetailPanel) is split
// off into its own chunk so the entry bundle stays under the 120 kB gzip
// budget for users who never open the graph.
const WikiGraphView = lazy(() => import('./components/WikiGraphView'));

const EMPTY_MESSAGES = [];

export default function App() {
  return <AppShell />;
}

function AppShell() {
  const agent = useAgent();
  const sessions = useSessions();
  const [composeValue, setComposeValue] = useState('');
  const [graphOpen, setGraphOpen] = useState(false);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const wasRunning = useRef(false);
  const appTheme = useAppTheme();

  // Plan 07 — Skills Installer wiring
  const [skillsOpen, setSkillsOpen] = useState(false);
  const [commandOpen, setCommandOpen] = useState(false);
  const [detailSource, setDetailSource] = useState<string | null>(null);
  const [stderrContext, setStderrContext] = useState<null | { skill: string; source: string; exitCode: number; stderrTail: string }>(null);
  const [uninstallTarget, setUninstallTarget] = useState<string | null>(null);

  const registry = useSkillsRegistry();
  const skillInstall = useSkillInstall({
    onInstalled: () => { void registry.refresh(); },
    onUninstalled: () => { void registry.refresh(); },
    onStderrLog: (payload) => setStderrContext(payload),
  });

  // Expose installed skills to the agent (D-19)
  useCopilotReadable({
    description: 'Lista delle skill attualmente installate (nome, publisher, verificato).',
    value: registry.installed,
  });

  // Chat-driven install (D-14, D-18)
  useCopilotAction({
    name: 'install_skill',
    description: "Installa una skill dal registry skills.sh. Source format: 'owner/repo' o 'owner/repo/path'.",
    parameters: [
      { name: 'source', type: 'string', description: 'GitHub source (es. anthropics/pdf-reader)', required: true },
    ],
    handler: async ({ source }: { source: string }) => {
      const body = await skillInstall.install(source);
      if (body.ok && !body.error) {
        return `Ho installato ${body.skill?.name ?? source}. Pronta all'uso.`;
      }
      if (body.ok && body.error) {
        return `Installazione riuscita con warning: ${body.error.message}`;
      }
      return `Non sono riuscito a installare ${source}: ${body.error?.message ?? 'errore sconosciuto'}. ${body.error?.recoverable ? 'Vuoi che riprovi?' : ''}`.trim();
    },
  });

  // Chat-driven uninstall (D-18, D-20 — agent asks confirmation BEFORE invoking)
  useCopilotAction({
    name: 'uninstall_skill',
    description: 'Disinstalla una skill. CHIEDI CONFERMA IN CHAT PRIMA DI INVOCARE.',
    parameters: [
      { name: 'name', type: 'string', description: 'Nome della skill da disinstallare', required: true },
    ],
    handler: async ({ name }: { name: string }) => {
      const body = await skillInstall.uninstall(name);
      if (body.ok && !body.error) return `Ho disinstallato ${name}.`;
      return `Non sono riuscito a disinstallare ${name}: ${body.error?.message ?? 'errore sconosciuto'}.`;
    },
  });

  // ESC chiude drawer sidebar mobile
  useEffect(() => {
    if (!sidebarOpen) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setSidebarOpen(false); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [sidebarOpen]);

  // Ctrl+K / Cmd+K global keybind → SkillsCommand palette
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
        const target = e.target as HTMLElement | null;
        const tag = target?.tagName?.toLowerCase();
        const editable = tag === 'input' || tag === 'textarea' || target?.isContentEditable;
        // UI-SPEC: activatable always except when input/textarea (other than palette itself) has focus
        if (editable) return;
        e.preventDefault();
        setCommandOpen((v) => !v);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const sessionMessages = sessions.activeSession?.messages || EMPTY_MESSAGES;

  // Live view: persisted history + currently-streaming agent messages.
  const messages = useMemo(
    () => [...sessionMessages, ...agent.messages],
    [sessionMessages, agent.messages]
  );

  const lastEvent = agent.events[agent.events.length - 1] || '';
  const currentStep = lastEvent.startsWith('STEP:') ? lastEvent.slice(5).trim()
    : lastEvent.startsWith('TOOL:') ? `Tool · ${lastEvent.slice(5).trim()}`
    : '';

  // When a run finishes, persist the streamed messages to the session and clear the buffer.
  useEffect(() => {
    if (wasRunning.current && !agent.isRunning) {
      const sess = sessions.activeSession;
      if (sess && agent.messages.length > 0) {
        sessions.updateMessages(sess.id, [...sess.messages, ...agent.messages]);
        agent.setMessages([]);
      }
    }
    wasRunning.current = agent.isRunning;
  }, [agent.isRunning, agent.messages, sessions, agent]);

  const onSend = useCallback(async (text: string) => {
    const sess = sessions.activeSession;
    if (!sess || !text.trim() || agent.isRunning) return;

    const userMsg = { id: `u_${Date.now()}`, role: 'user' as const, content: text.trim() };
    const nextHistory = [...sess.messages, userMsg];
    sessions.updateMessages(sess.id, nextHistory);

    if (['Banco vendita', 'Nuova sessione', 'Nuova conversazione'].includes(sess.title)) {
      sessions.setTitle(sess.id, text.trim().slice(0, 42));
    }

    await agent.run(text.trim(), sess.threadId, sess.userId, nextHistory);
    // Persistence handled by the post-run effect above.
  }, [agent, sessions]);

  const onNewSession = useCallback(() => {
    setGraphOpen(false);
    sessions.create();
    agent.setMessages([]);
  }, [sessions, agent]);

  const onClearSession = useCallback(() => {
    if (!sessions.activeSession) return;
    setGraphOpen(false);
    sessions.deleteSession(sessions.activeSession.id);
    agent.setMessages([]);
  }, [sessions, agent]);

  const onSwitchSession = useCallback((id: string) => {
    setGraphOpen(false);
    sessions.select(id);
    agent.setMessages([]);
  }, [sessions, agent]);

  const closeSidebar = useCallback(() => setSidebarOpen(false), []);

  // Find details for the currently selected skill (either curated or installed)
  const detailSkill = useMemo(() => {
    if (!detailSource) return null;
    const installed = registry.installed.find((s) => s.source === detailSource);
    if (installed) {
      return {
        name: installed.name,
        publisher: installed.source.split('/')[0] ?? '',
        source: installed.source,
        description: '',
        installed,
        // Permissions would ideally come from the skill metadata; for Phase 1 assume a reasonable default.
        requestedPermissions: ['read_files', 'network'] as Array<'read_files' | 'write_files' | 'run_subprocess' | 'network' | 'env_access'>,
      };
    }
    // Fallback: build a stub from source (Discover-tab selection before install)
    const parts = detailSource.split('/');
    return {
      name: parts[1] ?? detailSource,
      publisher: parts[0] ?? '',
      source: detailSource,
      description: '',
      installed: undefined,
      requestedPermissions: ['read_files', 'network'] as Array<'read_files' | 'write_files' | 'run_subprocess' | 'network' | 'env_access'>,
    };
  }, [detailSource, registry.installed]);

  return (
    <div className="sacchi-app-shell">
      {sidebarOpen && (
        <div
          className="sacchi-backdrop"
          aria-hidden="true"
          onClick={closeSidebar}
        />
      )}
      <Sidebar
        sessions={sessions.sessions}
        activeId={sessions.activeId}
        onNewSession={() => { onNewSession(); closeSidebar(); }}
        onClearSession={onClearSession}
        onSwitchSession={(id) => { onSwitchSession(id); closeSidebar(); }}
        threadId={sessions.activeSession?.threadId || 'banco-vendita'}
        userId={sessions.activeSession?.userId || 'davide'}
        onUpdateThread={() => sessions.touch(sessions.activeId)}
        onUpdateUser={() => sessions.touch(sessions.activeId)}
        isMobileOpen={sidebarOpen}
        onMobileClose={closeSidebar}
        onOpenChat={() => {
          setGraphOpen(false);
          setSkillsOpen(false);
          setCommandOpen(false);
          setDetailSource(null);
          setStderrContext(null);
          setUninstallTarget(null);
          closeSidebar();
        }}
        onOpenSkills={() => {
          setGraphOpen(false);
          setCommandOpen(false);
          setSkillsOpen(true);
          closeSidebar();
        }}
        onOpenGraph={() => { setSkillsOpen(false); setCommandOpen(false); setGraphOpen(true); closeSidebar(); }}
        onOpenCommand={() => { setGraphOpen(false); setCommandOpen(true); closeSidebar(); }}
        themeControl={<ThemeToggle theme={appTheme.theme} onSelect={appTheme.setTheme} />}
      />
      <div className="sacchi-app-shell__main">
        <div className="sacchi-topbar">
          <button
            type="button"
            onClick={() => setSidebarOpen(true)}
            aria-label="Apri menu"
            aria-expanded={sidebarOpen}
            className="sacchi-hamburger"
          >
            <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <line x1="4" y1="7" x2="20" y2="7" />
              <line x1="4" y1="12" x2="20" y2="12" />
              <line x1="4" y1="17" x2="20" y2="17" />
            </svg>
          </button>
          <img src="/logo.svg" alt="Sacchi" height={22} className="sacchi-topbar__logo" />
          <ThemeToggle theme={appTheme.theme} onSelect={appTheme.setTheme} compact />
        </div>
        {graphOpen ? (
          <Suspense fallback={<GraphSkeleton />}>
            <WikiGraphView
              onClose={() => setGraphOpen(false)}
              onSelectProduct={(code) => {
                // D-21: panel coexists with graph — do NOT close the graph here.
                // The chat composer is still pre-filled as a secondary action
                // (invoked by the ProductDetailPanel "Chiedi all'agente" CTA).
                setComposeValue(`Scheda prodotto ${code}`);
              }}
            />
          </Suspense>
        ) : (
          <>
        <ChatPanel
          messages={messages}
          isRunning={agent.isRunning}
          onSend={onSend}
          composeValue={composeValue}
          setComposeValue={setComposeValue}
          placeholder="Cerca un prodotto, chiedi una scheda tecnica…"
          currentStep={currentStep}
        />
        <EventStrip events={agent.events} />
          </>
        )}
      </div>

      {/* Skills Installer UI mounts (Plan 07) */}
      <SkillsDialog
        open={skillsOpen}
        onOpenChange={setSkillsOpen}
        installed={registry.installed}
        onInstall={(source) => void skillInstall.install(source)}
        onUninstall={(name) => setUninstallTarget(name)}
        onOpenDetail={(source) => setDetailSource(source)}
        stateFor={skillInstall.stateFor}
        loading={registry.loading}
        error={registry.error}
      />

      <SkillsCommand
        open={commandOpen}
        onOpenChange={setCommandOpen}
        onSelect={(source) => { void skillInstall.install(source); }}
      />

      {detailSkill && (
        <SkillDetailSheet
          open={!!detailSource}
          onOpenChange={(o) => { if (!o) setDetailSource(null); }}
          skillName={detailSkill.name}
          publisher={detailSkill.publisher}
          source={detailSkill.source}
          description={detailSkill.description}
          requestedPermissions={detailSkill.requestedPermissions}
          installed={detailSkill.installed}
          onInstall={() => void skillInstall.install(detailSkill.source)}
          onUninstall={detailSkill.installed ? () => setUninstallTarget(detailSkill.installed!.name) : undefined}
        />
      )}

      {stderrContext && (
        <StderrLogSheet
          open={!!stderrContext}
          onOpenChange={(o) => { if (!o) setStderrContext(null); }}
          skillName={stderrContext.skill}
          source={stderrContext.source}
          exitCode={stderrContext.exitCode}
          stderrTail={stderrContext.stderrTail}
        />
      )}

      <UninstallConfirmDialog
        open={!!uninstallTarget}
        onOpenChange={(o) => { if (!o) setUninstallTarget(null); }}
        skillName={uninstallTarget ?? ''}
        onConfirm={() => {
          if (uninstallTarget) void skillInstall.uninstall(uninstallTarget);
          setUninstallTarget(null);
        }}
      />

      <Toaster
        position="top-right"
        offset={32}
        duration={3000}
        closeButton
      />
    </div>
  );
}
