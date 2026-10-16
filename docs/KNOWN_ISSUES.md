# Known Issues

Open defects in shipped functionality. A fixed entry is deleted, with any lasting note moved into `docs/IMPLEMENTATION.md` beside the code it concerns.

---

### The overlap hint misses a whole copy of a split activity

The overlap hint (`SPEC.md` FR-3.7, `IMPLEMENTATION.md` §4.6) marks a candidate whose time range overlaps one of the account's activities by at least 80% of the longer one. After a split (`SPEC.md` FR-5.17, `IMPLEMENTATION.md` §4.7.8), a whole copy of the recording from a second source, such as a Health Connect session of a ride uploaded from the watch's file and then split, overlaps each part by far less, so its row in the Android app's Sync tab isn't marked. Ticked, it lands next to the parts: its time is counted twice in totals and its route drawn twice into the Heatmap. A copy of just one part is marked.

- [ ] In `ingest.Overlaps`, compare a span against a split group as one span (the group's earliest start to its latest end) as well as against each part, and answer with the group's first part.

### Photo prep in Chromium decodes a large PNG at full size

`preparePhoto` (`apps/web/src/ui/photoPrep.ts`, `IMPLEMENTATION.md` §4.27) avoids a full-size decode of a large photo in Chromium only for a JPEG, which Chromium decodes at a reduced scale when the `<img>` is drawn onto the 2048 px canvas. A PNG gets no scaled decode there: `ImageDecoder` hands back the whole frame, and the `<img>` is decoded whole too. A 192 MP PNG peaked at about 1 GB in desktop Chromium and 813 MB in Chrome on a Pixel 10a. Camera, panorama and drone images are JPEGs, so this needs a very large PNG, such as an exported or stitched picture. WebP and AVIF were not measured.

- [ ] Find a decode path for a large PNG in Chromium that doesn't hold it at full size, or refuse one above a pixel count with a reason, and measure WebP and AVIF while at it.

### A recording whose ingest fails after the app dies is not put back on the phone

A sent recording is deleted from the phone once the server takes it, and put back if its ingest then fails (`apps/android/docs/SPEC.md` FR-5.2). `SyncProgress` keeps what it sent only in memory, so if the process dies between the send and the failure, nothing puts the recording back and the phone no longer holds it.

- [ ] Keep sent recordings on disk until their ingest lands, or until the failure has put them back.

### Deleting an account on Android leaves two pieces of its state on the phone

Account deletion clears what the device keeps for the account (`AccountDeletion.forget`): unsynced recordings, hidden Sync rows, the Health Connect switch and what Load last read. It does not clear the last-used recording type (`RecordingTypes`) or an unfinished recording's journal (`LiveRecordingJournal`), so a new account signing in with the same email inherits them.

- [ ] Clear both in `AccountDeletion.forget`.