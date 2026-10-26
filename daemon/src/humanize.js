// Human-like input. The first half is pure (seedable RNG) so it can be unit tested;
// the second half drives a page with those primitives through an input backend (input.js).

import { UserError } from './errors.js';
import { cdpInput } from './input.js';

const defaultSleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// ---- pure ----

export function mulberry32(seed) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

function gauss(rand) {
  const u = Math.max(rand(), Number.EPSILON);
  return Math.sqrt(-2 * Math.log(u)) * Math.cos(2 * Math.PI * rand());
}

// Per-character delays in ms: log-normal around ~90ms, slower after spaces and
// punctuation, with an occasional longer "thinking" pause.
export function typingDelays(text, rand = Math.random) {
  return Array.from(text, (ch) => {
    let d = Math.exp(Math.log(90) + 0.35 * gauss(rand));
    if (' .,;:!?'.includes(ch)) d *= 1.4;
    if (rand() < 0.03) d += 300 + rand() * 300;
    return Math.round(Math.min(Math.max(d, 25), 1200));
  });
}

// Random point inside a box, biased toward the center, never within 15% of an edge.
export function pointInBox({ x, y, width, height }, rand = Math.random) {
  const bias = () => (rand() + rand()) / 2; // triangular around 0.5
  const u = 0.15 + 0.7 * bias();
  const v = 0.15 + 0.7 * bias();
  return { x: x + width * u, y: y + height * v };
}

// Cubic bezier from `from` to `to` with randomized control points, eased
// (slow-fast-slow) and lightly jittered. Step count scales with distance.
export function bezierPath(from, to, rand = Math.random) {
  const dx = to.x - from.x;
  const dy = to.y - from.y;
  const dist = Math.hypot(dx, dy);
  const steps = Math.round(Math.min(Math.max(dist / 12, 8), 60));
  const nx = dist ? -dy / dist : 0; // unit normal
  const ny = dist ? dx / dist : 0;
  const bend = (rand() - 0.5) * 0.4 * dist;
  const c1 = { x: from.x + dx * 0.3 + nx * bend, y: from.y + dy * 0.3 + ny * bend };
  const c2 = { x: from.x + dx * 0.7 - nx * bend * 0.5, y: from.y + dy * 0.7 - ny * bend * 0.5 };
  const pts = [];
  for (let i = 1; i <= steps; i++) {
    const raw = i / steps;
    const t = raw * raw * (3 - 2 * raw); // smoothstep easing
    const m = 1 - t;
    const x = m ** 3 * from.x + 3 * m * m * t * c1.x + 3 * m * t * t * c2.x + t ** 3 * to.x;
    const y = m ** 3 * from.y + 3 * m * m * t * c1.y + 3 * m * t * t * c2.y + t ** 3 * to.y;
    const last = i === steps;
    pts.push({ x: last ? to.x : x + (rand() - 0.5) * 1.5, y: last ? to.y : y + (rand() - 0.5) * 1.5 });
  }
  return pts;
}

// ---- backend-bound ----

// Every function below takes an optional `signal` (AbortSignal) in its options and checks
// it between steps, so a client that hangs up stops the input at the next checkpoint. They
// also take `input`, the backend that delivers the events (default: CDP); a call runs as one
// backend session, so `io` below is that session's pointer and keyboard.

// `pos` is the per-tab last mouse position ({x, y}) and is updated in place.
async function moveTo(io, pos, target, { rand = Math.random, sleep = defaultSleep, signal } = {}) {
  for (const p of bezierPath(pos, target, rand)) {
    signal?.throwIfAborted();
    await io.move(p.x, p.y);
    await sleep(4 + rand() * 8);
  }
  pos.x = target.x;
  pos.y = target.y;
}

async function click(io, pos, locator, opts) {
  const { rand = Math.random, sleep = defaultSleep, signal } = opts;
  await locator.scrollIntoViewIfNeeded();
  const box = await locator.boundingBox();
  if (!box) throw new UserError('element is not visible; run snap');
  await moveTo(io, pos, pointInBox(box, rand), opts);
  await sleep(30 + rand() * 90); // hover before pressing
  signal?.throwIfAborted(); // never leave the button held down
  await io.down();
  await sleep(40 + rand() * 80);
  await io.up();
}

export function humanClick(page, pos, locator, opts = {}) {
  const { input = cdpInput } = opts;
  return input.session(page, pos, (io) => click(io, pos, locator, opts));
}

// `submit` presses Enter after the text, in the same session.
export function humanType(page, pos, locator, text, opts = {}) {
  const { rand = Math.random, sleep = defaultSleep, signal, input = cdpInput, submit = false } = opts;
  return input.session(page, pos, async (io) => {
    await click(io, pos, locator, opts);
    // Replace existing content in text fields; non-inputs just receive keystrokes.
    const existing = await locator.inputValue().catch(() => '');
    if (existing) {
      await io.press('ControlOrMeta+A');
      await io.press('Delete');
    }
    const delays = typingDelays(text, rand);
    let i = 0;
    for (const ch of text) {
      signal?.throwIfAborted();
      await sleep(delays[i++]);
      if (ch === '\n') await io.press('Enter');
      else await io.type(ch);
    }
    if (submit) {
      await sleep(200 + rand() * 300);
      signal?.throwIfAborted(); // an interrupted type must not submit the form
      await io.press('Enter');
    }
  });
}

// Wheel notches in one unit: about 600px, as a hand turns the wheel.
const NOTCHES_PER_UNIT = 5;

export function humanScroll(page, pos, direction, units = 1, opts = {}) {
  const { rand = Math.random, sleep = defaultSleep, signal, input = cdpInput } = opts;
  return input.session(page, pos, async (io) => {
    const vp = await io.viewport();
    // The wheel acts where the pointer is: bring it over the page if it has not been there.
    if (pos.x <= 0 || pos.y <= 0 || pos.x >= vp.w || pos.y >= vp.h) {
      await moveTo(io, pos, { x: vp.w * (0.3 + rand() * 0.4), y: vp.h * (0.3 + rand() * 0.4) }, opts);
    }
    const sign = direction === 'up' ? -1 : 1;
    for (let u = 0; u < units; u++) {
      for (let n = 0; n < NOTCHES_PER_UNIT; n++) {
        signal?.throwIfAborted();
        await io.wheel(sign);
        await sleep(30 + rand() * 50);
      }
      await sleep(150 + rand() * 250);
    }
  });
}
