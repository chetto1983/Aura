// The classes of the cockpit's select, apart from its component so the searchable pickers built
// on cmdk can draw the same trigger, menu and rows (react/only-export-components).

/** Classes of the open menu. It portals to the body, so inside a dialog it must clear the
 * dialog's z-[100], or it opens behind the overlay and nothing can reach it. */
export const selectMenuClass =
  'z-[110] overflow-hidden rounded-lg border border-border-strong bg-surface-3 p-1 text-text shadow-xl data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95';

/** Classes of one row of the menu: highlighted on hover or keys, accent once chosen. A
 * disabled row is read from aria-disabled="true", which Radix and cmdk both write: cmdk also
 * writes data-disabled="false" on every enabled row, so a bare data-[disabled] greys them all. */
export const selectItemClass =
  'relative flex min-h-10 cursor-default items-center rounded-md py-2 pr-8 pl-3 text-[13px] outline-none select-none data-[highlighted]:bg-surface-2 data-[selected=true]:bg-surface-2 data-[state=checked]:font-semibold data-[state=checked]:text-info aria-disabled:pointer-events-none aria-disabled:opacity-50';

/** Classes of the closed trigger, at the 44 px touch size of the cockpit's inputs. */
export const selectTriggerClass =
  'flex min-h-[44px] w-full min-w-0 items-center justify-between gap-2 rounded-md border border-input bg-surface-3 px-3 py-2 text-left text-[13px] text-text outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-60 aria-[invalid=true]:border-danger data-[placeholder]:text-text-faint';
