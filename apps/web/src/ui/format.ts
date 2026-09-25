import type { UnitSystem } from './units';
import { lang, t, tMaybe } from '../i18n';

/**
 * Display formatting for real activity rows and totals. Kept apart from api.ts so the
 * transport layer stays about the wire shape and nothing else, and apart from units.ts so
 * this stays plain, React-free presentation logic — `system` is just a parameter here, not
 * something this file resolves for itself.
 *
 * Every metric the backend serves is nullable (IMPLEMENTATION.md §3.3), so
 * each of these takes `number | null` and renders an em dash rather than a fabricated zero.
 *
 * Every distance/pace/elevation formatter below takes a `UnitSystem` (units.ts) and returns
 * the **full string including its own unit suffix** ("12.3 km" / "7.6 mi") — never a bare
 * number a caller appends a hardcoded "km"/"m" to. That hardcoded-suffix pattern is exactly
 * what let the map's own ScaleControl drift to a hardcoded, wrong default for so long
 * unnoticed (see useMapInstance.ts) — a formatter that owns its own suffix can't drift that
 * way, since there's nowhere else for the unit to be decided.
 */

const EM_DASH = '—';

const METERS_PER_MILE = 1609.344;
const METERS_PER_FOOT = 0.3048;

export function unitLabel(system: UnitSystem): string {
  return system === 'imperial' ? t('unit.mi') : t('unit.km');
}

/** The short unit `formatElevation` below appends — pulled out so a caller that needs just the
 *  label (PrivateLocationsPanel.tsx's radius readout, a number shown beside a slider)
 *  isn't left duplicating the same ternary. */
export function elevationUnitLabel(system: UnitSystem): string {
  return system === 'imperial' ? t('unit.ft') : t('unit.m');
}

/** Bare numeric conversion, not a formatter — no rounding, no unit suffix. For a meters value
 *  a caller rounds and labels itself (PrivateLocationsPanel.tsx's radius), unlike every
 *  formatter below, which only ever produces a final display string. */
export function metersToFeet(meters: number): number {
  return meters / METERS_PER_FOOT;
}

/**
 * A row's fallback primary line, for an activity with no name set (ActivitiesPanel.tsx
 * prefers `activity.name` when present — §4.7's revised decision). The full local datetime,
 * not a bare date, since it's carrying the whole "when" on its own in that case.
 */
