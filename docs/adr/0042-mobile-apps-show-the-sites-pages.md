# ADR-0042: The mobile apps are native where the platform is the point, and show the site's own pages for the rest

## Status

Accepted. Built in the Android app: Settings, Activity graph & trends, Sync history and About are the site's pages in a WebView (`SPEC.md` FR-10.8, `apps/android/docs/SPEC.md` FR-1.5, FR-1.6, FR-4.1). The iOS app isn't started; it is built this way from the start.

## Context

The Android app had copied the web client screen by screen: Stories, Photos, the track editor, the Private location editor, Profile's graph and Trends, Settings and Sync history. About half its Kotlin, roughly 11,000 of 22,000 lines, reproduced something the web already had, and about 180 comments named the web code each piece had to match ("the web's `activityFacets.ts`, rule for rule"). Every feature shipped twice, a fix to one copy had to be carried to the other by hand, and nothing noticed when the two disagreed. An iOS app built the same way would make three copies.

The parts that have to be native are few and known. Health Connect and HealthKit hand out routes only to a native app on the phone (ARCHITECTURE.md §2), and recording GPS in the background, the platform's sign-in pickers and sharing a file into the app are native APIs too. Most of the rest — forms, lists, a graph — the server already renders as pages (ADR-0012), and those pages already work in a phone's browser (`IMPLEMENTATION.md` §5.9).

## Decision

- **Native where the platform is the point**: Health Connect and HealthKit sync, in-app GPS recording, sign-in, sharing files into the app to upload, and the map with the tools that work on it (the Activities panel, the track editor, the Private location editor).
- **The site's own page for the rest.** A screen the server already renders as a page is that page in a WebView (`WKWebView` on iOS), not a native copy. A new feature of that kind is built once, as a page.
- **The server renders a page for an app** when the request carries the `hmt_app` cookie: no site header, footer or Back to map, and none of what the app keeps as screens of its own (`IMPLEMENTATION.md` §4.19). The app signs the WebView in with the session as the site's own session cookie, in the WebView's own cookie store (`apps/android/docs/IMPLEMENTATION.md` §2.5).
- **Links between the two halves**: a link to the map opens the native map; a link within the page stays in the WebView; anything else opens in the browser.
- **The map stays native for now.** Whether the map page itself could be the WebView too is decided by measuring it on a mid-range phone, not assumed (`ROADMAP.md`).

## Alternatives considered

- **Keep building native copies.** What the app was doing. Each feature costs twice, and three times with iOS, with the copies kept in step by hand and comments.
- **The whole app as one WebView, the map included.** Removes nearly all the copying at once. But the map is the screen that matters most, and in a WebView it runs on MapLibre GL JS over WebGL instead of MapLibre Native, which reads the basemap archive directly; on a mid-range phone that risks a slower, heavier map with worse gestures. Not ruled out — it is what the measurement above decides — but not taken blind.
- **Native map with web panels over it.** The track editor drags points on the map, the Private location editor draws circles on it, and selecting a row flies the camera. Splitting those across a native map and a web panel needs a two-way bridge for every drag, selection and camera move: glue code in place of copied code, not less code.
- **A cross-platform framework (React Native, Flutter, Kotlin Multiplatform).** It would share code between Android and iOS, but not with the web, and it needs native modules for exactly the native parts listed above. It means rewriting the Android app for a smaller saving than reusing pages that already exist.
- **Hiding the site's header from the app instead of the server** (CSS the app injects into each page). Works against any server, but the header flashes before it's hidden, the rule for what an in-app page leaves out lives in two places, and a change to the page's markup breaks it silently.

## Consequences

- **One implementation of each page.** A change to Settings, Profile or the Sync page reaches the web and every installed app with a server deploy, with no app release and no copy to carry it to.
- **Installed apps depend on the server.** They rely on the page paths, the `hmt_app` cookie and the session cookie's name staying as they are; changing any of them breaks apps already on phones. And a page change ships into the apps without an app review — so, for one, About's funding section is inside the app, which Play's payments policy makes worth rechecking once donations open (`apps/android/docs/ROADMAP.md`).
- **These screens need the network**, where a native screen could show what it last loaded; without it they show an error with Try again. Nothing on them worked offline before either, since every one read the server.
- **They look and feel like the web**: the browser's own file input and pickers, the page's own typography. Acceptable for secondary screens, not for the map.
- **What a page saves reaches the native app only through the server** — units, the name, a first run's country — so the app reads the session again when it takes over, and forgetting that is a bug (`apps/android/docs/IMPLEMENTATION.md` §2.5).
- **A page has to work for an app as well as a browser**: no hover-only detail, no pointer to something only the site has, nothing that contradicts the app's own screens. The server leaves those out for an app, or the page grows a tap fallback for everyone.
- **A page's form only passes the server's same-origin check when the app talks to the server at `APP_BASE_URL`'s origin**, which is true in production and not against a stock dev stack (`docs/DEVELOPMENT.md`).
- **Some behavior changed with the screens.** Android's Sync history lists finished imports only, as the web's page does, so an upload still processing shows nowhere in the app until it finishes.