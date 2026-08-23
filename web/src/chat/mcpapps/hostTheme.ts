import type { HostContext } from './hostProtocol';

// Aura's design tokens (web/src/styles/theme.css) mapped onto the names MCP Apps
// defines for hostContext.styles.variables. A view written against the extension --
// ours or anyone's -- then themes itself while knowing nothing about Aura, which is
// the point of the mechanism; this previously sent four names of our own invention
// that only our own two views could ever have understood.
export const AURA_TO_MCP_APPS: readonly (readonly [string, string])[] = [
  ['--color-background-primary', '--color-bg'],
  ['--color-background-secondary', '--color-surface'],
  ['--color-background-tertiary', '--color-surface-2'],
  ['--color-background-ghost', '--color-surface-3'],
  ['--color-background-info', '--color-info'],
  ['--color-background-success', '--color-success'],
  ['--color-background-warning', '--color-warning'],
  ['--color-background-danger', '--color-danger'],
  ['--color-text-primary', '--color-text'],
  ['--color-text-secondary', '--color-text-muted'],
  ['--color-text-tertiary', '--color-text-faint'],
  ['--color-text-disabled', '--color-text-disabled'],
  ['--color-border-primary', '--color-border'],
  ['--color-border-secondary', '--color-border-strong'],
  ['--color-ring-primary', '--color-ring'],
  ['--font-sans', '--font-sans'],
  ['--font-mono', '--font-mono'],
  ['--border-radius-sm', '--radius-sm'],
  ['--border-radius-md', '--radius-md'],
  ['--border-radius-lg', '--radius-lg'],
  ['--border-radius-full', '--radius-pill'],
  ['--shadow-md', '--shadow-popover'],
  ['--shadow-lg', '--shadow-drawer'],
  // Kept until the WhatsApp view is republished against the standard names: it reads
  // --wa-bg for its background, and dropping this would leave that view unpainted.
  ['--wa-bg', '--color-surface'],
  ['--host-surface', '--color-surface'],
  ['--host-text', '--color-text'],
  ['--host-accent', '--color-accent'],
];

/**
 * The host context a view receives at ui/initialize: which theme the cockpit is in,
 * and the cockpit's tokens under the extension's own variable names.
 *
 * Pure in its argument so the mapping can be tested without a frame, a bridge or a
 * component -- which is how the theme below was found to have been wrong all along.
 */
export function hostContextFrom(root: HTMLElement): HostContext {
  const computed = getComputedStyle(root);
  const variables: Record<string, string> = {};
  for (const [name, token] of AURA_TO_MCP_APPS) {
    const value = computed.getPropertyValue(token).trim();
    // The spec lets a host provide any subset, so an unset token is omitted rather
    // than sent empty: an empty string would override the view's own fallback with
    // nothing, which is worse than staying silent.
    if (value) variables[name] = value;
  }

  return {
    // Aura themes with data-theme and has no `dark` class, so the classList probe this
    // replaces reported LIGHT to every view, including inside the dark cockpit. Dark is
    // also the default when the attribute is absent, matching styles/theme.css
    // (:root:not([data-theme]) { color-scheme: dark }) and applyTheme's DEFAULT_THEME.
    theme: root.getAttribute('data-theme') === 'light' ? 'light' : 'dark',
    styles: { variables },
  };
}
