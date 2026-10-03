# Known Issues

Defects in already-shipped, currently-live functionality — not planned work. `docs/ROADMAP.md` is the forward-looking build plan ("what we're going to do"); this file is the opposite direction ("what's currently broken and needs fixing"), independent of any phase or priority ordering.

This file stays lean and current-only. Once an entry is fixed, its root-cause/fix/verification write-up moves into `docs/IMPLEMENTATION.md` as a permanent implementation note next to the relevant pipeline step, and the entry is deleted from here. Nothing here is meant to accumulate as a permanent record — that record lives in `IMPLEMENTATION.md` once each issue is closed.

---

### A Google Health Takeout export over 512 MB, or split into several files, can't be imported

`handleUpload` refuses a `.zip` over `maxZipUploadBytes` (512 MiB, `services/server/internal/httpapi/server.go`), but Takeout's smallest part size is 1 GB and its default is 2 GB. The real sample the Takeout reader was built against (`IMPLEMENTATION.md` §4.0.2) was 1.9 GB, so it would be refused through the upload. Takeout also splits an export bigger than the chosen part size into several `.zip` files. `handleTakeoutUpload` reads one archive at a time, and the join needs an activity's exercise log (`exercise-N.json`) and its day's GPS file (`gps_location_*.csv`) in the same archive. Split across parts, an activity whose two halves land apart comes out with no route, or is skipped. The export guide (`SPEC.md` FR-10.5) states both limits rather than promising otherwise.

- [ ] Accept a Takeout archive above 512 MB. It's already read from a temp file rather than memory (`multipartMemoryBytes`), so the cap is about request size, not RAM. A separate, larger cap for an archive `isTakeoutArchive` recognizes would do, or a resumable upload.
- [ ] Join across the parts of one split export: hold the parts of one Takeout export (they share a name stem, `takeout-<timestamp>-NNN.zip`) until all have arrived, or index each part's exercise logs and GPS files and join across them.
- [ ] Check both against a real multi-part export, then drop the limit from the guide.

### Trends leaves out empty weeks and months, so its bars don't show the 12 months `SPEC.md` FR-9 describes

FR-9 behavior 3 says the Profile page renders the trailing 12 months as one bar per bucket. `activityTrendsQuery` (`services/server/internal/httpapi/activities.go`) groups only the account's activities, so a week or month with none never comes back, and both clients draw one bar per period returned (`buildTrendBars`, `profile/TrendsChartView`). A history with two active weeks draws two bars filling the whole chart, each half its width, with nothing to show the other 50 weeks were empty; the axis's two dates are the first and last active period, not the window's ends. Found on the Android emulator against a local stack while porting the page; the web's `/profile` draws the same two bars.

- [ ] Fill the gaps — in the query (`generate_series` over the window's buckets, left-joined) or in each client — so every week or month of the window has a bar, at the 2px minimum when empty, and the axis shows the window's ends.
- [ ] Check the web page and the Android screen against an account with a few active weeks months apart.

### Photo prep decodes a picked photo at full size before shrinking it

`preparePhoto` (`apps/web/src/ui/photoPrep.ts`) decodes the whole picture with `createImageBitmap` (about 4 bytes a pixel) and only then draws it at 2048 px and 320 px. A 100–200 MP panorama or drone image needs 400–800 MB for that one bitmap, which can kill a phone's tab and the unsaved Edit window with it. Found in the frontend audit; not yet reproduced on a device.

- [ ] Decode at the target size (`createImageBitmap`'s `resizeWidth`/`resizeHeight`, which need the oriented dimensions first), and check that an EXIF-rotated portrait still comes out upright and undistorted in Chrome, Firefox and Safari.

### Places across the 180° meridian: Show in this area misses half the view, and flying to a crossing activity zooms out to the whole world

Longitude is handled as a plain −180…180 range everywhere, so anything spanning the 180° meridian (Fiji, Tonga, Samoa, Chukotka, the western Aleutians, a Pacific crossing) is misread. Found in the frontend audit; not reproduced against real data.

- **Show in this area** (`apps/web/src/ui/ShowInArea.tsx`, `getSpotsInArea`) sends MapLibre's raw bounds, whose east edge goes past 180 when the view straddles the meridian (170…190). The server's `parseBBox` (`services/server/internal/httpapi/spots_area.go`) clamps that to 170…180, so every place from −180 to −170 is never searched and the "N places in this area" count comes up short with no warning. Its comment assumes that stretch is open ocean.
- **Fly to an activity** (`apps/web/src/map/bbox.ts`): an activity's `bbox` is its points' minimum and maximum longitude, so a track with points at 179.9 and −179.9 gets −179.9…179.9, nearly the whole globe. `flyToBBox` fits that at about zoom 1, below the tracks' zoom-4 minimum, and the map arrives empty. `unionBBox` does the same for a checked group on either side of the meridian (New Zealand and Hawaii), spanning the long way round.

- [ ] Split a crossing view into two boxes for the spots-in-area query, on the client or in `parseBBox`.
- [ ] Let a bbox wrap (west > east) where a track or group is shorter that way round: in how ingest computes an activity's `bbox`, in `unionBBox`, and in `flyToBBox`.
- [ ] Check both against a track that crosses the meridian.

### Activity start times show in the browser's timezone, not the account's

The server buckets activities into days by the account's own timezone (`SPEC.md` FR-1.7), but the web formats times with the browser's: `formatStartedAt` (`apps/web/src/ui/format.ts`, an unnamed activity's row label and the Edit window's subtitle) and the times of day in the Track and Photos tabs (`TrackEditor.tsx`, `PhotosTab.tsx`) call `toLocale*String` with no `timeZone`. With the account on America/New_York and the browser in Europe/Berlin, an unnamed activity started at 21:30 New York time on 9 March sits in the date slider's 9 March slot, but its row reads "10 Mar 2026, 03:30". The account's zone is already on hand (`UserProfile.timezone`). Found in the frontend audit.

- [ ] Decide which zone a time should be shown in: the account's (matching the slider's days), or the activity's own local zone where it was recorded (what a traveller expects from a trip abroad, but not stored today).
- [ ] Pass that zone as `timeZone` everywhere the web formats an activity's or a point's time, and check a browser in another zone against the slider.