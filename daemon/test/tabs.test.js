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
