import { useSyncExternalStore } from 'react';

/** Whether a CSS media query currently matches, re-rendering when that changes (rotating a
 *  phone, resizing a window across the breakpoint). For the few places a phone needs a
 *  different *component*, not just different styling — everything else stays in index.css's
 *  own `@media (max-width: 768px)` layer. */
export function useMediaQuery(query: string): boolean {
  return useSyncExternalStore(
    (onChange) => {
      const list = window.matchMedia(query);
      list.addEventListener('change', onChange);
      return () => list.removeEventListener('change', onChange);
    },
    () => window.matchMedia(query).matches,
  );
}

/** index.css's phone breakpoint — keep in step with its `@media (max-width: 768px)` rules. */
export const MOBILE_QUERY = '(max-width: 768px)';
