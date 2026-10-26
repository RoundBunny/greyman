import assert from 'node:assert/strict';
import { test } from 'node:test';
import { UserError } from '../src/errors.js';
import { compact, compactSnapshot } from '../src/snapshot.js';

// Captured from Playwright 1.64 page.ariaSnapshot({ mode: 'ai' }) on example.com (trimmed).
const RAW = `- generic [active] [ref=e1]:
  - heading "Example Domain" [level=1] [ref=e2]
  - paragraph [ref=e7]: This domain is for use in documentation examples.
  - link "Learn more" [ref=e13] [cursor=pointer]:
    - /url: https://iana.org/help/example-domains
`;

test('flattens bare generics, drops refs on non-interactive nodes, folds /url', () => {
  assert.equal(
    compact(RAW),
    [
      '- heading "Example Domain" [level=1]',
      '- paragraph: This domain is for use in documentation examples.',
      '- link "Learn more" [e13] -> https://iana.org/help/example-domains',
    ].join('\n'),
  );
});

test('keeps refs on clickable generics and form controls', () => {
  const out = compact(`- generic [ref=e1]:
  - generic [ref=e2] [cursor=pointer]: Menu
  - textbox "Search" [ref=e3]:
    - /placeholder: Search the web`);
  assert.equal(out, ['- generic [e2]: Menu', '- textbox "Search" [e3] placeholder Search the web'].join('\n'));
});

// Shape of Hacker News' layout tables (captured via ariaSnapshot, trimmed).
test('hoists layout-table wrappers, drops separators and empty rows', () => {
  const out = compact(`- table [ref=e3]:
  - rowgroup [ref=e4]:
    - row [ref=e5]:
      - cell [ref=e6]:
        - link "new" [ref=e17] [cursor=pointer]:
          - /url: newest
        - text: "|"
        - link "past" [ref=e18] [cursor=pointer]:
          - /url: front
    - row [ref=e30]:
      - cell [ref=e31]
      - cell "1." [ref=e32]
`);
  assert.equal(
    out,
    [
      '- table',
      '  - row',
      '    - link "new" [e17] -> newest',
      '    - link "past" [e18] -> front',
      '  - row',
      '    - cell "1."',
    ].join('\n'),
  );
});

test('drops nameless images and empty paragraphs; named images keep their ref', () => {
  assert.equal(compact('- img [ref=e1]\n- paragraph [ref=e2]\n- img "logo" [ref=e3]'), '- img "logo" [e3]');
});

test('clips very long text', () => {
  const out = compact(`- paragraph [ref=e1]: ${'x'.repeat(500)}`);
  assert.ok(out.length < 260, out.length);
  assert.ok(out.endsWith('...'));
});

test('the text limit is a parameter', () => {
  const raw = `- paragraph [ref=e1]: ${'x'.repeat(500)}`;
  assert.equal(compact(raw, { maxText: 10 }), `- paragraph: ${'x'.repeat(10)}...`);
  const wide = compact(raw, { maxText: 1000 });
  assert.ok(wide.length > 500 && !wide.endsWith('...'), wide.length);
});

test('unparseable lines survive as raw text', () => {
  assert.equal(compact('- ???weird line'), '- ???weird line');
});

// Shape of a store-picker page: the modal comes after the cookie banner's page content,
// and a second dialog (the cookie banner) follows it.
const DIALOGS = `- generic [ref=e1]:
  - heading "Product page" [level=1] [ref=e2]
  - button "Find a store" [ref=e3] [cursor=pointer]
  - paragraph [ref=e4]: Filler
  - dialog [ref=e5]:
    - heading "Find a Store" [level=2] [ref=e6]
    - textbox "ZIP" [ref=e7]:
      - /placeholder: Enter ZIP
    - button "Use my current location" [ref=e8]
  - dialog "Cookie consent" [ref=e9]:
    - button "Accept all" [ref=e10]
`;
const DIALOG_LINES = [
  '- dialog',
  '  - heading "Find a Store" [level=2]',
  '  - textbox "ZIP" [e7] placeholder Enter ZIP',
  '  - button "Use my current location" [e8]',
  '- dialog "Cookie consent"',
  '  - button "Accept all" [e10]',
];
const PAGE_LINES = ['- heading "Product page" [level=1]', '- button "Find a store" [e3]', '- paragraph: Filler'];

test('dialogs print before the page content they came after', () => {
  assert.equal(compact(DIALOGS), [...DIALOG_LINES, ...PAGE_LINES].join('\n'));
});

