import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { test } from 'node:test';
import { attachLog, formatLog, record } from '../src/log.js';

test('the log keeps the newest 200 entries', () => {
  const log = [];
  for (let i = 0; i < 250; i++) record(log, 'nav', `u${i}`);
  assert.equal(log.length, 200);
  assert.equal(log[0].text, 'u50');
});

test('long lines are clipped', () => {
  const log = [];
  record(log, 'pageerror', 'x'.repeat(1000));
  assert.ok(log[0].text.length < 260);
  assert.ok(log[0].text.endsWith('...'));
});

const fakePage = () => {
  const page = new EventEmitter();
  const main = {};
  page.mainFrame = () => main;
  page.main = main;
  return page;
};
const req = (method, url, errorText) => ({ method: () => method, url: () => url, failure: () => (errorText ? { errorText } : null) });

test('attachLog records errors, failed requests, 4xx/5xx and navigations, without query strings', () => {
  const page = fakePage();
  const log = [];
  attachLog(page, log);
  page.emit('console', { type: () => 'log', text: () => 'chatter' });
  page.emit('console', { type: () => 'error', text: () => 'geo failed\n  at x' });
  page.emit('console', { type: () => 'warning', text: () => 'deprecated' });
  page.emit('pageerror', new Error('boom\nstack'));
  page.emit('requestfailed', req('GET', 'https://a.test/api?token=SECRET', 'net::ERR_BLOCKED_BY_CLIENT'));
  page.emit('response', { status: () => 200, request: () => req('GET', 'https://a.test/ok'), url: () => 'https://a.test/ok' });
  page.emit('response', { status: () => 403, request: () => req('POST', 'https://a.test/geo?k=1'), url: () => 'https://a.test/geo?k=1' });
  page.emit('framenavigated', { url: () => 'https://a.test/page?x=1#h' }); // not the main frame
  page.emit('framenavigated', Object.assign(page.main, { url: () => 'https://a.test/page?x=1#h' }));
  assert.deepEqual(
    log.map((e) => `${e.kind} ${e.text}`),
    [
      'console.error geo failed',
      'console.warning deprecated',
      'pageerror boom',
      'requestfailed GET https://a.test/api net::ERR_BLOCKED_BY_CLIENT',
      'http 403 POST https://a.test/geo',
      'nav https://a.test/page',
    ],
  );
});

test('formatLog shows ages, the last N, and can drop warnings and navigation', () => {
  const now = 100_000;
  const log = [
    { t: now - 30_000, kind: 'nav', text: 'https://a.test/' },
    { t: now - 20_000, kind: 'console.warning', text: 'w' },
    { t: now - 10_000, kind: 'http 500', text: 'GET https://a.test/x' },
  ];
  assert.equal(formatLog(log, { now }), '30s ago nav https://a.test/\n20s ago console.warning w\n10s ago http 500 GET https://a.test/x');
  assert.equal(formatLog(log, { now, errorsOnly: true }), '10s ago http 500 GET https://a.test/x');
  assert.equal(formatLog(log, { now, max: 2 }).split('\n').length, 2);
  assert.equal(formatLog([], { now }), '(no log entries)');
});
