import type { ActivityQuery } from '../api';
import type { DateRange } from '../ui/dateMath';

/** What the list and the tracks layer show: the open Story, or the date range, never both. */
export function activityQuery(storyId: string | null, range: DateRange | null): ActivityQuery {
  if (storyId !== null) return { story: storyId };
  return range ? { from: range.from, to: range.to } : {};
}
