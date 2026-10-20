import assert from 'node:assert/strict';
import { test } from 'node:test';
import { normalizeUrl } from '../src/commands.js';
import { UserError } from '../src/errors.js';

test('bare hosts get https', () => {
  assert.equal(normalizeUrl('example.com'), 'https://example.com/');
  assert.equal(normalizeUrl('example.com/a?b=1'), 'https://example.com/a?b=1');
});

test('host:port is a host, not a scheme', () => {
  assert.equal(normalizeUrl('example.com:8080/x'), 'https://example.com:8080/x');
  assert.equal(normalizeUrl('localhost:3000'), 'https://localhost:3000/');
  assert.equal(normalizeUrl('127.0.0.1:3000'), 'https://127.0.0.1:3000/');
  assert.equal(normalizeUrl('[::1]:3000'), 'https://[::1]:3000/');
});

test('explicit http(s) schemes are kept', () => {
  assert.equal(normalizeUrl('http://a.test'), 'http://a.test/');
  assert.equal(normalizeUrl('HTTPS://a.test:8443/p'), 'https://a.test:8443/p');
});

test('other schemes are rejected', () => {
  for (const raw of ['mailto:a@b.test', 'javascript:alert(1)', 'file:///etc/passwd', 'data:text/html,hi', 'ftp://a.test']) {
    assert.throws(() => normalizeUrl(raw), UserError, raw);
  }
});

test('empty url is a user error', () => {
  assert.throws(() => normalizeUrl(''), UserError);
});
