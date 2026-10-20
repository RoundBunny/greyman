// Per-origin pacing. Each origin gets a serialized queue; every caller waits until
// a jittered minimum gap has passed since the previous caller on that origin was released.

import { setTimeout as sleepFor } from 'node:timers/promises';

// Rejects with an AbortError if `signal` aborts first.
const defaultSleep = (ms, signal) => sleepFor(ms, undefined, { signal });

export const DEFAULT_GAPS = { nav: [3000, 6000], act: [400, 1200] };

// "3000-6000" -> [3000, 6000]. Unset or empty means the fallback; anything else that is
// not MIN-MAX with MIN <= MAX throws, so a typo is not silently replaced by the default.
export function parseRange(name, raw, fallback) {
  if (raw === undefined || raw === '') return fallback;
  const m = /^(\d+)-(\d+)$/.exec(raw);
  const lo = Number(m?.[1]);
  const hi = Number(m?.[2]);
  if (!m || lo > hi) throw new Error(`${name} must be MIN-MAX milliseconds with MIN <= MAX, got "${raw}"`);
  return [lo, hi];
}

export function originOf(url) {
  try {
    return new URL(url).origin;
  } catch {
    return 'null';
  }
}

export function createLimiter({
  gaps = DEFAULT_GAPS,
  now = Date.now,
  sleep = defaultSleep,
  rand = Math.random,
} = {}) {
  const last = new Map(); // origin -> ms timestamp of last release
  const tails = new Map(); // origin -> promise chain

  // Resolves with the number of ms it had to wait. kind: 'nav' | 'act'. If `signal`
  // aborts, it rejects without counting as a hit, and later callers carry on.
  function wait(origin, kind, signal) {
    const [lo, hi] = gaps[kind];
    const prev = tails.get(origin) ?? Promise.resolve();
    const run = prev.then(async () => {
      signal?.throwIfAborted();
      const due = (last.get(origin) ?? -Infinity) + lo + rand() * (hi - lo);
      const delay = Math.max(0, due - now());
      if (delay > 0) await sleep(delay, signal);
      last.set(origin, now());
      return delay;
    });
    tails.set(origin, run.catch(() => {}));
    return run;
  }

  // Record that we just hit an origin without having gone through wait()
  // (e.g. a click that navigated cross-origin).
  function touch(origin) {
    last.set(origin, now());
  }

  return { wait, touch };
}
