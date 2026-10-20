import assert from 'node:assert/strict';
import { test } from 'node:test';
import { commands } from '../src/commands.js';
import { UserError } from '../src/errors.js';
import { allowGeolocation, initGeolocation, parseGeolocation, setGeolocation } from '../src/geolocation.js';
import { loadConfig } from '../src/server.js';

// Records what the daemon asks of the BrowserContext and of its browser-level CDP session.
function fakeContext() {
  const calls = [];
  const cdp = { send: async (method, params) => calls.push(['cdp', method, params]) };
  return {
    calls,
    sessionsOpened: 0,
    browser() {
      return { newBrowserCDPSession: async () => (this.sessionsOpened++, cdp) };
    },
    setGeolocation: async (g) => calls.push(['setGeolocation', g]),
  };
}

const granted = ['cdp', 'Browser.setPermission', { permission: { name: 'geolocation' }, setting: 'granted' }];
const reset = ['cdp', 'Browser.resetPermissions', undefined];

const DEFAULTS = { latitude: 0, longitude: 0, accuracy: 10 };

test('parseGeolocation: off by default, with 0,0 and 10 m accuracy', () => {
  assert.deepEqual(parseGeolocation({}), { enabled: false, point: DEFAULTS });
  assert.deepEqual(parseGeolocation({ GREYMAN_USE_GEOLOCATION: '0', GREYMAN_GEOLOCATION_COORD_LAT: '' }), {
    enabled: false,
    point: DEFAULTS,
  });
});

test('parseGeolocation: reads the coordinates and the switch', () => {
  const env = {
    GREYMAN_USE_GEOLOCATION: '1',
    GREYMAN_GEOLOCATION_COORD_LAT: '-33.8688',
    GREYMAN_GEOLOCATION_COORD_LON: '151.2093',
    GREYMAN_GEOLOCATION_ACCURACY: '0',
  };
  assert.deepEqual(parseGeolocation(env), {
    enabled: true,
    point: { latitude: -33.8688, longitude: 151.2093, accuracy: 0 },
  });
});

test('parseGeolocation: a bad value names its variable, even while disabled', () => {
  for (const [name, bad] of [
    ['GREYMAN_GEOLOCATION_COORD_LAT', '91'],
    ['GREYMAN_GEOLOCATION_COORD_LAT', 'north'],
    ['GREYMAN_GEOLOCATION_COORD_LON', '-180.5'],
    ['GREYMAN_GEOLOCATION_ACCURACY', '-1'],
  ]) {
    assert.throws(() => parseGeolocation({ [name]: bad }), new RegExp(name), `${name}=${bad}`);
  }
});

test('loadConfig carries the geolocation settings', () => {
  const cfg = loadConfig({ GREYMAN_TOKEN: 't', GREYMAN_USE_GEOLOCATION: '1', GREYMAN_GEOLOCATION_COORD_LAT: '40.7' });
  assert.deepEqual(cfg.geolocation, { enabled: true, point: { ...DEFAULTS, latitude: 40.7 } });
  assert.throws(() => loadConfig({ GREYMAN_TOKEN: 't', GREYMAN_GEOLOCATION_COORD_LON: '999' }), /GREYMAN_GEOLOCATION_COORD_LON/);
});

test('setGeolocation sets the point with the default accuracy, then grants the permission', async () => {
  const ctx = fakeContext();
  const text = await setGeolocation(ctx, { ...DEFAULTS, accuracy: 25 }, { option: 'geolocation', latitude: 51.5, longitude: -0.1 });
  assert.equal(text, 'ok geolocation=51.5,-0.1 accuracy=25');
  assert.deepEqual(ctx.calls, [['setGeolocation', { latitude: 51.5, longitude: -0.1, accuracy: 25 }], granted]);
});

test('setGeolocation: an accuracy in the request wins over the default', async () => {
  const ctx = fakeContext();
  await setGeolocation(ctx, DEFAULTS, { latitude: 1, longitude: 2, accuracy: 300 });
  assert.deepEqual(ctx.calls[0], ['setGeolocation', { latitude: 1, longitude: 2, accuracy: 300 }]);
});

test('setGeolocation off drops the override, back to the default the launch flag denies', async () => {
  const ctx = fakeContext();
  assert.equal(await setGeolocation(ctx, DEFAULTS, { off: true }), 'ok geolocation=off');
  assert.deepEqual(ctx.calls, [reset]);
});

