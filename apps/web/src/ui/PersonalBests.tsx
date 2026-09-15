import type { PersonalBest } from '../api';
import { formatShortDate, formatSplitTime } from './format';
import { usePersonalBests } from './usePersonalBests';

/** VISION.md §5.3's five standard distances, mirrored from
 *  internal/ingest.standardDistancesM — including the same int32 truncation the backend
 *  applies to the two fractional ones (21097.5, 42195 already whole), so lookups match. */
const STANDARD_DISTANCES: { distanceM: number; label: string }[] = [
  { distanceM: 1000, label: '1K' },
  { distanceM: 5000, label: '5K' },
  { distanceM: 10000, label: '10K' },
  { distanceM: 21097, label: 'Half Marathon' },
  { distanceM: 42195, label: 'Marathon' },
];

/**
 * VISION.md §5.3's personal bests — the fastest time ever recorded over each standard
 * distance, all-time. Mounted on ProfilePage below BestEfforts. Reuses ActivityGraph.tsx's
 * existing stat-card classes rather than inventing new ones — this is the same "a handful of
 * labeled numbers" shape, not a chart. A distance with no record yet still renders its card
 * (an em dash), so all five are always visible — seeing "not run yet" communicates more than
 * a shorter row would.
 */
export function PersonalBests() {
  const { personalBests, error } = usePersonalBests();

  const byDistance = new Map<number, PersonalBest>();
  for (const b of personalBests ?? []) {
    byDistance.set(b.distanceM, b);
  }

  return (
    <section className="personal-bests" aria-label="Personal bests">
      <div className="trends__head">
        <div>
          <h2 className="trends__title">Personal bests</h2>
          <p className="trends__subtext">Your fastest time ever recorded over each distance.</p>
        </div>
      </div>

      {error && <p className="trends__error">{error}</p>}

      <div className="activity-graph__cards">
        {STANDARD_DISTANCES.map(({ distanceM, label }) => {
          const best = byDistance.get(distanceM);
          return (
            <div className="activity-graph__card" key={distanceM} data-testid="personal-best-card">
              <span className="activity-graph__card-label">{label}</span>
              <span className="activity-graph__card-value">{best ? formatSplitTime(best.seconds) : '—'}</span>
              {best && <span className="personal-bests__card-date">{formatShortDate(best.startedAt)}</span>}
            </div>
          );
        })}
      </div>
    </section>
  );
}
