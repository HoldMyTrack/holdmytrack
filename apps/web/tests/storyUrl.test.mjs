import { test } from 'node:test';
import assert from 'node:assert/strict';
import { storiesTabParam, storyParam, urlWithStory } from '../src/map/storyUrl.ts';

test('the Story and the Stories tab are read from the query', () => {
  assert.equal(storyParam('?story=abc&x=1'), 'abc');
  assert.equal(storyParam('?story='), null);
  assert.equal(storyParam(''), null);
  assert.equal(storiesTabParam('?tab=stories'), true);
  assert.equal(storiesTabParam('?tab=activities'), false);
});

test('setting and removing the Story keeps the hash and other parameters', () => {
  const location = { pathname: '/', search: '?x=1', hash: '#12/59.4/24.7' };
  assert.equal(urlWithStory(location, 's1'), '/?x=1&story=s1#12/59.4/24.7');
  assert.equal(urlWithStory({ ...location, search: '?story=s1' }, null), '/#12/59.4/24.7');
});
