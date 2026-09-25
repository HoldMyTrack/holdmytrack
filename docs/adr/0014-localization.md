# ADR-0014: Localization with in-house catalogs, one language decided by the server

## Status

Accepted.

## Context

HoldMyTrack was English-only everywhere. The web app's React code, the Go-rendered pages, the two emails and the server's error messages were all English literals, and plurals were hand-rolled (`n === 1 ? 'activity' : 'activities'`). Only the Android app kept its text in resource files. The product is meant for anyone who has ever recorded a walk, anywhere (`VISION.md`), and the first non-English audience asked for is Russian speakers.

Four surfaces need translating, in three languages of code: the Go server (its pages, its emails, and the error messages the web and Android apps show as they are), the React map app (TypeScript), and the Android app (Kotlin, resource files). A Russian UI also needs Russian plural rules (one/few/many), and Russian number formatting (a no-break space between thousands, a decimal comma).

## Decision

**English and Russian ship; English is the source of truth.** Every other catalog is checked against it, key for key and placeholder for placeholder, by a test on each surface.

**No i18n library.** Each surface has a flat `key → message` catalog, `{name}` placeholders, and plural forms as sibling keys (`key.one`, `key.few`, `key.many`, `key.other`):

- Go: `internal/i18n`, with embedded `locales/en.json` and `locales/ru.json`, CLDR's plural rules for the two languages written out by hand, and the number formatting.
- React: `apps/web/src/i18n`, with `en.ts` (whose keys become a type) and `ru.ts` (typed against it), and plurals through the browser's `Intl.PluralRules`.
- Android: the platform's own `res/values-ru/`.

**The server decides the language, once per request.** The order is: the account's Language setting (`users.locale`, NULL meaning "automatic"), then the request's `Accept-Language`, then English. The server writes the language into the page's `<html lang>`. The React app reads it back from there rather than deciding again, so the two can never disagree. JSON error messages and emails use the same order.

**Long prose pages are translated whole, not by key.** About and Help have a whole-page translation (`about.ru.html`) that the renderer uses in that page's place. Every other page uses catalog keys.

**Only a signed-in account can pin a language.** A signed-out visitor gets whatever their browser asks for. There is no language cookie and no `?lang=` parameter.

## Alternatives considered

- **i18next / react-intl on the web, go-i18n on the server.** Mature, with ICU message syntax. But that is three dependencies (with their plugins) for two languages and a few hundred strings, two different message formats across the stack, and a much larger client bundle than the few dozen lines this needed. The in-house catalogs can be swapped for a library if a language arrives whose grammar outgrows `{name}` plus plural forms (gender agreement, nested selects).
- **The React app choosing its own language** (from `navigator.languages` and the account's setting in `/v1/auth/me`). That duplicates the server's rule in TypeScript. The two could also disagree, for example a Russian map under an English header, on the first paint before `/me` returns. Reading `<html lang>` is one source of truth, with no flash.
- **A language cookie for signed-out visitors, or `?lang=`.** A language switcher on the public pages would need one or the other. The cookie is a second cookie, which reopens `ROADMAP.md`'s cookie-consent reasoning (today there is exactly one, strictly-necessary cookie). `?lang=` URLs split each public page into several for search engines, and would need `hreflang`. The browser's own setting already expresses the preference, and a signed-in account can override it.
- **Separate URLs per language (`/ru/help`).** Better for search engines, which could then index both languages, but it means routing, canonical and `hreflang` work on every page, plus a language switcher. It's right if non-English search traffic matters, and it can be layered on this later.
- **Every paragraph of About and Help as a catalog key.** Consistent with the other pages, but hundreds of keys whose only use is one page, and prose that can't be edited as prose. The cost of whole-page translation is that two files have to be kept in step.
- **Translating the map's place names** (Protomaps can label in a chosen language). It's a basemap style build concern, not UI text, and is left for later (`BRAINSTORM.md`).

## Consequences

- Adding a language means a catalog per surface (`locales/xx.json`, `i18n/xx.ts`, `values-xx/`), whole-page translations of About and Help, its plural rule in `i18n.PluralForm`, its separators in `Localizer.separators`, and an entry in `Supported`, `locales_config.xml` and `localeFilters`. The parity tests list what's missing.
- A signed-out page is cached once per language (`RenderPublic`'s key includes it) and sent with `Vary: Cookie, Accept-Language`. A shared cache now keeps a copy per distinct `Accept-Language` header, which fragments it, a cost that grows with traffic behind a CDN.
- Search engines crawl in English (they send no `Accept-Language`, or an English one), so the Russian pages aren't indexed. That's acceptable while search traffic isn't a goal.
- The server's error messages are now full sentences in the request's language, where they used to be lowercase English fragments. A client that matched on message text would break; machine-readable codes (`email_not_verified`, `demo_read_only`) are unchanged, and nothing in either app matched on text.
- Server messages the apps don't show are left in English: ingest failure reasons, `zip_upload`'s per-entry reasons, internal errors and developer fallbacks. `KNOWN_ISSUES.md` lists where they surface.
- The Fraunces heading font has no Cyrillic, so Russian headings fall back to the browser's serif.