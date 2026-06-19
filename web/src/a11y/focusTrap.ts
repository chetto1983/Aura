const FOCUSABLE_SELECTOR =
  'button, [href], input, textarea, select, [tabindex]:not([tabindex="-1"])';

function isFocusable(node: HTMLElement): boolean {
  return !node.hasAttribute('disabled');
}

export function focusFirstDescendant(root: HTMLElement | null | undefined): void {
  root?.querySelector<HTMLElement>(FOCUSABLE_SELECTOR)?.focus();
}

export function trapTabKey(event: KeyboardEvent, root: HTMLElement | null | undefined): void {
  if (event.key !== 'Tab' || !root) return;
  const nodes = Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(
    isFocusable,
  );
  if (nodes.length === 0) return;
  const first = nodes[0];
  const last = nodes[nodes.length - 1];
  if (!first || !last) return;
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}
