package dev.holdmytrack.android.sync

import java.time.Duration
import java.time.Instant

/**
 * Which waiting rows cover the same stretch of time as each other — the phone's half of the
 * Sync tab's similar-tracks hint (`docs/SPEC.md` FR-3.7); the server answers for the account's
 * own activities (`POST /v1/activities/overlaps`). A hint only: nothing is unticked, hidden or
 * chosen on the user's behalf (ADR-0039).
 *
 * The rule is the server's `overlapMatches` (`services/server/internal/ingest/overlap.go`), so
 * "similar" means the same thing on both sides: two spans overlap when they share at least
 * [MIN_SHARE] of the *longer* one's duration. Measuring against the longer one keeps
 * back-to-back recordings with a little clock skew, a short walk inside a long hike and a day
 * inside a multi-day log apart. Type and distance aren't compared: sources disagree on both.
 */
object Similar {
    /** The server's `overlapMinShare`. */
    const val MIN_SHARE = 0.8

    fun overlaps(aStart: Instant, aEnd: Instant, bStart: Instant, bEnd: Instant): Boolean {
        val aDur = Duration.between(aStart, aEnd)
        val bDur = Duration.between(bStart, bEnd)
        if (aDur <= Duration.ZERO || bDur <= Duration.ZERO) return false
        val lo = maxOf(aStart, bStart)
        val hi = minOf(aEnd, bEnd)
        val shared = Duration.between(lo, hi).toMillis()
        return shared >= MIN_SHARE * maxOf(aDur, bDur).toMillis()
    }

    /** For each of [candidates], by [Candidate.key], the others it overlaps, newest first. A
     *  candidate that overlaps nothing isn't in the map. */
    fun waiting(candidates: List<Candidate>): Map<String, List<Candidate>> {
        val found = mutableMapOf<String, MutableList<Candidate>>()
        for ((i, a) in candidates.withIndex()) {
            for (b in candidates.subList(i + 1, candidates.size)) {
                if (a.key == b.key || !overlaps(a.startedAt, a.endedAt, b.startedAt, b.endedAt)) continue
                found.getOrPut(a.key) { mutableListOf() } += b
                found.getOrPut(b.key) { mutableListOf() } += a
            }
        }
        return found.mapValues { (_, others) -> others.sortedByDescending { it.startedAt } }
    }
}