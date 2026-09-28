import { useSyncExternalStore } from 'react';

export type Theme = 'light' | 'dark';

const DARK_QUERY = '(prefers-color-scheme: dark)';

/** The theme the page is drawn in: `<html data-theme>` when Settings' Theme control pinned
 *  one (templates/theme.html writes it from localStorage before first paint), otherwise the
 *  OS's preference — the same choice tokens.css makes. */
export function currentTheme(): Theme {
  const explicit = document.documentElement.dataset.theme;
  if (explicit === 'light' || explicit === 'dark') return explicit;
  return window.matchMedia(DARK_QUERY).matches ? 'dark' : 'light';
}

/** currentTheme, re-rendering when the OS preference changes. A pinned choice can't change
 *  while the map is open: the control is on the Settings page, and coming back is a page
 *  load. */
export function useTheme(): Theme {
  return useSyncExternalStore((onChange) => {
    const list = window.matchMedia(DARK_QUERY);
    list.addEventListener('change', onChange);
    return () => list.removeEventListener('change', onChange);
  }, currentTheme);
}
