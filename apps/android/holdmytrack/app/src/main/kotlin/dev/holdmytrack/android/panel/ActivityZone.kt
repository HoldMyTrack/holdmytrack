package dev.holdmytrack.android.panel

import java.time.ZoneId

/**
 * The zone an activity's times are shown in (`docs/IMPLEMENTATION.md` §4.30): the one it was
 * recorded in, else the account's, else the phone's. A zone the phone's tz data doesn't know yet
 * falls through to the next rather than failing to format — the web's `knownTimeZone`.
 */
object ActivityZone {
    fun of(own: String?, account: String?, phone: ZoneId = ZoneId.systemDefault()): ZoneId =
        sequenceOf(own, account).firstNotNullOfOrNull { id ->
            id?.takeIf { it.isNotEmpty() }?.let { runCatching { ZoneId.of(it) }.getOrNull() }
        } ?: phone
}
