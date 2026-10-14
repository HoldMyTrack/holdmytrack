# HoldMyTrack for Android: Implementation

> **Scope note.** This document covers each file's own implementation detail for the Android app — the app shell, auth, map overlays, Health Connect sync, in-app GPS recording, and the web's own pages shown in the app. See `apps/android/docs/ARCHITECTURE.md` first for the app's shape and the stack this runs on, and `docs/IMPLEMENTATION.md` for the server-side schema and pipeline every call here eventually reaches. The app's local persistence is two `SharedPreferences` files, covered in place below, plus one SQLite database (§7) for recordings pending sync.

---

## 1. App Shell & Lifecycle

### 1.1 `HoldMyTrackApplication`

Process-level setup, in a fixed order that matters: `Session.init` before anything reads a stored token; `MapLibre.getInstance` to load the native library a `MapView`'s constructor needs; then `HttpRequestUtil.setOkHttpClient(HoldMyTrackApi.client)`, replacing MapLibre Native's own HTTP stack with the app's shared client. **This last call has to happen in `Application.onCreate`, not an Activity's** — the map SDK may issue a request as soon as a `MapView` exists, and installing the client any later means some requests already went out through the SDK's default, tokenless client.

### 1.2 `MainActivity` and `MapFragment`

`MainActivity` is the main window, with the bottom bar; `MapFragment` is the map in it, with the state that governs what's drawn (`apps/android/docs/ARCHITECTURE.md` §1.1).

- **Never mounted without a session, or for an unconfirmed email.** `MainActivity.onCreate` checks the session before `setContentView` and hands a signed-out launch to `SignInActivity` (an unconfirmed one to `VerifyEmailActivity`, §2.4) — no layout, no `MapView`, no style request. It adds `MapFragment` only on a first create, since a recreation (rotation, theme, language) restores the fragment with its saved state.
- **Tabs are shown and hidden, never replaced** (`showTab`, with `commitNow`), so each tab keeps its state, the map's camera and layers included. Stories is the map with its panel on the Stories tab, and the panel reports its tab back, so the bar follows a tab chosen in the panel itself.
- **Insets are handled once, on the tabs' container**: it pads itself by the side navigation-bar and display-cutout insets and zeroes them for the tabs, so no tab's chrome sits under a landscape phone's three-button bar or camera, and none has to pad itself.
- **The record button's frame reaches up over the bar by as much as the button is raised**, since a view never gets a touch outside its parent: without it the raised part took no touches.
- **`syncSession()` is the single place session state and map state are reconciled**, called from `onResume` and once after the style loads. It routes to sign-in if the token was just rejected, verifies a restored token once, and only then attaches the user layers. The basemap style loads before verification finishes — it is unauthenticated (root `docs/ARCHITECTURE.md` §2.1) — but the user layers wait for a verified token. `verifying` is a re-entrancy guard, not a cache: `onResume` fires on every return, including from the sign-in screen, so a slow verify could otherwise start twice.
- **Only a `401` from verifying clears the session.** Any other failure is shown as a notice with Try again, and the same token is tried on the next resume. This is the one place the app discriminates by HTTP status rather than treating "the request didn't succeed" as one bucket.
- **The map says what's wrong in one notice, by importance** (`Notice`): a less important kind never replaces a more important one that is up, and a kind can only take itself down. "Checking" is posted after a delay and removed when the check answers, so a fast check never flashes.
- **`frameActivities()` runs once per process, and the flag goes into the saved state**, so after a recreation the camera MapLibre restored stands. The fit is capped at zoom 15, because a very short or heavily clipped activity has a near-zero box, and fitting it literally would zoom past the basemap's data onto an empty tile. It keeps a minimum span of map between its top and bottom padding: on a landscape phone the top row and the sheet left none, and the fit fell back to the whole world. A group is framed along the shortest arc of longitude (`map/BoxUnion`, the web's `unionBBox`, root `docs/IMPLEMENTATION.md` §4.7).
- **`insetSystemBars()` exists because of a bug found only on a real screen**: at `targetSdk` 35+ the system draws every app edge-to-edge and ignores the old opt-out, so a bar laid out with ordinary padding sat *underneath* the gesture pill. The fix adds the inset to each view's own padding and returns the insets unconsumed, so nothing downstream misses them.
- **MapLibre's `MapView` owns a native renderer and a GL surface**, so every lifecycle call forwards to it. Missing any one is a leaked surface or a crash on rotation, not a subtle bug.
- **The top row is not clickable**, so a touch in its gaps still reaches the map. The Edit window and the Private location editor open over where the Layers/locate rail would be, so the rail hides while either is open. MapLibre's compass is moved below the row and the notice whenever their layout changes.
- **The Layers menu** (`map/LayersMenu`, the web's `OverlaysMenu.tsx`) is a focusable popup inflated afresh each time. A focusable popup closes itself on an outside touch, including one on its own button, so a click landing just after that close doesn't reopen it. Its master switch is set from the saved choice, so it's handled with a click listener rather than a checked-change one. Every place the saved picks are applied goes through `shownPaths`/`shownSpots`, which give nothing while the switch is off. A checkable `MaterialButton` flips itself on click, so the button's state is re-rendered after each tap.
- **Paths** (`map/MapPaths`, `map/MapBikePaths`; root `docs/IMPLEMENTATION.md` §4.24) are laid out as on the web: coverage, then the trail and track layers lifted out of the basemap's roads — MapLibre Native has no `moveLayer`, so each is removed and added back below the labels — then the bike paths, then the tracks. A coverage refresh puts its layers back under whatever sat right over them, so the paths stay above the veil. **When the tile version moved while the app was away, the bike-path and spot sources are replaced at the new version**; without it, the app kept drawing tiles cached before an import. The legend's colors and dashes must match the web's `style.ts`.
- **Satellite** (`map/MapSatellite`, root `docs/IMPLEMENTATION.md` §4.26) is offered only when the served style has a `satellite` source, and applies the style metadata's hide and dim lists rather than a second copy of them. Night mode or satellite is a dark base, which picks Fog's veil; the coverage layers are re-added only when the veil actually changed.
- **Find my location is MapLibre's own `LocationComponent`, activated lazily** on the first tap, tracking until the user pans like the web's geolocate control. A recording's first fix sets the camera mode back to none, so the recording's own follow (§7) is the only thing moving the camera then. `ACCESS_COARSE_LOCATION` is merged in from MapLibre's manifest.
- **The date range** (Android SPEC FR-2.6) ports the web's logic as pure classes, tested against the web's cases: `ActivityDays` (`useActivityDays.ts`), `DateRangeSlider` (`DateRangeSlider.tsx`), `RangeShift` (`rangeShift.ts`, `RangeShiftTest`), and `ScrubberBars` (bar heights and labels, `ScrubberBarsTest`). `DayScrubberView` only draws and sorts each touch into a handle drag, a swipe or a tap, asking its parent not to intercept so the sheet doesn't drag with it. The default range uses the device's day, as the web uses the browser's. Days reload on every resume, keeping the window's anchor, so a range picked further back is still in view after Profile or Sync. **The logo's and attribution's base margins go into the saved state**: MapLibre restores its UI settings already lifted, and capturing them again lifted the logo twice its height above the sheet.
- **Style flavor follows the app's night mode**, so a dark map always sits under dark chrome, and the style URL carries the app's language for the labels (root `docs/IMPLEMENTATION.md` §4.21). A language change recreates the activity, so the map loads the new URL.

### 1.3 Theme (`res/values/themes.xml`)

**The palette, scales, fonts and icons are the web's** (root `docs/IMPLEMENTATION.md` §4.17, §4.18): `res/values/colors.xml` and `res/values-night/colors.xml` are the web's light and dark tokens value for value, under the same names, and `res/values/dimens.xml` its spacing, radius and type scales with CSS px read as dp — a phone browser draws one CSS px as one dp, so the mobile web and the app come out the same size. **A change to the web's tokens needs the matching change here.** The base theme maps the palette onto Material's color roles once, so the mapping holds for both palettes. Dynamic color is not applied, so the user's wallpaper never replaces the palette.

- **Which palette applies is AppCompat's night mode**: the You tab's Theme choice is applied in `HoldMyTrackApplication.onCreate` and again on a change, which recreates the open screens, and the map's flavor reads the same configuration (§1.2).
- **Shape is the web's radius scale, not Material's** pill buttons and 28dp dialogs.
- **Fonts are the web's Inter and Fraunces as variable fonts**, cut down with fontTools to the scripts and weights the app uses (about a quarter of the original size); `res/font-ru/` swaps in Source Serif 4 for the serif, the web's own Russian choice, because Fraunces has no Cyrillic. The licences ship in `assets/licenses/`. Code bolds with `Typeface.create(typeface, 700, false)`, because `setTypeface(null, BOLD)` would drop to the system font.
- **The default face reaches text through a TextView style, not the theme's `fontFamily`.** Setting `android:fontFamily` on the theme reaches every `TextView` as if set on it directly and so outranks its `textAppearance`: titles came out in Inter, and every role's weight flattened to regular.
- **Icons are Lucide at the web's version**, converted to vector drawables, every stroke white so the color comes from where the icon is used — the Android counterpart of the web's `currentColor`. Three glyphs are outside the set: the idle record button's solid red dot, since a filled signal color is what says "record", and two activity types Lucide has no icon for, drawn on its grid and stroke.
- **Map chrome panels carry the color and shadow, not the buttons in them**: a translucent `MaterialButton` with elevation draws its shadow through its own fill, which showed as a lighter square around the icon. Their 48dp minimum height is set on the style, since while recording the row holds nothing else and a `match_parent` button collapsed to nothing.
- **The mode buttons name themselves radio buttons to TalkBack**, since `MaterialButton` calls itself a toggle outside a `MaterialButtonToggleGroup` and sets that after any delegate. Selection is set by `MapFragment.setMode` on all three, and the mode is kept in the saved state: the buttons restore their own checked state after a recreation, so without it the toggle showed Heatmap over a map drawn in Normal.
- **Dialogs use `MaterialAlertDialogBuilder`**, not the platform `AlertDialog`, which would ignore the app theme. A dialog whose confirm destroys something opts into the destructive overlay, so no other dialog's confirm turns red.
- **Accessibility traps found by auditing every screen's view tree** at font scales 1.0 and 2.0 and in landscape (TalkBack itself was walked on a physical device, since it couldn't be driven on the emulator):
  - `MaterialButton` ignores `android:minWidth` from a style, so the mode buttons' minimum width is set in code.
  - `isSingleLine` set after `minHeight` undoes it.
  - MapLibre's attribution button is too small and has no id; it is found by its content description and padded only toward the empty map, so the icon stays put. Padding it evenly and moving it back through the attribution margins broke after a recreation, which restores those margins.
  - At large font scales the mode toggle keeps only the active mode's label, and rows that can't fit stack into a column (`ui/LargeText`).
  - A target drawn smaller than 48dp keeps a 48dp touch area — the sheet's handle reaches under the head's first row with a negative margin, the filter chips use an inset ripple.

