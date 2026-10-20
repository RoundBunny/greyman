import assert from 'node:assert/strict';
import { test } from 'node:test';
import { compact } from '../src/snapshot.js';

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

test('drops nameless images and empty paragraphs', () => {
  assert.equal(compact('- img [ref=e1]\n- paragraph [ref=e2]\n- img "logo" [ref=e3]'), '- img "logo"');
});

test('clips very long text', () => {
  const out = compact(`- paragraph [ref=e1]: ${'x'.repeat(500)}`);
  assert.ok(out.length < 260, out.length);
  assert.ok(out.endsWith('...'));
});

test('the text limit is a parameter', () => {
  const raw = `- paragraph [ref=e1]: ${'x'.repeat(500)}`;
  assert.equal(compact(raw, 10), `- paragraph: ${'x'.repeat(10)}...`);
  const wide = compact(raw, 1000);
  assert.ok(wide.length > 500 && !wide.endsWith('...'), wide.length);
});

test('unparseable lines survive as raw text', () => {
  assert.equal(compact('- ???weird line'), '- ???weird line');
});
