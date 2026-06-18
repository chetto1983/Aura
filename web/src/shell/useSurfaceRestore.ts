import { useCallback, useReducer } from 'react';

/**
 * §3.1c intent-aware restore — "one heavy surface at a time, remembered".
 *
 * Mobile/tablet only (at `lg` every region is a permanent column, so the shell never
 * routes through this). Distinct from the DOM focus-restore the Drawer already does and
 * from the mode-persistence hook (`useSurfaceIntent`): this tracks which *overlay surface*
 * owns the screen and whether the nav drawer should reappear when the overlay leaves.
 *
 * Contract:
 * - Only one heavy overlay owns the screen. Opening the runtime overlay while the nav
 *   drawer is open auto-dismisses the nav AND remembers it (`navWasOpenBeforeOverlay`).
 * - Closing the overlay **explicitly** (close button / Esc / backdrop tap) restores the
 *   remembered nav — the operator's "I'm done here, take me back" intent.
 * - Closing the overlay via **swipe-dismiss** does NOT restore — "get this out of my way,
 *   I want the chat".
 */
export type CloseIntent = 'explicit' | 'swipe';

interface SurfaceState {
  readonly navOpen: boolean;
  readonly overlayOpen: boolean;
  readonly navWasOpenBeforeOverlay: boolean;
}

type SurfaceAction =
  | { readonly type: 'openNav' }
  | { readonly type: 'closeNav' }
  | { readonly type: 'openOverlay' }
  | { readonly type: 'closeOverlay'; readonly intent: CloseIntent };

const initialState: SurfaceState = {
  navOpen: false,
  overlayOpen: false,
  navWasOpenBeforeOverlay: false,
};

function reducer(state: SurfaceState, action: SurfaceAction): SurfaceState {
  switch (action.type) {
    case 'openNav':
      // One heavy surface: the nav takes the screen, the overlay yields.
      return { navOpen: true, overlayOpen: false, navWasOpenBeforeOverlay: false };
    case 'closeNav':
      return { ...state, navOpen: false };
    case 'openOverlay':
      // Remember the nav only if it was actually open; dismiss it so the overlay is alone.
      return { navOpen: false, overlayOpen: true, navWasOpenBeforeOverlay: state.navOpen };
    case 'closeOverlay':
      return {
        navOpen: action.intent === 'explicit' && state.navWasOpenBeforeOverlay,
        overlayOpen: false,
        navWasOpenBeforeOverlay: false,
      };
    default:
      return state;
  }
}

export interface SurfaceRestore extends SurfaceState {
  readonly openNav: () => void;
  readonly closeNav: () => void;
  readonly openOverlay: () => void;
  readonly closeOverlay: (intent: CloseIntent) => void;
}

export function useSurfaceRestore(): SurfaceRestore {
  const [state, dispatch] = useReducer(reducer, initialState);
  const openNav = useCallback(() => {
    dispatch({ type: 'openNav' });
  }, []);
  const closeNav = useCallback(() => {
    dispatch({ type: 'closeNav' });
  }, []);
  const openOverlay = useCallback(() => {
    dispatch({ type: 'openOverlay' });
  }, []);
  const closeOverlay = useCallback((intent: CloseIntent) => {
    dispatch({ type: 'closeOverlay', intent });
  }, []);
  return { ...state, openNav, closeNav, openOverlay, closeOverlay };
}
