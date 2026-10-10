package dev.holdmytrack.android

import android.os.Bundle
import android.text.format.DateFormat
import android.view.View
import android.widget.HorizontalScrollView
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import com.google.android.material.button.MaterialButtonToggleGroup
import dev.holdmytrack.android.net.ActivityDay
import dev.holdmytrack.android.net.HoldMyTrackApi
import dev.holdmytrack.android.net.Session
import dev.holdmytrack.android.net.TrendPeriod
import dev.holdmytrack.android.panel.PanelFormat
import dev.holdmytrack.android.profile.GridStats
import dev.holdmytrack.android.profile.ProfileStats
import dev.holdmytrack.android.profile.Shade
import dev.holdmytrack.android.profile.TrendsChartView
import dev.holdmytrack.android.profile.YearGridView
import dev.holdmytrack.android.recording.RecordingFormat
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import kotlin.math.roundToLong

/**
 * Activity graph & trends, from the You tab's numbers: the web's `/profile` page (`services/server/internal/httpapi/profile_page.go`):
 * the four all-time stat cards, one activity grid per year back to the first activity, most
 * recent first, and Trends. Two reads cover it, as the web page's queries do — every day with
 * activity up to the end of this year (`HoldMyTrackApi.activityDays`), from which
 * `ProfileStats` derives every number and grid, and the trailing 12 months' weeks or months
 * (`HoldMyTrackApi.activityTrends`). Switching the shading redraws from the days in hand;
 * switching Week/Month reads Trends again.
 */
class ProfileActivity : AppCompatActivity() {


    private lateinit var graphStatus: TextView
    private lateinit var graph: View
    private lateinit var years: LinearLayout
    private lateinit var legend: TextView
    private lateinit var trends: View
    private lateinit var trendsChart: TrendsChartView
    private lateinit var trendsChartBox: View
    private lateinit var trendsFrom: TextView
    private lateinit var trendsTo: TextView
    private lateinit var trendsDetail: TextView
    private lateinit var trendsEmpty: TextView

    private var shade = Shade.COUNT
    private var bucket = BUCKET_WEEK

    private var days: List<ActivityDay>? = null
    private var firstYear = 0
    private var today: LocalDate = LocalDate.now()
    private var periods: List<TrendPeriod>? = null
    private var graphError: String? = null
    private var trendsError: String? = null
    private var trendsRequest = 0

    /** The tapped day, in whichever year's grid, or null. */
    private var selectedDay: LocalDate? = null

    private class YearHolder(val year: Int, val days: List<ActivityDay>, val stats: GridStats, val root: View) {
        val grid: YearGridView = root.findViewById(R.id.year_grid)
        val scroll: HorizontalScrollView = root.findViewById(R.id.year_scroll)
        val detail: TextView = root.findViewById(R.id.year_detail)
    }

