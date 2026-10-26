import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { test } from 'node:test';
import { UserError } from '../src/errors.js';
import { cdpInput, charKeysym, createXInput, keyCombo } from '../src/input.js';

// A stand-in for `xdotool -`: records the lines written to stdin and answers each
// getmouselocation with the last mousemove (or where the pointer starts).
function fakeXdotool({ silent = false } = {}) {
  const lines = [];
  const procs = [];
  const spawn = () => {
    const p = new EventEmitter();
    let at = { x: 960, y: 540 };
    p.stdout = Object.assign(new EventEmitter(), { setEncoding() {} });
    p.stderr = Object.assign(new EventEmitter(), { setEncoding() {} });
    p.stdin = Object.assign(new EventEmitter(), {
      write(line) {
        for (const l of line.split('\n').filter(Boolean)) {
          lines.push(l);
          const m = /^mousemove (\d+) (\d+)$/.exec(l);
          if (m) at = { x: Number(m[1]), y: Number(m[2]) };
          if (l.startsWith('getmouselocation') && !silent) {
            queueMicrotask(() => p.stdout.emit('data', `X=${at.x}\nY=${at.y}\nSCREEN=0\nWINDOW=1\n`));
          }
        }
      },
    });
    p.kill = () => p.emit('exit', null, 'SIGTERM');
    procs.push(p);
    return p;
  };
  return { spawn, lines, procs };
}

// A page whose window sits at the screen origin with 87px of toolbar above the viewport.
function fakePage({ zoom = 1 } = {}) {
  const calls = [];
  return {
    calls,
    bringToFront: async () => calls.push('bringToFront'),
    evaluate: async () => ({ x: 0, y: 87, zoom, w: 1536, h: 794 }),
    keyboard: { insertText: async (t) => calls.push(`insertText ${t}`) },
  };
}

const lastLines = (lines) => lines.filter((l) => !l.startsWith('getmouselocation'));

test('charKeysym: ASCII as Unicode keysyms, whitespace as named keys', () => {
  assert.equal(charKeysym('a'), 'U0061');
  assert.equal(charKeysym('A'), 'U0041');
  assert.equal(charKeysym(' '), 'U0020');
  assert.equal(charKeysym('~'), 'U007e');
  assert.equal(charKeysym('\n'), 'Return');
  assert.equal(charKeysym('\t'), 'Tab');
});

test('keyCombo maps Playwright key names', () => {
  assert.equal(keyCombo('Enter'), 'Return');
  assert.equal(keyCombo('Delete'), 'Delete');
  assert.equal(keyCombo('ControlOrMeta+A'), 'ctrl+U0061'); // select all, not Ctrl+Shift+A
  assert.equal(keyCombo('Control+Shift+A'), 'ctrl+shift+U0041');
  assert.throws(() => keyCombo('F13'), (err) => err instanceof UserError && /not supported/.test(err.message));
  assert.throws(() => keyCombo('Hyper+a'), /not supported/);
});

test('X session: moves are shifted by the window origin and rounded', async () => {
  const x = fakeXdotool();
  const input = createXInput({ spawn: x.spawn });
  const page = fakePage();
  const pos = { x: 0, y: 0 };
  await input.session(page, pos, async (io) => {
    assert.deepEqual(pos, { x: 960, y: 453 }, 'pos starts where the pointer is, in viewport coordinates');
    io.move(100.4, 200.6);
    io.down();
    io.up();
  });
  assert.deepEqual(lastLines(x.lines), ['mousemove 100 288', 'mousedown 1', 'mouseup 1']);
  assert.deepEqual(page.calls, ['bringToFront']);
  // The next session starts from where the pointer was left.
  await input.session(page, pos, async () => assert.deepEqual(pos, { x: 100, y: 201 }));
});

test('X session: page zoom scales viewport coordinates to the screen', async () => {
  const x = fakeXdotool();
  const input = createXInput({ spawn: x.spawn });
  const pos = { x: 0, y: 0 };
  await input.session(fakePage({ zoom: 1.25 }), pos, async (io) => {
    assert.deepEqual(pos, { x: 768, y: 362.4 }, 'pos is in CSS pixels');
    assert.deepEqual(io.viewport(), { w: 1536, h: 794 }, 'measured once, with the origin');
    io.move(100, 200);
  });
  assert.deepEqual(lastLines(x.lines), ['mousemove 125 337']);
});

test('X session: keys, combos and wheel notches', async () => {
  const x = fakeXdotool();
  const input = createXInput({ spawn: x.spawn });
  await input.session(fakePage(), { x: 0, y: 0 }, async (io) => {
    await io.type('h');
    await io.type('I');
    await io.type('\n');
    await io.press('ControlOrMeta+A');
    await io.press('Enter');
    io.wheel(1);
    io.wheel(-1);
  });
  assert.deepEqual(lastLines(x.lines), [
    'key --delay 0 U0068',
    'key --delay 0 U0049',
    'key --delay 0 Return',
    'key --delay 0 ctrl+U0061',
    'key --delay 0 Return',
    'click 5',
    'click 4',
  ]);
});

