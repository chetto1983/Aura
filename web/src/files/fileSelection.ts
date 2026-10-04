import {
  getMenuOptions,
  type IApi,
  type IFileMenuOption,
  type IParsedEntity,
  type TContextMenuType,
  type TID,
} from '@svar-ui/react-filemanager';

/**
 * Several files at once, for a pointer that has no Ctrl or Shift key -- which is every phone.
 *
 * SVAR already selects several: a Ctrl-click toggles a file, a Shift-click extends a range
 * (`select-file` with `toggle` / `range`), and its menu for several files copies, cuts and
 * deletes all of them in one request. Measured on the lab VM (2026-09-28, 2.6.0) it lacked
 * three things, added here through its own seams (`menuOptions`, `api.intercept`):
 *
 * - a way in without a modifier key: "Select" in a file's menu starts a mode in which every
 *   tap is the Ctrl-click the phone cannot make;
 * - Download in the menu for several files;
 * - a selection that survives its own menu: pressing a card's ⋮ fires the card's click too,
 *   and the widget answers it by selecting that card ALONE on a timer -- after the menu has
 *   been built for several -- so the Delete it offers removed one file.
 */
export interface FileSelection {
  readonly menuOptions: (mode: TContextMenuType) => IFileMenuOption[];
  readonly attach: (api: IApi) => void;
  /** Where the last click landed, noted before the widget's own handlers see it. */
  readonly notePress: (event: { readonly target: EventTarget }) => void;
}

export function createFileSelection(
  save: (files: readonly IParsedEntity[]) => unknown,
): FileSelection {
  // Assigned by attach, which the widget's init runs before it can open a single menu.
  let api!: IApi;
  let selecting = false;
  let pressedMore = false;

  function selected(panel?: number): TID[] {
    const { panels, activePanel } = api.getState();
    return panels?.[panel ?? activePanel ?? 0]?.selected ?? [];
  }

  function selectedFiles(): IParsedEntity[] {
    // The direct route streams one object; a folder is a prefix, and there is nothing to send.
    // Entities, not ids: a share sheet names each file, and the key does not always carry it.
    return selected()
      .map((id) => api.getFile(id))
      .filter((file): file is IParsedEntity => file !== null && file.type !== 'folder');
  }

  // Fresh objects per call: the widget writes the translated label and the hotkey hint back
  // onto the options it is handed.
  function menuOptions(mode: TContextMenuType): IFileMenuOption[] {
    const options: IFileMenuOption[] = getMenuOptions(mode);
    if (mode === 'multiselect') {
      const download = {
        id: 'download-selected',
        text: 'Download',
        icon: 'wxi-download',
        handler: () => save(selectedFiles()),
      };
      return [download, ...options];
    }
    if (mode === 'file' || mode === 'folder') {
      const select = {
        id: 'select-several',
        text: 'Select',
        icon: 'wxi-check',
        handler: () => {
          selecting = true;
        },
      };
      return [select, ...options];
    }
    return options;
  }

  function attach(next: IApi): void {
    api = next;
    api.intercept('select-file', (ev) => {
      // A clear is not a toggle, and a Shift range stays a range.
      if (!ev.id || ev.range) return;
      const current = selected(ev.panel);
      if (pressedMore && current.includes(ev.id)) return false;
      // The mode lasts while something is selected; emptying the selection is how it ends.
      if (current.length === 0) selecting = false;
      if (selecting) ev.toggle = true;
    });
  }

  function notePress({ target }: { readonly target: EventTarget }): void {
    // data-action-id is the widget's own marker for a card's ⋮ button.
    pressedMore = target instanceof Element && target.closest('[data-action-id]') !== null;
  }

  return { menuOptions, attach, notePress };
}