## 2. Authentication & Session

### 2.1 `net/Session`

**An `object` (process-wide singleton) rather than an instance**, because two independent HTTP call sites need the same token without being wired to each other: the app's own calls, and MapLibre Native's tile fetching, which goes through the shared `OkHttpClient` but never through `HoldMyTrackApi`.

**The token lives in two places at once, deliberately**: a `@Volatile` field for fast, thread-safe reads (the bearer interceptor runs on whichever thread OkHttp dispatches a tile request from, where reading `SharedPreferences` each time would be slower and not obviously safe), and `SharedPreferences` for persistence across process death. `init` must run before either is read, which `HoldMyTrackApplication.onCreate` guarantees.

**`verified` is separate from "a token is present"**: a token restored from disk after a cold start hasn't been checked against the server yet, while one just minted by a sign-in needs no check.

**`emailVerified` is on disk rather than per-process**, so a cold start routes straight to the right screen without drawing a map first. A missing value reads as `true` — a session stored by a build that didn't record it — and the cold start's session check corrects it before any user layer attaches.

### 2.2 `net/HoldMyTrackApi`

The app's whole HTTP surface in one object: one shared `OkHttpClient`, its interceptors, and the calls.

- **`client` is shared with MapLibre Native** (§1.1), with `maxRequestsPerHost` raised to what MapLibre's own default client uses — OkHttp's plain default would throttle the map's tile bursts.
- **`BearerInterceptor` runs per request, not once at startup**, and attaches the token only to the API's own origin. Per request is what makes sign-in, sign-out and token expiry take effect on the very next request, the next map tile included. The origin check exists because the basemap's assets are unauthenticated and live on a domain HoldMyTrack doesn't own; sending a live token there would only put a working credential in a third party's access logs.
- **`OriginInterceptor` sends the API's origin on every request that leaves it**, as a browser does on the web client's cross-origin fetches. The satellite imagery's key only accepts requests from the deployment's origin, and the basemap's edge rule refuses a request with no `Origin` (root `docs/IMPLEMENTATION.md` §5.4), so without it the map would draw nothing at all. It gives nothing away: the key is already public in the web bundle and the style.
- **Everything except `syncActivities` is callback-based**, posted back to the main thread — a deliberate rejection of coroutines-everywhere for this file: a handful of call sites don't amortize a coroutines layer over OkHttp's own async API.
- **`syncActivities` is the one `suspend` function, wrapping a blocking call on `Dispatchers.IO`**: its caller must not send its next batch until this one is answered, and Health Connect's own API already puts that caller on coroutines.
- **The Type picker's counts come from the same unpaginated activity list** the web's Type facet is built from; there is no dedicated facets endpoint.
- **`ApiException` carries the server's own response text as its message, unreworded**: the server's plain-text errors are written for people, and rewriting them risks saying something the server didn't mean. Refusals a client branches on come as `{error, message}`; those show their `message` and keep the code.
- **`image()` fetches any API image with the bearer token** and samples it down off the main thread. A browser tab or an image library would have no token to send, which is also why Full size opens in the app (§3.4).

