import assert from 'node:assert/strict';
import { test } from 'node:test';
import { bezierPath, mulberry32, pointInBox, typingDelays, humanClick, humanScroll, humanType } from '../src/humanize.js';

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
    evaluate: async () => ({ w: 1000, h: 800 }),
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
