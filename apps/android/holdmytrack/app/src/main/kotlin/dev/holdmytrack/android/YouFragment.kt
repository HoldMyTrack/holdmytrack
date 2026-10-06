package dev.holdmytrack.android

import android.content.Intent
import android.os.Bundle
import android.view.View
import android.widget.TextView
import androidx.browser.customtabs.CustomTabsIntent
import androidx.fragment.app.Fragment
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.settings.SettingsActivity

/**
 * The bottom bar's You tab: the app's own screens that aren't tabs — Profile, Upload, Settings,
 * and the map's Private locations —
 * then the web header's Donate and Info pages (About, Help, Contacts, Privacy), opened in a
 * browser tab, and last the app's version, a line to read rather than an action. Donate only
 * where `BuildConfig.DONATE_LINK` allows it, which the Play build doesn't.
 */
class YouFragment : Fragment(R.layout.fragment_you) {

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        super.onViewCreated(view, savedInstanceState)
        val context = requireContext()
        view.findViewById<View>(R.id.you_profile).setOnClickListener {
            startActivity(Intent(context, ProfileActivity::class.java))
        }
        view.findViewById<View>(R.id.you_upload).setOnClickListener {
            startActivity(Intent(context, UploadActivity::class.java))
        }
        view.findViewById<View>(R.id.you_settings).setOnClickListener { SettingsActivity.open(context) }
        view.findViewById<View>(R.id.you_private_locations).setOnClickListener { (activity as? MainActivity)?.showPrivacy() }
        view.findViewById<View>(R.id.you_donate).apply {
            visibility = if (BuildConfig.DONATE_LINK) View.VISIBLE else View.GONE
            setOnClickListener { openWebPage("/about#funding") }
        }
        view.findViewById<View>(R.id.you_about).setOnClickListener { openWebPage("/about") }
        view.findViewById<View>(R.id.you_help).setOnClickListener { openWebPage("/help") }
        view.findViewById<View>(R.id.you_contacts).setOnClickListener { openWebPage("/contacts") }
        view.findViewById<View>(R.id.you_privacy).setOnClickListener { openWebPage("/privacy") }
        view.findViewById<TextView>(R.id.you_version).text = getString(R.string.menu_version, HoldMyTrackApi.appVersion)
    }

    private fun openWebPage(path: String) {
        CustomTabsIntent.Builder().build().launchUrl(requireContext(), HoldMyTrackApi.webPageUri(path))
    }
}
