import { test } from 'node:test';
import assert from 'node:assert/strict';
import { emptySelection, initialSession, mapSessionReducer } from '../src/map/session.ts';

const reduce = (state, ...events) => events.reduce(mapSessionReducer, state);
const withSelection = (selection) => ({ ...initialSession(null), selection: { ...emptySelection(), ...selection } });

test('leaving Normal sets the checked group and focus aside, and Normal brings them back', () => {
  const start = withSelection({ checked: new Set(['a']), focused: 'b', hidden: new Set(['c']) });
  const fog = reduce(start, { type: 'modeChanged', mode: 'fog' });
  assert.equal(fog.selection.checked.size, 0);
  assert.equal(fog.selection.focused, null);
  // Hidden tracks and filters aren't set aside.
  assert.deepEqual([...fog.selection.hidden], ['c']);
  const back = reduce(fog, { type: 'modeChanged', mode: 'heatmap' }, { type: 'modeChanged', mode: 'normal' });
  assert.deepEqual([...back.selection.checked], ['a']);
  assert.equal(back.selection.focused, 'b');
  assert.equal(back.modeSnapshot, null);
});

test('the same mode changes nothing', () => {
  const start = withSelection({ checked: new Set(['a']) });
  assert.equal(reduce(start, { type: 'modeChanged', mode: 'normal' }), start);
});

test("viewing a day picks it as the range, without the range's own flight", () => {
  const next = reduce(withSelection({ checked: new Set(['a']) }), { type: 'dayViewed', day: '2026-10-05' });
  assert.deepEqual(next.range, { from: '2026-10-05', to: '2026-10-05' });
  assert.equal(next.userPicked, true);
  assert.equal(next.flyRequest, false);
  assert.equal(next.selection.checked.size, 0);
});

test('removed activities leave the selection; an unrelated removal keeps the state', () => {
  const start = withSelection({ checked: new Set(['a', 'b']), focused: 'a', hidden: new Set(['a']) });
  const next = reduce(start, { type: 'activitiesRemoved', ids: ['a'] });
  assert.deepEqual([...next.selection.checked], ['b']);
  assert.equal(next.selection.focused, null);
  assert.equal(next.selection.hidden.size, 0);
  assert.equal(reduce(start, { type: 'activitiesRemoved', ids: ['x'] }), start);
});

test('removing from the open Story only drops the focus', () => {
  const start = withSelection({ checked: new Set(['a']), focused: 'a' });
  const next = reduce(start, { type: 'activitiesRemoved', ids: ['a'], unfocusOnly: true });
  assert.equal(next.selection.focused, null);
  assert.deepEqual([...next.selection.checked], ['a']);
});

test('Show/Hide acts on the toolbar target', () => {
  const start = withSelection({ focused: 'f' });
  assert.deepEqual([...reduce(start, { type: 'visibilityToggled' }).selection.hidden], ['f']);
  const checked = withSelection({ checked: new Set(['a', 'b']), focused: 'f', hidden: new Set(['a']) });
  assert.equal(reduce(checked, { type: 'visibilityToggled' }).selection.hidden.size, 0);
});

test('a flight is consumed once', () => {
  const flying = reduce(initialSession(null), { type: 'storyEntered', id: 's' });
  assert.equal(flying.flyRequest, true);
  const done = reduce(flying, { type: 'flyConsumed' });
  assert.equal(done.flyRequest, false);
  assert.equal(reduce(done, { type: 'flyConsumed' }), done);
});
