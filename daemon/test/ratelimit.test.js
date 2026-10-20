import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createLimiter, originOf, parseRange } from '../src/ratelimit.js';

// Fake clock: sleep advances time instantly.
function fakeLimiter(gaps = { nav: [1000, 1000], act: [100, 100] }) {
  let t = 10_000;
  const slept = [];
  const limiter = createLimiter({
    gaps,
    now: () => t,
    sleep: async (ms) => {
      slept.push(ms);
      t += ms;
    },
    rand: () => 0.5,
  });
  return { limiter, slept, advance: (ms) => (t += ms) };
}

test('first hit on an origin does not wait', async () => {
  const { limiter, slept } = fakeLimiter();
  assert.equal(await limiter.wait('https://a.test', 'nav'), 0);
  assert.deepEqual(slept, []);
});

test('second hit waits out the remaining gap', async () => {
  const { limiter, slept, advance } = fakeLimiter();
  await limiter.wait('https://a.test', 'nav');
  advance(400);
  assert.equal(await limiter.wait('https://a.test', 'nav'), 600);
  assert.deepEqual(slept, [600]);
});

test('origins are independent', async () => {
  const { limiter, slept } = fakeLimiter();
  await limiter.wait('https://a.test', 'nav');
  await limiter.wait('https://b.test', 'nav');
  assert.deepEqual(slept, []);
});

test('touch() counts as a hit', async () => {
  const { limiter } = fakeLimiter();
  limiter.touch('https://a.test');
  assert.equal(await limiter.wait('https://a.test', 'act'), 100);
});

test('concurrent callers on one origin are serialized with a gap each', async () => {
  const { limiter, slept } = fakeLimiter();
  await Promise.all([1, 2, 3].map(() => limiter.wait('https://a.test', 'act')));
  assert.deepEqual(slept, [100, 100]);
});

test('parseRange: unset or empty means the fallback, anything malformed throws', () => {
  assert.deepEqual(parseRange('GAP', '3000-6000', [1, 2]), [3000, 6000]);
  assert.deepEqual(parseRange('GAP', '0-0', [1, 2]), [0, 0]);
  assert.deepEqual(parseRange('GAP', undefined, [1, 2]), [1, 2]);
  assert.deepEqual(parseRange('GAP', '', [1, 2]), [1, 2]);
  for (const bad of ['6000-3000', 'nope', '3000', '1-2-3', '-1-5', ' 1-2']) {
    assert.throws(() => parseRange('GAP', bad, [1, 2]), /GAP must be MIN-MAX.*got/, bad);
  }
});

test('originOf', () => {
  assert.equal(originOf('https://a.test/x?y=1'), 'https://a.test');
  assert.equal(originOf('about:blank'), 'null');
});

// A sleep that only ends when the signal aborts.
function abortOnlySleep(ms, signal) {
  return new Promise((_, reject) => signal.addEventListener('abort', () => reject(signal.reason), { once: true }));
}

test('an aborted wait rejects, does not count as a hit, and does not block later callers', async () => {
  let t = 10_000;
  const limiter = createLimiter({
    gaps: { nav: [1000, 1000], act: [100, 100] },
    now: () => t,
    sleep: (ms, signal) => (signal ? abortOnlySleep(ms, signal) : ((t += ms), Promise.resolve())),
    rand: () => 0.5,
  });
  await limiter.wait('https://a.test', 'nav'); // first hit, free

  const ac = new AbortController();
  const waiting = limiter.wait('https://a.test', 'nav', ac.signal);
  ac.abort(new Error('hung up'));
  await assert.rejects(waiting, /hung up/);

  // The next caller still owes the gap since the real first hit, not the aborted one.
  assert.equal(await limiter.wait('https://a.test', 'nav'), 1000);
});

test('an already-aborted signal rejects without waiting', async () => {
  const { limiter, slept } = fakeLimiter();
  await limiter.wait('https://a.test', 'act');
  const ac = new AbortController();
  ac.abort(new Error('gone'));
  await assert.rejects(limiter.wait('https://a.test', 'act', ac.signal), /gone/);
  assert.deepEqual(slept, []);
});
