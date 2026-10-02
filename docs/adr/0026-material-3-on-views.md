# ADR-0026: Android UI is Material 3 on the existing Views, not Compose

## Status

Accepted.

## Context

The Android app was built screen by screen to work, not to look finished: seven layouts and two custom Views (`RecordButton`, `TrackSilhouetteView`) on plain AppCompat. Before its design pass could start, carrying the web's tokens (palette, fonts, spacing, radius and type scales) onto the app needed a toolkit to land in, since that decides what "apply the design tokens" even means. The map screen is the product, and MapLibre's `MapView` is an Android View.

## Decision

**The app stays on Views and adopts Material 3 (`com.google.android.material`, `Theme.Material3.DayNight`), with the web's tokens mapped onto Material's color roles, type roles and corner sizes** (`apps/android/docs/IMPLEMENTATION.md` §1.3). Dynamic color is not applied.

## Alternatives considered

- **Jetpack Compose.** Rejected: it meant rewriting every existing layout and both custom Views before any styling could start, and hosting `MapView` through `AndroidView` with its lifecycle forwarded by hand, friction on exactly the screen that matters most. Compose's real advantage, that Google's new component work lands there first, pays off for an app that keeps growing screens; this one is deliberately small (an ingest path, a map, casual recording).
- **Hand-rolled tokens on plain AppCompat.** Rejected: Material already has the vocabulary the tokens need (color roles, a type scale, shape), plus components that meet the 48dp touch target and behave under TalkBack by default, which the accessibility pass would otherwise have had to rebuild. Measured on the emulator, that holds even where a style zeroes a button's insets: the map's mode buttons still come out 48dp tall.
- **Material You dynamic color.** Rejected: it replaces the palette with one derived from the user's wallpaper, and HoldMyTrack's look is a brand, not a system accent.

## Consequences

- Compose isn't closed off. Views and Compose interoperate both ways (`ComposeView` in a layout, `AndroidView` in Compose), so a later move could go screen by screen, and Material 3's color and type roles map almost one-to-one onto Compose's `MaterialTheme`.
- Material's defaults have to be overridden where the web differs: its pill buttons, 4dp fields and 28dp dialogs give way to the web's radius scale, its cream container tone to the web's white dialogs, and `MaterialButton` ignores a style's minimum width, so the map's mode buttons set theirs in code.
- New components arrive in Compose first; a component Material has only there has to be built or done without.