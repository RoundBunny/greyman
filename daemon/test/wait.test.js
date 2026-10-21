import assert from 'node:assert/strict';
import { test } from 'node:test';
import { UserError } from '../src/errors.js';
import { DEFAULT_WAIT_MS, checkWait, parseWait, waitFor, waitTimeout } from '../src/wait.js';

test('parseWait accepts idle, text=, css= and none; unset means idle', () => {
  assert.deepEqual(parseWait(undefined), { kind: 'idle', spec: 'idle' });
  assert.deepEqual(parseWait(''), { kind: 'idle', spec: 'idle' });
  assert.equal(parseWait('none'), null);
  assert.deepEqual(parseWait('idle'), { kind: 'idle', spec: 'idle' });
  assert.deepEqual(parseWait('text=Find a Store'), { kind: 'text', value: 'Find a Store', spec: 'text=Find a Store' });
  assert.equal(parseWait('css=dialog[open]').value, 'dialog[open]');
  assert.equal(parseWait('text=a=b').value, 'a=b');
  for (const bad of ['soon', 'text=', 'xpath=//a', 'idle=1']) assert.throws(() => parseWait(bad), UserError, bad);
});

test('waitTimeout defaults and bounds', () => {
  assert.equal(DEFAULT_WAIT_MS, 3000);
  assert.equal(waitTimeout(undefined), DEFAULT_WAIT_MS);
  assert.equal(waitTimeout(undefined, 1500), 1500);
  assert.equal(waitTimeout(500, 1500), 500);
  assert.equal(waitTimeout(500), 500);
  for (const bad of [0, -1, 1.5, 999999]) assert.throws(() => waitTimeout(bad), UserError, String(bad));
});

const timeoutErr = () => Object.assign(new Error('Timeout'), { name: 'TimeoutError' });
const fakePage = (fail) => {
  const calls = [];
  const locator = (what) => ({
    count: async () => {
      if (fail) throw fail;
      return 1;
    },
    // only visible matches count: the first match in the DOM may be hidden
    filter: (f) => ({
      first: () => ({
        waitFor: async (o) => {
          calls.push([what, f, o]);
          if (fail) throw fail;
        },
      }),
    }),
  });
  return {
    calls,
    waitForLoadState: async (state, o) => {
      calls.push([state, o]);
      if (fail) throw fail;
    },
    getByText: (t) => locator(`text:${t}`),
    locator: (sel) => locator(`css:${sel}`),
  };
};

test('waitFor runs the right wait and reports success', async () => {
  const page = fakePage();
  assert.equal(await waitFor(page, parseWait('idle'), 100), true);
  assert.equal(await waitFor(page, parseWait('text=Hi'), 100), true);
  assert.equal(await waitFor(page, parseWait('css=#a'), 100), true);
  assert.deepEqual(page.calls, [
    ['networkidle', { timeout: 100 }],
    ['text:Hi', { visible: true }, { timeout: 100 }],
    ['css:#a', { visible: true }, { timeout: 100 }],
  ]);
});

test('a timeout is false, not an error', async () => {
  assert.equal(await waitFor(fakePage(timeoutErr()), parseWait('idle'), 10), false);
  assert.equal(await waitFor(fakePage(timeoutErr()), parseWait('text=x'), 10), false);
});

test('a malformed selector is a user error; other failures pass through', async () => {
  await assert.rejects(waitFor(fakePage(new Error('Unexpected token "!"\nlog')), parseWait('css=!!'), 10), /bad --wait selector: Unexpected token/);
  await assert.rejects(waitFor(fakePage(new Error('boom')), parseWait('idle'), 10), /boom/);
});

test('checkWait rejects a malformed selector before anything has run, and ignores other kinds', async () => {
  const bad = fakePage(new Error('Unexpected token "!"\nlog'));
  await assert.rejects(checkWait(bad, parseWait('css=!!')), (err) => err instanceof UserError && /bad --wait selector: Unexpected token/.test(err.message));
  await checkWait(bad, parseWait('text=!!'));
  await checkWait(bad, parseWait('idle'));
  await checkWait(bad, null);
  await checkWait(fakePage(), parseWait('css=#ok'));
});

// A wait that never ends on its own, as a Playwright wait for text that never appears.
const stuckPage = () => ({
  waitForLoadState: () => new Promise(() => {}),
  getByText: () => ({ filter: () => ({ first: () => ({ waitFor: () => new Promise(() => {}) }) }) }),
  locator: () => ({ filter: () => ({ first: () => ({ waitFor: () => new Promise(() => {}) }) }) }),
});

test('a client hang-up ends a long wait at once instead of holding the tab', async () => {
  const ctl = new AbortController();
  setTimeout(() => ctl.abort(new UserError('client disconnected')), 20);
  const t = Date.now();
  await assert.rejects(waitFor(stuckPage(), parseWait('text=never'), 60_000, ctl.signal), /client disconnected/);
  assert.ok(Date.now() - t < 1000, `${Date.now() - t} ms`);
});

test('an already-aborted signal rejects without waiting, and a normal wait ignores a live one', async () => {
  const dead = new AbortController();
  dead.abort(new UserError('gone'));
  await assert.rejects(waitFor(stuckPage(), parseWait('idle'), 60_000, dead.signal), /gone/);
  const live = new AbortController();
  assert.equal(await waitFor(fakePage(), parseWait('idle'), 100, live.signal), true);
  live.abort(); // after the wait finished: nothing pending, nothing thrown
});

test('a timeout still reports false when a signal is passed', async () => {
  const live = new AbortController();
  assert.equal(await waitFor(fakePage(timeoutErr()), parseWait('idle'), 10, live.signal), false);
});
