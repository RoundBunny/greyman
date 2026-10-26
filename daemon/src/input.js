// Input backends: how humanize.js delivers mouse and keyboard input to a page.
//
// Headless Chromium has no display, so input goes in over CDP (cdpInput). Headed, input
// goes to the virtual display as real X events through xdotool (createXInput): the pointer
// really moves, so the live view (`up --watch`) shows it, and the browser takes the input
// as it would a user's. (Measured on Chromium 156: CDP mouse events are trusted and carry
// the right screenX/screenY too, so those do not tell the two apart.)
//
// Both implement session(page, pos, fn). fn gets { viewport, move, down, up, wheel, type,
// press }: viewport() is the viewport's size in CSS pixels ({ w, h }), move takes viewport
// coordinates, wheel is one notch (1 down, -1 up), press takes
// Playwright key names such as 'Enter' or 'ControlOrMeta+A'. `pos` is the tab's last
// pointer position; a backend may overwrite it when the pointer is somewhere else.

import { spawn as nodeSpawn } from 'node:child_process';
import { UserError } from './errors.js';
import { serial } from './serial.js';

const WHEEL_DELTA = 120; // what one wheel notch scrolls in Chromium, in CSS pixels

export const cdpInput = {
  close() {},
  session: (page, _pos, fn) =>
    fn({
      viewport: () => page.evaluate(() => ({ w: innerWidth, h: innerHeight })),
      move: (x, y) => page.mouse.move(x, y),
      down: () => page.mouse.down(),
      up: () => page.mouse.up(),
      wheel: (notches) => page.mouse.wheel(0, notches * WHEEL_DELTA),
      type: (ch) => page.keyboard.type(ch),
      press: (key) => page.keyboard.press(key),
    }),
};

// ---- key names (pure) ----

const MODIFIERS = { Control: 'ctrl', ControlOrMeta: 'ctrl', Shift: 'shift', Alt: 'alt' };
const NAMED_KEYS = { Enter: 'Return', Tab: 'Tab', Delete: 'Delete', Backspace: 'BackSpace', Escape: 'Escape' };

// Characters with a key on the US layout, which is the one the virtual display has.
// Chromium reads the keymap once at startup and ignores keycodes remapped later, so
// xdotool's on-the-fly remap for anything else types nothing (measured on Chromium 156).
const HAS_KEY = /^[\n\r\t\x20-\x7e]$/;

// A typeable character as a Unicode keysym. X maps U0020-U007E to the plain keysyms, so
// this needs no table of punctuation names.
export function charKeysym(ch) {
  if (ch === '\n' || ch === '\r') return NAMED_KEYS.Enter;
  if (ch === '\t') return NAMED_KEYS.Tab;
  return `U${ch.codePointAt(0).toString(16).padStart(4, '0')}`;
}

// 'ControlOrMeta+A' -> 'ctrl+U0061'. Without Shift held, a letter is its lowercase key,
// as in Playwright, so Ctrl+A is select-all and not Ctrl+Shift+A.
export function keyCombo(spec) {
  const parts = spec.split('+');
  const key = parts.pop();
  const mods = parts.map((m) => MODIFIERS[m] ?? unsupported(spec));
  let sym = NAMED_KEYS[key];
  if (!sym) {
    if ([...key].length !== 1) unsupported(spec);
    sym = charKeysym(mods.includes('shift') ? key : key.toLowerCase());
  }
  return [...mods, sym].join('+');
}

function unsupported(spec) {
  throw new UserError(`key ${spec} is not supported by the X input backend`);
}

// ---- X backend ----

