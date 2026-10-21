import assert from 'node:assert/strict';
import { after, before, test } from 'node:test';
import { UserError } from '../src/errors.js';
import { createServer, loadConfig } from '../src/server.js';

const TOKEN = 'secret';
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

test('loadConfig: GREYMAN_MAX_CHARS must be a positive integer, empty means default', () => {
  const cfg = (extra) => loadConfig({ GREYMAN_TOKEN: TOKEN, ...extra });
  assert.equal(cfg({}).maxChars, 24000);
  assert.equal(cfg({ GREYMAN_MAX_CHARS: '' }).maxChars, 24000);
  assert.equal(cfg({ GREYMAN_MAX_CHARS: '500' }).maxChars, 500);
  for (const bad of ['0', '-5', 'abc', '1.5']) {
    assert.throws(() => cfg({ GREYMAN_MAX_CHARS: bad }), /GREYMAN_MAX_CHARS/, bad);
  }
});

test('loadConfig: timeouts and snapshot text limit default and can be set', () => {
  const cfg = (extra) => loadConfig({ GREYMAN_TOKEN: TOKEN, ...extra });
  const defaults = cfg({});
  assert.equal(defaults.actionTimeoutMs, 5000);
  assert.equal(defaults.navTimeoutMs, 30000);
  assert.equal(defaults.snapMaxText, 200);
  assert.equal(defaults.defaultWaitMs, 3000);
  const set = cfg({ GREYMAN_ACTION_TIMEOUT_MS: '1000', GREYMAN_NAV_TIMEOUT_MS: '60000', GREYMAN_SNAP_MAX_TEXT: '400', GREYMAN_DEFAULT_WAIT: '1500' });
  assert.equal(set.actionTimeoutMs, 1000);
  assert.equal(set.navTimeoutMs, 60000);
  assert.equal(set.snapMaxText, 400);
  assert.equal(set.defaultWaitMs, 1500);
  for (const name of ['GREYMAN_ACTION_TIMEOUT_MS', 'GREYMAN_NAV_TIMEOUT_MS', 'GREYMAN_SNAP_MAX_TEXT', 'GREYMAN_DEFAULT_WAIT']) {
    for (const bad of ['0', '-1', 'abc', '1.5']) {
      assert.throws(() => cfg({ [name]: bad }), new RegExp(name), `${name}=${bad}`);
    }
  }
});

test('loadConfig: gaps default, can be set, and a malformed value is an error', () => {
  const cfg = (extra) => loadConfig({ GREYMAN_TOKEN: TOKEN, ...extra });
  assert.deepEqual(cfg({}).gaps, { nav: [3000, 6000], act: [400, 1200] });
  assert.deepEqual(cfg({ GREYMAN_NAV_GAP: '0-0', GREYMAN_ACT_GAP: '10-20' }).gaps, { nav: [0, 0], act: [10, 20] });
  assert.throws(() => cfg({ GREYMAN_NAV_GAP: '6000-3000' }), /GREYMAN_NAV_GAP/);
  assert.throws(() => cfg({ GREYMAN_ACT_GAP: 'fast' }), /GREYMAN_ACT_GAP/);
});

test('loadConfig: proxy credentials become separate fields', () => {
  const cfg = loadConfig({ GREYMAN_TOKEN: TOKEN, GREYMAN_PROXY: 'http://u:p@proxy.test:3128' });
  assert.deepEqual(cfg.proxy, { server: 'http://proxy.test:3128', username: 'u', password: 'p' });
});

const handlers = {
  async status() {
    return { text: 'ok' };
  },
  async slow() {
    await sleep(120);
    return { text: 'done' };
  },
  async slowfail() {
    await sleep(120);
    throw new UserError('nope');
  },
  async user() {
    throw new UserError('bad ref');
  },
  async timeout() {
    throw Object.assign(new Error('locator.click: Timeout 5000ms exceeded.\nCall log:\n  - waiting'), { name: 'TimeoutError' });
  },
  async crash() {
    throw new Error('kaboom\n    at somewhere');
  },
};

let server;
let base;
before(async () => {
  server = createServer({ cfg: { token: TOKEN } }, { handlers, keepaliveMs: 20 });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  base = `http://127.0.0.1:${server.address().port}`;
});
after(() => server.close());

