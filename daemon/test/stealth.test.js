import assert from 'node:assert/strict';
import { test } from 'node:test';
import { launchArgs } from '../src/stealth.js';

test('launch args deny permission prompts, which nothing could answer', () => {
  assert.ok(launchArgs().includes('--deny-permission-prompts'));
  assert.ok(launchArgs({ locale: 'en-US' }).includes('--deny-permission-prompts'));
});

test('launch args carry the locale only when there is one', () => {
  assert.ok(launchArgs({ locale: 'fr-FR' }).includes('--lang=fr-FR'));
  assert.ok(!launchArgs().some((a) => a.startsWith('--lang=')));
});
