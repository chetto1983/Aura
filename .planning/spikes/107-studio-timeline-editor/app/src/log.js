// Forensic log: every event carries an ISO timestamp and a category, and the whole list can be
// exported as JSON from the page (button) or read by Playwright (window.__spikeLog).
const events = [];
const listeners = new Set();

export function log(category, message, data) {
  const event = { at: new Date().toISOString(), category, message, ...(data ? { data } : {}) };
  events.push(event);
  console.log(`[${category}] ${message}`, data ?? '');
  listeners.forEach((fn) => fn(events.slice()));
}

export function subscribe(fn) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

export function summary() {
  const byCategory = {};
  for (const e of events) byCategory[e.category] = (byCategory[e.category] ?? 0) + 1;
  const errors = events.filter((e) => e.category === 'error').length;
  return { count: events.length, byCategory, errors, userAgent: navigator.userAgent };
}

export function exportLog() {
  const blob = new Blob([JSON.stringify({ summary: summary(), events }, null, 2)], { type: 'application/json' });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = `spike-107-log-${Date.now()}.json`;
  a.click();
}

window.__spikeLog = { events, summary };
window.addEventListener('error', (e) => log('error', e.message));
window.addEventListener('unhandledrejection', (e) => log('error', String(e.reason?.message ?? e.reason)));
