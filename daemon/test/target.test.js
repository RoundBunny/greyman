import assert from 'node:assert/strict';
import { test } from 'node:test';
import { UserError } from '../src/errors.js';
import { resolveName, resolveRef } from '../src/target.js';

// A tab whose page hands out fake locators. `count` is what the role re-check finds.
function setup({ refs, count = 1, snapUrl = 'https://a.test/', url = snapUrl } = {}) {
  const calls = [];
  const loc = {
    and: (other) => {
      calls.push(['and', other.desc]);
      return { count: async () => count };
    },
  };
  const page = {
    url: () => url,
    getByRef: (r) => {
      calls.push(['getByRef', r]);
      return loc;
    },
    getByRole: (role, opts) => ({ desc: `${role}:${opts.name}${opts.exact ? ':exact' : ''}` }),
  };
  return { tab: { page, refs, snapUrl }, calls, loc };
}
const BUTTON = { role: 'button', name: 'Use my current location', frame: false };

test('a ref that is still the same role and name resolves', async () => {
  const { tab, calls, loc } = setup({ refs: new Map([['e4', BUTTON]]) });
  assert.equal(await resolveRef(tab, 'e4'), loc);
  // whole name only: a substring match would accept "Follow" -> "Unfollow"
  assert.deepEqual(calls.at(-1), ['and', 'button:Use my current location:exact']);
});

test('a reused element with a new name is an error, not a click on the wrong thing', async () => {
  const { tab } = setup({ refs: new Map([['e4', BUTTON]]), count: 0 });
  await assert.rejects(resolveRef(tab, 'e4'), (err) => err instanceof UserError && /ref e4 was button "Use my current location".*gone or changed; run snap/.test(err.message));
});

test('a gone ref after navigation says the page changed', async () => {
  const { tab } = setup({ refs: new Map([['e4', BUTTON]]), count: 0, url: 'https://a.test/next', snapUrl: 'https://a.test/' });
  await assert.rejects(resolveRef(tab, 'e4'), /gone or changed \(the page has changed since the last snapshot\); run snap/);
});

test('no snapshot yet, and a ref from another page, say so', async () => {
  await assert.rejects(resolveRef(setup({ refs: null, snapUrl: '', url: 'https://a.test/' }).tab, 'e4'), /no snapshot of this tab yet; run snap/);
  // snapped once, then a new document loaded (Tabs clears refs on domcontentloaded)
  await assert.rejects(resolveRef(setup({ refs: null }).tab, 'e4'), /the page was loaded again since the last snapshot; run snap/);
  const moved = setup({ refs: new Map(), url: 'https://a.test/next' });
  await assert.rejects(resolveRef(moved.tab, 'f9e4'), /not in the last snapshot \(the page has changed since the last snapshot\); run snap/);
  const same = setup({ refs: new Map() });
  await assert.rejects(resolveRef(same.tab, 'e999'), (err) => /not in the last snapshot; run snap/.test(err.message));
});

test('a ref whose document was replaced is an earlier-page error, not an internal one', async () => {
  const { tab } = setup({ refs: new Map([['f9e4', BUTTON]]), url: 'https://a.test/next', snapUrl: 'https://a.test/' });
  tab.page.getByRef = () => ({
    and: () => ({
      count: async () => {
        throw new Error('locator.count: Invalid frame in aria-ref selector "aria-ref=f9e4"');
      },
    }),
  });
  await assert.rejects(resolveRef(tab, 'f9e4'), (err) => err instanceof UserError && /ref f9e4 is from an earlier page \(the page has changed since the last snapshot\); run snap/.test(err.message));
  tab.page.getByRef = () => ({
    and: () => ({
      count: async () => {
        throw new Error('something else');
      },
    }),
  });
  await assert.rejects(resolveRef(tab, 'f9e4'), (err) => !(err instanceof UserError) && /something else/.test(err.message));
});

test('refs inside iframes, unnamed nodes and unknown roles are not re-checked', async () => {
  for (const info of [{ role: 'button', name: 'In frame', frame: true }, { role: 'button', name: '', frame: false }, { role: 'paragraph', name: 'p', frame: false }]) {
    const { tab, calls } = setup({ refs: new Map([['e4', info]]), count: 0 });
    await resolveRef(tab, 'e4');
    assert.ok(!calls.some((c) => c[0] === 'and'), JSON.stringify(info));
  }
});

test('a malformed ref is rejected before any lookup', async () => {
  const { tab, calls } = setup({ refs: new Map() });
  await assert.rejects(resolveRef(tab, 'e1; drop'), /ref required/);
  await assert.rejects(resolveRef(tab, undefined), /ref required/);
  assert.equal(calls.length, 0);
});

// A page for name lookups: every getByRole(...) is one locator; .or() chains them.
function namePage({ n, rows = [] }) {
  const loc = {
    or: () => loc,
    count: async () => n,
    nth: (i) => ({ picked: i }),
    evaluateAll: async () => rows,
  };
  return { getByRole: () => loc };
}

test('a single match resolves; none or several are errors that say what was found', async () => {
  assert.ok(await resolveName(namePage({ n: 1 }), { name: 'Close' }));
  await assert.rejects(resolveName(namePage({ n: 0 }), { name: 'Close' }), /no interactive element named "Close"; run snap/);
  await assert.rejects(resolveName(namePage({ n: 0 }), { name: 'Close', role: 'button' }), /no button named "Close"/);
  const rows = [{ role: 'button', text: 'Close' }, { role: 'link', text: 'Close account' }];
  await assert.rejects(
    resolveName(namePage({ n: 2, rows }), { name: 'Close' }),
    /2 elements match interactive element named "Close": 1\. button "Close"; 2\. link "Close account"\. Add --role, --exact or --nth N/,
  );
});

test('--nth picks one of several, 1-based, and is bounded', async () => {
  assert.deepEqual(await resolveName(namePage({ n: 3 }), { name: 'x', nth: 2 }), { picked: 1 });
  await assert.rejects(resolveName(namePage({ n: 3 }), { name: 'x', nth: 4 }), /--nth must be 1 to 3/);
});

test('an unknown role is a user error', async () => {
  const page = { getByRole: () => ({ or: () => page, count: async () => { throw new Error('Unknown role: "nope"\nlog'); } }) };
  const loc = page.getByRole();
  loc.or = () => loc;
  await assert.rejects(resolveName({ getByRole: () => loc }, { name: 'x', role: 'nope' }), /bad --role "nope": Unknown role/);
});
