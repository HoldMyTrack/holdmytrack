# Known Issues

Defects in already-shipped, currently-live functionality — not planned work. `docs/ROADMAP.md` is the forward-looking build plan ("what we're going to do"); this file is the opposite direction ("what's currently broken and needs fixing"), independent of any phase or priority ordering.

This file stays lean and current-only. Once an entry is fixed, its root-cause/fix/verification write-up moves into `docs/IMPLEMENTATION.md` as a permanent implementation note next to the relevant pipeline step, and the entry is deleted from here. Nothing here is meant to accumulate as a permanent record — that record lives in `IMPLEMENTATION.md` once each issue is closed.

---

### The overlap hint misses a whole copy of a split activity

The overlap hint (`SPEC.md` FR-3.7, `IMPLEMENTATION.md` §4.6) marks a candidate whose time range overlaps one of the account's activities by at least 80% of the longer one. After a split (FR-5.17, §4.7.8), a whole copy of the same recording from a second source, say a Health Connect session of a ride uploaded earlier from the watch's file and then split, overlaps each part by far less than that, so its row in the Android app's Sync tab isn't marked. Ticked, it lands next to the parts: the time is counted twice in totals, and the route is drawn twice into the Heatmap. A copy of just one part is marked, since that part and the copy overlap almost entirely.

- [ ] In `ingest.Overlaps`, compare a span against a split group as one span (the group's earliest start to its latest end) as well as against each part, and answer with the group's first part.

### Photo prep in Chromium decodes a large PNG at full size

`preparePhoto` (`apps/web/src/ui/photoPrep.ts`, `IMPLEMENTATION.md` §4.27) avoids a full-size decode of a large photo in Chromium only for a JPEG, which Chromium decodes at a reduced scale when the `<img>` is drawn onto the 2048 px canvas. A PNG gets no scaled decode there: `ImageDecoder` hands back the whole frame, and the `<img>` is decoded whole too. A 192 MP PNG peaked at about 1 GB in desktop Chromium and 813 MB in Chrome on a Pixel 10a. Camera, panorama and drone images are JPEGs, so this needs a very large PNG, such as an exported or stitched picture. WebP and AVIF were not measured.

- [ ] Find a decode path for a large PNG in Chromium that doesn't hold it at full size, or refuse one above a pixel count with a reason, and measure WebP and AVIF while at it.