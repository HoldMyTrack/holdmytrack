import { test } from 'node:test';
import assert from 'node:assert/strict';
// Plain .mjs, like editTrackOps.test.mjs: Node strips the types from the catalogs itself.
// tsc already refuses a ru.ts missing one of en.ts's keys; this checks what a type can't.
import { en } from '../src/i18n/en.ts';
import { ru } from '../src/i18n/ru.ts';

const catalogs = { en, ru };
const pluralSuffix = /\.(one|few|many|other)$/;
const base = (key) => key.replace(pluralSuffix, '');
const placeholders = (msg) => [...new Set(msg.match(/\{\w+\}/g) ?? [])].sort();

test('every catalog has the same messages as English, and nothing else', () => {
  const englishBases = new Set(Object.keys(en).map(base));
  for (const [lang, catalog] of Object.entries(catalogs)) {
    const bases = new Set(Object.keys(catalog).map(base));
    assert.deepEqual([...bases].filter((k) => !englishBases.has(k)), [], `${lang}: keys English doesn't have`);
    assert.deepEqual([...englishBases].filter((k) => !bases.has(k)), [], `${lang}: missing keys`);
  }
});

test("each message has English's placeholders — no more, no fewer", () => {
  for (const [lang, catalog] of Object.entries(catalogs)) {
    for (const [key, msg] of Object.entries(catalog)) {
      const englishKey = key in en ? key : `${base(key)}.other`;
      assert.deepEqual(placeholders(msg), placeholders(en[englishKey]), `${lang} ${key}`);
    }
  }
});

test('a plural message has every form its language picks for a whole number', () => {
  for (const [lang, catalog] of Object.entries(catalogs)) {
    const rules = new Intl.PluralRules(lang);
    const forms = new Set([0, 1, 2, 3, 5, 11, 21, 22, 25, 101, 111].map((n) => rules.select(n)));
    for (const b of new Set(Object.keys(catalog).filter((k) => pluralSuffix.test(k)).map(base))) {
      for (const form of forms) assert.ok(`${b}.${form}` in catalog, `${lang}: ${b}.${form} missing`);
    }
  }
});

test('no message is empty or padded', () => {
  for (const [lang, catalog] of Object.entries(catalogs)) {
    for (const [key, msg] of Object.entries(catalog)) {
      assert.ok(msg !== '' && msg.trim() === msg, `${lang} ${key}`);
    }
  }
});
