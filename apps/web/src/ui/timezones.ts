/**
 * Every IANA timezone the browser knows about, for Settings' Timezone field
 * (SettingsPage.tsx). Unlike countries.ts's ISO-code list (generated once, offline, since a
 * country code needs a separate display-name lookup step), an IANA zone name is already the
 * string worth showing, and `Intl.supportedValuesOf` reads live from the browser's own tz
 * database rather than a list this file would otherwise have to keep in sync by hand.
 */
export const TIMEZONES: string[] =
  typeof Intl.supportedValuesOf === 'function' ? Intl.supportedValuesOf('timeZone') : ['UTC'];
