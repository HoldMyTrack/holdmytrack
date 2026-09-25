import { useAuth } from '../auth/AuthContext';

export type UnitSystem = 'metric' | 'imperial';

/** The only three countries where everyday distance is customarily miles/feet, not km/m —
 *  everyone else on Earth uses metric for this. Liberia and Myanmar are the two commonly
 *  cited alongside the US; deliberately not a larger "mostly metric but with local quirks"
 *  list, since this only decides which unit a distance/pace/elevation number is *displayed*
 *  in, not anything a country's own official policy needs to be litigated over. */
const IMPERIAL_COUNTRIES = new Set(['US', 'LR', 'MM']);

/** `country` is `''` for an account that hasn't set one yet (UserProfile's own convention,
 *  api.ts) — defaults to metric, matching the only behavior this app had before Settings
 *  existed at all. */
export function unitSystemForCountry(country: string): UnitSystem {
  return IMPERIAL_COUNTRIES.has(country) ? 'imperial' : 'metric';
}

/** A hook, not a prop threaded through every component that formats a distance — every
 *  caller reaches this directly via `useAuth()`, avoiding prop-drilling through chains like
 *  `MapView → ActivitiesPanel → DistanceFilter` for a value that only ever changes when the
 *  Settings page saves a new Country. (The server-rendered pages have their own copy of the
 *  rule, services/server/internal/web/format.go's Imperial.) */
export function useUnitSystem(): UnitSystem {
  const { user } = useAuth();
  return unitSystemForCountry(user.country);
}
