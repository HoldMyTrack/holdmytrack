package dev.holdmytrack.android.panel

import android.content.res.Resources
import android.graphics.PointF
import android.view.MotionEvent
import android.view.View
import android.view.inputmethod.InputMethodManager
import android.widget.Button
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AlertDialog
import androidx.core.widget.doAfterTextChanged
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import com.google.android.material.slider.Slider
import com.google.android.material.textfield.TextInputEditText
import dev.holdmytrack.android.R
import dev.holdmytrack.android.map.Circle
import dev.holdmytrack.android.map.CircleHit
import dev.holdmytrack.android.map.PrivateLocationsOverlay
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.PrivateLocation
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.recording.RecordingFormat
import org.maplibre.android.camera.CameraUpdateFactory
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.Style
import java.text.NumberFormat
import kotlin.math.roundToInt

/**
 * The Private location editor on the map — the web's `apps/web/src/ui/PrivateLocationsPanel.tsx`
 * window (`docs/SPEC.md` FR-8.1), opened from the Privacy screen (`PrivacyActivity`): the
 * account's Private locations, circles whose contents are clipped off the ends of every track,
 * drawn on the map (`map/PrivateLocationsOverlay`), and one of them — or a new one — in a card
 * along the bottom: Name, Radius, Delete for a saved one, Cancel and Save. A new one is a 200 m
 * circle at the map's centre, flying in first when the map is too far out to place one
 * sensibly; moving it is dragging its handle, and another circle tapped on the map opens that
 * one instead. Saving or deleting one reprocesses the activities it touches ([onChanged]).
 *
 * The map is this editor's from [start] to [stop], which clears the circles and throws an
 * unsaved draft away; the editor closing ([onEditorClose]) is the end of it. The demo account
 * never opens it — the Privacy screen only lists its locations.
 */
