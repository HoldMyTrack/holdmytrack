# ADR-0025: A language menu in the header for everyone, remembered in a cookie

## Status

Accepted. Supersedes ADR-0014's decision that only a signed-in account can pin a language; the rest of ADR-0014 stands.

## Context

ADR-0014 let the server pick the language from the account's Language setting, then the browser's `Accept-Language`, then English. Choosing a language meant the Language field in Settings, which is behind sign-in. A visitor whose browser asks for the wrong language had no way to change it: someone on a shared or work computer set to English, or someone who reads Russian more easily but keeps their OS in English. These are the people deciding whether to sign up, on pages (the front page, About, Help, sign-up itself) they can't read comfortably.

ADR-0014 rejected a switcher on the public pages because it would need a cookie or a `?lang=` parameter. Its objection to the cookie was that a second cookie would reopen `ROADMAP.md`'s cookie-consent question. That objection doesn't hold for this cookie. The ePrivacy Directive's consent requirement (Art. 5(3)) exempts storage strictly necessary for a service the user explicitly asked for, and the EU data protection authorities' guidance on that exemption (Article 29 Working Party, Opinion 04/2012) names a user-interface customization cookie that remembers a chosen language as an example of one. Its objection to `?lang=` still holds.

## Decision

**The shared header has a language menu on every page, signed in or not.** It shows the page's language code and lists each language by its own name. Choosing one is a form POST to `/language`, so it works without script, like the header's other menus.

**The choice is remembered in a cookie, `hmt_lang`.** It is set only when someone picks a language, lasts a year, and is `HttpOnly` and `SameSite=Lax`. It holds a language code and nothing else. The server's order becomes: the account's Language setting, then this cookie, then `Accept-Language`, then English. URLs don't change.

**Signed in, the menu also saves the account's Language setting.** The setting outranks the cookie, so a menu that only set the cookie would do nothing for an account that has a language set. A demo account can't save settings, so for it the menu sets the cookie alone. The menu ends with "Automatic (browser)", which clears the cookie and sets the account's setting back to automatic.

**The header menu is the web's only language control; Settings loses its Language field.** Two controls writing one value would only be two places to look. Settings had one thing the menu lacked, a way back to the browser's language, and the menu's Automatic entry covers it. The Android app keeps Language on its own Settings screen, since it has no web header; it saves through the same account setting.

## Alternatives considered

- **Keep the switcher in Settings only (ADR-0014).** Rejected: it leaves out exactly the visitors who don't have an account yet.
- **`?lang=` or `/ru/…` URLs.** These are still what search engines would need to index both languages (`BRAINSTORM.md`). But they're a routing, canonical and `hreflang` change to every page, and would be the only way to keep the choice from one page to the next. A cookie gives the visitor a lasting choice without any of that, and doesn't stop language-specific URLs being added later.
- **`localStorage`, like the theme (ADR-0019).** The server decides the language before the page is sent, so it has to see the choice in the request. `localStorage` would need a script to re-render or redirect after the page loads, sending the wrong language first.
- **Don't save the account's setting from the menu; tell the user to use Settings.** Rejected: the menu would silently fail for every account with a language set.
- **Keep Language in Settings beside the menu.** Rejected: two controls for one value, on the web, with nothing either does that the other can't once the menu has Automatic.
- **Copy the cookie into `users.locale` at sign-up.** Not needed: an account starts with no setting (NULL, "automatic"), and the cookie keeps applying to it on that browser. Saving it into the account would turn a browser preference into an account setting the person never chose.

## Consequences

- The web client sets two cookies instead of one. `ROADMAP.md`'s cookie-consent item lists both, and the privacy policy needs a line for the language cookie when it's written.
- Signed-out pages are still cached once per language (`RenderPublic`), and are already sent with `Vary: Cookie`, so a cache never serves one language's copy to a browser that picked another. A shared cache now also keeps a copy per distinct cookie value.
- Crawlers send no cookies, so what search engines index is unchanged.
- One more control in the header. It fits at 320px wide with just the language code, no globe icon.
- An Android user who picks a language in the app's own settings is unaffected: the app sends `Accept-Language` and never holds this cookie.