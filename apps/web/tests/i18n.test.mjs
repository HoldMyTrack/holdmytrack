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
  // Except {n} in a plural form: tn always supplies it, and English's "one" can leave it out
  // where another language's "one" form can't (below).
  const withoutN = (msg, plural) => placeholders(msg).filter((p) => !(plural && p === '{n}'));
  for (const [lang, catalog] of Object.entries(catalogs)) {
    for (const [key, msg] of Object.entries(catalog)) {
      const plural = pluralSuffix.test(key);
      const englishKey = key in en ? key : `${base(key)}.other`;
      assert.deepEqual(withoutN(msg, plural), withoutN(en[englishKey], plural), `${lang} ${key}`);
    }
  }
});

test('a plural form used for more than one count shows the count', () => {
  const counts = [0, 1, 2, 3, 5, 11, 21, 22, 25, 101, 111];
  for (const [lang, catalog] of Object.entries(catalogs)) {
    const rules = new Intl.PluralRules(lang);
    for (const [key, msg] of Object.entries(catalog)) {
      const form = key.match(pluralSuffix)?.[1];
      if (!form) continue;
      // Russian's "one" is 1, 21, 31, 101…: a "История" with no number would read 21 as one.
      const covers = counts.filter((n) => rules.select(n) === form);
      if (covers.length > 1) assert.ok(msg.includes('{n}'), `${lang} ${key} is used for ${covers.join(', ')}`);
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
