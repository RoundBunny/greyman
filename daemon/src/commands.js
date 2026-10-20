import { UserError, firstLine } from './errors.js';
import { setGeolocation } from './geolocation.js';
import { humanClick, humanScroll, humanType } from './humanize.js';
import { originOf } from './ratelimit.js';
import { readPage } from './read.js';
import { snapshot } from './snapshot.js';

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const REF_RE = /^[a-z0-9]+$/i;

// A leading "name:" is a scheme unless a port follows, so "example.com:8080" and
// "localhost:3000" are hosts while "mailto:x" and "javascript:x" are rejected below.
const SCHEME_RE = /^[a-z][a-z0-9+.-]*:(?!\d+(?:[/?#]|$))/i;

export function normalizeUrl(raw) {
  if (!raw) throw new UserError('url required');
  const url = SCHEME_RE.test(raw) ? raw : `https://${raw}`;
  let parsed;
  try {
    parsed = new URL(url);
  } catch {
    throw new UserError(`invalid url: ${raw}`);
  }
  if (!['http:', 'https:'].includes(parsed.protocol)) throw new UserError(`unsupported scheme ${parsed.protocol}`);
  return parsed.href;
}

function locate(page, ref) {
  if (!REF_RE.test(ref ?? '')) throw new UserError('ref required (e.g. e12); run snap');
  return page.getByRef(ref);
}

// Refs only live until the next snapshot/navigation; a miss surfaces as a Playwright timeout.
async function withRef(ref, fn) {
  try {
    return await fn();
  } catch (err) {
    if (err?.name === 'TimeoutError') throw new UserError(`ref ${ref} not found or not actionable; run snap`);
    throw err;
  }
}

// Serialize work per tab.
function exclusive(tab, fn) {
  const run = tab.queue.then(fn);
  tab.queue = run.catch(() => {});
  return run;
}

// Serialized and tracked as an agent action (see Tabs.during).
const action = (ctx, tab, fn) => exclusive(tab, () => ctx.tabs.during(fn));

// Playwright reports DNS failures, refused connections and aborted downloads as plain
// errors with a multi-line call log; surface them as the user-facing result they are.
async function navigate(page, url) {
  try {
    await page.goto(url, { waitUntil: 'domcontentloaded' });
  } catch (err) {
    if (err?.name === 'TimeoutError') throw new UserError(`navigation to ${url} timed out`);
    throw new UserError(firstLine(err?.message).replace(/^page\.goto: /, ''));
  }
}

async function settle(page) {
  await sleep(500 + Math.random() * 400);
  await page.waitForLoadState('domcontentloaded', { timeout: 5000 }).catch(() => {});
}

// One-line result. Only reports location when it changed, to keep output small.
async function statusLine({ tabs, limiter }, tab, before, { force = false } = {}) {
  const parts = ['ok'];
  const fresh = tabs.takeFresh();
  const switched = fresh.includes(tabs.activeId);
  const cur = switched ? await tabs.get() : tab;
  // new_tab: opened by this action and now active. bg_tab: opened on its own, left alone.
  if (fresh.length) parts.push(`${switched ? 'new_tab' : 'bg_tab'}=${fresh.join(',')}`);
  const url = cur.page.url();
  if (force || switched || url !== before) {
    parts.push(`url=${url}`, `title=${JSON.stringify(await cur.page.title())}`);
    limiter.touch(originOf(url));
  }
  return parts.join(' ');
}

// Wrap an in-page action: pace it, run it serialized, report the result.
async function act(ctx, args, fn) {
  const tab = await ctx.tabs.get(args.tab);
  return action(ctx, tab, async () => {
    const before = tab.page.url();
    await ctx.limiter.wait(originOf(before), 'act', ctx.signal);
    await fn(tab);
    await settle(tab.page);
    return { text: await statusLine(ctx, tab, before) };
  });
}

export const commands = {
  async status({ tabs, cfg }) {
    const active = tabs.activeId ?? 'none';
    return {
      text: `ok browser=${cfg.channel} headless=${cfg.headless} tabs=${tabs.ids.join(',') || 'none'} active=${active}`,
    };
  },

  async open(ctx, args) {
    const url = normalizeUrl(args.url);
    const tab = await ctx.tabs.get(args.tab);
    return action(ctx, tab, async () => {
      await ctx.limiter.wait(originOf(url), 'nav', ctx.signal);
      await navigate(tab.page, url);
      await tab.page.waitForLoadState('load', { timeout: 8000 }).catch(() => {});
      return { text: await statusLine(ctx, tab, null, { force: true }) };
    });
  },

  async snap(ctx, args) {
    const tab = await ctx.tabs.get(args.tab);
    return exclusive(tab, async () => ({ text: await snapshot(tab.page, args.max ?? ctx.cfg.maxChars, ctx.cfg.snapMaxText) }));
  },

  async read(ctx, args) {
    const tab = await ctx.tabs.get(args.tab);
    return exclusive(tab, async () => ({ text: await readPage(tab.page, args.max ?? ctx.cfg.maxChars) }));
  },

  async shot(ctx, args) {
    if (args.ref && args.full) throw new UserError('full cannot be combined with a ref');
    const tab = await ctx.tabs.get(args.tab);
    return exclusive(tab, async () => {
      const png = args.ref
        ? await withRef(args.ref, () => locate(tab.page, args.ref).screenshot({ type: 'png' }))
        : await tab.page.screenshot({ type: 'png', fullPage: Boolean(args.full) });
      return { text: 'ok', png: png.toString('base64') };
    });
  },

  async click(ctx, args) {
    return act(ctx, args, (tab) =>
      withRef(args.ref, () => humanClick(tab.page, tab.pos, locate(tab.page, args.ref), { signal: ctx.signal })),
    );
  },

  async type(ctx, args) {
    if (typeof args.text !== 'string') throw new UserError('text required');
    return act(ctx, args, async (tab) => {
      const opts = { signal: ctx.signal };
      await withRef(args.ref, () => humanType(tab.page, tab.pos, locate(tab.page, args.ref), args.text, opts));
      if (args.submit) {
        await sleep(200 + Math.random() * 300);
        ctx.signal.throwIfAborted(); // an interrupted type must not submit the form
        await tab.page.keyboard.press('Enter');
      }
    });
  },

  async scroll(ctx, args) {
    if (!['up', 'down'].includes(args.direction)) throw new UserError('direction must be up or down');
    const units = Math.min(Math.max(Math.trunc(args.units ?? 1), 1), 20);
    return act(ctx, args, (tab) => humanScroll(tab.page, tab.pos, args.direction, units, { signal: ctx.signal }));
  },

  async back(ctx, args) {
    return act(ctx, args, async (tab) => {
      const res = await tab.page.goBack({ waitUntil: 'domcontentloaded' });
      if (!res) throw new UserError('no history to go back to');
    });
  },

  // Live changes to the running browser. Nothing here touches the config.
  async set(ctx, args) {
    if (args.option !== 'geolocation') throw new UserError(`unknown option ${args.option}; options: geolocation`);
    return { text: await setGeolocation(ctx.tabs.context, ctx.cfg.geolocation.point, args) };
  },

  async close(ctx, args) {
    const tab = await ctx.tabs.get(args.tab);
    await tab.page.close();
    return { text: `ok closed=${tab.id} active=${ctx.tabs.activeId ?? 'none'}` };
  },
};
