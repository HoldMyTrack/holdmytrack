import { en, type MessageKey } from './en';
import { ru } from './ru';

/**
 * The map app's localization (ADR-0014, IMPLEMENTATION.md §4.21) — the client-side twin of
 * services/server/internal/i18n. The app doesn't choose its language: the server already did
 * (the account's Language setting, else the browser's Accept-Language, else English) when it
 * rendered the app shell, and wrote the answer into `<html lang>`. Reading it back keeps the
 * two from ever disagreeing, and makes the language a constant for the page's lifetime — a
 * change in Settings is a navigation away and back, a fresh page load.
 *
 * Catalogs are flat `key → message` objects. `{name}` in a message is filled from `vars`; a
 * count's message is `key.one` / `key.few` / `key.many` / `key.other`, of which `tn` picks the
 * form Intl.PluralRules gives for the count. en.ts is the source of truth — ru.ts is typed
 * against its keys, so a missing translation is a type error.
 */

const catalogs = { en, ru } as const;

export type Lang = keyof typeof catalogs;

function detectLang(): Lang {
  const tag = typeof document === 'undefined' ? '' : document.documentElement.lang;
  const primary = tag.toLowerCase().split('-')[0];
  return primary && primary in catalogs ? (primary as Lang) : 'en';
}

/** The page's language, for anything formatting by locale: `toLocaleString(lang, …)`. */
export const lang: Lang = detectLang();

const messages: Record<string, string> = catalogs[lang];
const fallback: Record<string, string> = en;
const plurals = new Intl.PluralRules(lang);
const numbers = new Intl.NumberFormat(lang);

type Vars = Record<string, string | number>;

function fill(message: string, vars?: Vars): string {
  if (!vars) return message;
  return message.replace(/\{(\w+)\}/g, (whole, name: string) => {
    const v = vars[name];
    if (v === undefined) return whole;
    return typeof v === 'number' ? numbers.format(v) : v;
  });
}

function lookup(key: string): string {
  return messages[key] ?? fallback[key] ?? key;
}

/** A catalog message, `{name}`s filled from `vars` (numbers get this language's separators). */
export function t(key: MessageKey, vars?: Vars): string {
  return fill(lookup(key), vars);
}

/** Keys whose messages come in plural forms — `activities` for `activities.one`/`.other`. */
type PluralBase = MessageKey extends infer K ? (K extends `${infer B}.other` ? B : never) : never;

/** A count's message in the plural form `n` takes, with `{n}` set to it. */
export function tn(key: PluralBase, n: number, vars?: Vars): string {
  const form = plurals.select(n);
  const msg = messages[`${key}.${form}`] ?? messages[`${key}.other`] ?? fallback[`${key}.${form === 'one' ? 'one' : 'other'}`] ?? key;
  return fill(msg, { n, ...vars });
}

/** A message that may not exist — for a free-form value with translations for its common cases
 *  (an activity type): the translation, or `undefined` to fall back on something else. */
export function tMaybe(key: string): string | undefined {
  return messages[key] ?? fallback[key];
}
