import { chromium } from 'playwright';
import { UserError } from './errors.js';
import { initGeolocation } from './geolocation.js';
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

  #add(page, opened) {
    const id = `t${++this.#n}`;
    const tab = { id, page, pos: { x: 0, y: 0 }, queue: Promise.resolve() };
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