    private val yearHolders = mutableListOf<YearHolder>()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_profile)


        graphStatus = findViewById(R.id.graph_status)
        graph = findViewById(R.id.graph)
        years = findViewById(R.id.years)
        legend = findViewById(R.id.legend)
        trends = findViewById(R.id.trends)
        trendsChart = findViewById(R.id.trends_chart)
        trendsChartBox = findViewById(R.id.trends_chart_box)
        trendsFrom = findViewById(R.id.trends_from)
        trendsTo = findViewById(R.id.trends_to)
        trendsDetail = findViewById(R.id.trends_detail)
        trendsEmpty = findViewById(R.id.trends_empty)

        savedInstanceState?.let { state ->
            shade = if (state.getString(STATE_SHADE) == Shade.DISTANCE.name) Shade.DISTANCE else Shade.COUNT
            bucket = if (state.getString(STATE_BUCKET) == BUCKET_MONTH) BUCKET_MONTH else BUCKET_WEEK
        }

        findViewById<MaterialButtonToggleGroup>(R.id.shade_toggle).apply {
            check(if (shade == Shade.COUNT) R.id.shade_count else R.id.shade_distance)
            addOnButtonCheckedListener { _, id, checked ->
                if (!checked) return@addOnButtonCheckedListener
                shade = if (id == R.id.shade_distance) Shade.DISTANCE else Shade.COUNT
                renderShading()
            }
        }
        findViewById<MaterialButtonToggleGroup>(R.id.bucket_toggle).apply {
            check(if (bucket == BUCKET_WEEK) R.id.bucket_week else R.id.bucket_month)
            addOnButtonCheckedListener { _, id, checked ->
                if (!checked) return@addOnButtonCheckedListener
                bucket = if (id == R.id.bucket_month) BUCKET_MONTH else BUCKET_WEEK
                loadTrends()
            }
        }
        trendsChart.onBarTap = { i -> selectBar(i) }
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        outState.putString(STATE_SHADE, shade.name)
        outState.putString(STATE_BUCKET, bucket)
    }

    override fun onResume() {
        super.onResume()
        loadGraph()
        loadTrends()
    }

    // The activity graph.

    /** Every day with activity through the end of this year, in the account's timezone — the
     *  server files each day under it, so "this year" and "today" are its, not the phone's. */
    private fun loadGraph() {
        graphError = null
        if (days == null) showStatus()
        today = LocalDate.now(accountZone())
        HoldMyTrackApi.activityDays(EPOCH_DAY, "${today.year}-12-31") { result ->
            result.onSuccess { page ->
                val earliest = page.earliest?.let { runCatching { LocalDate.parse(it) }.getOrNull() }
                firstYear = minOf(earliest?.year ?: today.year, today.year)
                days = page.days
                renderGraph()
            }.onFailure { e -> graphError = e.message.orEmpty() }
            showStatus()
        }
    }

    private fun renderGraph() {
        val all = days ?: return
        val stats = ProfileStats.statsOf(all)
        setStat(R.id.stat_activities, R.string.profile_card_activities, PanelFormat.count(resources, stats.count))
        setStat(R.id.stat_distance, R.string.profile_card_distance, PanelFormat.totalDistance(resources, stats.distanceMeters))
        setStat(R.id.stat_active_days, R.string.profile_card_active_days, PanelFormat.count(resources, stats.activeDays))
        setStat(R.id.stat_streak, R.string.profile_card_longest_streak, plural(R.plurals.profile_days, stats.longestStreakDays))

        years.removeAllViews()
        yearHolders.clear()
        for (year in today.year downTo firstYear) {
            val yearDays = ProfileStats.daysInYear(all, year)
            val holder = YearHolder(year, yearDays, ProfileStats.statsOf(yearDays), layoutInflater.inflate(R.layout.item_profile_year, years, false))
            holder.root.findViewById<TextView>(R.id.year_title).text = year.toString()
            val statsLine = yearStatsLine(holder.stats)
            holder.root.findViewById<TextView>(R.id.year_stats).text = statsLine
            holder.grid.contentDescription = getString(R.string.profile_year_grid_label, year, statsLine)
            holder.grid.onDayTap = { day -> selectDay(day) }
            years.addView(holder.root)
            yearHolders += holder
            // This year opens on today, at the grid's right edge, rather than on January.
            if (year == today.year) {
                holder.scroll.post { holder.scroll.scrollTo(holder.grid.xOf(today) + CELL_PITCH_DP.dp() - holder.scroll.width, 0) }
            }
        }
        if (selectedDay?.let { it.year !in firstYear..today.year } == true) selectedDay = null
        renderShading()
        graph.visibility = View.VISIBLE
    }

    private fun renderShading() {
        legend.setText(if (shade == Shade.COUNT) R.string.profile_legend_count else R.string.profile_legend_distance)
        for (h in yearHolders) h.grid.layout = ProfileStats.layout(h.year, h.days, shade)
        renderSelectedDay()
    }

    private fun selectDay(day: LocalDate?) {
        selectedDay = day
        renderSelectedDay()
    }

    private fun renderSelectedDay() {
        val sel = selectedDay
        for (h in yearHolders) {
            val mine = sel?.takeIf { it.year == h.year }
            h.grid.selected = mine
            h.detail.visibility = if (mine == null) View.GONE else View.VISIBLE
            if (mine == null) continue
            val date = DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM).withLocale(locale()).format(mine)
            val d = h.days.firstOrNull { it.date == mine.toString() }
            h.detail.text = if (d == null || d.count == 0) {
                getString(R.string.profile_cell_empty, date)
            } else {
                getString(R.string.profile_cell, date, plural(R.plurals.profile_activities, d.count), PanelFormat.distance(resources, d.distanceMeters))
            }
        }
    }

    private fun setStat(id: Int, label: Int, value: String) {
        val card = findViewById<View>(id)
        card.findViewById<TextView>(R.id.stat_label).setText(label)
        card.findViewById<TextView>(R.id.stat_value).text = value
    }

    private fun yearStatsLine(s: GridStats) = getString(
        R.string.profile_year_stats,
        plural(R.plurals.profile_activities, s.count),
        PanelFormat.totalDistance(resources, s.distanceMeters),
        plural(R.plurals.profile_active_days, s.activeDays),
        plural(R.plurals.profile_days, s.longestStreakDays),
    )

    // Trends.

    private fun loadTrends() {
        val request = ++trendsRequest
        HoldMyTrackApi.activityTrends(bucket) { result ->
            // A Week/Month switch made while this was in flight has already asked again.
            if (request != trendsRequest) return@activityTrends
            trendsError = null
            result.onSuccess { list ->
                periods = list
                renderTrends()
            }.onFailure { e -> trendsError = e.message.orEmpty() }
            showStatus()
        }
    }

    private fun renderTrends() {
        val list = periods ?: return
        trendsChart.heights = ProfileStats.trendHeights(list)
        trendsChart.selected = -1
        trendsDetail.visibility = View.GONE
        // Every period of the window comes back, the empty ones as zeroes.
        val recorded = list.any { it.count > 0 }
        trendsChartBox.visibility = if (recorded) View.VISIBLE else View.GONE
        trendsEmpty.visibility = if (recorded) View.GONE else View.VISIBLE
        if (recorded) {
            trendsFrom.text = shortDate(list.first().periodStart)
            trendsTo.text = shortDate(list.last().periodStart)
        }
        trends.visibility = View.VISIBLE
    }

    private fun selectBar(i: Int) {
        val list = periods ?: return
        trendsChart.selected = i
        val p = list.getOrNull(i)
        trendsDetail.visibility = if (p == null) View.GONE else View.VISIBLE
        if (p == null) return
        trendsDetail.text = getString(
            R.string.profile_trend_bar,
            shortDate(p.periodStart),
            PanelFormat.totalDistance(resources, p.distanceMeters),
            plural(R.plurals.profile_activities, p.count),
            PanelFormat.count(resources, (p.movingSeconds / 3600.0).roundToLong().toInt()),
            elevation(p.elevationGainM),
        )
    }

    // Shared.

    /** "Loading…" until both reads are in; then nothing, or a failure — the graph's first. */
    private fun showStatus() {
        val error = graphError ?: trendsError
        val message = when {
            error != null -> getString(R.string.profile_failed, error)
            days == null || periods == null -> getString(R.string.profile_loading)
            else -> null
        }
        graphStatus.text = message
        graphStatus.visibility = if (message == null) View.GONE else View.VISIBLE
    }

    private fun accountZone(): ZoneId = runCatching { ZoneId.of(Session.timezone) }.getOrDefault(ZoneId.systemDefault())

    private fun locale() = resources.configuration.locales[0]

    private fun plural(id: Int, n: Int) = resources.getQuantityString(id, n, PanelFormat.count(resources, n))

    /** The web's `ShortDate`: "Sep 8", "8 сент.". */
    private fun shortDate(day: String): String = runCatching {
        DateTimeFormatter.ofPattern(DateFormat.getBestDateTimePattern(locale(), "MMMd"), locale()).format(LocalDate.parse(day))
    }.getOrDefault(day)

    /** The web's `FormatElevation`: whole meters, or feet in the imperial countries. */
    private fun elevation(meters: Double): String {
        val imperial = RecordingFormat.imperial()
        val value = if (imperial) (meters / METERS_PER_FOOT).roundToLong() else meters.roundToLong()
        val unit = getString(if (imperial) R.string.panel_unit_ft else R.string.panel_unit_m)
        return "${PanelFormat.count(resources, value.toInt())} $unit"
    }

    private fun Int.dp(): Int = (this * resources.displayMetrics.density).toInt()

    private companion object {
        const val BUCKET_WEEK = "week"
        const val BUCKET_MONTH = "month"
        const val STATE_SHADE = "shade"
        const val STATE_BUCKET = "bucket"
        /** The histogram's `from` for "since the beginning": it returns only days with activity. */
        const val EPOCH_DAY = "1970-01-01"
        const val METERS_PER_FOOT = 0.3048
        /** A grid column's width, cell and gap (`YearGridView`). */
        const val CELL_PITCH_DP = 16
    }
}