const call = async (name, { method = 'POST', token = TOKEN } = {}) => {
  const res = await fetch(`${base}/v1/${name}`, { method, headers: { authorization: `Bearer ${token}` } });
  const raw = await res.text();
  return { status: res.status, raw, body: JSON.parse(raw), version: res.headers.get('x-greyman-version') };
};

test('fast commands answer with a plain JSON response', async () => {
  const r = await call('status', { method: 'GET' });
  assert.equal(r.status, 200);
  assert.deepEqual(r.body, { ok: true, text: 'ok' });
  assert.equal(r.raw, JSON.stringify(r.body));
});

test('slow commands keep the connection alive with whitespace, then answer', async () => {
  const r = await call('slow');
  assert.equal(r.status, 200);
  assert.match(r.raw, /^ +\{/);
  assert.deepEqual(r.body, { ok: true, text: 'done' });
});

test('a failure after a keepalive is reported in the body', async () => {
  const r = await call('slowfail');
  assert.equal(r.status, 200);
  assert.deepEqual(r.body, { ok: false, error: 'nope' });
});

test('user errors and timeouts are 400 with the first line only', async () => {
  assert.deepEqual((await call('user')).body, { ok: false, error: 'bad ref' });
  assert.equal((await call('user')).status, 400);
  const t = await call('timeout');
  assert.equal(t.status, 400);
  assert.equal(t.body.error, 'locator.click: Timeout 5000ms exceeded.');
});

test('unexpected errors are 500, first line only', async (t) => {
  t.mock.method(console, 'error', () => {});
  const r = await call('crash');
  assert.equal(r.status, 500);
  assert.equal(r.body.error, 'internal: kaboom');
});

test('auth, routing and methods', async () => {
  assert.equal((await call('status', { method: 'GET', token: 'wrong' })).status, 401);
  assert.equal((await call('nope')).status, 404);
  assert.equal((await call('status')).status, 405);
});

test('a client that hangs up aborts the running command', async () => {
  let seen;
  const finished = new Promise((resolve) => {
    const s = createServer(
      { cfg: { token: TOKEN } },
      {
        handlers: {
          async hang(ctx) {
            seen = ctx.signal;
            await new Promise((done) => ctx.signal.addEventListener('abort', done, { once: true }));
            resolve();
            throw ctx.signal.reason; // surfaces as a rejected handler; the server must not log it
          },
        },
        keepaliveMs: 20,
      },
    );
    s.listen(0, '127.0.0.1', async () => {
      const ac = new AbortController();
      setTimeout(() => ac.abort(), 80);
      await fetch(`http://127.0.0.1:${s.address().port}/v1/hang`, {
        method: 'POST',
        headers: { authorization: `Bearer ${TOKEN}` },
        signal: ac.signal,
      }).catch(() => {});
    });
    after(() => s.close());
  });
  await finished;
  assert.equal(seen.aborted, true);
});

test('a command that completes is not aborted', async () => {
  let signal;
  const s = createServer({ cfg: { token: TOKEN } }, { handlers: { async ok(ctx) { signal = ctx.signal; return { text: 'ok' }; } } });
  await new Promise((resolve) => s.listen(0, '127.0.0.1', resolve));
  await fetch(`http://127.0.0.1:${s.address().port}/v1/ok`, { method: 'POST', headers: { authorization: `Bearer ${TOKEN}` } }).then((r) => r.text());
  await sleep(30);
  assert.equal(signal.aborted, false);
  s.close();
});

test('every response names the build that answered, kept-alive ones and errors too', async () => {
  assert.equal((await call('status', { method: 'GET' })).version, 'dev');
  assert.equal((await call('slow')).version, 'dev');
  assert.equal((await call('user')).version, 'dev');
  assert.equal((await call('status', { method: 'GET', token: 'wrong' })).version, 'dev');
});

test('loadConfig: the version comes from the image, default dev', () => {
  assert.equal(loadConfig({ GREYMAN_TOKEN: TOKEN }).version, 'dev');
  assert.equal(loadConfig({ GREYMAN_TOKEN: TOKEN, GREYMAN_VERSION: 'v1.2.3' }).version, 'v1.2.3');
});