class PrivateLocationEditor(
    private val editor: View,
    private val map: () -> MapLibreMap?,
    private val style: () -> Style?,
    /** The editor opened: the sheet collapses, so it doesn't cover the circle being edited. */
    private val onEditorOpen: () -> Unit,
    /** The editor closed. */
    private val onEditorClose: () -> Unit,
    /** A location was saved or deleted. */
    private val onChanged: () -> Unit,
    /** The middle of the map left showing under the editor and above the collapsed panel, in
     *  map pixels — where a circle being edited is brought to, so the editor never covers it. */
    private val openAreaCenterY: () -> Float,
) {
    private val context = editor.context
    private val res = context.resources
    private val density = res.displayMetrics.density

    private val editorTitle: TextView = editor.findViewById(R.id.private_editor_title)
    private val nameField: TextInputEditText = editor.findViewById(R.id.private_editor_name)
    private val radiusReadout: TextView = editor.findViewById(R.id.private_editor_radius)
    private val slider: Slider = editor.findViewById(R.id.private_editor_slider)
    private val error: TextView = editor.findViewById(R.id.private_editor_error)
    private val cancel: Button = editor.findViewById(R.id.private_editor_cancel)
    private val save: Button = editor.findViewById(R.id.private_editor_save)
    private val delete: Button = editor.findViewById(R.id.private_editor_delete)

    private data class Draft(val id: String?, val name: String, val lon: Double, val lat: Double, val radiusM: Int)

    private var locations: List<PrivateLocation>? = null
    private var draft: Draft? = null
    private var busy = false
    private var shown = false
    private var dragging = false

    /** What [start] was asked to open, until the list and the map are both in: an id, or
     *  [NEW] for a new one. */
    private var pending: String? = null

    private val readOnly: Boolean
        get() = Session.isDemo

    init {
        cancel.setOnClickListener { closeEditor() }
        delete.setOnClickListener { locations?.firstOrNull { it.id == draft?.id }?.let(::confirmDelete) }
        save.setOnClickListener { save() }
        slider.addOnChangeListener { _, value, fromUser ->
            if (!fromUser) return@addOnChangeListener
            draft = draft?.copy(radiusM = value.roundToInt())
            renderEditor()
            draw()
        }
        nameField.doAfterTextChanged { text ->
            val current = draft ?: return@doAfterTextChanged
            if (current.name != text.toString()) {
                draft = current.copy(name = text.toString())
                renderEditor()
            }
        }
    }

    val isEditing: Boolean
        get() = editor.visibility == View.VISIBLE

    /** Whether the map is this editor's, from [start] to [stop]. */
    val isShowing: Boolean
        get() = shown

    /** Takes the map: reads the list, draws it, and opens [open] — a saved location's id, or
     *  [NEW] — once both the list and the map are in. A list that can't be read ends it. */
    fun start(open: String) {
        shown = true
        pending = open
        locations = null
        HoldMyTrackApi.privateLocations { result ->
            if (!shown) return@privateLocations
            result.onSuccess {
                locations = it
                draw()
                openPending()
            }.onFailure {
                Toast.makeText(context, it.message?.takeIf { m -> m.isNotBlank() } ?: res.getString(R.string.map_unreachable), Toast.LENGTH_LONG).show()
                stop()
            }
        }
    }

    /** The map's style is (again) in: the circles go back on it, and what was asked for opens. */
    fun onStyleReady() {
        if (!shown) return
        draw()
        openPending()
    }

    /** Gives the map back — the editor closed, or another map mode: the circles and any unsaved
     *  draft go. */
    fun stop() {
        shown = false
        pending = null
        dragging = false
        draft = null
        hideEditor()
        style()?.let(PrivateLocationsOverlay::clear)
    }

    private fun openPending() {
        val id = pending ?: return
        val list = locations ?: return
        val instance = map() ?: return
        if (style() == null) return
        pending = null
        if (id == NEW) {
            create()
            return
        }
        val location = list.firstOrNull { it.id == id }
        if (location == null) {
            stop()
            return
        }
        open(location)
        bringIntoView(LatLng(location.lat, location.lon), maxOf(instance.cameraPosition.zoom, OPEN_ZOOM))
    }

    /** A tap on the map while the editor has it — another saved circle opens that one. */
    fun onMapTap(point: LatLng) {
        val instance = map() ?: return
        if (busy) return
        val hit = PrivateLocationsOverlay.hit(instance, instance.projection.toScreenLocation(point), density)
        if (hit is CircleHit.Saved) locations?.firstOrNull { it.id == hit.id }?.let(::open)
    }

    /**
     * The map's own touches, before it pans: a press on the handle of the circle being edited
     * starts a drag that moves the circle with the finger, and the map stays put meanwhile —
     * the web's `dragPan.disable()`. One finger only. Returns whether the touch was taken.
     */
    fun onMapTouch(event: MotionEvent): Boolean {
        val instance = map() ?: return false
        if (!shown || readOnly || draft == null || busy) return false
        when (event.actionMasked) {
            MotionEvent.ACTION_DOWN -> {
                dragging = PrivateLocationsOverlay.onHandle(instance, PointF(event.x, event.y), density)
                return dragging
            }
            MotionEvent.ACTION_POINTER_DOWN -> {
                dragging = false
                return false
            }
            MotionEvent.ACTION_MOVE -> {
                if (!dragging) return false
                val at = instance.projection.fromScreenLocation(PointF(event.x, event.y))
                draft = draft?.copy(lon = at.longitude, lat = at.latitude)
                renderEditor()
                draw()
                return true
            }
            MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                val was = dragging
                dragging = false
                return was
            }
        }
        return dragging
    }

    private fun create() {
        val instance = map() ?: return
        val center = instance.cameraPosition.target ?: return
        draft = Draft(null, "", center.longitude, center.latitude, DEFAULT_RADIUS_M)
        showEditor()
        val zoom = instance.cameraPosition.zoom
        bringIntoView(center, if (zoom < PrivateLocationsOverlay.MIN_PLACE_ZOOM) PrivateLocationsOverlay.MIN_PLACE_ZOOM + 2 else zoom)
    }

    /**
     * The camera onto [target] at [zoom], then [target] moved down into the map left showing
     * under the editor. The web centres a new circle on the map; here the editor covers the
     * top of the screen, and the centre of the map sits under its bottom edge, the handle
     * half hidden.
     */
    private fun bringIntoView(target: LatLng, zoom: Double) {
        val instance = map() ?: return
        editor.post {
            val shift = openAreaCenterY() - instance.height / 2f
            instance.animateCamera(
                CameraUpdateFactory.newLatLngZoom(target, zoom),
                CAMERA_MS,
                object : MapLibreMap.CancelableCallback {
                    override fun onFinish() = instance.scrollBy(0f, shift, CAMERA_MS / 2L)
                    override fun onCancel() = Unit
                },
            )
        }
    }

    private fun open(location: PrivateLocation) {
        draft = Draft(location.id, location.name, location.lon, location.lat, location.radiusM)
        showEditor()
    }

    private fun showEditor() {
        val current = draft ?: return
        editorTitle.setText(if (current.id == null) R.string.private_new_title else R.string.private_edit_title)
        nameField.setText(current.name)
        slider.value = current.radiusM.coerceIn(PrivateLocation.MIN_RADIUS_M, PrivateLocation.MAX_RADIUS_M).toFloat()
        showError(null)
        delete.visibility = if (current.id == null) View.GONE else View.VISIBLE
        editor.visibility = View.VISIBLE
        renderEditor()
        draw()
        onEditorOpen()
    }

    private fun closeEditor() {
        if (busy) return
        draft = null
        hideEditor()
    }

    private fun hideEditor() {
        if (editor.visibility != View.VISIBLE) return
        context.getSystemService(InputMethodManager::class.java)?.hideSoftInputFromWindow(editor.windowToken, 0)
        editor.findFocus()?.clearFocus()
        editor.visibility = View.GONE
        onEditorClose()
    }

    /** Unchanged from what's saved — the web's `sameDraft`; a new one is always a change. */
    private val dirty: Boolean
        get() {
            val current = draft ?: return false
            val saved = current.id?.let { id -> locations?.firstOrNull { it.id == id } } ?: return true
            return current.name.trim() != saved.name || current.lon != saved.lon || current.lat != saved.lat || current.radiusM != saved.radiusM
        }

    private fun save() {
        val current = draft ?: return
        busy = true
        showError(null)
        renderEditor()
        HoldMyTrackApi.savePrivateLocation(current.id, current.name.trim(), current.lon, current.lat, current.radiusM) { result ->
            busy = false
            result.onSuccess { savedLocation ->
                val list = locations.orEmpty()
                locations = if (current.id == null) list + savedLocation else list.map { if (it.id == savedLocation.id) savedLocation else it }
                draft = null
                onChanged()
                hideEditor()
            }.onFailure {
                showError(it.message?.takeIf { m -> m.isNotBlank() } ?: res.getString(R.string.edit_save_failed))
                renderEditor()
            }
        }
    }

    private fun confirmDelete(location: PrivateLocation) {
        val dialog = MaterialAlertDialogBuilder(context, R.style.ThemeOverlay_HoldMyTrack_Dialog_Destructive)
            .setTitle(R.string.private_delete_title)
            .setMessage(R.string.private_delete_body)
            .setPositiveButton(R.string.panel_delete_confirm, null)
            .setNegativeButton(android.R.string.cancel, null)
            .create()
        dialog.setOnShowListener {
            val confirm = dialog.getButton(AlertDialog.BUTTON_POSITIVE)
            confirm.setOnClickListener {
                confirm.isEnabled = false
                confirm.setText(R.string.panel_deleting)
                HoldMyTrackApi.deletePrivateLocation(location.id) { result ->
                    result.onSuccess {
                        dialog.dismiss()
                        locations = locations.orEmpty().filterNot { it.id == location.id }
                        draft = null
                        onChanged()
                        hideEditor()
                    }.onFailure {
                        confirm.isEnabled = true
                        confirm.setText(R.string.panel_delete_confirm)
                        dialog.setMessage(res.getString(R.string.private_delete_failed, it.message.orEmpty()))
                    }
                }
            }
        }
        dialog.show()
    }

    private fun draw() {
        val loaded = style() ?: return
        if (!shown) return
        val current = draft
        PrivateLocationsOverlay.set(
            loaded,
            locations.orEmpty().map { Circle(it.id, it.lon, it.lat, it.radiusM) },
            current?.let { Circle(it.id, it.lon, it.lat, it.radiusM) },
        )
    }

    private fun renderEditor() {
        val current = draft ?: return
        radiusReadout.text = radius(current.radiusM)
        save.isEnabled = !busy && dirty
        cancel.isEnabled = !busy
        delete.isEnabled = !busy
        save.setText(if (busy) R.string.edit_saving else R.string.edit_save)
        slider.isEnabled = !busy
        nameField.isEnabled = !busy
    }

    private fun showError(message: String?) {
        error.text = message
        error.visibility = if (message == null) View.GONE else View.VISIBLE
    }

    private fun radius(meters: Int) = radius(res, meters)

    companion object {
        /** [start]'s "a new one". */
        const val NEW = ""

        /** "200 m", or in feet for the imperial countries — the web's radius readout. */
        fun radius(res: Resources, meters: Int): String {
            val imperial = RecordingFormat.imperial()
            val value = if (imperial) meters * FEET_PER_METER else meters.toDouble()
            val number = NumberFormat.getIntegerInstance(res.configuration.locales[0]).format(value.roundToInt())
            return "$number " + res.getString(if (imperial) R.string.panel_unit_ft else R.string.panel_unit_m)
        }

        private const val DEFAULT_RADIUS_M = 200
        private const val OPEN_ZOOM = 14.0
        private const val FEET_PER_METER = 3.28084
        private const val CAMERA_MS = 600
    }
}
