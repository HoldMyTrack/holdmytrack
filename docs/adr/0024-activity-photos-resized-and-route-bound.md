# ADR-0024: An activity can carry the user's photos, kept as resized copies on our own storage, each placed on the route by the time it was taken

## Status

Accepted. Built for the server and the web (`SPEC.md` FR-16, `IMPLEMENTATION.md` §4.27); the Android app is a `ROADMAP.md` item. Amends the "only the geographical data you trust us with" line of ADR-0017, which it otherwise leaves as it is.

## Context

A Story keeps a trip as one thing — its tracks, its name, its totals (ADR-0020) — but a trip is remembered as much by what was seen on it as by where it went. The request was to add one's own photos to an activity, each bound to a point on the route, so that looking back at a journey shows the pictures where they were taken. Four choices decide what that means: where the files live, what is kept of them, how a photo finds its point on the route, and what Private locations do to it. A fifth is the cost to the promise ADR-0017 made: "we don't keep your health profile, only the geographical data you trust us with." A photo isn't geographical data; it can show faces, other people and the inside of a home.

## Decision

**Photos are stored on our own object storage, as a resized copy and a thumbnail, with no original.** The browser decodes the picked file, draws it at most 2048 px on its long side and 320 px for the thumbnail, and uploads the two re-encoded images (WebP, or JPEG where the browser can't encode WebP). Re-encoding drops every EXIF field, the position included, so the stored file says nothing beyond its pixels. The server sniffs both parts, refuses anything that isn't a JPEG or WebP within the size and dimension limits, and keeps both under `photos/{userID}/`. An account holds at most 2,000 photos. HoldMyTrack is not a photo backup: the original stays wherever the user keeps it.

**A photo's place on the route is a moment, not a position.** The browser reads the file's EXIF capture time and GPS position before resizing and sends them as fields. The server places the photo at the capture time if it falls within the activity's track (five minutes either side are clamped to its ends); without a usable time, at the point of the track nearest the EXIF position, if that's within 500 m. What is stored is a time on the track's own time axis (`route_at`); the position is worked out from the track on every read. A photo with no usable time or position is kept unplaced, shown in the activity's photos but not on the map, and the user can place it — or move a placed one — by putting it on the track, which snaps to the nearest point.

**A photo inside a Private location keeps no place on the map.** A worked-out position inside any of the account's Private locations isn't returned, so the photo shows only among the activity's photos — and because it's worked out on every read, adding a Private location later hides photos already there. A moment the track no longer covers — cut by Private locations or by Edit track — places it nowhere, the same way.

**Photos are private,** like everything else in an account: served only to their owner, never by a public link.

**The promise is reworded, not dropped:** "we don't keep your health profile — only the geographical data you trust us with, and the photos you choose to add to it." Nothing about health data changes.

## Alternatives considered

- **Links only, to a public image host (Imgur, Flickr, IPFS).** Rejected: an image there is public to anyone holding its link, which a family photo or a home's interior must not be; free hosts delete old uploads and forbid serving as another app's storage, so the map would break when they do.
- **Links only, to the user's own cloud (Google Drive, Dropbox, OneDrive).** Rejected for now: an OAuth integration per provider, an image served only once the user opens its sharing to "anyone with the link", hot-linking that Drive throttles, and a broken photo every time the user tidies their files. Google Photos is out entirely: its API hands out image URLs that expire within the hour, and reads only what the app itself uploaded. Kept as the fallback if storage ever matters: our own thumbnail plus a link to the original wherever the user keeps it.
- **Keep the original as well.** Rejected: several MB a photo, roughly ten times the storage, for a resolution the map and the viewer never show — and the original carries the full EXIF, position included.
- **Resize on the server.** Rejected: the server would have to decode HEIC and every JPEG variant, and receive 5–10 MB originals only to throw most of each away. The browser already decodes what it can show, and resizing there is what the Android app already does for avatars.
- **Store the position.** Rejected: a stored point would go stale when Edit track or a Private location change rebuilds the track, and it would keep a position inside a Private location added after the upload. A moment on the track can't outlive the track's own clipping.
- **Place by EXIF position first.** Rejected as the first choice: a phone's photo position is often missing, cached from minutes earlier or off by tens of metres in a town, while the camera's clock is nearly always set; position is the fallback for a photo whose clock is unusable.
- **Refuse a photo taken inside a Private location.** Rejected: the photo is still the user's own; hiding its place, as the track's own ends are hidden, is enough.

## Consequences

- The data promise changes for the first time since ADR-0017: HoldMyTrack now keeps something that isn't geography. The About and Help pages say so.
- Photos are a new storage line that grows with users, about 0.5 MB a photo, bounded by the per-account limit (`VISION.md` §4.3). Dormancy and retention (`IMPLEMENTATION.md` §5.7) will have to cover them.
- A shared Story link (`VISION.md` §5.7), if it is ever built, would make these images visible to others, and with that come moderation and takedown duties that private photos don't have. That decision gets harder once photos exist.
- A file whose EXIF the browser can't read (most HEIC files outside Safari, which can't be decoded there at all) arrives unplaced, or not at all; placing it is then the user's job.
- An EXIF capture time with no time zone is read in the account's time zone first, then in whichever other zone puts it on the track; a trip across a zone change can still misplace one, which the user can move.
- Deleting an activity deletes its photos; when deduplication hides an activity behind a richer copy, its photos move to that copy.