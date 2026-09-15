import { useMemo, useState } from 'react';
import { formatTotalDistance } from './format';
import { useUnitSystem } from './units';
import { useYearGraph } from './useYearGraph';
import { YearGrid, type ShadeBy } from './YearGrid';

/**
 * §4.8's "private, per-user contribution grid" — one stacked block per year, most recent
 * first, down to this account's first activity. Originally built single-user, against the
 * one seeded placeholder user, ahead of real accounts — the bet that the backend's
 * already-`user_id`-scoped queries wouldn't need redoing once real accounts landed (§4.9)
 * paid off unchanged; see IMPLEMENTATION.md §4.8 for the full account.
 *
 * Everything else `profile-v1.png` draws around this panel — the avatar, "Account since,"
 * connected-sources count, Edit profile, Manage sources — is real-accounts chrome and
 * deliberately not built here; see ProfilePage.tsx.
 */
export function ActivityGraph() {
  const [shadeBy, setShadeBy] = useState<ShadeBy>('count');
  const system = useUnitSystem();
  const currentYear = useMemo(() => new Date().getUTCFullYear(), []);

  // Fetched here only to learn `earliest` (so the year list below knows where to stop) and
  // `allTime` (the cards above) — the current year's own YearGrid below fetches the identical
  // pair of requests again for its own grid/stats. A small, accepted duplication: sharing this
  // response with that YearGrid would mean either lifting all per-year fetching up into this
  // component (every YearGrid stops being self-contained, for one year's benefit) or a cache
  // layer neither this app nor its request volume needs yet.
  const { graph: currentYearGraph, stats: currentYearStats, error } = useYearGraph(currentYear);
  const earliestYear = currentYearGraph?.earliest
    ? Number(currentYearGraph.earliest.slice(0, 4))
    : currentYear;

  const years = useMemo(() => {
    const list: number[] = [];
    for (let y = currentYear; y >= earliestYear; y -= 1) list.push(y);
    return list;
  }, [currentYear, earliestYear]);

  const allTime = currentYearStats?.allTime;

  return (
    <section className="activity-graph" aria-label="Activity graph">
      <div className="activity-graph__cards">
        <div className="activity-graph__card">
          <span className="activity-graph__card-label">Activities</span>
          <span className="activity-graph__card-value">{allTime ? allTime.count.toLocaleString() : '—'}</span>
        </div>
        <div className="activity-graph__card">
          <span className="activity-graph__card-label">Distance</span>
          <span className="activity-graph__card-value">
            {allTime ? formatTotalDistance(allTime.distanceMeters, system) : '—'}
          </span>
        </div>
        <div className="activity-graph__card">
          <span className="activity-graph__card-label">Active days</span>
          <span className="activity-graph__card-value">{allTime ? allTime.activeDays.toLocaleString() : '—'}</span>
        </div>
        <div className="activity-graph__card">
          <span className="activity-graph__card-label">Longest streak</span>
          <span className="activity-graph__card-value">
            {allTime ? `${allTime.longestStreakDays.toLocaleString()} days` : '—'}
          </span>
        </div>
      </div>

      <div className="activity-graph__head">
        <div>
          <h2 className="activity-graph__title">Activity grid</h2>
          <p className="activity-graph__subtext">Every day you moved, year by year. Only you can see this.</p>
        </div>
        <div className="activity-graph__shade" role="group" aria-label="Shade grid by">
          <span className="activity-graph__shade-label">Shade by</span>
          <button
            type="button"
            className="activity-graph__shade-btn"
            aria-pressed={shadeBy === 'count'}
            onClick={() => setShadeBy('count')}
          >
            Activities
          </button>
          <button
            type="button"
            className="activity-graph__shade-btn"
            aria-pressed={shadeBy === 'distance'}
            onClick={() => setShadeBy('distance')}
          >
            Distance
          </button>
        </div>
      </div>

      {error && <p className="activity-graph__error">{error}</p>}

      {years.map((year) => (
        <YearGrid key={year} year={year} shadeBy={shadeBy} />
      ))}

      <p className="activity-graph__legend">
        <span>Shaded by {shadeBy === 'count' ? 'activity count' : 'distance'}: less</span>
        <span className="year-grid__cell year-grid__cell--level-0" aria-hidden="true" />
        <span className="year-grid__cell year-grid__cell--level-1" aria-hidden="true" />
        <span className="year-grid__cell year-grid__cell--level-2" aria-hidden="true" />
        <span className="year-grid__cell year-grid__cell--level-3" aria-hidden="true" />
        <span>more</span>
      </p>
    </section>
  );
}