export function formatStartedAt(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString(lang, {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

/** `distanceValue` returns just the number (no unit suffix) — for the one place
 *  (DistanceFilter.tsx's range-slider readout) that wants a single shared suffix at the end
 *  of a range ("5 – 20 km") rather than one per value. Every other caller wants
 *  `formatDistance`, which includes its own suffix. */
export function distanceValue(meters: number, system: UnitSystem): string {
  const converted = system === 'imperial' ? meters / METERS_PER_MILE : meters / 1000;
  return converted.toLocaleString(lang, { minimumFractionDigits: 1, maximumFractionDigits: 1, useGrouping: false });
}

export function formatDistance(meters: number | null, system: UnitSystem): string {
  if (meters === null) return EM_DASH;
  return `${distanceValue(meters, system)} ${unitLabel(system)}`;
}

/** "4:32/km" / "7:17/mi" — TrackProfile.tsx's per-activity pace, converted from the raw
 *  m/s speed value to the pace runners actually think in. Guards zero/negative input rather
 *  than dividing by it — a stray zero shouldn't render as an infinite pace. */
export function formatPace(metersPerSecond: number, system: UnitSystem): string {
  if (metersPerSecond <= 0) return EM_DASH;
  const perUnitMeters = system === 'imperial' ? METERS_PER_MILE : 1000;
  const totalSec = Math.round(perUnitMeters / metersPerSecond);
  const m = Math.floor(totalSec / 60);
  const s = totalSec % 60;
  return `${m}:${s.toString().padStart(2, '0')}/${unitLabel(system)}`;
}

/** "3h 52m" / "43m" / "20s". */
export function formatDuration(seconds: number | null): string {
  if (seconds === null) return EM_DASH;
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (h > 0) return t('duration.hm', { h, m });
  if (m > 0) return t('duration.m', { m });
  return t('duration.s', { s: seconds });
}

/**
 * The range-summary numbers (§4.7's `GET /v1/activities/summary`), rounded for a one-line
 * stats display. These are totals, so they are never null — a sum over no activities is 0.
 *
 * Distance keeps one decimal below 10 (km or mi): rounding a real but small history to a
 * flat "0 km" reads as broken, where "0.5 km" reads as a short one.
 */
export function formatTotalDistance(meters: number, system: UnitSystem): string {
  const converted = system === 'imperial' ? meters / METERS_PER_MILE : meters / 1000;
  return `${converted.toLocaleString(lang, { maximumFractionDigits: converted < 10 ? 1 : 0 })} ${unitLabel(system)}`;
}

export function formatElevation(meters: number, system: UnitSystem): string {
  const converted = system === 'imperial' ? metersToFeet(meters) : meters;
  return `${Math.round(converted).toLocaleString(lang)} ${elevationUnitLabel(system)}`;
}

/** "9 Sep" — the Sync tab's finished rows (SyncTab.tsx) need "which day did this
 *  land on", not a full datetime; `formatStartedAt` above is a row's primary line, this
 *  is a compact subtitle next to a filename that's already the row's primary line. */
export function formatShortDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleDateString(lang, { day: 'numeric', month: 'short' });
}

/** "9 MAR 2026" — the date-range footer's labels (ActivityHistogram.tsx's legend, the mobile
 *  DateRangeSlider.tsx), per main-screen-v6.png. Takes a YYYY-MM-DD day, read as UTC so the
 *  label is exactly that calendar day in every browser time zone. */
export function formatDayLabel(date: string): string {
  const d = new Date(`${date}T00:00:00Z`);
  const month = d.toLocaleDateString(lang, { month: 'short', timeZone: 'UTC' }).toUpperCase();
  return `${d.getUTCDate()} ${month} ${d.getUTCFullYear()}`;
}

/** "412 KB" / "1.4 MB" — a dropped or picked file's own size, before any network transfer
 *  has happened, so this can't come from the backend. Binary (1024-based) units, matching
 *  what every OS file picker and Chrome's own devtools already show for a local file. */
export function formatFileSize(bytes: number): string {
  if (bytes < 1024) return t('size.b', { v: bytes });
  if (bytes < 1024 * 1024) return t('size.kb', { v: Math.round(bytes / 1024) });
  return t('size.mb', { v: (bytes / (1024 * 1024)).toLocaleString(lang, { minimumFractionDigits: 1, maximumFractionDigits: 1 }) });
}

/**
 * Humanizes an `activity_type` value for display — spaces out `snake_case` and title-cases
 * each word — without remapping its meaning. §4.7 resolved `activity_type` as "whatever the
 * source reports, not a controlled vocabulary": a fixed cosmetic mapping (e.g. treating
 * "gravel_cycling" as a synonym for "Ride") would quietly reintroduce the vocabulary this
 * app deliberately doesn't have. "unknown" formats the same as any other value on purpose —
 * it is itself a real value a parser can report, not a special case.
 */
export function formatActivityType(activityType: string): string {
  const translated = tMaybe(`activity_type.${activityType.toLowerCase()}`);
  if (translated) return translated;
  return activityType
    .split('_')
    .filter(Boolean)
    .map((word) => word[0]!.toUpperCase() + word.slice(1))
    .join(' ');
}

/** The schema's `source` values (`IMPLEMENTATION.md` §3.3), said the way a person would say
 *  them — same closed mapping and wording as the Android app's own `sourceName()`
 *  (`SyncStatusActivity.kt`), so a duplicate's origin reads the same on both clients. Unlike
 *  `formatActivityType`, this *is* a fixed vocabulary — `source` is a small enum the ingest
 *  pipeline itself defines, not open text a source can invent. */
export function formatIngestSource(source: string): string {
  switch (source) {
    case 'healthconnect':
      return 'Health Connect';
    case 'healthkit':
      return 'HealthKit';
    case 'upload':
      return t('source.upload_phrase');
    case 'takeout':
      return t('source.takeout_phrase');
    case 'recorded':
      return t('source.recorded_phrase');
    default:
      return source;
  }
}

/** The same `source` values as `formatIngestSource`, but as a short title rather than a
 *  sentence fragment — SyncTab.tsx's titles for synced rows, where a synced row's own
 *  `filename` is a raw external id never meant to be shown directly. Kept as its own switch
 *  rather than stripping `formatIngestSource`'s leading article: the wording itself differs
 *  too ("GPS Logger" vs. "a GPS recording"), not just the article. */
export function formatSourceLabel(source: string): string {
  switch (source) {
    case 'healthconnect':
      return 'Health Connect';
    case 'healthkit':
      return 'HealthKit';
    case 'upload':
      return t('source.upload');
    case 'takeout':
      return 'Google Takeout';
    case 'recorded':
      return t('source.recorded');
    default:
      return source;
  }
}
