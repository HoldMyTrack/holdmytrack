import { useSyncExternalStore } from 'react';

export type Theme = 'light' | 'dark';

const DARK_QUERY = '(prefers-color-scheme: dark)';

/** The event the header's theme control (static/theme.js) fires on `document` after it
 *  changes `<html data-theme>`. */
const THEME_CHANGE_EVENT = 'hmt:themechange';

/** The theme the page is drawn in: `<html data-theme>` when the account menu set one (the
 *  server writes it from the hmt_theme cookie), otherwise the OS's preference — the same
 *  choice tokens.css makes. */
export function currentTheme(): Theme {
  const explicit = document.documentElement.dataset.theme;
  if (explicit === 'light' || explicit === 'dark') return explicit;
  return window.matchMedia(DARK_QUERY).matches ? 'dark' : 'light';
}

/** currentTheme, re-rendering when either the OS preference or the account menu's choice
 *  changes. */
export function useTheme(): Theme {
  return useSyncExternalStore((onChange) => {
    const list = window.matchMedia(DARK_QUERY);
    list.addEventListener('change', onChange);
    document.addEventListener(THEME_CHANGE_EVENT, onChange);
    return () => {
      list.removeEventListener('change', onChange);
      document.removeEventListener(THEME_CHANGE_EVENT, onChange);
    };
  }, currentTheme);
}
