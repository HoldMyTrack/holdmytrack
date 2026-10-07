# Known Issues

Defects in already-shipped, currently-live functionality — not planned work. `docs/ROADMAP.md` is the forward-looking build plan ("what we're going to do"); this file is the opposite direction ("what's currently broken and needs fixing"), independent of any phase or priority ordering.

This file stays lean and current-only. Once an entry is fixed, its root-cause/fix/verification write-up moves into `docs/IMPLEMENTATION.md` as a permanent implementation note next to the relevant pipeline step, and the entry is deleted from here. Nothing here is meant to accumulate as a permanent record — that record lives in `IMPLEMENTATION.md` once each issue is closed.

---

### A Google Health Takeout export over 512 MB, or split into several files, can't be imported

`handleUpload` refuses a `.zip` over `maxZipUploadBytes` (512 MiB, `services/server/internal/httpapi/server.go`), but Takeout's smallest part size is 1 GB and its default is 2 GB. The real sample the Takeout reader was built against (`IMPLEMENTATION.md` §4.0.2) was 1.9 GB, so it would be refused through the upload. Takeout also splits an export bigger than the chosen part size into several `.zip` files. `handleTakeoutUpload` reads one archive at a time, and the join needs an activity's exercise log (`exercise-N.json`) and its day's GPS file (`gps_location_*.csv`) in the same archive. Split across parts, an activity whose two halves land apart comes out with no route, or is skipped. The export guide (`SPEC.md` FR-10.5) states both limits rather than promising otherwise.

- [ ] Accept a Takeout archive above 512 MB. It's already read from a temp file rather than memory (`multipartMemoryBytes`), so the cap is about request size, not RAM. A separate, larger cap for an archive `isTakeoutArchive` recognizes would do, or a resumable upload.
- [ ] Join across the parts of one split export: hold the parts of one Takeout export (they share a name stem, `takeout-<timestamp>-NNN.zip`) until all have arrived, or index each part's exercise logs and GPS files and join across them.
- [ ] Check both against a real multi-part export, then drop the limit from the guide.

### A split activity's ride arriving again from another source shows up beside its parts

Cross-source duplicate detection (`SPEC.md` FR-3.7, `IMPLEMENTATION.md` §4.6) calls two activities the same one when their time ranges overlap by at least 80% of the longer one. After a split (FR-5.17, §4.7.8), a whole-activity copy of the same recording from a second source, say a Garmin export after a Health Connect sync, overlaps each part by far less than that, so it goes live next to them: the time is counted twice in totals, and the route is drawn twice into the Heatmap. A later copy of just one part is caught, since that part and the copy overlap almost entirely.

- [ ] In `ResolveDuplicates`, compare a new activity against a split group as one span (the group's earliest start to its latest end) as well as against each part. When it matches, mark it superseded by the group's first part.
- [ ] Decide which copy wins when the new one is richer: keep the parts, since they hold the user's own split, and say so in FR-3.7.

### Trends leaves out empty weeks and months, so its bars don't show the 12 months `SPEC.md` FR-9 describes

FR-9 behavior 3 says the Profile page renders the trailing 12 months as one bar per bucket. `activityTrendsQuery` (`services/server/internal/httpapi/activities.go`) groups only the account's activities, so a week or month with none never comes back, and both clients draw one bar per period returned (`buildTrendBars`, `profile/TrendsChartView`). A history with two active weeks draws two bars filling the whole chart, each half its width, with nothing to show the other 50 weeks were empty; the axis's two dates are the first and last active period, not the window's ends. Found on the Android emulator against a local stack while porting the page; the web's `/profile` draws the same two bars.

- [ ] Fill the gaps — in the query (`generate_series` over the window's buckets, left-joined) or in each client — so every week or month of the window has a bar, at the 2px minimum when empty, and the axis shows the window's ends.
- [ ] Check the web page and the Android screen against an account with a few active weeks months apart.

### Photo prep decodes a picked photo at full size before shrinking it

`preparePhoto` (`apps/web/src/ui/photoPrep.ts`) decodes the whole picture with `createImageBitmap` (about 4 bytes a pixel) and only then draws it at 2048 px and 320 px. A 100–200 MP panorama or drone image needs 400–800 MB for that one bitmap, which can kill a phone's tab and the unsaved Edit window with it. Found in the frontend audit; not yet reproduced on a device.

- [ ] Decode at the target size (`createImageBitmap`'s `resizeWidth`/`resizeHeight`, which need the oriented dimensions first), and check that an EXIF-rotated portrait still comes out upright and undistorted in Chrome, Firefox and Safari.