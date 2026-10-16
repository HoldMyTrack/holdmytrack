import { test } from 'node:test';
import assert from 'node:assert/strict';
import { invertedChecks, mapHiddenIds, toggled, toolbarTarget, withoutIds } from '../src/map/selection.ts';

const activity = (id, activityType = 'walking', distanceMeters = 1000, pending = false) => ({ id, activityType, distanceMeters, pending });

test('toggled adds a missing item and removes a present one', () => {
  assert.deepEqual([...toggled(new Set(['a']), 'b')], ['a', 'b']);
  assert.deepEqual([...toggled(new Set(['a', 'b']), 'a')], ['b']);
});

test('the toolbar target is the checked set itself, so state identity holds', () => {
  const checked = new Set(['a']);
  assert.equal(toolbarTarget(checked, 'b'), checked);
});

test('withoutIds returns the same set when none of the ids are in it', () => {
  const set = new Set(['a', 'b']);
  assert.equal(withoutIds(set, new Set(['x'])), set);
  assert.deepEqual([...withoutIds(set, new Set(['a', 'x']))], ['b']);
});

test('inverting checks only the listed rows', () => {
  const listed = [activity('a'), activity('b'), activity('c')];
  // "z" is checked but filtered out of the list: dropped.
  assert.deepEqual([...invertedChecks(listed, new Set(['a', 'z']))].sort(), ['b', 'c']);
});

test('the map hides filtered-out, hidden and pending tracks', () => {
  const activities = [activity('walk'), activity('run', 'running'), activity('ride', 'cycling'), activity('busy', 'walking', 1000, true)];
  const hidden = mapHiddenIds(activities, new Set(['running']), null, new Set(['ride']), ['busy']);
  assert.deepEqual([...hidden].sort(), ['busy', 'ride', 'run']);
});
