import { useMemo } from 'react';
import { fold, SearchPicker, type PickerOption } from './SearchPicker';
import { TIMEZONES, timezonesByOffset, type TimezoneInfo } from './timezones';

function toOption(tz: TimezoneInfo): PickerOption {
  const full = tz.offsetLabel.slice(3).replace('\u2212', '-'); // "-05:00", "+05:30"
  const short = full.replace(/^([+-])0?(\d+):00$/, '$1$2').replace(/^([+-])0(\d)/, '$1$2'); // "-5", "+5:30"
  // Typed exactly, an offset floats its zones to the top the way an ISO code does for Country
  // ("gmt+1" is Berlin, not also Auckland's "+13:00" or India's "+05:30").
  const offsets = [full, short].flatMap((o) => [`gmt${o}`, `utc${o}`, o]);
  if (tz.offsetMinutes === 0) offsets.push('gmt', 'utc');
  return {
    value: tz.id,
    label: tz.place,
    detail: tz.region,
    leading: <span className="search-picker__offset">({tz.offsetLabel})</span>,
    // The raw IANA name is searchable too ("america/new_york").
    keywords: [fold(tz.id), ...offsets],
    exact: offsets,
  };
}

export interface TimezonePickerProps {
  /** An IANA zone name. */
  value: string;
  onChange: (id: string) => void;
  labelledBy: string;
}

/** Settings' Timezone field: SearchPicker rows of (GMT offset) · place · region, ordered west
 *  to east by today's offset. */
export function TimezonePicker({ value, onChange, labelledBy }: TimezonePickerProps) {
  // Offsets are computed once per page visit — close enough to "now"; a DST change mid-visit
  // only mislabels the offset until the next one. A saved zone this browser's list lacks (some
  // engines leave out "UTC" itself) is added, so it still reads as a normal, offset-sorted row.
  const unlisted = TIMEZONES.includes(value) ? '' : value;
  const options = useMemo(() => timezonesByOffset(unlisted ? [unlisted] : []).map(toOption), [unlisted]);
  return (
    <SearchPicker
      options={options}
      value={value}
      onChange={onChange}
      labelledBy={labelledBy}
      searchLabel="Search timezones"
      noun="timezone"
    />
  );
}
