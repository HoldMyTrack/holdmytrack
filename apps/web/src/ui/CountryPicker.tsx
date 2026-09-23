import { COUNTRIES } from './countries';
import { SearchPicker, type PickerOption } from './SearchPicker';

/** A regional-indicator flag emoji for an ISO 3166-1 alpha-2 code — each letter maps onto its
 *  own "regional indicator symbol" code point, and the pair renders as that country's flag.
 *  Derived, not stored, so countries.ts stays the single list of codes. */
function flagEmoji(code: string): string {
  return String.fromCodePoint(...[...code.toUpperCase()].map((c) => 0x1f1a5 + c.charCodeAt(0)));
}

/** Whether this platform's emoji font actually draws flags. Windows ships none — it renders a
 *  regional-indicator pair as two plain letters, which inside a round flag badge reads as a
 *  glitch — so there the badge shows the code in text instead. Measured once, by drawing a
 *  flag to a canvas and checking for any colored pixel (the letter fallback is monochrome). */
function supportsFlagEmoji(): boolean {
  const ctx = document.createElement('canvas').getContext('2d', { willReadFrequently: true });
  if (!ctx) return false;
  ctx.canvas.width = ctx.canvas.height = 20;
  ctx.font = '16px sans-serif';
  ctx.textBaseline = 'top';
  ctx.fillText(flagEmoji('FR'), 0, 0);
  const data = ctx.getImageData(0, 0, 20, 20).data;
  for (let i = 0; i < data.length; i += 4) {
    const r = data[i]!, g = data[i + 1]!, b = data[i + 2]!;
    if (data[i + 3]! > 0 && (Math.abs(r - g) > 20 || Math.abs(g - b) > 20)) return true;
  }
  return false;
}

function Flag({ code, emoji }: { code: string; emoji: boolean }) {
  if (!emoji) {
    return (
      <span className="search-picker__flag search-picker__flag--text" aria-hidden="true">
        {code}
      </span>
    );
  }
  return (
    <span className="search-picker__flag" aria-hidden="true">
      <span className="search-picker__flag-emoji">{flagEmoji(code)}</span>
    </span>
  );
}

let options: PickerOption[] | undefined;
let unset: PickerOption | undefined;

/** Built once, on first render — COUNTRIES never changes, and the flag check needs a DOM. */
function countryOptions(): [PickerOption[], PickerOption] {
  if (!options || !unset) {
    const emoji = supportsFlagEmoji();
    options = COUNTRIES.map((c) => ({
      value: c.code,
      label: c.name,
      detail: c.code,
      leading: <Flag code={c.code} emoji={emoji} />,
      exact: [c.code],
    }));
    unset = {
      value: '',
      label: 'Not set (metric)',
      detail: '',
      leading: <span className="search-picker__flag search-picker__flag--none" aria-hidden="true" />,
    };
  }
  return [options, unset];
}

export interface CountryPickerProps {
  /** An ISO 3166-1 alpha-2 code from countries.ts, or `''` for unset. */
  value: string;
  onChange: (code: string) => void;
  labelledBy: string;
}

/** Settings' Country field: SearchPicker rows of round flag · name · ISO code, A–Z, with
 *  "Not set (metric)" first. */
export function CountryPicker({ value, onChange, labelledBy }: CountryPickerProps) {
  const [list, unsetOption] = countryOptions();
  return (
    <SearchPicker
      options={list}
      unsetOption={unsetOption}
      value={value}
      onChange={onChange}
      labelledBy={labelledBy}
      searchLabel="Search countries"
      noun="country"
    />
  );
}
