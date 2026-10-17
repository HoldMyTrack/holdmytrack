package dev.holdmytrack.android.net

import android.content.Context
import android.util.Log
import androidx.fragment.app.Fragment
import dev.holdmytrack.android.R
import dev.holdmytrack.android.SignInActivity
import dev.holdmytrack.android.VerifyEmailActivity
import dev.holdmytrack.android.map.MapNotice
import dev.holdmytrack.android.map.NoticePanel
import dev.holdmytrack.android.settings.AppLanguage
import dev.holdmytrack.android.web.WebPageActivity

/**
 * Whether the map may show the user's layers: a session, confirmed by the server, with a
 * confirmed email and a Country. No session goes to `SignInActivity`.
 *
 * A token restored from disk isn't trusted until the server confirms it: a revoked one would
 * otherwise show as a blank map, its 401s visible only in logcat. An unconfirmed email goes to
 * `VerifyEmailActivity`, since every tile and sync request would be a `403`.
 */
class SessionGate(
    private val fragment: Fragment,
    private val notices: NoticePanel,
    /** The check answered: run [passes] again. */
    private val onChecked: () -> Unit,
) {
    /** Guards against a second check while one is in flight; every `onResume` asks. */
    private var verifying = false

    /** Posted when a check starts, so "Checking your session…" appears only if the check is slow
     *  enough to notice; removed when it answers. */
    private val showChecking = Runnable {
        notices.show(MapNotice.CHECKING, fragment.getString(R.string.checking_session))
    }

    /** True when the map may go on; otherwise the user is sent where they need to go, or the
     *  check runs and [onChecked] follows. */
    fun passes(): Boolean {
        if (!Session.isSignedIn) {
            leaveFor { SignInActivity.open(it) }
            return false
        }
        if (!Session.verified) {
            verify()
            return false
        }
        if (!Session.emailVerified) {
            leaveFor { VerifyEmailActivity.open(it) }
            return false
        }
        // The first run, as on the web: an account with no Country goes to Settings first. The
        // Country decides the units, and nothing else asks for it.
        if (!Session.isDemo && Session.country.isEmpty()) {
            leaveFor { WebPageActivity.openOnboarding(it) }
            return false
        }
        return true
    }

    private fun leaveFor(open: (Context) -> Unit) {
        open(fragment.requireContext())
        fragment.requireActivity().finish()
    }

    private fun verify() {
        if (verifying) return
        verifying = true
        notices.view.postDelayed(showChecking, CHECKING_DELAY_MS)
        HoldMyTrackApi.verifySession { result ->
            verifying = false
            notices.view.removeCallbacks(showChecking)
            notices.hide(MapNotice.CHECKING)
            result.onSuccess { profile ->
                Session.markVerified(profile)
                AppLanguage.followAccount(fragment.requireContext().applicationContext, profile.locale)
                notices.hide(MapNotice.UNREACHABLE)
            }.onFailure { failure ->
                // Only a 401 means the token is dead. Any other failure keeps it for the next
                // resume, and the notice says why the map is empty.
                if (failure is ApiException && failure.code == 401) {
                    Session.clear()
                } else {
                    Log.w(TAG, "could not verify the stored session", failure)
                    notices.show(
                        MapNotice.UNREACHABLE,
                        fragment.getString(R.string.map_unreachable),
                        failed = true,
                        action = R.string.retry,
                    ) { verify() }
                }
            }
            onChecked()
        }
    }

    private companion object {
        const val TAG = "HoldMyTrack"

        /** How long a check runs before the notice shows, so a fast one doesn't flash it. */
        const val CHECKING_DELAY_MS = 600L
    }
}