test('X session: characters without a key are inserted as text after the keys before them', async () => {
  const x = fakeXdotool();
  const input = createXInput({ spawn: x.spawn });
  const page = fakePage();
  await input.session(page, { x: 0, y: 0 }, async (io) => {
    await io.type('a');
    await io.type('é');
    await io.type('😀');
  });
  assert.deepEqual(lastLines(x.lines), ['key --delay 0 U0061']);
  assert.deepEqual(page.calls, ['bringToFront', 'insertText é', 'insertText 😀']);
  // A getmouselocation between the key and the text proves the key was already run.
  assert.ok(x.lines.indexOf('key --delay 0 U0061') < x.lines.lastIndexOf('getmouselocation --shell'));
});

test('X sessions run one at a time across tabs', async () => {
  const x = fakeXdotool();
  const input = createXInput({ spawn: x.spawn });
  const order = [];
  const hold = () => new Promise((r) => setTimeout(r, 10));
  await Promise.all(
    [1, 2, 3].map((n) =>
      input.session(fakePage(), { x: 0, y: 0 }, async () => {
        order.push(`start${n}`);
        await hold();
        order.push(`end${n}`);
      }),
    ),
  );
  assert.deepEqual(order, ['start1', 'end1', 'start2', 'end2', 'start3', 'end3']);
  assert.equal(x.procs.length, 1, 'one xdotool for all sessions');
});

test('X session: an error in the session still waits for what was sent, then rethrows', async () => {
  const x = fakeXdotool();
  const input = createXInput({ spawn: x.spawn });
  await assert.rejects(
    input.session(fakePage(), { x: 0, y: 0 }, async (io) => {
      io.down();
      throw new Error('hung up');
    }),
    /hung up/,
  );
  assert.equal(x.lines.at(-1), 'getmouselocation --shell');
});

test('X session: a dead xdotool is a user-facing error, and the next session restarts it', async () => {
  const x = fakeXdotool();
  const input = createXInput({ spawn: x.spawn });
  await input.session(fakePage(), { x: 0, y: 0 }, async () => {});
  x.procs[0].emit('exit', 1, null);
  await input.session(fakePage(), { x: 0, y: 0 }, async () => {});
  assert.equal(x.procs.length, 2);

  // Dying mid-session loses events, so it must not look like success.
  await assert.rejects(
    input.session(fakePage(), { x: 0, y: 0 }, async (io) => {
      x.procs[1].stderr.emit('data', "Can't open display\n");
      x.procs[1].emit('exit', 1, null);
      io.move(1, 1);
    }),
    /input backend failed: xdotool exited \(1\): Can't open display/,
  );
});

test('X session: no reply from xdotool times out, and the next session starts a fresh one', async () => {
  const x = fakeXdotool({ silent: true });
  const input = createXInput({ spawn: x.spawn, timeoutMs: 20 });
  await assert.rejects(input.session(fakePage(), { x: 0, y: 0 }, async () => {}), /did not respond/);
  // A late reply from the wedged process must not answer the next session.
  x.procs[0].stdout.emit('data', 'X=1\nY=1\n');
  await assert.rejects(input.session(fakePage(), { x: 0, y: 0 }, async () => {}), /did not respond/);
  assert.equal(x.procs.length, 2);
});

test('X session: a missing xdotool says so', async () => {
  const spawn = () => {
    const p = fakeXdotool().spawn();
    queueMicrotask(() => p.emit('error', Object.assign(new Error('spawn xdotool ENOENT'), { code: 'ENOENT' })));
    return p;
  };
  const input = createXInput({ spawn });
  await assert.rejects(input.session(fakePage(), { x: 0, y: 0 }, async () => {}), /xdotool is not installed/);
});

test('CDP session passes input to the page', async () => {
  const seen = [];
  const page = {
    mouse: {
      move: async (x, y) => seen.push(`move ${x} ${y}`),
      down: async () => seen.push('down'),
      up: async () => seen.push('up'),
      wheel: async (dx, dy) => seen.push(`wheel ${dx} ${dy}`),
    },
    keyboard: { type: async (c) => seen.push(`type ${c}`), press: async (k) => seen.push(`press ${k}`) },
  };
  await cdpInput.session(page, { x: 0, y: 0 }, async (io) => {
    await io.move(1, 2);
    await io.down();
    await io.up();
    await io.wheel(-1);
    await io.type('é');
    await io.press('Enter');
  });
  assert.deepEqual(seen, ['move 1 2', 'down', 'up', 'wheel 0 -120', 'type é', 'press Enter']);
});
