import { chromium } from 'playwright';
import { UserError } from './errors.js';
import { initGeolocation } from './geolocation.js';
import { attachLog } from './log.js';
import { IGNORE_DEFAULT_ARGS, applyToContext, launchArgs } from './stealth.js';

// Tab registry over a persistent BrowserContext. Tabs get short stable ids (t1, t2...).
export class Tabs {
  #n = 0;
  #byId = new Map();
  #active = null;
  #fresh = [];
  #acting = 0;

  constructor(context) {
    this.context = context;
    for (const page of context.pages()) this.#add(page, false);
    context.on('page', (page) => this.#add(page, true));
  }

  // Runs one agent-initiated action. A tab opened meanwhile (target=_blank, window.open)
  // becomes active; one that appears on its own (ad, timer) is tracked but stays in the
  // background, so a later snap or click never silently lands on it.
  async during(fn) {
    this.#acting++;
    try {
      return await fn();
    } finally {
      this.#acting--;
    }
  }

  // A throwaway page for work the agent never sees, such as fetching an image. The context
  // registers it like any page; it is forgotten again as soon as newPage() returns, found by
  // identity so a popup that opens meanwhile keeps its own tab. Its id is reused, being unseen.
  async scratch(fn) {
    const prevActive = this.#active;
    const page = await this.context.newPage();
    this.#forget(page, prevActive);
    try {
      return await fn(page);
    } finally {
      await page.close().catch(() => {});
    }
  }

  #forget(page, prevActive) {
    for (const [id, tab] of this.#byId) {
      if (tab.page !== page) continue;
      this.#byId.delete(id);
      this.#fresh = this.#fresh.filter((f) => f !== id);
      if (id === `t${this.#n}`) this.#n--;
      if (this.#active === id) this.#active = this.#byId.has(prevActive) ? prevActive : ([...this.#byId.keys()].pop() ?? null);
    }
  }

  #add(page, opened) {
    const id = `t${++this.#n}`;
    // refs/snapUrl: what the last snap on this tab reported, so a ref can be checked
    // before it is used (cleared when a new document loads). log: console and network
    // errors (see log.js).
    const tab = { id, page, pos: { x: 0, y: 0 }, queue: Promise.resolve(), refs: null, snapUrl: '', log: [] };
    attachLog(page, tab.log);
    // A new document rebuilds the page's ref table, so refs from the last snapshot name
    // nothing reliable any more. (Same-document navigation keeps them.)
    page.on('domcontentloaded', () => {
      tab.refs = null;
    });
    this.#byId.set(id, tab);
    if (opened) this.#fresh.push(id);
    if ((opened && this.#acting > 0) || !this.#active) this.#active = id;
    page.on('close', () => {
      this.#byId.delete(id);
      if (this.#active === id) this.#active = [...this.#byId.keys()].pop() ?? null;
    });
  }

  get ids() {
    return [...this.#byId.keys()];
  }

  get activeId() {
    return this.#active;
  }

  // Ids of tabs opened (e.g. by target=_blank) since the last call.
  takeFresh() {
    const fresh = this.#fresh;
    this.#fresh = [];
    return fresh;
  }

  async get(id) {
    if (id) {
      const tab = this.#byId.get(id);
      if (!tab) throw new UserError(`no such tab ${id}; open tabs: ${this.ids.join(',') || 'none'}`);
      return tab;
    }
    if (!this.#active) {
      await this.context.newPage(); // 'page' event registers and activates it
      this.takeFresh();
    }
    return this.#byId.get(this.#active);
  }
}

export async function launchBrowser(cfg) {
  const context = await chromium.launchPersistentContext(cfg.profileDir, {
    channel: cfg.channel,
    headless: cfg.headless,
    viewport: null, // use the real window size
    args: launchArgs({ locale: cfg.locale }),
    ignoreDefaultArgs: IGNORE_DEFAULT_ARGS,
    proxy: cfg.proxy,
    geolocation: cfg.geolocation.enabled ? cfg.geolocation.point : undefined,
    // server.js owns shutdown. Playwright's own signal handlers race with it and can
    // kill Chromium before it flushes cookies to the profile.
    handleSIGINT: false,
    handleSIGTERM: false,
    handleSIGHUP: false,
  });
  context.setDefaultTimeout(cfg.actionTimeoutMs);
  context.setDefaultNavigationTimeout(cfg.navTimeoutMs);
  await applyToContext(context);
  await initGeolocation(context, cfg.geolocation.enabled);
  return new Tabs(context);
}