test('modal prints only the dialogs, or says there are none', () => {
  assert.equal(compact(DIALOGS, { modal: true }), DIALOG_LINES.join('\n'));
  assert.equal(compact('- generic [ref=e1]:\n  - button "x" [ref=e2]', { modal: true }), '(no dialog open)');
});

test('a wrapper that held only a dialog goes with it', () => {
  const out = compact('- region "Overlay" [ref=e1]:\n  - dialog [ref=e2]:\n    - button "OK" [ref=e3]\n- button "Page" [ref=e4]');
  assert.equal(out, ['- dialog', '  - button "OK" [e3]', '- region "Overlay"', '- button "Page" [e4]'].join('\n'));
});

test('grep keeps matches and their ancestors', () => {
  assert.equal(compact(DIALOGS, { grep: 'location' }), '- dialog\n  - button "Use my current location" [e8]');
  assert.equal(compact(DIALOGS, { grep: 'zzz' }), '(no matches)');
});

test('role filters by role, comma separated', () => {
  assert.equal(
    compact(DIALOGS, { role: 'button' }),
    [
      '- dialog',
      '  - button "Use my current location" [e8]',
      '- dialog "Cookie consent"',
      '  - button "Accept all" [e10]',
      '- button "Find a store" [e3]',
    ].join('\n'),
  );
  assert.equal(compact(DIALOGS, { role: 'textbox,paragraph' }).split('\n').length, 3);
});

test('within takes a ref or a pattern', () => {
  assert.equal(compact(DIALOGS, { within: 'e7' }), '- textbox "ZIP" [e7] placeholder Enter ZIP');
  assert.equal(compact(DIALOGS, { within: 'cookie' }), '- dialog "Cookie consent"\n  - button "Accept all" [e10]');
  assert.equal(compact(DIALOGS, { within: 'nothing here' }), '(no matches)');
  assert.equal(compact(DIALOGS, { within: 'Cookie', role: 'button' }), '- dialog "Cookie consent"\n  - button "Accept all" [e10]');
});

test('a bad pattern is a user error', () => {
  assert.throws(() => compact(DIALOGS, { grep: '(' }), UserError);
  assert.throws(() => compact(DIALOGS, { within: '(' }), UserError);
  // a ref that is gone is an error, not a pattern (/e7/ would also match e70-e79); with
  // --modal, so is one outside the dialogs (e3)
  assert.throws(() => compact(DIALOGS, { within: 'e999' }), (err) => err instanceof UserError && /--within e999: no such ref in this snapshot; run snap/.test(err.message));
  assert.throws(() => compact(DIALOGS, { within: 'e3', modal: true }), /outside the dialogs/);
});

test('a long run of ref-less options collapses, keeping the selected one', () => {
  const opts = Array.from({ length: 10 }, (_, i) => `  - option "L${i}"${i === 7 ? ' [selected]' : ''}`);
  const out = compact(['- combobox "Language" [ref=e1]:', ...opts].join('\n'));
  assert.equal(
    out,
    [
      '- combobox "Language" [e1]',
      ...[0, 1, 2, 3, 4].map((i) => `  - option "L${i}"`),
      '  - option "L7" [selected]',
      '  - (+4 more options)',
    ].join('\n'),
  );
});

test('options with refs are never collapsed', () => {
  const opts = Array.from({ length: 10 }, (_, i) => `  - option "L${i}" [ref=e${i + 2}]`);
  assert.equal(compact(['- listbox [ref=e1]:', ...opts].join('\n')).split('\n').length, 11);
});

test('identical siblings collapse but their refs stay listed', () => {
  const item = (n) => `  - listitem [ref=e${n}]:\n    - button [ref=e${n + 1}] [cursor=pointer]:\n      - img "Photo" [ref=e${n + 2}]`;
  assert.equal(
    compact(['- list [ref=e1]:', item(2), item(5), item(8)].join('\n')),
    ['- list', '  - listitem', '    - button [e3]', '      - img "Photo" [e4]', '  - (+2 similar: e6 e7 e9 e10)'].join('\n'),
  );
  // two are not a run
  assert.equal(compact(['- list [ref=e1]:', item(2), item(5)].join('\n')).split('\n').length, 7);
});

test('siblings that differ only in a name do not collapse', () => {
  const raw = ['- list [ref=e1]:', ...[1, 2, 3].map((i) => `  - link "Page ${i}" [ref=e${i + 1}]`)].join('\n');
  assert.equal(compact(raw).split('\n').length, 4);
});