### 2.3 `SignInActivity`

**The root of its own task** (`NEW_TASK | CLEAR_TASK`), from a signed-out cold start, a rejected token or Sign out, so no screen that needs a session is ever left underneath it and Back leaves the app. A successful sign-in replaces the task the same way with the map, or with `VerifyEmailActivity` (§2.4) for an unconfirmed email.

**The web's two pages as two modes of one screen**, rather than two activities, because the Facebook round trip returns here with the verifier this screen keeps, whichever mode started it. The password's autofill hint follows the mode, so a password manager suggests a new password only on sign-up. The mode survives recreation, including a process death behind the Facebook tab. **"Forgot password?"** opens the web's own `/forgot` in a Custom Tab rather than a native form, since the emailed reset link opens the web's `/reset` anyway.

**Client-side validation is only "both fields are non-empty"**, deliberately, so the server remains the single source of truth for what an acceptable email or password looks like, and its error text is what the user sees.

**Google and Facebook** (the server side is root `docs/IMPLEMENTATION.md` §4.15 and §4.22) show only once the server reports each configured; if that request fails they stay hidden, since a button that can only fail is worse than none.
- **Google** asks Credential Manager for an ID token for the server's *web* client id, so the token's audience is the one the server checks. Any failure other than closing the picker is logged: in development it's almost always a missing Android OAuth client for the debug key's SHA-1, which the picker reports only as a generic error. `googleid` is pinned at 1.2.0: 1.2.1 is compiled with Kotlin 2.4 metadata, which this project's compiler can't read.
- **Facebook** doesn't use Meta's SDK (root `docs/IMPLEMENTATION.md` §4.22). The verifier is saved to `SharedPreferences` before the Custom Tab opens, since the process can die while the tab is in front. **`OAuthRedirectActivity`** receives the `holdmytrack://oauth` redirect: an exported, no-display activity whose only job is forwarding the URI to `SignInActivity` with `CLEAR_TOP | SINGLE_TOP`, which closes the tab above it and keeps `SignInActivity` itself unexported. The buttons re-enable on resume when the tab was simply closed.

### 2.4 `VerifyEmailActivity`

The "check your email" screen (`apps/android/docs/SPEC.md` FR-1.4). Without it, an unconfirmed account opened the map with nothing on it: the server answers every tile and sync request `403`, which MapLibre reports only in logcat.

- **Root of its own task**, like `SignInActivity`, so Back leaves the app instead of revealing the empty map.
- **The link is clicked in a browser the app never hears from**, so the screen asks the server on every `onResume` — returning from the mail app is when the answer is most likely to have changed — and on Continue. Only Continue reports "not yet" or a failure; the automatic check is silent.
- **No change-email form**, unlike the web's: the recordings, the type list and the hidden candidates are keyed by the session's email on the phone, so changing it from here would need those migrated too.
- **`SyncActivity` checks the email too**, since Health Connect's rationale intent can open it without passing through the map.

### 2.5 `web/WebPageActivity` — the web's own pages in the app

Settings, Activity graph & trends, Sync history and About are the web's own pages in a `WebView`, not native copies (ADR-0042), so a change to one reaches both clients with a server deploy and there is no second implementation to keep in step. How the server renders a page for the app is root `docs/IMPLEMENTATION.md` §4.19.

- **Signed in by cookie, not by the bearer token.** A WebView's requests never pass through the app's `OkHttpClient` (`apps/android/docs/ARCHITECTURE.md` §1.1), so the session is written into the WebView's own cookie store before every load, beside the in-app marker and the language. A request header would reach only the first request; a cookie rides on every link, form POST and redirect after it. The store is the app's own, and signing out empties it.
- **The language cookie decides only for Automatic**, where the app can run in a per-app language other than the phone's, which is all the WebView's `Accept-Language` would say.
- **The app's Theme, with no code for it.** A WebView in an app targeting API 33+ answers `prefers-color-scheme` from the activity theme, which follows the app's night mode, so a page follows the app's Theme rather than the phone's.
- **Where a navigation goes is `route`**, a decision over the URL: the page's own path stays in the WebView, `/` opens the native map, the sign-in pages just finish and leave the map's own session check to act, and anything else opens in a Custom Tab. Trap: `shouldOverrideUrlLoading` is never offered a POST and can't be relied on for the redirect after one, so `route` runs again in `onPageStarted`, which stops the load; that's how Settings' first-run save reaches the map.
- **The avatar**: the page's file input opens the photo picker, and the picked image is shrunk to a small JPEG before the page's form uploads it — a phone photo is often past the server's limit, or HEIC, which it refuses. The file input's callback must be answered exactly once: with the file, or with null for a cancel or an unreadable image.
- **Read again on return**, except Settings, so a half-filled form survives the photo picker.
- **What a Settings save changes in the app arrives with the next session check**, since the page saves through the server, not the app.
- **First run**: a confirmed account with no country is sent to Settings as the root of a new task. Its save redirects to `/`, and `route` reads the session before opening the map: the map gates on the country, which so far only the server has heard about, and without the read it sent the account straight back. A stored country is only trusted once a session check has refreshed it, so an account that saved Settings on the web isn't sent there by a stale value.
- **Against a dev stack, a page's form POST is refused** unless the app's API base is the stack's `APP_BASE_URL` origin (root `docs/IMPLEMENTATION.md` §4.19, `docs/DEVELOPMENT.md`). Production's are both `https://holdmytrack.com`.

