/**
 * Every IANA timezone the browser knows about, for Settings' Timezone field
 * (SettingsPage.tsx). Unlike countries.ts's ISO-code list (generated once, offline, since a
 * country code needs a separate display-name lookup step), an IANA zone name is already the
 * string worth showing, and `Intl.supportedValuesOf` reads live from the browser's own tz
 * database rather than a list this file would otherwise have to keep in sync by hand.
 */
export const TIMEZONES: string[] =
  typeof Intl.supportedValuesOf === 'function' ? Intl.supportedValuesOf('timeZone') : ['UTC'];

export interface TimezoneInfo {
  /** The IANA name — what the account stores. */
  id: string;
  /** Minutes east of GMT, right now. */
  offsetMinutes: number;
  /** "GMT+05:30", "GMT−05:00", "GMT+00:00" — a true minus sign, always two-digit hours. */
  offsetLabel: string;
  /** The last segment, underscores as spaces: "New York", "Buenos Aires". */
  place: string;
  /** Everything before it: "America", "America / Argentina"; `''` for "UTC". */
  region: string;
}

/** A zone's offset from GMT at `at`, in minutes, read off `Intl`'s own "GMT-05:00" rendering
 *  — which it writes as a bare "GMT" at zero. */
function offsetMinutes(timeZone: string, at: Date): number {
  let name: string | undefined;
  try {
    name = new Intl.DateTimeFormat('en-US', { timeZone, timeZoneName: 'longOffset' })
      .formatToParts(at)
      .find((p) => p.type === 'timeZoneName')?.value;
  } catch {
    return 0; // a saved name this browser's tz database rejects — shown, just not placed
  }
  const m = name?.match(/GMT([+-])(\d{1,2})(?::(\d{2}))?/);
  if (!m) return 0;
  return (m[1] === '-' ? -1 : 1) * (Number(m[2]) * 60 + Number(m[3] ?? 0));
}

function formatOffset(minutes: number): string {
  const abs = Math.abs(minutes);
  const hh = String(Math.floor(abs / 60)).padStart(2, '0');
  const mm = String(abs % 60).padStart(2, '0');
  return `GMT${minutes < 0 ? '−' : '+'}${hh}:${mm}`;
}

export function describeTimezone(id: string, at = new Date()): TimezoneInfo {
  const offset = offsetMinutes(id, at);
  const parts = id.split('/').map((p) => p.replace(/_/g, ' '));
  return {
    id,
    offsetMinutes: offset,
    offsetLabel: formatOffset(offset),
    place: parts[parts.length - 1]!,
    region: parts.slice(0, -1).join(' / '),
  };
}

/** TIMEZONES (plus any `extra` ids) west to east by their offset *today* — so a DST zone sorts
 *  by whichever side of its change today falls on, which is also what "GMT" in brackets means
 *  right now — then A–Z by place within one offset. */
export function timezonesByOffset(extra: string[] = [], at = new Date()): TimezoneInfo[] {
  return [...TIMEZONES, ...extra].map((id) => describeTimezone(id, at)).sort(
    (a, b) => a.offsetMinutes - b.offsetMinutes || a.place.localeCompare(b.place),
  );
}
