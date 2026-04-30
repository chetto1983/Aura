import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Menu, Keyboard } from 'lucide-react';
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet';
import { Sidebar } from '@/components/Sidebar';

// Shell wraps the auth'd dashboard pages. On md+ the sidebar is always
// visible to the left; under md it collapses to a button-triggered
// slide-over (radix Sheet under the hood). Also installs the global
// keyboard shortcut handler — `?` opens the help dialog and `g X`
// chords navigate.
export function Shell({ children }: { children: React.ReactNode }) {
  const [mobileOpen, setMobileOpen] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);
  useKeyboardShortcuts({ onShowHelp: () => setHelpOpen(true) });

  return (
    <div className="flex h-screen w-screen overflow-hidden">
      {/* Desktop sidebar */}
      <div className="hidden md:flex">
        <Sidebar />
      </div>

      {/* Mobile sidebar drawer */}
      <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
        <SheetContent side="left" className="w-72 p-0 [&>button]:hidden">
          <SheetTitle className="sr-only">Navigation</SheetTitle>
          <Sidebar onNavigate={() => setMobileOpen(false)} />
        </SheetContent>
      </Sheet>

      <div className="flex-1 flex flex-col overflow-hidden">
        {/* Mobile top bar with hamburger */}
        <header className="md:hidden flex items-center gap-3 border-b bg-card px-3 py-2">
          <button
            type="button"
            onClick={() => setMobileOpen(true)}
            className="rounded-md p-2 hover:bg-accent"
            aria-label="Open navigation"
          >
            <Menu size={18} />
          </button>
          <h1 className="text-base font-semibold">Aura</h1>
        </header>

        <main className="flex-1 overflow-auto">{children}</main>
      </div>

      <ShortcutHelpDialog open={helpOpen} onOpenChange={setHelpOpen} />
    </div>
  );
}

// useKeyboardShortcuts installs a single global keydown listener with a
// tiny state machine for "g X" chords. We deliberately ignore key events
// when the focused element is an input/textarea/select/contenteditable so
// chords don't hijack form typing.
function useKeyboardShortcuts({ onShowHelp }: { onShowHelp: () => void }) {
  const navigate = useNavigate();

  useEffect(() => {
    let pendingG = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const isTypingTarget = (el: EventTarget | null): boolean => {
      if (!(el instanceof HTMLElement)) return false;
      const tag = el.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true;
      if (el.isContentEditable) return true;
      return false;
    };

    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (isTypingTarget(e.target)) return;

      // ? help (Shift+/ on most layouts)
      if (e.key === '?') {
        e.preventDefault();
        onShowHelp();
        return;
      }

      if (pendingG) {
        let dest: string | null = null;
        switch (e.key.toLowerCase()) {
          case 'h': dest = '/'; break;
          case 'w': dest = '/wiki'; break;
          case 'g': dest = '/graph'; break;
          case 's': dest = '/sources'; break;
          case 't': dest = '/tasks'; break;
          case 'k': dest = '/skills'; break;
          case 'm': dest = '/mcp'; break;
        }
        pendingG = false;
        if (timer) { clearTimeout(timer); timer = null; }
        if (dest) {
          e.preventDefault();
          navigate(dest);
        }
        return;
      }

      if (e.key.toLowerCase() === 'g') {
        pendingG = true;
        // Reset the chord if the second key doesn't arrive within 1.2s.
        timer = setTimeout(() => { pendingG = false; timer = null; }, 1200);
      }
    };

    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('keydown', onKey);
      if (timer) clearTimeout(timer);
    };
  }, [navigate, onShowHelp]);
}

function ShortcutHelpDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
}) {
  if (!open) return null;
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={() => onOpenChange(false)}
    >
      <div
        className="w-full max-w-sm rounded-lg border bg-card p-5 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 mb-3">
          <Keyboard size={18} />
          <h2 className="text-base font-semibold">Keyboard shortcuts</h2>
        </div>
        <table className="w-full text-sm">
          <tbody>
            <Row keys={['?']} desc="Show this help" />
            <Row keys={['g', 'h']} desc="Home" />
            <Row keys={['g', 'w']} desc="Wiki" />
            <Row keys={['g', 'g']} desc="Graph" />
            <Row keys={['g', 's']} desc="Sources" />
            <Row keys={['g', 't']} desc="Tasks" />
            <Row keys={['g', 'k']} desc="Skills" />
            <Row keys={['g', 'm']} desc="MCP" />
          </tbody>
        </table>
        <div className="mt-4 flex justify-end">
          <button
            type="button"
            onClick={() => onOpenChange(false)}
            className="rounded-md border px-3 py-1.5 text-sm hover:bg-muted"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  );
}

function Row({ keys, desc }: { keys: string[]; desc: string }) {
  return (
    <tr className="border-t first:border-t-0">
      <td className="py-2 pr-3">
        <span className="inline-flex gap-1">
          {keys.map((k) => (
            <kbd key={k} className="rounded border bg-muted px-1.5 py-0.5 text-xs font-mono">
              {k}
            </kbd>
          ))}
        </span>
      </td>
      <td className="py-2 text-muted-foreground">{desc}</td>
    </tr>
  );
}
