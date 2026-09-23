/** A plain chevron for the icon-only Earlier/Later buttons — ActivityHistogram.tsx's desktop
 *  paging buttons and DateRangeSlider.tsx's phone ones. Text labels live in `aria-label`/
 *  `title`, since both buttons are too narrow for a word. */
export function ChevronIcon({ direction }: { direction: 'left' | 'right' }) {
  const d = direction === 'left' ? 'M9 4l-6 6 6 6' : 'M7 4l6 6-6 6';
  return (
    <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" focusable="false">
      <path d={d} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
