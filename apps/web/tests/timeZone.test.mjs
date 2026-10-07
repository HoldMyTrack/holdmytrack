import { test } from 'node:test';
import assert from 'node:assert/strict';
// Plain .mjs, like rangeShift.test.mjs: Node strips the types from the imported .ts module itself.
import { knownTimeZone } from '../src/ui/timeZone.ts';

test('a zone the runtime knows is passed through', () => {
  assert.equal(knownTimeZone('Asia/Tokyo'), 'Asia/Tokyo');
  assert.equal(knownTimeZone('Etc/GMT+3'), 'Etc/GMT+3');
});

test('an unknown or missing zone falls back to the runtime\'s own rather than throwing', () => {
  assert.equal(knownTimeZone('Mars/Olympus_Mons'), undefined);
  assert.equal(knownTimeZone(''), undefined);
  assert.equal(knownTimeZone(undefined), undefined);
  // What a caller does with it: format in the activity's zone, or the browser's.
  const at = new Date('2026-03-09T22:30:00Z');
  const tokyo = at.toLocaleString('en-GB', { timeZone: knownTimeZone('Asia/Tokyo'), dateStyle: 'medium', timeStyle: 'short' });
  assert.equal(tokyo, '10 Mar 2026, 07:30');
  assert.doesNotThrow(() => at.toLocaleString('en-GB', { timeZone: knownTimeZone('Mars/Olympus_Mons') }));
});
