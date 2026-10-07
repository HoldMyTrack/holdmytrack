const knownZones = new Map<string, boolean>();

/**
 * `zone` as a `timeZone` option, or undefined (the browser's own) when this browser's tz data
 * doesn't know it — a zone newer than the browser would otherwise throw a RangeError. Every
 * time of an activity's is shown in the zone it was recorded in (IMPLEMENTATION.md §4.30),
 * which the server names from its own, possibly newer, tz data.
 */
export function knownTimeZone(zone: string | undefined): string | undefined {
  if (!zone) return undefined;
  let known = knownZones.get(zone);
  if (known === undefined) {
    try {
      new Intl.DateTimeFormat('en', { timeZone: zone });
      known = true;
    } catch {
      known = false;
    }
    knownZones.set(zone, known);
  }
  return known ? zone : undefined;
}