### 2.7 `YouFragment`

The You tab (`apps/android/docs/SPEC.md` FR-2.9).

- **Read on every showing**, and an answer after the view is gone is dropped. The totals come from the same per-day histogram the web's Profile page reads, summed by `ProfileStats`, a port of the page's own sums.
- **The country is named by `Locale.getDisplayCountry`** in the app's language rather than from the server's list, which would cost a request for one label.
- **The Language save resends the profile just read**, since the settings endpoint replaces Name, Country and Timezone along with Language.
- **Units** — `recording/RecordingFormat` is the one place distances, heights and speeds become text, with the web's imperial countries (`services/server/internal/web/format.go`) read from the session's country.
- **`settings/AppLanguage`** sets the per-app language only when it differs, since setting it recreates every open screen. Automatic can't be told apart from "never chosen", so a sign-in or session check only applies an explicit language, and only the You tab's Language save clears one.
- **Delete account** (`settings/AccountDeletion`) replaces the dialog's positive-button listener, so a mismatched email lands on the field as its error without closing the dialog. After the server closes the account it **clears what the phone keys by the email** — recordings, hidden candidates, sync sources — before clearing the session, so a new account made with the same address starts clean.

### 2.8 `privacy/PrivacyActivity`

The Privacy screen (`apps/android/docs/SPEC.md` FR-1.7).

- **A row and Add a location open the map's editor and finish this screen**, so Back from the editor doesn't land on a list it may have changed.
- **Download your data** (`privacy/DataExportActivity`) polls while an archive is being prepared, only while the screen is resumed. Each part is handed to Android's `DownloadManager` with the session's `Authorization` header: the download outlives the screen, resumes on its own, and needs no storage permission at minSdk 34.

## 3. Map Rendering

### 3.1 `map/MapOverlays`

Owns the history layers — tracks, Fog and Heatmap — and the logic to add, remove and toggle them; the web's counterpart is root `docs/IMPLEMENTATION.md` §4.2–§4.3.

