# ADR-0033: Android's main screens are tabs of one Activity, each a Fragment, under a bottom navigation bar

## Status

Accepted. The map is the first: `MapFragment`, hosted by `MainActivity` (`apps/android/docs/IMPLEMENTATION.md` §1.2). The bottom bar and the other tabs follow in the phone-first redesign (`apps/android/docs/ROADMAP.md`).

## Context

The Android app carried the web's phone layout over: a burger menu at the map's top-start opening every other screen (Profile, Upload, Sync, Settings, the web's pages), and the map's own chrome stacked in bands — the mode row, a second row for Layers and Record, the Activities panel's tab row, and a two-row date footer. On a phone those bands take much of the screen, and the burger hides the places a person goes most after the map: Sync, and their own account. The redesign replaces the burger with a bottom navigation bar — Map, Stories, Record, Sync, You — which raises how the app moves between its main screens. Every screen was its own Activity, opened with an explicit `Intent`, and the map Activity alone was some 2,100 lines that assumed it owned the window.

## Decision

**The main screens are tabs of one Activity, `MainActivity`, each a Fragment under one bottom navigation bar the Activity owns.** Switching tabs shows and hides fragments rather than replacing them, so the map's `MapView`, camera and loaded layers survive a trip to Sync and back. The map moved first, unchanged in behavior, into `MapFragment`; Sync and You follow as fragments of their own. Screens reached from a tab rather than being one — Upload, Settings, the recording's Save screen, the sign-in flow — stay Activities.

## Alternatives considered

- **Keep each tab a separate Activity, each drawing the same bar, and switch with `FLAG_ACTIVITY_REORDER_TO_FRONT`.** The smaller change: no screen would be restructured, and Sync's foreground-only Health Connect run would keep its Activity lifecycle as it is. Rejected: every switch is a window change, so the bar redraws and the content flashes, and the bar's state (the selected tab, the Sync badge, the record button's recording state) would have to be kept in step across separate windows.
- **Keep the burger menu.** Rejected: it hides the screens a person returns to most, and it is the web's answer to a wide header, not a phone's.

## Consequences

- The map fragment forwards MapLibre's lifecycle from its own callbacks, and the `MapView` is destroyed with the fragment's view, not the Activity. Its permission launchers, Back callback and recording-service binding all hang off the fragment.
- Sync's run must stay foreground-only (`apps/android/docs/ARCHITECTURE.md` §1.1). As a fragment it is cancelled when the fragment stops and when its tab is hidden, not only when a window goes to the background.
- Intents that used to land on one Activity — Health Connect's rationale, which opens Sync, and the notification's Stop and the Sync screen's View on map, which reach the map — arrive at `MainActivity` and are routed to the right tab.
- `androidx.fragment` becomes a declared dependency, at the version AppCompat and Material already resolved, so nothing is upgraded by declaring it.