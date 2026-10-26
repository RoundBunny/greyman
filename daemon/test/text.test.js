import assert from 'node:assert/strict';
import { test } from 'node:test';
import { truncate } from '../src/text.js';

test('short text is untouched', () => {
  assert.equal(truncate('abc', 10), 'abc');
});

test('cuts at a line break', () => {
  assert.equal(truncate('aaa\nbbb\nccc\nddd', 9), 'aaa\nbbb');
});

test('one very long line is cut mid-line', () => {
  assert.equal(truncate('x'.repeat(30), 10), 'x'.repeat(10));
});
