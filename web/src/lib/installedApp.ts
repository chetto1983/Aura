/**
 * Whether the cockpit runs as the app added to an iPhone's or iPad's home screen
 * (manifest `display: 'standalone'`, vite.config.ts).
 *
 * `navigator.standalone` exists only in WebKit's iOS family, iPadOS included, and is true in
 * that app (Apple, "Configuring Web Applications"). The app runs in its own WebView, apart
 * from Safari, and has no back button: a file opened in a new tab or downloaded by navigation
 * leaves it for a page with no way back, in a browser that may hold no session (prd.md §3).
 * Android and desktop installs open tabs and download in place, so they keep the browser's
 * behaviour.
 */
export function isIOSHomeScreenApp(): boolean {
  return (
    typeof navigator !== 'undefined' &&
    (navigator as Navigator & { standalone?: boolean }).standalone === true
  );
}
