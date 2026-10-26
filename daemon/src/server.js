import { createHash, timingSafeEqual } from 'node:crypto';
import http from 'node:http';
import { launchBrowser } from './browser.js';
import { commands } from './commands.js';
import { UserError, firstLine } from './errors.js';
import { parseGeolocation } from './geolocation.js';
import { createInput } from './input.js';
import { parseProxy } from './proxy.js';
import { DEFAULT_GAPS, createLimiter, parseRange } from './ratelimit.js';
import { DEFAULT_MAX_TEXT } from './snapshot.js';
import { DEFAULT_WAIT_MS } from './wait.js';

const env = process.env;

// How often a still-running command tells the client it is alive.
const KEEPALIVE_MS = 10_000;

// Unset or empty means the default; anything else must be a positive integer.
function positiveInt(name, raw, fallback) {
  if (raw === undefined || raw === '') return fallback;
  const n = Number(raw);
  if (!Number.isInteger(n) || n < 1) throw new Error(`${name} must be a positive integer, got "${raw}"`);
  return n;
}

export function loadConfig(e = env) {
  const token = e.GREYMAN_TOKEN;
  if (!token) throw new Error('GREYMAN_TOKEN is required');
  return {
    token,
    version: e.GREYMAN_VERSION || 'dev', // baked into the image; the CLI compares it with its own
    host: e.GREYMAN_HOST ?? '0.0.0.0', // inside the container; the host publishes it to loopback only
    port: Number(e.GREYMAN_PORT ?? 7777),
    headless: e.GREYMAN_HEADLESS === '1',
    profileDir: e.GREYMAN_PROFILE ?? '/data/profile',
    proxy: parseProxy(e.GREYMAN_PROXY),
    locale: e.GREYMAN_LOCALE || undefined,
    geolocation: parseGeolocation(e),
    maxChars: positiveInt('GREYMAN_MAX_CHARS', e.GREYMAN_MAX_CHARS, 24000),
    snapMaxText: positiveInt('GREYMAN_SNAP_MAX_TEXT', e.GREYMAN_SNAP_MAX_TEXT, DEFAULT_MAX_TEXT),
    actionTimeoutMs: positiveInt('GREYMAN_ACTION_TIMEOUT_MS', e.GREYMAN_ACTION_TIMEOUT_MS, 5000),
    navTimeoutMs: positiveInt('GREYMAN_NAV_TIMEOUT_MS', e.GREYMAN_NAV_TIMEOUT_MS, 30000),
    defaultWaitMs: positiveInt('GREYMAN_DEFAULT_WAIT', e.GREYMAN_DEFAULT_WAIT, DEFAULT_WAIT_MS),
    gaps: {
      nav: parseRange('GREYMAN_NAV_GAP', e.GREYMAN_NAV_GAP, DEFAULT_GAPS.nav),
      act: parseRange('GREYMAN_ACT_GAP', e.GREYMAN_ACT_GAP, DEFAULT_GAPS.act),
    },
  };
}

const digest = (s) => createHash('sha256').update(s).digest();

export function createServer(ctx, { handlers = commands, keepaliveMs = KEEPALIVE_MS } = {}) {
  const expected = digest(ctx.cfg.token);
  // Every response says which build answered, so a CLI can notice a stale container.
  const base = { 'x-greyman-version': ctx.cfg.version ?? 'dev' };
  const authed = (req) => {
    const m = /^Bearer (.+)$/.exec(req.headers.authorization ?? '');
    return m !== null && timingSafeEqual(digest(m[1]), expected);
  };

  // After a keepalive the 200 is already on the wire, so the outcome travels in body.ok.
  const send = (res, status, body) => {
    const data = JSON.stringify(body);
    if (res.headersSent) return res.end(data);
    res.writeHead(status, { ...base, 'content-type': 'application/json', 'content-length': Buffer.byteLength(data) });
    res.end(data);
  };

  // Long commands (typing, pacing waits) would look hung to the client. While one runs,
  // write a space every keepalive interval: leading whitespace is valid JSON.
  const keepalive = (res) => {
    const timer = setInterval(() => {
      if (!res.headersSent) res.writeHead(200, { ...base, 'content-type': 'application/json' });
      res.write(' ');
    }, keepaliveMs);
    res.on('close', () => clearInterval(timer));
    return () => clearInterval(timer);
  };

  const readJSON = async (req) => {
    const chunks = [];
    let size = 0;
    for await (const chunk of req) {
      size += chunk.length;
      if (size > 1_000_000) throw new UserError('request body too large');
      chunks.push(chunk);
    }
    if (!size) return {};
    try {
      return JSON.parse(Buffer.concat(chunks).toString());
    } catch {
      throw new UserError('invalid JSON body');
    }
  };

  return http.createServer(async (req, res) => {
    // A client that hangs up before we answer (Ctrl-C, killed process) cancels the work.
    const interrupt = new AbortController();
    res.on('close', () => {
      if (!res.writableFinished) interrupt.abort(new UserError('client disconnected'));
    });
    try {
      if (!authed(req)) return send(res, 401, { ok: false, error: 'unauthorized' });
      const { pathname } = new URL(req.url, 'http://greyman');
      const name = /^\/v1\/([a-z]+)$/.exec(pathname)?.[1];
      const handler = name && Object.hasOwn(handlers, name) ? handlers[name] : undefined;
      if (!handler) return send(res, 404, { ok: false, error: `unknown route ${pathname}` });
      const expectedMethod = name === 'status' ? 'GET' : 'POST';
      if (req.method !== expectedMethod) return send(res, 405, { ok: false, error: `use ${expectedMethod}` });
      const args = expectedMethod === 'POST' ? await readJSON(req) : {};
      const stop = keepalive(res);
      try {
        return send(res, 200, { ok: true, ...(await handler({ ...ctx, signal: interrupt.signal }, args)) });
      } finally {
        stop();
      }
    } catch (err) {
      if (interrupt.signal.aborted) return; // nobody is listening
      // Timeouts (a ref, a screenshot, a busy page) are outcomes the agent can act on.
      const user = err instanceof UserError || err?.name === 'TimeoutError';
      if (!user) console.error(err);
      const msg = firstLine(err?.message);
      send(res, user ? 400 : 500, { ok: false, error: user ? msg : `internal: ${msg}` });
    }
  });
}

async function main() {
  const cfg = loadConfig();
  const tabs = await launchBrowser(cfg);
  const limiter = createLimiter({ gaps: cfg.gaps });
  const input = createInput(cfg);
  const server = createServer({ cfg, tabs, limiter, input });
  server.listen(cfg.port, cfg.host, () => {
    console.log(`greyman daemon listening on ${cfg.host}:${cfg.port} (headless=${cfg.headless})`);
  });

  let closing = false;
  const shutdown = async () => {
    closing = true;
    console.log('shutting down');
    server.close();
    input.close();
    // Closing the context shuts Chromium down cleanly, which flushes cookies to the profile dir.
    await tabs.context.close().catch((err) => console.error('context close failed:', err));
    console.log('context closed');
    process.exit(0);
  };
  process.on('SIGTERM', shutdown);
  process.on('SIGINT', shutdown);
  // Browser died on its own: exit non-zero so the failure is visible in `docker ps`/logs.
  tabs.context.on('close', () => closing || process.exit(1));
}

if (import.meta.url === `file://${process.argv[1]}`) {
  main().catch((err) => {
    console.error(err);
    process.exit(1);
  });
}
