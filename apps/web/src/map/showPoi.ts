/**
 * The Show POI toggle's state (SPEC.md FR-15.2), remembered per browser: a view preference,
 * like which panel is open, not an account setting. Storage can be unavailable (a private
 * window, blocked site data) — then the toggle still works, it just starts off every time.
 */
const KEY = 'hmt.showPoi';

export function readShowPoi(): boolean {
  try {
    return window.localStorage.getItem(KEY) === '1';
  } catch {
    return false;
  }
}

export function writeShowPoi(show: boolean): void {
  try {
    window.localStorage.setItem(KEY, show ? '1' : '0');
  } catch {
    // Not remembered this time; nothing else depends on it.
  }
}