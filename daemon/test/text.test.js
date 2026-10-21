import assert from 'node:assert/strict';
import { test } from 'node:test';
import { truncate } from '../src/text.js';

test('short text is untouched', () => {
  assert.equal(truncate('abc', 10, 'hint'), 'abc');
});

test('cuts at a line break and says how to get the rest', () => {
  assert.equal(truncate('aaa\nbbb\nccc\nddd', 9, 'raise --max'), 'aaa\nbbb\n... [truncated 8 chars; raise --max]');
});

test('one very long line is cut mid-line', () => {
  assert.equal(truncate('x'.repeat(30), 10), `${'x'.repeat(10)}\n... [truncated 20 chars]`);
});
