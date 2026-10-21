import { largestDialog, openDialogsHtml } from './dialogs.js';
import { UserError, firstLine } from './errors.js';
import { setGeolocation } from './geolocation.js';
import { humanClick, humanScroll, humanType } from './humanize.js';
import { bestImageUrl, decodeDataUrl, describeDataUrl, imageSources } from './images.js';
import { DEFAULT_SHOWN, formatLog } from './log.js';
import { originOf } from './ratelimit.js';
import { readHtml, readPage } from './read.js';
import { snapshot } from './snapshot.js';
import { locate, resolveName, resolveRef } from './target.js';
import { checkWait, parseWait, waitFor, waitTimeout } from './wait.js';

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

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

// Optional string argument: absent or empty is undefined, anything else must be a string.
function optString(v, name) {
  if (v === undefined || v === '') return undefined;
  if (typeof v !== 'string') throw new UserError(`${name} must be a string`);
  return v;
}

// A target that vanishes mid-action surfaces as a Playwright timeout.
async function withTarget(args, fn) {
  try {
    return await fn();
  } catch (err) {
    if (err?.name === 'TimeoutError') {
      const what = args.name !== undefined ? `element "${args.name}"` : `ref ${args.ref}`;
      throw new UserError(`${what} not found or not actionable; run snap`);
    }
    throw err;
  }
}

