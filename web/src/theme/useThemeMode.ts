import { useSyncExternalStore } from 'react';
import { getTheme, type Theme } from './applyTheme';

/**
 * The live theme, for the components whose look is decided in JS rather than in CSS.
 *
 * `data-theme` on <html> is the single source of truth (applyTheme writes it), and a switch
 * mutates that attribute without touching React state -- so a component that merely READS
 * the attribute while rendering keeps the theme it happened to be born with until something
 * else re-renders it. A MutationObserver on that one attribute is what makes the switch
 * immediate instead of "correct after the next navigation".
 */
function subscribeToTheme(onChange: () => void): () => void {
  const observer = new MutationObserver(onChange);
  observer.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['data-theme'],
  });
  return () => {
    observer.disconnect();
  };
}

function readTheme(): Theme {
  return document.documentElement.getAttribute('data-theme') === 'light' ? 'light' : 'dark';
}

export function useThemeMode(): Theme {
  // getTheme() is the server-snapshot fallback: it reads localStorage rather than the DOM,
  // so a render before applyTheme() has stamped the attribute still picks the right one.
  return useSyncExternalStore(subscribeToTheme, readTheme, getTheme);
}