test('refs are reported with role, name and whether they sit in an iframe', () => {
  const { refs } = compactSnapshot(`- generic [ref=e1]:
  - button "Outer" [ref=e2]
  - iframe [ref=e3]:
    - button "Inner" [ref=f2e2]`);
  assert.deepEqual(refs.get('e2'), { role: 'button', name: 'Outer', frame: false });
  assert.deepEqual(refs.get('f2e2'), { role: 'button', name: 'Inner', frame: true });
  assert.equal(refs.get('e3').frame, false);
});

test('an empty page is empty, not "no matches"', () => {
  assert.equal(compact(''), '');
});

// Names with ": " make Playwright quote the whole line. Without unwrapping, the node
// loses its role and ref and cannot be clicked.
test('YAML-quoted lines keep their role, ref and children', () => {
  const raw = `- generic [ref=e1]:
  - 'button "Sale: 50% off" [ref=e2]'
  - 'navigation "Menu: main [x]" [ref=e3]':
    - link "Alpha" [ref=e4]:
      - /url: /a
  - 'link "Link with: colon" [ref=e5] [cursor=pointer]':
    - /url: /d
  - 'button "It''s: ok" [ref=e6]'
  - "button \\"x: y\\" [ref=e7]"
  - paragraph: "Plain text with a colon: and [brackets] and 'quotes'"
  - paragraph: 'say "hi": now'
  - link "Frag" [ref=e8]:
    - /url: "#top"`;
  assert.equal(
    compact(raw),
    [
      '- button "Sale: 50% off" [e2]',
      '- navigation "Menu: main [x]"',
      '  - link "Alpha" [e4] -> /a',
      '- link "Link with: colon" [e5] -> /d',
      '- button "It\'s: ok" [e6]',
      '- button "x: y" [e7]',
      "- paragraph: Plain text with a colon: and [brackets] and 'quotes'",
      '- paragraph: say "hi": now',
      '- link "Frag" [e8] -> #top',
    ].join('\n'),
  );
  const { refs } = compactSnapshot(raw);
  assert.equal(refs.get('e2').name, 'Sale: 50% off');
  assert.equal(refs.get('e6').name, "It's: ok");
});

test('quoted-looking text that is not a whole quoted scalar is left alone', () => {
  assert.equal(compact('- paragraph [ref=e1]: "half quoted'), '- paragraph: "half quoted');
  assert.equal(compact(`- paragraph [ref=e1]: it's 'fine'`), `- paragraph: it's 'fine'`);
});

const CARDS = (n) =>
  ['- list [ref=e1]:', ...Array.from({ length: n }, (_, i) => `  - listitem [ref=e${i + 2}]:\n    - button "Add to cart" [ref=e${30 + i}]`)].join('\n');

test('filters run before the collapse, so no matching ref is lost', () => {
  assert.equal(
    compact(CARDS(5), { role: 'button' }),
    ['- list', '  - listitem', '    - button "Add to cart" [e30]', '  - (+4 similar: e31 e32 e33 e34)'].join('\n'),
  );
  assert.equal(compact(CARDS(5), { grep: 'cart' }).split('\n').pop(), '  - (+4 similar: e31 e32 e33 e34)');
  // a ref inside a collapsed run can still be addressed
  assert.equal(compact(CARDS(5), { within: 'e33' }), '- button "Add to cart" [e33]');
});

test('equal siblings collapse whatever collapses inside them, wherever they sit in the list', () => {
  const item = (n) => `  - listitem [ref=e${n}]:\n${[1, 2, 3, 4].map((k) => `    - link "A" [ref=e${n + k}]`).join('\n')}`;
  const raw = ['- list [ref=e1]:', item(2), item(7), item(12), item(17)].join('\n');
  assert.equal(
    compact(raw),
    [
      '- list',
      '  - listitem',
      '    - link "A" [e3]',
      '    - (+3 similar: e4 e5 e6)',
      '  - (+3 similar: e8 e9 e10 e11 e13 e14 e15 e16 e18 e19 e20 e21)',
    ].join('\n'),
  );
});

test('a catastrophic pattern is stopped instead of freezing the daemon', () => {
  const raw = `- paragraph [ref=e1]: ${'a'.repeat(40)}!`;
  assert.throws(() => compact(raw, { grep: '(a+)+$' }), (err) => err instanceof UserError && /catastrophic backtracking/.test(err.message));
  assert.throws(() => compact(raw, { within: '(a+)+$' }), UserError);
  assert.match(compact(raw, { grep: 'aaa' }), /paragraph/);
});
