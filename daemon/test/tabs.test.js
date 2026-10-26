import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { test } from 'node:test';
import { Tabs } from '../src/browser.js';

class FakeContext extends EventEmitter {
  constructor(pages = []) {
    super();
    this.open = pages;
  }
  pages() {
    return this.open;
  }
  async newPage() {
    const page = new EventEmitter();
    this.emit('page', page);
    return page;
  }
}

const setup = () => {
  const ctx = new FakeContext([new EventEmitter()]);
  return { ctx, tabs: new Tabs(ctx) };
};

test('a tab opened during an action becomes active and is reported fresh', async () => {
  const { ctx, tabs } = setup();
  await tabs.during(() => ctx.newPage());
  assert.equal(tabs.activeId, 't2');
  assert.deepEqual(tabs.takeFresh(), ['t2']);
  assert.deepEqual(tabs.takeFresh(), []);
});

test('a tab that appears on its own stays in the background', async () => {
  const { ctx, tabs } = setup();
  await ctx.newPage();
  assert.equal(tabs.activeId, 't1');
  assert.deepEqual(tabs.ids, ['t1', 't2']);
  assert.deepEqual(tabs.takeFresh(), ['t2']);
});

test('the action window closes when the action ends, even on error', async () => {
  const { ctx, tabs } = setup();
  await assert.rejects(
    tabs.during(async () => {
      throw new Error('boom');
    }),
    /boom/,
  );
  await ctx.newPage();
  assert.equal(tabs.activeId, 't1');
});

test('with no tabs left, the next one becomes active', async () => {
  const { ctx, tabs } = setup();
  ctx.open[0].emit('close');
  assert.equal(tabs.activeId, null);
  await ctx.newPage();
  assert.equal(tabs.activeId, 't2');
});

test('get() opens a page when there is none', async () => {
  const { ctx, tabs } = setup();
  ctx.open[0].emit('close');
  const tab = await tabs.get();
  assert.equal(tab.id, 't2');
  assert.deepEqual(tabs.takeFresh(), []);
});

test('a scratch page is forgotten at once, never reported or active, and closed after', async () => {
  const { ctx, tabs } = setup();
  let closed = false;
  const out = await tabs.during(() =>
    tabs.scratch(async (page) => {
      // while the work runs it is not a tab
      assert.deepEqual(tabs.ids, ['t1']);
      assert.equal(tabs.activeId, 't1');
      page.close = async () => {
        closed = true;
      };
      return 'fetched';
    }),
  );
  assert.equal(out, 'fetched');
  assert.equal(closed, true);
  assert.deepEqual(tabs.ids, ['t1']);
  assert.equal(tabs.activeId, 't1');
  assert.deepEqual(tabs.takeFresh(), []);
  // its id was never seen, so the next real tab gets it
  await ctx.newPage();
  assert.deepEqual(tabs.ids, ['t1', 't2']);
});

test('a popup that opens while a scratch page is being created keeps its own tab', async () => {
  const { ctx, tabs } = setup();
  const realNewPage = ctx.newPage.bind(ctx);
  ctx.newPage = async () => {
    await realNewPage(); // a popup's page event arrives first
    return realNewPage(); // then the scratch page's
  };
  // the fake returns the second page, which scratch must be the one to forget
  await tabs.scratch(async (page) => {
    page.close = async () => {};
  });
  assert.equal(tabs.ids.length, 2, tabs.ids.join());
  assert.equal(tabs.takeFresh().length, 1);
});

test('a popup that closes itself hands control back to the tab it came from', async () => {
  const { ctx, tabs } = setup();
  await ctx.newPage(); // t2: a background ad tab
  tabs.takeFresh();
  const popup = await tabs.during(() => ctx.newPage()); // t3, opened by an action on t1
  assert.equal(tabs.activeId, 't3');
  popup.emit('close');
  assert.equal(tabs.activeId, 't1');
});

test('a tab that opened and closed again before the next action is not reported', async () => {
  const { ctx, tabs } = setup();
  const popup = await tabs.during(() => ctx.newPage());
  popup.emit('close');
  assert.deepEqual(tabs.takeFresh(), []);
  assert.equal(tabs.activeId, 't1');
});

test('closing a scratch page leaves alone a tab that reused its id', async () => {
  const { ctx, tabs } = setup();
  await tabs.during(async () => {
    await tabs.scratch(async (page) => {
      await ctx.newPage(); // a popup opens mid-download and takes the scratch page's id
      page.close = async () => page.emit('close');
    });
  });
  assert.deepEqual(tabs.ids, ['t1', 't2']);
  assert.equal(tabs.activeId, 't2');
});

test('a scratch page is closed when its work fails', async () => {
  const { tabs } = setup();
  let closed = false;
  await assert.rejects(
    tabs.scratch(async (page) => {
      page.close = async () => {
        closed = true;
      };
      throw new Error('fetch failed');
    }),
    /fetch failed/,
  );
  assert.equal(closed, true);
});

test('with no tabs open, a scratch page leaves none behind', async () => {
  const { ctx, tabs } = setup();
  ctx.open[0].emit('close');
  await tabs.scratch(async (page) => {
    page.close = async () => {};
  });
  assert.deepEqual(tabs.ids, []);
  assert.equal(tabs.activeId, null);
});

test('a new document clears the tab refs, but not a same-document change', async () => {
  const { ctx, tabs } = setup();
  const tab = await tabs.get();
  tab.refs = new Map([['e1', { role: 'button', name: 'x', frame: false }]]);
  const frame = { url: () => 'https://a.test/#x' };
  ctx.open[0].mainFrame = () => frame;
  ctx.open[0].emit('framenavigated', frame);
  assert.ok(tab.refs, 'framenavigated alone keeps refs');
  ctx.open[0].emit('domcontentloaded');
  assert.equal(tab.refs, null);
});

test('each tab starts with an empty log and no snapshot', () => {
  const { tabs } = setup();
  return tabs.get().then((tab) => {
    assert.deepEqual(tab.log, []);
    assert.equal(tab.refs, null);
  });
});
