import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { Archive, Download, MoreHorizontal, Pencil, RotateCcw, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { ariaInvalid } from '../a11y/aria';
import { exportConversationMarkdown } from './exportConversation';
import { displayTitle, isArchived, type Conversation } from './useConversations';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

interface ConversationRowProps {
  readonly conv: Conversation;
  readonly selected: boolean;
  readonly onSelect: (id: string) => void;
  readonly onRename: (title: string) => void;
  readonly onArchive: () => void;
  readonly onUnarchive: () => void;
  readonly onRequestDelete: () => void;
}

interface MenuPosition {
  readonly left: number;
  readonly top: number;
  readonly width: number;
}

function positionMenuForButton(
  button: HTMLButtonElement | null,
  menuHeight: number | undefined,
): MenuPosition | null {
  if (!button || typeof window === 'undefined') return null;

  function clamp(value: number, min: number, max: number): number {
    return Math.min(Math.max(value, min), max);
  }

  const rect = button.getBoundingClientRect();
  const viewportWidth = document.documentElement.clientWidth || window.innerWidth;
  const viewportHeight = document.documentElement.clientHeight || window.innerHeight;
  const gutter = 8;
  const width = Math.min(224, Math.max(160, viewportWidth - gutter * 2));
  const height = menuHeight ?? 156;
  const maxLeft = Math.max(gutter, viewportWidth - width - gutter);
  const below = rect.bottom + 6;
  const above = rect.top - height - 6;
  const top =
    below + height <= viewportHeight - gutter || above < gutter
      ? Math.min(below, viewportHeight - height - gutter)
      : above;

  return {
    left: clamp(rect.right - width, gutter, maxLeft),
    top: Math.max(gutter, top),
    width,
  };
}

export function ConversationRow({
  conv,
  selected,
  onSelect,
  onRename,
  onArchive,
  onUnarchive,
  onRequestDelete,
}: ConversationRowProps) {
  const { t } = useTranslation();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState('');
  const [menuOpen, setMenuOpen] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const rowRef = useRef<HTMLLIElement>(null);
  const actionButtonRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const [menuPosition, setMenuPosition] = useState<MenuPosition | null>(null);

  const archived = isArchived(conv);
  const label = displayTitle(conv, t('conversations.untitled'));
  const menuLabel = t('conversations.actions.more');

  useEffect(() => {
    if (editing) {
      inputRef.current?.focus();
      inputRef.current?.select();
    }
  }, [editing]);

  useEffect(() => {
    if (!menuOpen) return;

    function onPointerDown(event: PointerEvent) {
      const target = event.target as Node;
      if (rowRef.current?.contains(target)) return;
      if (menuRef.current?.contains(target)) return;
      setMenuOpen(false);
      setMenuPosition(null);
    }

    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        setMenuOpen(false);
        setMenuPosition(null);
      }
    }

    window.addEventListener('pointerdown', onPointerDown);
    window.addEventListener('keydown', onKeyDown);
    return () => {
      window.removeEventListener('pointerdown', onPointerDown);
      window.removeEventListener('keydown', onKeyDown);
    };
  }, [menuOpen]);

  useLayoutEffect(() => {
    if (!menuOpen || editing || typeof window === 'undefined') {
      return;
    }

    function syncMenuPosition() {
      const next = positionMenuForButton(actionButtonRef.current, menuRef.current?.offsetHeight);
      if (next !== null) setMenuPosition(next);
    }

    const frame = window.requestAnimationFrame(syncMenuPosition);
    window.addEventListener('resize', syncMenuPosition);
    window.addEventListener('scroll', syncMenuPosition, true);
    return () => {
      window.cancelAnimationFrame(frame);
      window.removeEventListener('resize', syncMenuPosition);
      window.removeEventListener('scroll', syncMenuPosition, true);
    };
  }, [editing, menuOpen]);

  function startEditing() {
    setMenuOpen(false);
    setMenuPosition(null);
    setDraft(label);
    setEditing(true);
  }

  function commit() {
    const next = draft.trim();
    setEditing(false);
    if (next.length > 0 && next !== label) {
      onRename(next);
    }
  }

  const invalid = editing && draft.trim().length === 0;

  return (
    <li ref={rowRef} className="relative">
      <div
        className={`group grid grid-cols-[minmax(0,1fr)_auto] items-center rounded-md transition-colors ${
          selected ? 'bg-surface-2 text-text' : 'text-text-muted hover:bg-surface-2/70'
        }`}
        data-animate="surface"
      >
        {editing ? (
          <Input
            ref={inputRef}
            value={draft}
            onChange={(event) => {
              setDraft(event.target.value);
            }}
            onBlur={commit}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                event.preventDefault();
                commit();
              } else if (event.key === 'Escape') {
                event.preventDefault();
                setEditing(false);
              }
            }}
            aria-label={t('conversations.renameLabel')}
            aria-invalid={ariaInvalid(invalid)}
            className="col-span-2 min-h-9 bg-surface text-sm"
          />
        ) : (
          <>
            <Button
              type="button"
              variant="ghost"
              onClick={() => {
                onSelect(conv.ID);
              }}
              onDoubleClick={startEditing}
              aria-current={selected ? 'true' : undefined}
              aria-label={label}
              className="h-9 min-h-9 min-w-0 justify-start rounded-md px-2 py-1.5 text-left text-[13px] font-medium text-text hover:bg-transparent"
            >
              <span className="flex min-w-0 flex-1 items-center gap-2">
                {archived ? (
                  <Badge variant="secondary" className="shrink-0 text-[0.75rem]">
                    {t('conversations.archivedTag')}
                  </Badge>
                ) : null}
                <span className="truncate">{label}</span>
              </span>
            </Button>
            <Button
              ref={actionButtonRef}
              type="button"
              variant="ghost"
              size="icon"
              aria-label={menuLabel}
              aria-haspopup="menu"
              aria-expanded={menuOpen}
              data-open={menuOpen ? 'true' : undefined}
              title={menuLabel}
              onClick={() => {
                setMenuPosition(
                  positionMenuForButton(actionButtonRef.current, menuRef.current?.offsetHeight),
                );
                setMenuOpen((open) => !open);
              }}
              className="h-[32px] min-h-[32px] w-[32px] rounded-md text-text-faint opacity-100 hover:bg-surface-3 hover:text-text focus-visible:opacity-100 data-[open=true]:bg-surface-3 data-[open=true]:text-text md:opacity-0 md:group-hover:opacity-100"
            >
              <MoreHorizontal data-icon="icon" aria-hidden="true" focusable="false" />
            </Button>
          </>
        )}

        {menuOpen && !editing && typeof document !== 'undefined'
          ? createPortal(
              <div
                ref={menuRef}
                role="menu"
                aria-label={menuLabel}
                style={{
                  left: menuPosition?.left ?? 0,
                  top: menuPosition?.top ?? 0,
                  width: menuPosition?.width ?? 224,
                  visibility: menuPosition === null ? 'hidden' : 'visible',
                }}
                className="fixed z-[70] flex flex-col gap-1 rounded-xl border border-border bg-surface-2 p-1.5 text-[13px] text-text shadow-[var(--shadow-popover)]"
              >
                <MenuAction
                  label={t('conversations.actions.rename')}
                  icon={<Pencil data-icon aria-hidden="true" className="size-4" />}
                  onClick={startEditing}
                />
                {archived ? (
                  <MenuAction
                    label={t('conversations.actions.unarchive')}
                    icon={<RotateCcw data-icon aria-hidden="true" className="size-4" />}
                    onClick={() => {
                      setMenuOpen(false);
                      setMenuPosition(null);
                      onUnarchive();
                    }}
                  />
                ) : (
                  <MenuAction
                    label={t('conversations.actions.archive')}
                    icon={<Archive data-icon aria-hidden="true" className="size-4" />}
                    onClick={() => {
                      setMenuOpen(false);
                      setMenuPosition(null);
                      onArchive();
                    }}
                  />
                )}
                {/* fix-plan 2.10: wire the dark-code export endpoint. Read-only GET;
                    the server names the file via Content-Disposition. A failure is
                    non-blocking (console, the menu's fire-and-forget error pattern). */}
                <MenuAction
                  label={t('conversations.actions.export')}
                  icon={<Download data-icon aria-hidden="true" className="size-4" />}
                  onClick={() => {
                    setMenuOpen(false);
                    setMenuPosition(null);
                    void exportConversationMarkdown(conv.ID).catch((err: unknown) => {
                      console.error('aura: conversation export failed', err);
                    });
                  }}
                />
                <div className="my-1 h-px bg-border" />
                <MenuAction
                  label={t('conversations.actions.delete')}
                  icon={<Trash2 data-icon aria-hidden="true" className="size-4" />}
                  onClick={() => {
                    setMenuOpen(false);
                    setMenuPosition(null);
                    onRequestDelete();
                  }}
                  danger
                />
              </div>,
              document.body,
            )
          : null}
      </div>
    </li>
  );
}

function MenuAction({
  label,
  icon,
  onClick,
  danger,
}: {
  readonly label: string;
  readonly icon: ReactNode;
  readonly onClick: () => void;
  readonly danger?: boolean;
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      role="menuitem"
      onClick={onClick}
      className={`w-full justify-start rounded-lg px-3 text-[13px] font-medium ${
        danger
          ? 'text-danger hover:bg-danger/15 hover:text-danger'
          : 'text-text hover:bg-surface-3 hover:text-text'
      }`}
    >
      {icon}
      {label}
    </Button>
  );
}
