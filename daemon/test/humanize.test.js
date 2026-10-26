import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  bezierPath,
  humanClick,
  humanScroll,
  humanType,
  mulberry32,
  pointInBox,
  typingDelays,
} from '../src/humanize.js';

test('seeded RNG is deterministic', () => {
  const a = mulberry32(42);
  const b = mulberry32(42);
  assert.deepEqual([a(), a(), a()], [b(), b(), b()]);
});

test('typingDelays: one per char, bounded, not constant', () => {
  const d = typingDelays('hello world, this is a test', mulberry32(1));
  assert.equal(d.length, 27);
  assert.ok(d.every((x) => x >= 25 && x <= 1200));
  assert.ok(new Set(d).size > 5);
});

test('typingDelays median is in a human range', () => {
  const d = typingDelays('a'.repeat(2000), mulberry32(7)).sort((x, y) => x - y);
  const median = d[d.length >> 1];
  assert.ok(median > 60 && median < 130, `median ${median}`);
});

test('pointInBox stays inside the central 70% of the box', () => {
  const rand = mulberry32(3);
  const box = { x: 100, y: 200, width: 80, height: 40 };
  for (let i = 0; i < 500; i++) {
    const p = pointInBox(box, rand);
    assert.ok(p.x >= 100 + 80 * 0.15 && p.x <= 100 + 80 * 0.85);
    assert.ok(p.y >= 200 + 40 * 0.15 && p.y <= 200 + 40 * 0.85);
  }
});

test('bezierPath ends exactly on target, is not a straight line, and scales with distance', () => {
  const rand = mulberry32(5);
  const short = bezierPath({ x: 0, y: 0 }, { x: 40, y: 0 }, rand);
  const long = bezierPath({ x: 0, y: 0 }, { x: 900, y: 500 }, rand);
  assert.deepEqual(long.at(-1), { x: 900, y: 500 });
  assert.ok(long.length > short.length);
  const offLine = long.some((p) => Math.abs(p.y - (p.x * 500) / 900) > 2);
  assert.ok(offLine, 'path should deviate from the straight line');
});

test('bezierPath handles zero distance', () => {
  const p = bezierPath({ x: 5, y: 5 }, { x: 5, y: 5 }, mulberry32(1));
  assert.deepEqual(p.at(-1), { x: 5, y: 5 });
});

// Minimal page/locator doubles that record what was typed.
function fakeBrowser() {
  const typed = [];
  const page = {
    mouse: { move: async () => {}, down: async () => {}, up: async () => {}, wheel: async () => {} },
    keyboard: { type: async (ch) => typed.push(ch), press: async () => {} },
    evaluate: async () => ({ w: 1000, h: 800 }), // the CDP backend's viewport()
  };
  const locator = {
    scrollIntoViewIfNeeded: async () => {},
    boundingBox: async () => ({ x: 10, y: 10, width: 100, height: 20 }),
    inputValue: async () => '',
  };
  return { page, locator, typed };
}

test('humanType stops typing when its signal aborts', async () => {
  const { page, locator, typed } = fakeBrowser();
  const ac = new AbortController();
  page.keyboard.type = async (ch) => {
    typed.push(ch);
    if (typed.length === 3) ac.abort(new Error('hung up'));
  };
  await assert.rejects(
    humanType(page, { x: 0, y: 0 }, locator, 'abcdefgh', { sleep: async () => {}, signal: ac.signal }),
    /hung up/,
  );
  assert.deepEqual(typed, ['a', 'b', 'c']);
});

test('humanType without a signal types everything', async () => {
  const { page, locator, typed } = fakeBrowser();
  await humanType(page, { x: 0, y: 0 }, locator, 'abc', { sleep: async () => {} });
  assert.deepEqual(typed, ['a', 'b', 'c']);
});

test('humanClick never presses the button once aborted', async () => {
  const { page, locator } = fakeBrowser();
  const ac = new AbortController();
  let down = 0;
  page.mouse.down = async () => down++;
  ac.abort(new Error('hung up'));
  await assert.rejects(humanClick(page, { x: 0, y: 0 }, locator, { sleep: async () => {}, signal: ac.signal }), /hung up/);
  assert.equal(down, 0);
});

