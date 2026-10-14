package dev.holdmytrack.android.map

import java.time.Instant

/**
 * What the map asks `GET /v1/spots/captures` for on a return to the app: `since` the newest
 * capture it already has, so the answer is usually empty (`docs/SPEC.md` FR-15.2), or null, for
 * the whole list, when it has none.
 */
fun capturesSince(known: Map<Long, Instant>): Instant? = known.values.maxOrNull()

/**
 * [known] with [fresh] added. A capture is never changed or removed (root
 * `docs/IMPLEMENTATION.md` §4.25), so what an answer `since` the newest leaves out is still as
 * the map has it; the newest itself comes back again and is kept once.
 */
fun mergeCaptures(known: Map<Long, Instant>, fresh: Map<Long, Instant>): Map<Long, Instant> = known + fresh
