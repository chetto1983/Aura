import { useEffect, useRef, type ReactNode } from 'react';
import { createPortal } from 'react-dom';

// MediaEditorLayer — the full-screen surface both editors draw in. It is deliberately not a
// Radix Dialog: Filerobot's menus, colour picker and modals portal to document.body, and a Radix
// focus trap with outside-dismiss treats each of them as "outside" and closes the editor on the
// first click. Instead the app root goes inert while the layer is open, which keeps pointer and
// keyboard off the page underneath without trapping the editor's own portals.

interface MediaEditorLayerProps {
  readonly label: string;
  readonly onEscape: () => void;
  readonly children: ReactNode;
}

export function MediaEditorLayer({ label, onEscape, children }: MediaEditorLayerProps) {
  const layerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const appRoot = document.getElementById('root');
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    appRoot?.setAttribute('inert', '');
    layerRef.current?.focus();
    return () => {
      appRoot?.removeAttribute('inert');
      opener?.focus();
    };
  }, []);

  return createPortal(
    // A React handler, not a document listener like shell/Drawer: it runs after the editor's own
    // key handlers and its stopPropagation keeps Escape from also reaching other overlays' document
    // listeners. Escape is a dialog's keyboard contract, not a pointer affordance on static content.
    // oxlint-disable-next-line jsx-a11y/no-noninteractive-element-interactions
    <div
      ref={layerRef}
      role="dialog"
      aria-modal="true"
      aria-label={label}
      tabIndex={-1}
      onKeyDown={(event) => {
        // React bubbles events through portals, so a key pressed in a Filerobot menu reaches this
        // handler too; only a key pressed in the layer's own DOM closes the editor.
        if (event.key !== 'Escape') return;
        if (!(event.target instanceof Node) || !layerRef.current?.contains(event.target)) return;
        event.stopPropagation();
        onEscape();
      }}
      className="fixed inset-0 z-50 flex flex-col bg-bg text-text outline-none"
    >
      {children}
    </div>,
    document.body,
  );
}
