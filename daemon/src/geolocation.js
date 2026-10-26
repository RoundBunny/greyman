import { UserError } from './errors.js';
import { serial } from './serial.js';

// Keep the env names and defaults equal to the registry in internal/settings.
const FIELDS = {
  latitude: { env: 'GREYMAN_GEOLOCATION_COORD_LAT', fallback: 0, lo: -90, hi: 90 },
  longitude: { env: 'GREYMAN_GEOLOCATION_COORD_LON', fallback: 0, lo: -180, hi: 180 },
  accuracy: { env: 'GREYMAN_GEOLOCATION_ACCURACY', fallback: 10, lo: 0, hi: Infinity },
};

function check(label, n, { lo, hi }) {
  if (typeof n !== 'number' || !Number.isFinite(n) || n < lo || n > hi) {
    throw new UserError(`${label} must be a number ${hi === Infinity ? `>= ${lo}` : `from ${lo} to ${hi}`}`);
  }
  return n;
}

function point(values, label = (name) => name) {
  return Object.fromEntries(Object.entries(FIELDS).map(([name, f]) => [name, check(label(name), values[name], f)]));
}

// { enabled, point }: whether to report a location from launch, and the point (whose
// accuracy `set geolocation` falls back on). Validated even when disabled.
export function parseGeolocation(e) {
  const raw = Object.fromEntries(
    Object.entries(FIELDS).map(([name, f]) => [name, e[f.env] ? Number(e[f.env]) : f.fallback]),
  );
  return { enabled: e.GREYMAN_USE_GEOLOCATION === '1', point: point(raw, (name) => FIELDS[name].env) };
}

// One browser-level CDP session per context, made on first use. A failed attempt is
// forgotten, so the next call tries again.
const sessions = new WeakMap();

function browserSession(context) {
  if (!sessions.has(context)) {
    const browser = context.browser();
    if (!browser) throw new Error('no browser handle to send CDP commands through');
    const session = browser.newBrowserCDPSession();
    sessions.set(context, session);
    session.catch(() => sessions.delete(context));
  }
  return sessions.get(context);
}

// Allows location requests from every origin, or drops that override so the permission is
// back to the launch default, which is denial: launchArgs starts Chromium with
// --deny-permission-prompts, so no prompt is ever shown. Dropping the override, and not
// setting 'prompt', keeps the state moving from "prompt" to "denied" after a request, as
// it does on a launch that never allowed anything. This is the only override greyman sets.
export async function allowGeolocation(context, allow) {
  const cdp = await browserSession(context);
  if (allow) {
    await cdp.send('Browser.setPermission', { permission: { name: 'geolocation' }, setting: 'granted' });
  } else {
    await cdp.send('Browser.resetPermissions');
  }
}

// Called once after launch. Denied is the launch state already, so only allowing needs
// work. It was asked for, so a failure stops the daemon (after closing the browser
// cleanly).
export async function initGeolocation(context, enabled) {
  if (!enabled) return;
  try {
    await allowGeolocation(context, true);
  } catch (err) {
    await context.close().catch(() => {});
    throw err;
  }
}

// `set` calls run one at a time per context. Each takes two steps, and interleaved they
// can leave the browser in neither of the states that were reported.
const queues = new WeakMap();

function inTurn(context, fn) {
  if (!queues.has(context)) queues.set(context, serial());
  return queues.get(context)(fn);
}

// `greyman set geolocation`: args is { off: true } or { latitude, longitude, accuracy? }.
// The change covers every tab in the context. Pages that already read the position keep
// what they read; ones using watchPosition see the new fix.
export function setGeolocation(context, defaults, args) {
  return inTurn(context, async () => {
    if (args.off === true) {
      await allowGeolocation(context, false);
      return 'ok geolocation=off';
    }
    const p = point({ accuracy: defaults.accuracy, ...args });
    await context.setGeolocation(p);
    await allowGeolocation(context, true);
    return `ok geolocation=${p.latitude},${p.longitude} accuracy=${p.accuracy}`;
  });
}