// Keeps one `xdotool -` running: it executes each line of stdin as it arrives, so a mouse
// path costs a pipe write per point, not a process per point. There is one pointer and one
// keyboard, so sessions run one at a time, across tabs.
export function createXInput({ spawn = nodeSpawn, timeoutMs = 5000 } = {}) {
  let proc = null;
  let pointer = null; // last known screen position
  const exclusive = serial();

  function start() {
    const p = spawn('xdotool', ['-'], { stdio: ['pipe', 'pipe', 'pipe'] });
    let out = '';
    let err = '';
    // p.waiter: the pending getmouselocation reply. Kept per process, so a dying old
    // process cannot answer or fail the next one's.
    p.fail = (why) => {
      p.failure ??= new UserError(`input backend failed: ${why}`);
      if (proc === p) {
        proc = null;
        pointer = null;
      }
      const w = p.waiter;
      p.waiter = null;
      w?.reject(p.failure);
    };
    p.stdout.setEncoding('utf8');
    p.stdout.on('data', (d) => {
      out += d;
      const m = /X=(-?\d+)\nY=(-?\d+)\n/.exec(out);
      if (!m) return;
      out = out.slice(m.index + m[0].length);
      const w = p.waiter;
      p.waiter = null;
      w?.resolve({ x: Number(m[1]), y: Number(m[2]) });
    });
    p.stderr.setEncoding('utf8');
    p.stderr.on('data', (d) => (err = (err + d).slice(-300)));
    p.stdin.on('error', () => {}); // surfaces through 'exit' and the timeout
    p.on('error', (e) => p.fail(e.code === 'ENOENT' ? 'xdotool is not installed' : e.message));
    p.on('exit', (code, signal) => p.fail(`xdotool exited (${signal ?? code})${err ? `: ${err.trim()}` : ''}`));
    proc = p;
    return p;
  }

  // Waits until xdotool has run everything written so far, and learns where the pointer is.
  async function sync(p) {
    if (p !== proc) throw p.failure;
    let timer;
    const reply = new Promise((resolve, reject) => {
      p.waiter = { resolve, reject };
      // A wedged xdotool is replaced, not reused: the next session starts a fresh one.
      timer = setTimeout(() => {
        p.fail('xdotool did not respond');
        p.kill();
      }, timeoutMs);
    });
    p.stdin.write('getmouselocation --shell\n');
    try {
      pointer = await reply;
    } finally {
      clearTimeout(timer);
    }
  }

  return {
    session: (page, pos, fn) =>
      exclusive(async () => {
        const p = proc ?? start();
        const send = (line) => {
          if (p !== proc) throw p.failure;
          p.stdin.write(`${line}\n`);
        };
        // Input goes to whatever tab is showing. The viewport sits below the toolbar, so
        // its origin on the screen comes from the window's own geometry. The outer size is
        // in screen pixels and the inner size in CSS pixels, which differ by the page zoom
        // (devicePixelRatio, as the virtual display has no scaling of its own).
        await page.bringToFront();
        const view = await page.evaluate(() => ({
          x: screenX + (outerWidth - innerWidth * devicePixelRatio) / 2,
          y: screenY + outerHeight - innerHeight * devicePixelRatio,
          zoom: devicePixelRatio,
          w: innerWidth,
          h: innerHeight,
        }));
        if (!pointer) await sync(p);
        pos.x = (pointer.x - view.x) / view.zoom;
        pos.y = (pointer.y - view.y) / view.zoom;

        const io = {
          viewport: () => ({ w: view.w, h: view.h }),
          move: (x, y) => {
            pointer = { x: Math.round(view.x + x * view.zoom), y: Math.round(view.y + y * view.zoom) };
            send(`mousemove ${pointer.x} ${pointer.y}`);
          },
          down: () => send('mousedown 1'),
          up: () => send('mouseup 1'),
          wheel: (notches) => send(`click ${notches > 0 ? 5 : 4}`),
          type: async (ch) => {
            if (HAS_KEY.test(ch)) return send(`key --delay 0 ${charKeysym(ch)}`);
            // Insert the text instead, as Playwright does for characters off the layout.
            await sync(p); // keep it behind the keys already sent
            await page.keyboard.insertText(ch);
          },
          press: (key) => send(`key --delay 0 ${keyCombo(key)}`),
        };
        let result;
        try {
          result = await fn(io);
        } catch (err) {
          await sync(p).catch(() => {}); // let what was sent land before reporting
          throw err;
        }
        await sync(p);
        return result;
      }),
    close() {
      proc?.kill();
    },
  };
}

// CDP when there is no display to type into.
export const createInput = (cfg) => (cfg.headless ? cdpInput : createXInput());