test('allowGeolocation grants or drops the override through one CDP session, opened once', async () => {
  const ctx = fakeContext();
  await allowGeolocation(ctx, true);
  await allowGeolocation(ctx, false);
  await allowGeolocation(ctx, false);
  assert.deepEqual(ctx.calls, [granted, reset, reset]);
  assert.equal(ctx.sessionsOpened, 1);
});

test('allowGeolocation says so when the context has no browser to send CDP through', async () => {
  await assert.rejects(allowGeolocation({ browser: () => null }, true), /no browser handle/);
});

test('set calls run one at a time, so the last one sent is the state the browser ends in', async () => {
  const ctx = fakeContext();
  const record = ctx.setGeolocation;
  ctx.setGeolocation = async (g) => {
    await new Promise((resolve) => setTimeout(resolve, 20)); // the slow first step of a grant
    return record(g);
  };
  await Promise.all([
    setGeolocation(ctx, DEFAULTS, { latitude: 1, longitude: 1 }),
    setGeolocation(ctx, DEFAULTS, { off: true }),
  ]);
  assert.deepEqual(ctx.calls, [
    ['setGeolocation', { latitude: 1, longitude: 1, accuracy: 10 }],
    granted,
    reset,
  ]);
});

test('a failed set does not hold up the ones after it', async () => {
  const ctx = fakeContext();
  await assert.rejects(setGeolocation(ctx, DEFAULTS, { latitude: 99, longitude: 0 }), UserError);
  assert.equal(await setGeolocation(ctx, DEFAULTS, { off: true }), 'ok geolocation=off');
});

test('a failed CDP session is not kept: the next call tries again', async () => {
  let attempts = 0;
  const sent = [];
  const cdp = { send: async (method) => sent.push(method) };
  const ctx = {
    browser: () => ({
      newBrowserCDPSession: async () => {
        if (++attempts === 1) throw new Error('cdp down');
        return cdp;
      },
    }),
  };
  await assert.rejects(allowGeolocation(ctx, true), /cdp down/);
  await allowGeolocation(ctx, true);
  assert.equal(attempts, 2);
  assert.deepEqual(sent, ['Browser.setPermission']);
});

test('at launch, only allowing needs the browser; denied is the state it starts in', async () => {
  const ctx = fakeContext();
  await initGeolocation(ctx, false);
  assert.deepEqual(ctx.calls, []);
  assert.equal(ctx.sessionsOpened, 0, 'no CDP session for the default state');
  await initGeolocation({ browser: () => null }, false); // cannot fail: nothing is asked of the browser

  await initGeolocation(ctx, true);
  assert.deepEqual(ctx.calls, [granted]);
});

test('at launch, failing to allow closes the browser and stops the daemon', async () => {
  let closed = false;
  const broken = { browser: () => null, close: async () => void (closed = true) };
  await assert.rejects(initGeolocation(broken, true), /no browser handle/);
  assert.ok(closed, 'the browser is closed before the daemon gives up');
});

test('setGeolocation rejects a missing or out-of-range value without touching the browser', async () => {
  for (const bad of [
    {},
    { longitude: 1 },
    { latitude: 1 },
    { latitude: 91, longitude: 0 },
    { latitude: 0, longitude: -181 },
    { latitude: '1', longitude: 0 }, // a string is not a coordinate
    { latitude: null, longitude: 0 },
    { latitude: 0, longitude: 0, accuracy: -1 },
    { latitude: Infinity, longitude: 0 },
    { off: 'yes' },
  ]) {
    const ctx = fakeContext();
    await assert.rejects(setGeolocation(ctx, DEFAULTS, bad), UserError, JSON.stringify(bad));
    assert.deepEqual(ctx.calls, [], JSON.stringify(bad));
  }
});

test('the set command only knows geolocation, and reads its default accuracy from the config', async () => {
  const context = fakeContext();
  const ctx = { tabs: { context }, cfg: { geolocation: { point: { ...DEFAULTS, accuracy: 7 } } } };
  await assert.rejects(commands.set(ctx, { option: 'timezone' }), /unknown option timezone; options: geolocation/);
  await assert.rejects(commands.set(ctx, {}), UserError);
  assert.deepEqual(await commands.set(ctx, { option: 'geolocation', latitude: 3, longitude: 4 }), {
    text: 'ok geolocation=3,4 accuracy=7',
  });
});
