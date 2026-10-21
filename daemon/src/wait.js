import { UserError, firstLine } from './errors.js';

export const DEFAULT_WAIT_MS = 3000;
const MAX_WAIT_MS = 120_000;

// idle | text=TEXT | css=SELECTOR | none. Unset means idle: every command waits for the
// page unless told not to.
export function parseWait(spec) {
  if (spec === undefined || spec === '') return { kind: 'idle', spec: 'idle' };
  if (spec === 'none') return null;
  if (spec === 'idle') return { kind: 'idle', spec };
  const m = /^(text|css)=(.+)$/s.exec(String(spec));
  if (!m) throw new UserError(`--wait must be idle, text=TEXT, css=SELECTOR or none, got "${spec}"`);
  return { kind: m[1], value: m[2], spec };
}

// `fallback` is the DEFAULT_WAIT setting.
export function waitTimeout(ms, fallback = DEFAULT_WAIT_MS) {
  if (ms === undefined) return fallback;
  if (!Number.isInteger(ms) || ms < 1 || ms > MAX_WAIT_MS) {
    throw new UserError(`--wait-timeout must be 1 to ${MAX_WAIT_MS} ms`);
  }
  return ms;
}

// Rejects a malformed css= selector before the action it belongs to has run, so an
// error never hides an action that already happened.
export async function checkWait(page, wait) {
  if (wait?.kind !== 'css') return;
  try {
    await page.locator(wait.value).count();
  } catch (err) {
    throw new UserError(`bad --wait selector: ${firstLine(err?.message)}`);
  }
}

// True when the condition held. A timeout returns false rather than throwing: the page
// may simply be slow, and the caller still has a result to report. `signal` is the
// client hanging up (Ctrl-C): Playwright waits cannot be cancelled, so the race ends the
// command at once and the wait is left to time out unobserved, instead of holding the
// tab's queue for up to the full timeout.
export async function waitFor(page, wait, timeout, signal) {
  const pending = runWait(page, wait, timeout);
  if (!signal) return pending;
  let onAbort;
  const aborted = new Promise((_, reject) => {
    onAbort = () => reject(signal.reason);
    if (signal.aborted) onAbort();
    else signal.addEventListener('abort', onAbort, { once: true });
  });
  try {
    return await Promise.race([pending, aborted]);
  } finally {
    signal.removeEventListener('abort', onAbort);
    pending.catch(() => {});
  }
}

async function runWait(page, wait, timeout) {
  try {
    if (wait.kind === 'idle') await page.waitForLoadState('networkidle', { timeout });
    // The first match in the DOM may be hidden (a template, a collapsed menu) while a later one is on screen.
    else if (wait.kind === 'text') await page.getByText(wait.value).filter({ visible: true }).first().waitFor({ timeout });
    else await page.locator(wait.value).filter({ visible: true }).first().waitFor({ timeout });
    return true;
  } catch (err) {
    if (err?.name === 'TimeoutError') return false;
    if (wait.kind === 'css') throw new UserError(`bad --wait selector: ${firstLine(err?.message)}`);
    throw err;
  }
}
