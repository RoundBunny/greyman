import { firstLine } from './errors.js';

const MAX_ENTRIES = 200;
const MAX_LINE = 240;
export const DEFAULT_SHOWN = 50;

// Query strings and fragments carry tokens and make lines unreadable.
const bare = (url) => url.split(/[?#]/)[0];
const clip = (s) => (s.length > MAX_LINE ? `${s.slice(0, MAX_LINE)}...` : s);

export function record(log, kind, text) {
  log.push({ t: Date.now(), kind, text: clip(text) });
  if (log.length > MAX_ENTRIES) log.shift();
}

// What a person would see in DevTools: console errors and warnings, uncaught exceptions,
// failed and 4xx/5xx requests, and where the page went. Listeners only; nothing is
// injected into the page.
export function attachLog(page, log) {
  page.on('console', (m) => {
    const type = m.type();
    if (type === 'error' || type === 'warning') record(log, `console.${type}`, firstLine(m.text()));
  });
  page.on('pageerror', (err) => record(log, 'pageerror', firstLine(err?.message ?? String(err))));
  page.on('requestfailed', (r) => record(log, 'requestfailed', `${r.method()} ${bare(r.url())} ${r.failure()?.errorText ?? ''}`.trim()));
  page.on('response', (r) => {
    if (r.status() >= 400) record(log, `http ${r.status()}`, `${r.request().method()} ${bare(r.url())}`);
  });
  page.on('framenavigated', (f) => {
    if (f === page.mainFrame()) record(log, 'nav', bare(f.url()));
  });
}

// Last `max` entries, oldest first. errorsOnly drops warnings and navigation markers.
export function formatLog(log, { errorsOnly = false, max = DEFAULT_SHOWN, now = Date.now() } = {}) {
  const rows = log.filter((e) => !errorsOnly || (e.kind !== 'nav' && e.kind !== 'console.warning')).slice(-max);
  if (!rows.length) return '(no log entries)';
  return rows.map((e) => `${Math.round((now - e.t) / 1000)}s ago ${e.kind} ${e.text}`).join('\n');
}