- **`attach` is idempotent and re-runnable**, checking each source and layer before adding it: a style *reload* (the app's night mode changing) discards every custom layer, so this is not a one-shot setup path.
- **The insertion point is computed, not hardcoded**: every custom layer goes below the basemap's first symbol layer, so labels stay on top, with the rasters under the tracks — the web's ordering, reproduced layer by layer, since there is no shared layer-ordering module between the clients.
- **`setMode` only changes properties**, never adds or removes layers, so switching is instant and a style reload re-applies it. Fog dims the basemap's labels to the web's value and other modes reset them to the default the served style never overrides. **Heatmap's basemap wash is switched by its opacity, not its visibility**: MapLibre Native doesn't draw a background layer again once it has been hidden, so the second switch to Heatmap had no wash.
- **Only the tracks URL carries a filter, and only the date range** (or the open Story). Type, Distance and the hidden set are applied on the device as layer filters, as on the web. **Changing the range replaces the tracks layer and source**, because MapLibre Native's `VectorSource` has no way to change its tile URL; the old layer's visibility is carried over, so a mode or a recording in progress is respected.
- **The selected track is two more layers, not feature-state.** The web bolds it with feature-state, which MapLibre Native Android doesn't have, so a casing and a wider line on the same source are filtered to the selected (or checked) ids. The filters are kept on the object, since replacing the source re-adds every layer.
- **Refreshing tiles means a new URL** (root `docs/IMPLEMENTATION.md` §4.2.6). The tile version is kept on disk, so a cold start attaches at the last one read and MapLibre's cache draws at once, then the status is read and everything refreshed only if it moved — on attach and on every resume. Tracks also carry the app's own `v`, since a change the app hasn't read a new version for yet (a type edit) would otherwise be answered from cache. `map/CoverageWatch` is the web's `useCoverageRefresh.ts`, the same rules, and a resume that finds the account still rendering starts one, so an import made on the web is followed to its end.
- **The zoom tiers' constants must match the web's `zoomTiers.ts`** (`ZoomTier`, unit-tested against the boundaries), and so must the level notice, which names the tier on camera idle, so a pinch through two levels names only where it lands.
- **The track color is the shared map style's, not this app's theme**, so it is not folded into the palette tokens.

### 3.2 The Activities panel (`panel/`)

The web's phone Activities panel (`apps/web/src/ui/ActivitiesPanel.tsx`; `apps/android/docs/SPEC.md` FR-2.7), ported rule for rule.

- **Three pieces, split where the web splits them.** `PanelState` is the state the web keeps in `MapView.tsx` — the list, the checked group, the selected id, the hidden set, the filters — with its rules as methods; `ActivityFacets` is `activityFacets.ts`. Both are plain Kotlin, covered by `PanelStateTest`. `ActivitiesPanel` binds the views and redraws everything from the state in one `render()`. `MapFragment` owns the map and the fetch, as `MapView.tsx` does.
- **The rows are a diffing `ListAdapter`, so a render rebinds only rows that changed**: rebinding every row cancelled a tap that landed mid-rebind, when a list reload came in between two ticks. The list's note is a second adapter after the rows, as the web's is a last `<li>`, not drawn over the list, where a failed poll would write it across the first row.
- **The sheet is a `BottomSheetBehavior`**, peeking at its head's bottom edge, recomputed after layout whenever that edge moves, at any font scale. The expanded offset is computed from the head's *natural* height, not the one a too-short sheet squeezed it to, which on a landscape phone cut the scrubber's labels off. The content is only ever covered, never removed, so the list keeps its scroll position. While an edit window is over the map the sheet is held collapsed without forgetting its state.
- **A track tap queries a box around the finger**, the web's touch tolerance, and scrolls the row into view on the list alone, never the sheet — the same reason the web scrolls the list rather than calling `scrollIntoView`.
- **Selecting is just "the checked group is non-empty"**, so nothing can leave the list selecting with nothing checked. The selection bar and the selected activity's card are bound to one action set described from the same target, so the two never disagree. Back ends selecting before it collapses the sheet.
- **The master checkbox's state is set from the rows on every render**, and its click only decides between "uncheck all" and "check all", so the widget's own toggle never disagrees with the state.
- **The Distance slider holds `Float`s, which can't carry every distance exactly**, so a knob within a meter of either end is snapped onto that end's activity; otherwise dragging only the low knob dropped the range's longest activity.
- **Times are shown in the zone the activity was recorded in** (root `docs/IMPLEMENTATION.md` §4.30), skipping a zone the phone's tz data doesn't know. One Story reads "Story" from a plain string rather than the plural's `one`, which in Russian also covers 21, 31 and so on.
- **The selected activity's pace bands** are read whenever the selection moves to another activity and again when its reprocess lands, an answer for an activity no longer selected dropped; `map/TrackBands` is the web's `trackBands.ts`, unit-tested.
- **The Track tab** (`panel/TrackEditor`) ports the web's editor; its rules are `panel/EditTrackOps`, a port of `editTrackOps.ts`, unit-tested. The knobs are the times of the points they sit on, so a deleted point never moves them. Cut's and Split's previews show while the button is held, where the web shows them on hover. A refused split that would hide a part (`split_hides_part`) asks before resending with `allow_hidden`.
- **The editor's point drags and the Private location handle are taken in a touch listener on the `MapView` itself**, which runs before MapLibre's gestures: a press on a point or handle takes the whole gesture, so the map doesn't pan (the web's `dragPan.disable()`); every other touch still reaches the map. A press let go unmoved, a cancel or a second finger commits nothing.
- **Private locations** (`panel/PrivateLocationEditor`, the web's `PrivateLocationsPanel.tsx`) are edited on the map from the Privacy screen. A request waits until both the list and the style are in. The circle is scrolled into the map left above the editor rather than centred, since at phone width centring put the handle under the card. **A change reprocesses whole activities**, so the coverage watch takes a callback and the tracks, list, days and metrics are re-read once the server has drained — a batch done before the first read never shows a row Pending at all.
- **Edit is a card, not a dialog**, so its Track tab can edit on the map, as the web's does; while it's open the sheet, the bottom bar and the rail step aside. Save checks the server's length limits before any request. A group resends each activity's own name and description (a full replace), and a group of mixed types writes no type until one is picked — the web's rule, for the same retyping bug. Saves that already went are skipped on a retry.
- **Delete runs one request at a time** (parallel deletes race each other's coverage re-render) inside a dialog that stays up and resumes from where a failure stopped.
- **Pending** is the web's two Pending effects: a row entering or leaving Pending refetches the tracks and starts the coverage watch, and while any is Pending the list is polled. A failed read reschedules the poll, so a dropped poll doesn't strand a Pending badge. An edit saved from the Track tab also counts as finished the first time a list shows it not Pending, since the job is often done before the reload after the save answers.
- **The Stories tab** (`panel/StoriesTab`) is the web's `StoriesTab.tsx`. A delete drops the Story from the list in hand rather than refetching, for the web's reason: a refetch would bring it back to the open-the-newest rule. The open Story belongs to `MapFragment`, as it's `MapView.tsx`'s on the web, and leaves the range alone; every re-read asks for the open Story, else the range, and an answer for another is dropped. The open Story isn't saved across a recreation.
- **Copies of Stories** (root `docs/IMPLEMENTATION.md` §4.23) are polled only while one is arriving, and an arrival gets the same refresh as a delete.
- **Add to story and New story** keep their dialog up through the request and keep a refusal under the fields, taking over the dialog's confirm button as Delete does.

### 3.3 Spots (`map/MapSpots`, `map/SpotPopup`, `map/ShowInArea`)

The web's `spots.ts`, `SpotPopup.tsx` and `ShowInArea.tsx` (root `docs/IMPLEMENTATION.md` §4.25), on the same tiles and endpoints.

- **`MapSpots` is the layers and the choice**, re-added on every style load. Its layers go on top of everything; `add` puts a layer back under the first of those above it already on the map, which `refresh` relies on when the tile version moves, since a vector source's URL can't change. They're hidden while a track is edited, and left out of Fog's label dimming.
- **The badges are bitmaps drawn once per style load**, since a style reload drops its images. The captured and retired rules are style expressions over the account's capture list, as on the web: the captures are read in full when the overlays attach and only `since` the newest on every resume (`map/CapturesSince.kt`).
- **`spotAt` is asked first on every map tap**, in every mode, unless an editor has the map, so a spot's tap never selects or clears a track underneath.
- **`SpotPopup` is a view, not a MapLibre annotation**, placed above the spot's screen point on every camera move and whenever its height changes, flipping under it when it would pass the top chrome. Its text rules are `map/SpotText`, unit-tested.
- **`ShowInArea` keeps the web component's state.** Its requests can't be cancelled, so a counter drops any answer but the latest; the loaded places are kept for the next style load.
- **Capture mode** (`map/CaptureMode`, FR-2.8 item 6) asks for precise location, since the capture needs a fix within 25 m, and listens on the GPS provider and the rotation sensor only while the map is resumed. **Its rules are plain Kotlin** — `SpotCapture` (the states, dropping inaccurate fixes, and counting at most a few seconds across a gap, so time away counts for little) and `SpotGeometry` (inside-test, nearest point and bearing on a local plane, well under a metre off across a place's few hundred metres) — unit-tested on the JVM. A place retired since the map loaded it ends the mode with its own message. Stopping undoes everything, leaving MapLibre's location component on only if Find my location had it on.

### 3.4 Photos (`photos/`, `map/PhotoMarkers`, `map/PhotoPopup`, `panel/PhotosTab`)

The web's photos (root `docs/IMPLEMENTATION.md` §4.27, ADR-0024), against the same endpoints.

- **Five pure pieces in `photos/`, ported line for line**, with no Android type, so `app/src/test` covers them: the EXIF time rules (`PhotoExifFields`), the slider's walk (`PhotoTrack`), the draft (`PhotoDraft`), the grouping (`PhotoClusters`) and the viewer's zoom math (`PhotoZoom`).
- **`PhotoPrep` decodes a picked photo upright and already scaled** with `ImageDecoder`, so a large photo is never held at full size, and re-encodes it, which drops every EXIF field. `ExifInterface` reads JPEG, HEIC and WebP alike, where the web reads JPEG only. Thumbnails are cached with requests for one path coalesced, since markers are rebuilt on every regrouping.
- **`MapFragment` owns the photos**, as the web's `MapView` does, with the web's scope rule, re-reading when the photos may have changed under it; a generation counter drops a superseded answer.
- **Markers are views, not a symbol layer**, as the web's are HTML markers, in a pass-through layer over the map. A marker's tap is the view's, so the map's own tap handling never sees it and the selection stays.
- **The full-size viewer is in the app**, since a browser tab would have no session. Its fit is worked from the photo's own size, not the bitmap's, so the swap from the smaller copy to the stored one keeps the zoom.
- **The Photos tab's rows are kept for as long as their photo is listed and moved rather than re-inflated**, so an open row's slider and caption survive the renders a drag causes. The slider isn't set from the draft while a finger has it, since the place rounds to a second and would pull the knob back a step. Add photos is registered in `MapFragment`, since only an activity or fragment can register for a result.

## 4. Health Connect Integration

### 4.1 `health/HealthConnect`

The three permission constants, and the readiness state machine (Android SPEC specifies its states).

- **`READ_EXERCISE_ROUTES` is a raw string constant**: the library ships a write-side constant but no read-side one, consistent with a permission the platform won't let an app request programmatically (requesting it silently omits it from what's granted).
- **Readiness is computed fresh on every call, never cached**: the routes permission can only be granted by leaving the app for Health Connect's settings and coming back, so a cached value would be stale at the exact moment the user needs to see it update.
- **The settings intent targets Health Connect's home screen, not its per-app permission page.** The obvious API for the latter is guarded by a signature-level permission, so an ordinary app calling it gets a `SecurityException` that kills the process — found on a device — which is why the onboarding spells out the path by hand.

### 4.2 `health/ExerciseTypes`

A hand-written map from Health Connect's exercise types to HoldMyTrack's `activity_type` vocabulary.

- **Written against public constants, not the library's own int-to-string map**, which is `@RestrictTo` (removable without a semver bump). Every key is a named constant, so a transcription mistake is a compile error, not a mislabeled type.
- **A few entries rename Health Connect's term to the FIT vocabulary** the other ingest paths already speak (biking → cycling, treadmill → running, both swims → swimming), or one real type would split in two in the Type filter.
- **An unmapped type falls back to `"unknown"`**, the value the server already uses for an activity with no type.

### 4.3 AndroidManifest permission declarations

**Exactly three Health Connect permissions, and no more**, since declaring an unused data type is a documented Play Store rejection cause (`docs/VISION.md` §7). `READ_HEALTH_DATA_IN_BACKGROUND` is deliberately absent: measured on a device, granting it changes nothing for route reads (root `docs/IMPLEMENTATION.md` §4.0), so it would only ask for an alarming-sounding permission that buys nothing.

**`SyncActivity` handles Health Connect's rationale intent**, and without a registered handler the permission dialog can refuse to appear at all — found on a device. An `activity-alias` is where Health Connect's own "see how this app used your data" link lands. Arriving by either opens the rationale dialog, only on a fresh start, so a dismissed dialog stays dismissed after a rotation; it leads to `/privacy`, which Health Connect requires.

## 5. Sync Engine

### 5.1 `panel/SyncTab` — the Sync sheet

The Sync tab (`apps/android/docs/SPEC.md` FR-3.2–FR-3.5) is the map's own sheet on a third tab, as Stories is (§3.2). `MapFragment` builds it and hands it to the panel, since it reads the phone and draws on the map.

- **Foreground-only is enforced by cancelling** (root `docs/IMPLEMENTATION.md` §4.0): leaving the screen, or the You tab covering the map, cancels a Health Connect read and a run in flight. Cancelling is safe by construction: a read replaces what's kept only once it has finished, in one transaction (§5.7), so there's no half-written state to tear down.
- **Opening the tab reads nothing from Health Connect.** It lists what the last Load kept and the phone's recordings, and asks the server about overlaps. Health Connect is read only by Load, and once by itself after it's switched on (§5.7).
- **Similar tracks has two halves.** The account's comes from the server (`POST /v1/activities/overlaps`, root `docs/IMPLEMENTATION.md` §4.6); the phone's compares the rows still to be sent with each other by the server's rule ported to Kotlin (`sync/Similar`). **`SimilarTest` runs the server's own cases both ways round, so the two can't drift apart.** A failed request leaves out only the account's half: it's advice, and the list doesn't depend on it.
- **What's on its way is `sync/SyncProgress`**, process-wide like `imports/FileImports` (§5.4): the candidates sent whose ingest hasn't finished. While it holds any and has a listener, it asks `POST /v1/sync/known` about them (root `docs/IMPLEMENTATION.md` §4.0.3): one still processing stays; one known and not processing has landed; one in neither failed, and a failed recording is put back on the phone, since the phone held its only copy. A dropped read decides nothing. **A landing refetches the tracks tile at once**, since the tile version only moves once the coverage render after the ingest has finished, and waiting on it left a landed track undrawn until then. A processing row isn't drawn, framed or sent to the overlap check, since it would only overlap itself once it lands.
- **The demo account** reads no Health Connect, and its rows can't be ticked.
- **`SyncActivity` is Settings › Sync.** **Health Connect's switch is the app's own** (`sync/SyncSources`), off until the user turns it on, whatever Health Connect already granted (ADR-0039); turning it off clears what the last Load kept. The first load after it's switched on runs on this screen once Health Connect is readable; if the screen is left first, the flag stays set and the Sync tab runs it instead. Opening Health Connect's settings catches every exception, not only `ActivityNotFoundException`, because the failure actually met was a `SecurityException` (§4.1): a button that can't open a screen says so rather than crashing the app around it.

### 5.2 `sync/SyncCandidates` and `sync/SyncRunner`

What's waiting, and sending what's ticked.

- **`SyncCandidates.readHealthConnect`** reads the last 90 days, keeping each session with a route and counting a route that's there but wasn't handed over as refused. Without the history permission, Health Connect itself cuts the window to 30 days.
- **`build` and `listed` are pure and the whole of the list's rule** (`SyncCandidatesTest`). A session's distance is computed from its route, since no distance is read from Health Connect.
- **"Known" is the server's, asked on every Load; there is no cursor.** Each Load works the list out again from Health Connect and the server, which is what lets a reinstall or a second phone agree with the server and an activity deleted on the web come back. Recordings aren't asked about, since one that synced is deleted from the phone.
- **`SyncRunner.send`** sends sessions in batches smaller than the server allows, so one request stays modest on mobile data, then each recording in a request of its own, deleting it from the phone once the server has it. A session restored from the last Load carries no route, so it is read again by id when it's sent, and one that's gone or no longer handed over is reported as refused. A route over the server's point ceiling is refused rather than sent. A failed request stops the run, leaving the rest ticked.
- **Each activity's JSON is built off the main thread from the route as read, never the simplified line**, with elevation only when the location carries it — zero is a real, wrong value for "unknown altitude".

### 5.3 `sync/HiddenCandidates`

The rows swiped aside, as candidate keys per account, so two people on one phone each set aside their own. **A session's key is pruned only after a Load that refused nothing**, so a route that merely couldn't be read this time keeps its place. Deleting the account clears it with the recordings.

### 5.4 `UploadActivity` and `imports/FileImports` — uploading files

The Upload screen (`apps/android/docs/SPEC.md` FR-3.6) is the web's Upload menu (`upload.js`) ported, against the same endpoint (root `docs/IMPLEMENTATION.md` §4.0.1).

- **A process-wide object, not the screen's**, with its own scope, so an upload outlives a rotation or a trip to another app — unlike a Health Connect read or send, which leaving cancels on purpose (§5.1).
- **The picker asks for `*/*`.** Providers label these files inconsistently — the emulator's Downloads called a `.gpx` `application/octet-stream` — so filtering by MIME type would hide the very files asked for, and the queue decides by name instead. For the same reason the app doesn't claim `application/octet-stream` for opened or shared files, or it would offer itself for every binary file.
- **The file is streamed into the request, not read into memory**, opened afresh on each write so OkHttp's retry resends all of it, through a client with long timeouts: a large file over a slow connection takes longer than the default ten seconds.

### 5.6 `map/SyncCandidatesOverlay`, `map/Simplify` and `map/Geo` — the candidates on the map

- **The account's tracks step aside on the tab**, so only the candidates are drawn, not the date range's slice of what's already synced. The candidates are redrawn after a style reload, which drops every custom layer.
- **`SyncCandidatesOverlay` is `TrackEditOverlay`'s pattern** (§3.2): one GeoJSON source with layers filtered on a role, plus the picked candidate's pace bands, built by the same function as the Map tab's. Only one set of bands is on the map at a time.
- **The sheet stays visible on the Sync tab while recording**, so what's on the phone can be sent with a walk under way.
- **`Simplify`** is Douglas–Peucker on a local plane, iterative so a long ride can't overflow the stack, loosening until the line fits a point budget. It is for drawing only; the route is sent as read. Each kept vertex's speed follows the server's track-metrics rule (root `docs/IMPLEMENTATION.md` §4.3.1), on a coarser line, so a candidate's bands are a little smoother than the same activity's once synced. Both are unit-tested.

### 5.7 `sync/WaitingLoader` and `sync/WaitingStore` — Load

**Health Connect is read in one place**, `WaitingLoader.load`, run only from a screen's own scope, so leaving the screen cancels it (§5.1). It asks the server which sessions are already known, hands those still processing to `SyncProgress`, and keeps the rest — the sessions, the time and the refused count — in one transaction. A failure changes nothing.

**`WaitingStore` keeps what a Load found, so the tab opens without reading Health Connect**: a second SQLite database beside the recordings' (§7.2), every row keyed by account, holding each session's drawn line with its speeds and times packed (round-tripped in `WaitingStoreTest`). The route isn't kept: a restored session is read again when it's sent (§5.2).

## 6. Sync History

The sync history (`apps/android/docs/SPEC.md` FR-4) is the web's `/sync` page in `WebPageActivity` (§2.5).

**View on map** is the page's link, which `route` hands to the native map with the activity's id and its own local day. An existing map comes to the front through `onNewIntent`, and a request replayed from recents is ignored. The map acts on it once the session is confirmed: inside the selected range it selects the activity as a track tap does; outside it, it applies that one day as a range and selects the activity once that day's list has it. **The fit is padded by the height the sheet is heading to, not the height it has mid-collapse** — fitting to the expanded sheet as it animated down put the activity at the top of the screen.

## 7. In-App GPS Recording

`recording/` (`RecordingService`, `RecordingActivity`, `recording/db`), the Sync tab's rows (§5.1), and the record button and live-track layer on the map own this feature end to end. The wire contract it submits to is root `docs/IMPLEMENTATION.md` §4.0.4, with [ADR-0007](../../../docs/adr/0007-in-app-gps-recording-submits-directly.md)'s decision not to add an endpoint for it. What follows is the client half that decision left to this app to design.

### 7.1 Recording and submitting are two separate steps, not one

**Recording is one tap, and nothing is asked.** Tap to start, tap to pause or resume, hold to stop (Android SPEC FR-5). The button (`recording/RecordButton.kt`) replaces the platform long-press with its own timed hold, drawn as a ring outside the button's edge where a fingertip doesn't cover it. **The click a released hold would produce is suppressed** unless it came within the long-press timeout, so an abandoned hold never pauses. TalkBack gets the stop as the node's long-click action. Haptics are given on the button, so the system's touch-feedback setting applies. Only location is required to start.

**The button only sends intents to the service; the notification's Stop goes through the map.** Pause/Resume goes straight to the service, but Stop opens the map, because the Save screen that follows needs an app window in the foreground to open over, and because it's confirmed there first: one tap in the shade is an easy mistake, where the map's hold is its own confirmation. A Stop that arrives before the map has bound to the service waits for the bind; one replayed from recents is ignored.

**The map binds to the service only to watch it**, which is also how reopening the app mid-recording — even after swiping it out of recents, since the foreground service keeps running — redraws the whole track so far. The status card's clock is a `Chronometer` based on the service's elapsed time, ticking on its own while recording.

**While recording, the map shows that recording and nothing else**: every history layer hides, and ending the recording restores whichever mode was selected. The camera eases to each fix without changing the zoom, so a zoom the user picks sticks.

**The notification's channel is default importance with sound and vibration off, not low**: a low-importance notification is filed under the shade's collapsed "Silent" section, which hides its action buttons behind an extra tap. Channel importance can't be raised once created, so the channel got a new id and the service deletes the old one.

**Stop saves in the service, not in a screen**, because Stop can come from the notification with no screen open. A recording too short to keep is dropped; otherwise the row is inserted with the last-used type (§7.5), in the same transaction that clears the journal (§7.9), on an application-lifetime scope, since the service may be destroyed as soon as it leaves the foreground. Only then does the Save screen open. **Saving first and asking second** means Back, or the screen never getting to open, still leaves the recording stored — the screen only refines a row that already exists.

**Nothing reaches the network until the user sends it from the Sync tab** (§5.2); a sent recording is deleted from the phone, and from then on lives on the server. `external_id` is a UUID minted when the recording starts, which also keys its journal (§7.9), since no platform store gives the activity an identity.

### 7.1.1 A recording's row on the Sync sheet

A recording is one of the Sync tab's candidates (§5.1, §5.2), drawn like any other with Edit and Delete added. The recordings are read again on every return to the tab, which is how it learns of an Edit.

### 7.2 `recording/db/RecordedActivityStore`

**Plain `SQLiteOpenHelper`, not Room**: this project's Kotlin was newer than any published KSP release, confirmed by trying it rather than assumed from documentation.

**The table is scoped per account**, so a recording made under one account (or the shared demo session) never shows up in another's list on the same device. Demo sessions share one key, which is harmless: a demo account can never sync (§7.4).

**Upgrades drop and recreate, except where a row exists nowhere else.** An unsynced recording is the only copy, so a version that changes its table keeps those rows. The database is in WAL mode, since the journal (§7.9) commits on every fix while other screens read.

### 7.3 Delete is local-only

Every recording row is unsynced (§7.1), so Delete discards the only copy, after a confirmation that says so. Once a recording has synced, deleting it is the server-side delete, since the phone no longer holds it.

### 7.4 Demo accounts are blocked from sync client-side too

**Matching the web's disabled controls, not just the server's `requireNotDemo`**, so a demo session never meets the block as a raw `demo_read_only` error through a generic failure path. Its recordings and history still list, reading not being a mutation.

### 7.5 A recording's starting type

**Each new recording starts on the account's last-used type**, kept in a per-account preference by every Edit save with a real type, so only an account's first recording starts as `"unknown"`. A preference rather than "the newest row's type" because synced rows are deleted and the store is often empty. An untyped recording still gets the overlap hint, since the rule compares time, not type (root `docs/IMPLEMENTATION.md` §4.6).

### 7.6 Type picker (`recording/ActivityTypePicker`)

The Android counterpart of the web's `ActivityTypePicker.tsx`, with `SearchPicker.tsx`'s ranking. The list is the server's per-type counts plus this device's unsynced recordings — a synced one is deleted locally and counted in the server's number instead — then the presets the account hasn't used, so a new account still starts on the shared vocabulary. **A dialog rather than a dropdown under the field**, because the soft keyboard takes half the screen and a dialog keeps both the search field and the list above it.

### 7.8 GPX download

**The file goes through the system's own save screen**, so the app needs no storage permission. GPX because every other mapping tool opens it, and HoldMyTrack's own upload reads its type back, so a downloaded track can come back in unchanged.

### 7.9 Crash and kill recovery (`recording/db/LiveRecordingJournal`)

**A foreground service survives the screen going away, not the process**: a low-memory kill, a crash or Force stop takes the in-memory recording and its notification with it. So every fix is also written to a journal in the same database, a row per fix rather than rewriting the whole point list, which keeps each write constant-size over a multi-hour recording. Writes are queued in order on one single-threaded dispatcher on an application-lifetime scope, and a failed write is logged rather than thrown, so a full disk doesn't end the recording.

**The id is minted at Start**, with the account signed in at that moment. A pause writes the moving time, since the time moved since the last fix would otherwise be lost; moving time is stored as a number rather than as `elapsedRealtime` marks, which reset on reboot. Stop moves the recording into the saved table and deletes the journal in one transaction, so there is no moment when a stopped recording is in neither place — **a journal row with no recording running in this process is always a leftover from one that died.**

**Recovery is offered, not automatic.** The service stays `START_NOT_STICKY`: a sticky restart counts as a start from the background, which gets no while-in-use location access, so it could fail or record nothing, and it would resume without the user knowing. Instead the map looks for a leftover when it binds to an idle service — queued behind pending writes, so a Stop that has only just happened is never mistaken for one — and offers Resume, Save or Discard. Resume comes back **paused**, with the last fix cleared, so the time the process was dead adds neither moving time nor distance. Another account's leftover isn't offered, since its Save screen would find nothing under the signed-in account. A Start that finds a leftover saves it as it stands rather than overwriting it — the journal holds one recording at a time.

## 8. Build Configuration

**The API's address is the one build-time value from outside the build**, set per build type from a Gradle property, so the Play build can't pick up a dev stack's address. The release signing config reads the upload keystore from properties kept outside the repository (`apps/android/holdmytrack/README.md`). Pointing a physical device at a host dev stack needs `adb reverse` for both the API port and the web dev server port the style's asset URLs resolve against (`apps/android/holdmytrack/README.md`). **Donate is in debug builds only**, the APK the website offers, since Play's payments policy doesn't allow a link to payment outside its billing.

**The version comes from git.** `versionCode` is the commit count, so it rises with every commit and never needs bumping — Play refuses an upload whose code isn't higher than the last — and the short SHA is baked in, the same one the server's `/healthz` reports. Outside a git checkout the code is 1.

**Cleartext `http://` traffic is permitted in debug builds only**, so a release build cannot be pointed at a plaintext dev endpoint by accident — the network security configuration enforces this, not a code-review convention.

**`libs.versions.toml` pins every dependency with an inline reason for that version**, so an upgrade is a decision made against the reasoning rather than a blind bump.

**Languages: English and Russian** (root `docs/IMPLEMENTATION.md` §4.21). Strings that are the same in every language are `translatable="false"`, so lint's missing-translation check covers everything else. The Russian avoids plural forms by phrasing counts as "label: N". The APK keeps only `en` and `ru` resources, so a phone set to a language the app doesn't have gets the English app, not English mixed with a library's own translations. The app's language is the platform's per-app language (minSdk 34), with no AppCompat per-app-locale API needed.

## 9. Verification Methodology & Known Gaps

**Automated tests are few**: JUnit tests over the pure logic (`app/src/test`), which CI runs on every push with coverage (root `docs/DEVELOPMENT.md`, "CI"), and no instrumentation tests. Every behavior in `apps/android/docs/SPEC.md` has instead been verified by hand against a live stack, on a physical device for Health Connect sync and on an emulator for most of the rest. This is a real gap relative to the server codebase, worth naming rather than leaving implicit:

- **Regressions are only caught by re-running the manual checks**, which don't run automatically and aren't gated in CI. A change to the sync classes in particular should be re-checked against real Health Connect data in the scenarios a quick smoke test tends to skip: a run cut off midway, a route not handed over, a reinstall (synced sessions mustn't come back), and an activity deleted on the web (its session must).
- **Verification has only ever run against one physical device and one Health Connect data provider (Fitbit).** "Samsung is unsupported" rests on Samsung's own published documentation, not on a device measurement the way every other finding here does, for lack of a Galaxy Watch to test against.
- **No test double exists for Health Connect**: the read and the send are exercised only against a real client on a real device.