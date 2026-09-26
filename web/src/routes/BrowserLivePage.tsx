import { useRef, type KeyboardEvent, type MouseEvent, type WheelEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useParams } from 'react-router';
import { Keyboard, ShieldAlert } from 'lucide-react';
import { keyEvent, mouseEvent, textEvents, toViewport, wheelEvent } from '../browserLive/liveInput';
import { useBrowserLive } from '../browserLive/useBrowserLive';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

/**
 * Live view of one agent-browser session in the signed-in user's box (prd.md §12). The agent
 * opens a login page and hands the user this link; the user signs in here, so the box keeps a
 * session and never a reusable password. Frames are the page itself, input goes back through
 * the relay: nothing typed here passes through the model.
 */
export function BrowserLivePage() {
  const { session = '' } = useParams();
  return <BrowserLiveView key={session} session={session} />;
}

function BrowserLiveView({ session }: { session: string }) {
  const { t } = useTranslation();
  const { state, send } = useBrowserLive(session);
  const frameRef = useRef<HTMLImageElement>(null);
  const softKeys = useRef<HTMLInputElement>(null);
  const live = state.phase === 'live';

  const point = (e: MouseEvent | WheelEvent) => {
    const img = frameRef.current;
    if (!img || !state.meta) return null;
    return toViewport(e.clientX, e.clientY, img.getBoundingClientRect(), state.meta);
  };
  const onMouse = (eventType: 'mousePressed' | 'mouseReleased') => (e: MouseEvent<HTMLElement>) => {
    const p = point(e);
    if (!p || !live) return;
    // preventDefault stops the image drag, and with it the focus a click would give the stage,
    // so the stage takes focus itself: otherwise the keys typed after a click go nowhere.
    e.preventDefault();
    e.currentTarget.focus();
    send(mouseEvent(eventType, p, e.button, e));
  };
  const onWheel = (e: WheelEvent) => {
    const p = point(e);
    if (p && live) send(wheelEvent(p, e.deltaX, e.deltaY));
  };
  const onKey = (eventType: 'keyDown' | 'keyUp') => (e: KeyboardEvent) => {
    if (!live || e.nativeEvent.isComposing) return;
    e.preventDefault();
    send(keyEvent(e, eventType));
  };

  return (
    <main className="flex h-dvh flex-col bg-bg text-text">
      <header className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-3">
        <span
          aria-hidden
          className={cn(
            'size-2.5 shrink-0 rounded-full',
            live ? 'bg-success' : state.phase === 'ended' ? 'bg-danger' : 'bg-warning',
          )}
        />
        <h1 className="text-sm font-medium">{t('browserLive.title')}</h1>
        <code className="min-w-0 flex-1 truncate rounded-md border border-border px-2 py-1 font-mono text-xs text-text-muted">
          {state.url || session}
        </code>
        <Button
          variant="outline"
          size="sm"
          disabled={!live}
          onClick={() => {
            softKeys.current?.focus();
          }}
          aria-label={t('browserLive.keyboard')}
        >
          <Keyboard className="size-4" />
        </Button>
        <Button
          size="sm"
          onClick={() => {
            window.close();
          }}
        >
          {t('browserLive.done')}
        </Button>
      </header>

      <p className="flex items-start gap-2 border-b border-border px-4 py-2 text-xs text-text-muted">
        <ShieldAlert aria-hidden className="mt-0.5 size-3.5 shrink-0 text-warning" />
        {t('browserLive.notice')}
      </p>

      {/* A remote browser is an application surface: every key and click belongs to the page it
          shows, so the stage takes focus and input the way a canvas-based remote desktop does. */}
      {/* oxlint-disable-next-line jsx-a11y/no-noninteractive-element-interactions */}
      <div
        role="application"
        // oxlint-disable-next-line jsx-a11y/no-noninteractive-tabindex -- the stage must take keyboard focus
        tabIndex={0}
        aria-label={t('browserLive.stageLabel')}
        onMouseDown={onMouse('mousePressed')}
        onMouseUp={onMouse('mouseReleased')}
        onContextMenu={(e) => {
          e.preventDefault();
        }}
        onWheel={onWheel}
        onKeyDown={onKey('keyDown')}
        onKeyUp={onKey('keyUp')}
        onPaste={(e) => {
          e.preventDefault();
          if (!live) return;
          for (const ev of textEvents(e.clipboardData.getData('text/plain'))) send(ev);
        }}
        className="relative grid min-h-0 flex-1 place-items-center overflow-hidden p-3 outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        {state.frame ? (
          // The frame carries no border or padding: its box IS the page, so a click maps 1:1.
          <img
            ref={frameRef}
            src={state.frame}
            alt={t('browserLive.frameAlt')}
            draggable={false}
            className={cn(
              'max-h-full max-w-full select-none rounded-md shadow-[0_0_0_1px_var(--color-border),0_10px_30px_rgb(0_0_0/0.35)]',
              !live && 'opacity-50',
            )}
          />
        ) : null}
        {state.phase !== 'live' ? (
          <p
            role="status"
            className="absolute inset-x-0 bottom-6 mx-auto w-fit rounded-md bg-surface px-3 py-2 text-sm"
          >
            {state.phase === 'connecting'
              ? t('browserLive.connecting')
              : t(`browserLive.ended.${endedKey(state.reason)}`)}
          </p>
        ) : null}
        <input
          ref={softKeys}
          aria-label={t('browserLive.keyboard')}
          autoCapitalize="off"
          autoComplete="off"
          autoCorrect="off"
          spellCheck={false}
          className="absolute size-px opacity-0"
          // Printable keys arrive through onInput (a phone's keyboard sends no usable key codes);
          // only editing keys go as key events. Neither may bubble to the stage and be sent twice.
          onKeyDown={(e) => {
            e.stopPropagation();
            if (e.key.length !== 1) onKey('keyDown')(e);
          }}
          onKeyUp={(e) => {
            e.stopPropagation();
            if (e.key.length !== 1) onKey('keyUp')(e);
          }}
          onInput={(e) => {
            const input = e.currentTarget;
            if (live) for (const ev of textEvents(input.value)) send(ev);
            input.value = '';
          }}
        />
      </div>
    </main>
  );
}

const ENDED_REASONS = new Set([
  'taken_over',
  'no_such_session',
  'stream_closed',
  'stream_unreachable',
]);

function endedKey(reason: string): string {
  return ENDED_REASONS.has(reason) ? reason : 'disconnected';
}
