import assert from 'node:assert/strict';
import { test } from 'node:test';
import { commands, normalizeUrl } from '../src/commands.js';
import { UserError } from '../src/errors.js';
import { serial } from '../src/serial.js';

test('bare hosts get https', () => {
  assert.equal(normalizeUrl('example.com'), 'https://example.com/');
  assert.equal(normalizeUrl('example.com/a?b=1'), 'https://example.com/a?b=1');
});

test('host:port is a host, not a scheme', () => {
  assert.equal(normalizeUrl('example.com:8080/x'), 'https://example.com:8080/x');
  assert.equal(normalizeUrl('localhost:3000'), 'https://localhost:3000/');
  assert.equal(normalizeUrl('127.0.0.1:3000'), 'https://127.0.0.1:3000/');
  assert.equal(normalizeUrl('[::1]:3000'), 'https://[::1]:3000/');
});

test('explicit http(s) schemes are kept', () => {
  assert.equal(normalizeUrl('http://a.test'), 'http://a.test/');
  assert.equal(normalizeUrl('HTTPS://a.test:8443/p'), 'https://a.test:8443/p');
});

test('other schemes are rejected', () => {
  for (const raw of ['mailto:a@b.test', 'javascript:alert(1)', 'file:///etc/passwd', 'data:text/html,hi', 'ftp://a.test']) {
    assert.throws(() => normalizeUrl(raw), UserError, raw);
  }
});

test('empty url is a user error', () => {
  assert.throws(() => normalizeUrl(''), UserError);
});

// A snap with a page that records how it was waited on.
async function snapWith(cfg, args) {
  const calls = [];
  const page = {
    waitForLoadState: async (state, o) => calls.push([state, o.timeout]),
    ariaSnapshot: async () => '- button "Go" [ref=e1]',
    title: async () => 'T',
    url: () => 'https://example.com/',
  };
  const tab = { page, queue: serial() };
  const ctx = { cfg, tabs: { get: async () => tab } };
  await commands.snap(ctx, args);
  return calls;
}

test('snap waits for idle for DEFAULT_WAIT by default, and --wait/--wait-timeout/none override', async () => {
  const cfg = { defaultWaitMs: 3000, maxChars: 24000, snapMaxText: 200 };
  assert.deepEqual(await snapWith(cfg, {}), [['networkidle', 3000]]);
  assert.deepEqual(await snapWith({ ...cfg, defaultWaitMs: 1500 }, {}), [['networkidle', 1500]]);
  assert.deepEqual(await snapWith(cfg, { waitTimeout: 500 }), [['networkidle', 500]]);
  assert.deepEqual(await snapWith(cfg, { wait: 'none' }), []);
});

test('a screenshot too big for the response cap is a user error, with a hint for --full', async () => {
  const big = Buffer.alloc(41 * 2 ** 20);
  const tab = { page: { screenshot: async () => big }, queue: serial() };
  const ctx = { tabs: { get: async () => tab } };
  await assert.rejects(commands.shot(ctx, { full: true }), (err) => err instanceof UserError && /screenshot is 41 MB, over the 40 MB limit; shoot a ref/.test(err.message));
  await assert.rejects(commands.shot(ctx, {}), (err) => err instanceof UserError && /over the 40 MB limit$/.test(err.message));
});

test('close waits for the action running on the tab', async () => {
  let finish;
  const order = [];
  const tab = { id: 't1', queue: serial(), page: { close: async () => order.push('close') } };
  tab.queue(async () => {
    await new Promise((r) => (finish = r));
    order.push('action');
  });
  const ctx = { tabs: { get: async () => tab, activeId: null }, cfg: { defaultWaitMs: 1000 } };
  const closing = commands.close(ctx, {});
  await new Promise((r) => setImmediate(r));
  assert.deepEqual(order, []);
  finish();
  assert.equal(await closing.then((r) => r.text), 'ok closed=t1 active=none');
  assert.deepEqual(order, ['action', 'close']);
});

test('close does not wait forever on an action that hangs', async () => {
  let closed = false;
  const tab = { id: 't1', queue: serial(), page: { close: async () => (closed = true) } };
  tab.queue(() => new Promise(() => {}));
  const ctx = { tabs: { get: async () => tab, activeId: null }, cfg: { defaultWaitMs: 20 } };
  assert.equal((await commands.close(ctx, {})).text, 'ok closed=t1 active=none');
  assert.equal(closed, true);
});