// A click or shot target: a ref (checked against the last snapshot) or a name lookup.
function target(tab, args) {
  if (args.name !== undefined) {
    if (args.ref) throw new UserError('give a ref or --name, not both');
    return resolveName(tab.page, args);
  }
  return resolveRef(tab, args.ref);
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
async function statusLine({ tabs, limiter, signal }, tab, before, { force = false, wait = null, waitMs } = {}) {
  const parts = ['ok'];
  const fresh = tabs.takeFresh();
  const switched = fresh.includes(tabs.activeId);
  const cur = switched ? await tabs.get() : tab;
  // new_tab: opened by this action and now active. bg_tab: opened on its own, left alone.
  if (fresh.length) parts.push(`${switched ? 'new_tab' : 'bg_tab'}=${fresh.join(',')}`);
  if (wait && !(await waitFor(cur.page, wait, waitMs, signal))) parts.push('wait=timeout');
  const url = cur.page.url();
  if (force || switched || url !== before) {
    parts.push(`url=${url}`, `title=${JSON.stringify(await cur.page.title())}`);
    limiter.touch(originOf(url));
  }
  return parts.join(' ');
}

// Wrap an in-page action: pace it, run it serialized, report the result.
async function act(ctx, args, fn) {
  const wait = parseWait(args.wait);
  const waitMs = waitTimeout(args.waitTimeout, ctx.cfg.defaultWaitMs);
  const tab = await ctx.tabs.get(args.tab);
  await checkWait(tab.page, wait);
  return action(ctx, tab, async () => {
    const before = tab.page.url();
    await ctx.limiter.wait(originOf(before), 'act', ctx.signal);
    await fn(tab);
    await settle(tab.page);
    return { text: await statusLine(ctx, tab, before, { wait, waitMs }) };
  });
}

export const commands = {
  async status({ tabs, cfg }) {
    const active = tabs.activeId ?? 'none';
    return {
      text: `ok browser=${cfg.channel} headless=${cfg.headless} tabs=${tabs.ids.join(',') || 'none'} active=${active} version=${cfg.version}`,
    };
  },

  async open(ctx, args) {
    const url = normalizeUrl(args.url);
    const wait = parseWait(args.wait);
    const waitMs = waitTimeout(args.waitTimeout, ctx.cfg.defaultWaitMs);
    const tab = await ctx.tabs.get(args.tab);
    await checkWait(tab.page, wait);
    return action(ctx, tab, async () => {
      await ctx.limiter.wait(originOf(url), 'nav', ctx.signal);
      await navigate(tab.page, url);
      await tab.page.waitForLoadState('load', { timeout: 8000 }).catch(() => {});
      return { text: await statusLine(ctx, tab, null, { force: true, wait, waitMs }) };
    });
  },

  async snap(ctx, args) {
    const wait = parseWait(args.wait);
    const waitMs = waitTimeout(args.waitTimeout, ctx.cfg.defaultWaitMs);
    const opts = {
      maxText: ctx.cfg.snapMaxText,
      modal: Boolean(args.modal),
      within: optString(args.within, 'within'),
      role: optString(args.role, 'role'),
      grep: optString(args.grep, 'grep'),
    };
    const tab = await ctx.tabs.get(args.tab);
    await checkWait(tab.page, wait);
    return exclusive(tab, async () => {
      const timedOut = wait && !(await waitFor(tab.page, wait, waitMs, ctx.signal));
      const { text, refs } = await snapshot(tab.page, args.max ?? ctx.cfg.maxChars, opts);
      tab.refs = refs;
      tab.snapUrl = tab.page.url();
      return { text: timedOut ? `(wait timed out: ${wait.spec})\n${text}` : text };
    });
  },

  async read(ctx, args) {
    if (args.ref && args.modal) throw new UserError('give a ref or --modal, not both');
    const tab = await ctx.tabs.get(args.tab);
    const max = args.max ?? ctx.cfg.maxChars;
    return exclusive(tab, async () => {
      if (args.ref) {
        const html = await withTarget(args, async () => (await resolveRef(tab, args.ref)).evaluate((el) => el.outerHTML));
        return { text: readHtml([html], tab.page.url(), max) };
      }
      if (args.modal) {
        const parts = await openDialogsHtml(tab.page);
        if (!parts.length) throw new UserError('no dialog open (looked for <dialog> and role=dialog/alertdialog)');
        return { text: readHtml(parts, tab.page.url(), max) };
      }
      return { text: await readPage(tab.page, max) };
    });
  },

  async shot(ctx, args) {
    if (args.ref && args.full) throw new UserError('full cannot be combined with a ref');
    if (args.modal && (args.ref || args.full)) throw new UserError('modal cannot be combined with a ref or full');
    const tab = await ctx.tabs.get(args.tab);
    return exclusive(tab, async () => {
      let png;
      if (args.modal) png = await (await largestDialog(tab.page)).screenshot({ type: 'png' });
      else if (args.ref) png = await withTarget(args, async () => (await resolveRef(tab, args.ref)).screenshot({ type: 'png' }));
      else png = await tab.page.screenshot({ type: 'png', fullPage: Boolean(args.full) });
      return { text: 'ok', png: png.toString('base64') };
    });
  },

  // Saves the image behind a ref at full resolution, or just reports where it lives.
  async save(ctx, args) {
    const tab = await ctx.tabs.get(args.tab);
    return exclusive(tab, async () => {
      const found = await withTarget(args, async () => (await resolveRef(tab, args.ref)).evaluate(imageSources));
      const url = bestImageUrl(found, tab.page.url());
      if (!url) throw new UserError(`ref ${args.ref} has no image`);
      if (args.url) return { text: url.startsWith('data:') ? describeDataUrl(url) : url };
      const { body, type } = await fetchImage(ctx, tab, url);
      return { text: 'ok', file: body.toString('base64'), content_type: type };
    });
  },

  async log(ctx, args) {
    const max = args.max ?? DEFAULT_SHOWN;
    if (!Number.isInteger(max) || max < 1) throw new UserError('max must be a positive integer');
    const tab = await ctx.tabs.get(args.tab);
    const text = formatLog(tab.log, { errorsOnly: Boolean(args.errors), max });
    if (args.clear) tab.log.length = 0;
    return { text };
  },

  async click(ctx, args) {
    return act(ctx, args, (tab) =>
      withTarget(args, async () =>
        humanClick(tab.page, tab.pos, await target(tab, args), { signal: ctx.signal }),
      ),
    );
  },

  async type(ctx, args) {
    if (typeof args.text !== 'string') throw new UserError('text required');
    return act(ctx, args, async (tab) => {
      const opts = { signal: ctx.signal };
      await withTarget(args, async () => humanType(tab.page, tab.pos, await resolveRef(tab, args.ref), args.text, opts));
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

// The base64 body has to fit the client's 64 MiB response cap.
const MAX_IMAGE_BYTES = 40 * 1024 * 1024;

// The image's bytes, fetched the way the browser would: a real navigation in a throwaway
// tab, so headers and TLS are Chromium's own. Paced like any other navigation.
async function fetchImage(ctx, tab, url) {
  if (url.startsWith('data:')) return decodeDataUrl(url);
  if (!/^https?:/.test(url)) throw new UserError(`cannot download ${url.split(':')[0]}: images; use shot <ref>`);
  await ctx.limiter.wait(originOf(url), 'nav', ctx.signal);
  return ctx.tabs.scratch(async (page) => {
    let res;
    try {
      res = await page.goto(url, { waitUntil: 'commit', referer: tab.page.url() });
    } catch (err) {
      const msg = firstLine(err?.message).replace(/^page\.goto: /, '');
      if (/download is starting/i.test(msg)) {
        throw new UserError(`${url} is served as a file download (Content-Disposition: attachment, or a type browsers do not display such as application/octet-stream), which save cannot fetch`);
      }
      throw new UserError(msg);
    }
    if (!res) throw new UserError(`no response for ${url}`);
    if (!res.ok()) throw new UserError(`HTTP ${res.status()} for ${url}`);
    const type = (res.headers()['content-type'] ?? '').split(';')[0].trim();
    if (!type.startsWith('image/')) throw new UserError(`${url} returned ${type || 'no content type'}, not an image (hotlink protection?)`);
    const tooBig = (n) => new UserError(`image is ${Math.round(n / 2 ** 20)} MB, over the ${MAX_IMAGE_BYTES / 2 ** 20} MB limit`);
    // Chromium drops very large bodies from the cache the daemon reads them through, so
    // refuse on the declared size before asking, and explain a failure after it.
    const declared = Number(res.headers()['content-length']);
    if (declared > MAX_IMAGE_BYTES) throw tooBig(declared);
    let body;
    try {
      body = await res.body();
    } catch (err) {
      throw new UserError(`could not read the image body (it may be too large): ${firstLine(err?.message)}`);
    }
    if (body.length > MAX_IMAGE_BYTES) throw tooBig(body.length);
    return { body, type };
  });
}
