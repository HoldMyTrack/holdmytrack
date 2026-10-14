import { test } from 'node:test';
import assert from 'node:assert/strict';
// Plain .mjs, like the other unit tests: Node strips the types from the imported .ts module.
import { parseErrorBody } from '../src/apiError.ts';

test("the server's error shape gives its code and message", () => {
  assert.deepEqual(parseErrorBody('{"error":"photo_needs_place","message":"Where was it taken?"}\n', 'fallback'), {
    code: 'photo_needs_place',
    message: 'Where was it taken?',
  });
});

test('a code without a message shows the fallback, not the JSON', () => {
  assert.deepEqual(parseErrorBody('{"error":"internal"}', 'Request failed (500)'), {
    code: 'internal',
    message: 'Request failed (500)',
  });
});

test("a body that isn't the error shape has no code", () => {
  assert.deepEqual(parseErrorBody('', 'Request failed (502)'), { code: null, message: 'Request failed (502)' });
  assert.deepEqual(parseErrorBody('Bad Gateway', 'Request failed (502)'), { code: null, message: 'Bad Gateway' });
  assert.deepEqual(parseErrorBody('[1,2]', 'fallback'), { code: null, message: '[1,2]' });
});