import assert from 'node:assert/strict';
import { test } from 'node:test';
import { UserError } from '../src/errors.js';
import { bestImageUrl, decodeDataUrl, describeDataUrl, parseSrcset } from '../src/images.js';

test('parseSrcset reads w and x descriptors, with or without spaces after commas', () => {
  assert.deepEqual(parseSrcset('a.jpg 500w, b.jpg 2000w'), [
    { url: 'a.jpg', w: 500, x: 0 },
    { url: 'b.jpg', w: 2000, x: 0 },
  ]);
  assert.deepEqual(parseSrcset('a.jpg 1x,b.jpg 2x'), [
    { url: 'a.jpg', w: 0, x: 1 },
    { url: 'b.jpg', w: 0, x: 2 },
  ]);
  assert.deepEqual(parseSrcset('a.jpg, b.jpg 1.5x'), [
    { url: 'a.jpg', w: 0, x: 0 },
    { url: 'b.jpg', w: 0, x: 1.5 },
  ]);
});

test('commas inside a URL do not split it', () => {
  assert.deepEqual(parseSrcset('https://cdn.test/w_300,h_200/a.jpg 300w, https://cdn.test/w_600,h_400/a.jpg 600w'), [
    { url: 'https://cdn.test/w_300,h_200/a.jpg', w: 300, x: 0 },
    { url: 'https://cdn.test/w_600,h_400/a.jpg', w: 600, x: 0 },
  ]);
});

const BASE = 'https://shop.test/p/1';

test('bestImageUrl picks the widest candidate across srcsets and resolves it', () => {
  const found = { src: '/small.jpg', srcsets: ['/small.jpg 500w, /big.jpg 2000w', '/mid.webp 800w'], bg: '' };
  assert.equal(bestImageUrl(found, BASE), 'https://shop.test/big.jpg');
});

test('density descriptors rank by x; plain src is the fallback; then the background', () => {
  assert.equal(bestImageUrl({ src: '/a.jpg', srcsets: ['/a.jpg 1x, /b.jpg 2x'], bg: '' }, BASE), 'https://shop.test/b.jpg');
  assert.equal(bestImageUrl({ src: '/a.jpg', srcsets: [], bg: '/bg.jpg' }, BASE), 'https://shop.test/a.jpg');
  assert.equal(bestImageUrl({ src: '', srcsets: [], bg: '/bg.jpg' }, BASE), 'https://shop.test/bg.jpg');
  assert.equal(bestImageUrl({ src: '', srcsets: [], bg: '' }, BASE), '');
});

test('descriptor-less srcset entries do not beat the current source', () => {
  assert.equal(bestImageUrl({ src: '/cur.jpg', srcsets: ['/x.jpg'], bg: '' }, BASE), 'https://shop.test/cur.jpg');
});

test('decodeDataUrl handles base64 and percent-encoded payloads', () => {
  assert.deepEqual(decodeDataUrl('data:image/png;base64,aGk='), { type: 'image/png', body: Buffer.from('hi') });
  const svg = decodeDataUrl('data:image/svg+xml,%3Csvg%2F%3E');
  assert.equal(svg.type, 'image/svg+xml');
  assert.equal(svg.body.toString(), '<svg/>');
  assert.throws(() => decodeDataUrl('data:nonsense'), UserError);
});

test('decodeDataUrl rejects malformed base64 and percent-encoding instead of saving garbage', () => {
  assert.throws(() => decodeDataUrl('data:image/png;base64,%%%bad'), /malformed base64/);
  assert.throws(() => decodeDataUrl('data:image/png;base64,'), /malformed base64/);
  assert.throws(() => decodeDataUrl('data:image/svg+xml,%E0%A4%A'), /malformed data: URL/);
  assert.equal(decodeDataUrl('data:image/png;base64,aGk=\n').body.toString(), 'hi');
});

test('a data: URL is described, not printed', () => {
  const url = `data:image/png;base64,${'A'.repeat(5000)}`;
  assert.equal(describeDataUrl(url), `data: URL (image/png, ${url.length} chars)`);
  assert.equal(describeDataUrl('data:,hi'), 'data: URL (text/plain, 8 chars)');
});
