import { createContext, useContext } from 'react';

// assetSourceContext — the R-05 seam: WHERE an artifact renderer resolves an asset's bytes
// from is a context value, not a hardcoded URL. Kept in a NON-component `.ts` module
// (mirroring voiceModeContext.ts) so no component shares this file — a `.tsx` exporting both
// a context and a component would fail the `react-refresh/only-export-components` lint rule
// and therefore `make quality`'s web gate.
//
// The default is the IDENTITY-SCOPED lane so every existing call site and test stays
// byte-identical to today's behavior; the public share page (37F-16) wraps its tree in a
// provider returning the token-scoped URL instead, which is what lets HtmlPreview and the
// other renderers work on the public page with ZERO edits (D-03/D-09).
//
// The alternative — threading an `assetUrl` prop through six lazy renderer chunks — is churn
// across every call site and every existing test; context-with-a-default is the "extract a
// helper, never duplicate" answer.
//
// `credentials` lives on the context value rather than being hardcoded because a public-link
// recipient has no session: sending cookies to an unauthenticated route is needless and the
// public provider supplies `omit` (R-05, T-37F-28).

export interface AssetSource {
  /** The URL a renderer should fetch/link to for `assetId`. Percent-encodes the id — an id is
   *  never interpolated raw into a path segment. Declared as a property (not a method) so
   *  destructuring `const { assetUrl } = useAssetSource()` never trips
   *  `@typescript-eslint/unbound-method` — there is no `this` to lose. */
  readonly assetUrl: (assetId: string) => string;
  /** The `fetch()` credentials mode a renderer should pair with `assetUrl`. */
  readonly credentials: RequestCredentials;
}

const IDENTITY_SCOPED: AssetSource = {
  assetUrl: (assetId) => `/api/assets/${encodeURIComponent(assetId)}/download`,
  credentials: 'same-origin',
};

// No explicit generic type argument on createContext here: IDENTITY_SCOPED is already typed
// as AssetSource above, so the type is inferred — this file exports no React component, and
// stays free of any construct an angle-bracket-generic/JSX grep would flag.
export const AssetSourceContext = createContext(IDENTITY_SCOPED);

/** Read the active asset source; the identity-scoped default when no provider is mounted. */
export function useAssetSource(): AssetSource {
  return useContext(AssetSourceContext);
}
