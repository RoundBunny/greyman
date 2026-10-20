import assert from 'node:assert/strict';
import { test } from 'node:test';
import { parseProxy } from '../src/proxy.js';

test('unset means no proxy', () => {
  assert.equal(parseProxy(undefined), undefined);
  assert.equal(parseProxy(''), undefined);
});

test('credentials are split out of the server URL', () => {
  assert.deepEqual(parseProxy('http://us%40er:p%3Ass@proxy.test:3128'), {
    server: 'http://proxy.test:3128',
    username: 'us@er',
    password: 'p:ss',
  });
});

test('no credentials, no auth fields', () => {
  assert.deepEqual(parseProxy('socks5://proxy.test:1080'), { server: 'socks5://proxy.test:1080' });
});

test('short form is an HTTP proxy', () => {
  assert.deepEqual(parseProxy('proxy.test:3128'), { server: 'http://proxy.test:3128' });
});

test('an invalid value is rejected without echoing it', () => {
  assert.throws(() => parseProxy('http://:pw@'), (err) => !err.message.includes('pw'));
});