test('humanScroll stops between wheel notches when aborted', async () => {
  const { page } = fakeBrowser();
  const ac = new AbortController();
  let wheels = 0;
  page.mouse.wheel = async () => {
    if (++wheels === 4) ac.abort(new Error('hung up'));
  };
  await assert.rejects(
    humanScroll(page, { x: 5, y: 5 }, 'down', 10, { sleep: async () => {}, signal: ac.signal }),
    /hung up/,
  );
  assert.equal(wheels, 4);
});

// An input backend that records what the session was asked to do.
function recordingInput() {
  const log = [];
  const io = {
    viewport: () => ({ w: 1000, h: 800 }),
    move: async () => log.push('move'),
    down: async () => log.push('down'),
    up: async () => log.push('up'),
    wheel: async (n) => log.push(`wheel ${n}`),
    type: async (ch) => log.push(`type ${ch}`),
    press: async (key) => log.push(`press ${key}`),
  };
  const input = {
    session: async (_page, _pos, fn) => {
      log.push('session');
      return fn(io);
    },
  };
  return { log, input };
}

test('humanType is one input session: click, clear what is there, then keys', async () => {
  const { page, locator } = fakeBrowser();
  locator.inputValue = async () => 'old';
  const { log, input } = recordingInput();
  await humanType(page, { x: 0, y: 0 }, locator, 'a\nb', { sleep: async () => {}, input });
  assert.equal(log.filter((l) => l === 'session').length, 1);
  const from = log.indexOf('up') + 1;
  assert.deepEqual(log.slice(from), ['press ControlOrMeta+A', 'press Delete', 'type a', 'press Enter', 'type b']);
  assert.deepEqual(log.slice(0, 2), ['session', 'move']);
  assert.ok(log.indexOf('down') < log.indexOf('up'));
});

test('humanClick runs as one session', async () => {
  const { page, locator } = fakeBrowser();
  const { log, input } = recordingInput();
  await humanClick(page, { x: 0, y: 0 }, locator, { sleep: async () => {}, input });
  assert.equal(log.filter((l) => l === 'session').length, 1);
});

test('humanType with submit presses Enter in the same session, unless aborted first', async () => {
  const { page, locator } = fakeBrowser();
  const { log, input } = recordingInput();
  await humanType(page, { x: 0, y: 0 }, locator, 'a', { sleep: async () => {}, input, submit: true });
  assert.equal(log.filter((l) => l === 'session').length, 1);
  assert.deepEqual(log.slice(-2), ['type a', 'press Enter']);

  const ac = new AbortController();
  const aborted = recordingInput();
  const sleep = async (ms) => {
    if (ms >= 200) ac.abort(); // the pause before submitting; with rand 0.5 a key waits ~60ms
  };
  const opts = { sleep, rand: () => 0.5, input: aborted.input, submit: true, signal: ac.signal };
  await assert.rejects(humanType(page, { x: 0, y: 0 }, locator, 'a', opts));
  assert.equal(aborted.log.at(-1), 'type a');
});

test('humanScroll turns the wheel five notches a unit, in the given direction', async () => {
  const { page } = fakeBrowser();
  const { log, input } = recordingInput();
  await humanScroll(page, { x: 500, y: 400 }, 'up', 2, { sleep: async () => {}, input });
  assert.deepEqual(log, ['session', ...Array(10).fill('wheel -1')]);
});

test('humanScroll first brings a pointer that is not over the page onto it', async () => {
  for (const pos of [{ x: 0, y: 0 }, { x: 500, y: -40 }, { x: 1500, y: 400 }]) {
    const { page } = fakeBrowser(); // viewport 1000x800
    const { log, input } = recordingInput();
    await humanScroll(page, pos, 'down', 1, { sleep: async () => {}, input });
    assert.ok(log.includes('move'), `no move from ${JSON.stringify(pos)}`);
    assert.ok(pos.x > 0 && pos.x < 1000 && pos.y > 0 && pos.y < 800);
  }
});
