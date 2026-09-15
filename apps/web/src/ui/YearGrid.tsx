import { useMemo } from 'react';
import type { HistogramBucket } from '../api';
import { useYearGraph } from './useYearGraph';
import { formatDistance, formatTotalDistance } from './format';
import { useUnitSystem, type UnitSystem } from './units';

export type ShadeBy = 'count' | 'distance';

const MS_PER_DAY = 24 * 60 * 60 * 1000;
// Must match .year-grid__cell/.year-grid__month's width in index.css — the inline
// grid-template-columns below is what makes each cell square (a fixed px column, not `1fr`,
// which would stretch to the container's width and make wide, short rectangles instead).
const CELL_SIZE_PX = 12;
const MONTH_NAMES = ['JAN', 'FEB', 'MAR', 'APR', 'MAY', 'JUN', 'JUL', 'AUG', 'SEP', 'OCT', 'NOV', 'DEC'];
// GitHub's own convention: label every other row rather than all seven, which would crowd the
// narrow gutter these sit in. Row 0 is Sunday (`Date.getUTCDay()`'s own numbering), so Mon/Wed/
// Fri are rows 1/3/5.
const ROW_LABELS: Record<number, string> = { 1: 'Mon', 3: 'Wed', 5: 'Fri' };

function toDateKey(t: number): string {
  return new Date(t).toISOString().slice(0, 10);
}

interface GridCell {
  date: string;
  col: number;
  row: number;
  /** False for the padding days before Jan 1 or after Dec 31 that fill out the first/last
   *  week — the grid is always whole weeks, Sunday to Saturday, so a year not starting on a
   *  Sunday needs some. */
  inYear: boolean;
  count: number;
  distanceMeters: number;
}

/**
 * One cell per day of `year`, Sunday-to-Saturday weeks (GitHub's own layout), column-major so
 * mapping this straight into a `grid-auto-flow: column` container lands each cell in its
 * correct column without explicit `grid-column`/`grid-row` per cell. Padding cells outside
 * the year (`inYear: false`) fill out the first and last week rather than being omitted, since
 * a ragged grid — an incomplete first or last column — would make columns not all mean "one
 * week" any more.
 */
function buildYearCells(year: number, days: HistogramBucket[]): { cells: GridCell[]; weekCount: number } {
  const byDate = new Map(days.map((d) => [d.date, d]));
  const jan1 = Date.UTC(year, 0, 1);
  const dec31 = Date.UTC(year, 11, 31);
  const startWeekday = new Date(jan1).getUTCDay();
  const gridStart = jan1 - startWeekday * MS_PER_DAY;
  const totalDays = Math.round((dec31 - gridStart) / MS_PER_DAY) + 1;
  const weekCount = Math.ceil(totalDays / 7);

  const cells: GridCell[] = [];
  for (let col = 0; col < weekCount; col += 1) {
    for (let row = 0; row < 7; row += 1) {
      const t = gridStart + (col * 7 + row) * MS_PER_DAY;
      const inYear = t >= jan1 && t <= dec31;
      const date = toDateKey(t);
      const bucket = inYear ? byDate.get(date) : undefined;
      cells.push({ date, col, row, inYear, count: bucket?.count ?? 0, distanceMeters: bucket?.distanceMeters ?? 0 });
    }
  }
  return { cells, weekCount };
}

/** One label per calendar month, positioned at the column holding that month's 1st — not
 *  evenly spaced, for the same reason RangePicker's month ticks aren't: an evenly-spaced axis
 *  would claim a regularity real calendar months don't have (28–31 days each). Skips a month
 *  whose 1st lands in the same column as the previous label, which can't happen for a whole
 *  month here (every month spans at least 4 columns) but is cheap insurance regardless. */
function monthLabels(year: number, cells: GridCell[]): { label: string; col: number }[] {
  const labels: { label: string; col: number }[] = [];
  let lastCol = -1;
  for (let month = 0; month < 12; month += 1) {
    const firstOfMonth = toDateKey(Date.UTC(year, month, 1));
    const cell = cells.find((c) => c.inYear && c.date === firstOfMonth);
    if (!cell || cell.col === lastCol) continue;
    labels.push({ label: MONTH_NAMES[month]!, col: cell.col });
    lastCol = cell.col;
  }
  return labels;
}

/** Distance has no natural small-integer scale the way a count of activities does ("one, two,
 *  three or more"), so its three non-empty shade levels are quantile thresholds over this
 *  year's own active days rather than a fixed distance — relative to what a *walker's* busy
 *  day looks like when shading a walker's year, and to a cyclist's when shading a cyclist's,
 *  rather than one hardcoded number reading as "quiet" for one and "empty" for the other. */
function distanceThresholds(days: HistogramBucket[]): [number, number] {
  const distances = days
    .map((d) => d.distanceMeters)
    .filter((d) => d > 0)
    .sort((a, b) => a - b);
  if (distances.length === 0) return [0, 0];
  const at = (p: number) => distances[Math.min(distances.length - 1, Math.floor(p * distances.length))]!;
  return [at(1 / 3), at(2 / 3)];
}

function shadeLevel(cell: GridCell, shadeBy: ShadeBy, distanceBounds: [number, number]): 0 | 1 | 2 | 3 {
  if (shadeBy === 'count') {
    if (cell.count <= 0) return 0;
    if (cell.count === 1) return 1;
    if (cell.count === 2) return 2;
    return 3;
  }
  const [t1, t2] = distanceBounds;
  if (cell.distanceMeters <= 0) return 0;
  if (cell.distanceMeters <= t1) return 1;
  if (cell.distanceMeters <= t2) return 2;
  return 3;
}

function cellTitle(cell: GridCell, system: UnitSystem): string | undefined {
  if (!cell.inYear) return undefined;
  if (cell.count === 0) return `${cell.date}: no activity`;
  const activities = `${cell.count} ${cell.count === 1 ? 'activity' : 'activities'}`;
  return `${cell.date}: ${activities}, ${formatDistance(cell.distanceMeters, system)}`;
}

function statsLine(
  stats: { count: number; distanceMeters: number; activeDays: number; longestStreakDays: number },
  system: UnitSystem,
): string {
  const activities = `${stats.count.toLocaleString()} ${stats.count === 1 ? 'activity' : 'activities'}`;
  const activeDays = `${stats.activeDays.toLocaleString()} active ${stats.activeDays === 1 ? 'day' : 'days'}`;
  const streak = `longest streak ${stats.longestStreakDays.toLocaleString()} ${stats.longestStreakDays === 1 ? 'day' : 'days'}`;
  return `${activities} · ${formatTotalDistance(stats.distanceMeters, system)} · ${activeDays} · ${streak}`;
}

export interface YearGridProps {
  year: number;
  shadeBy: ShadeBy;
}

/**
 * One §4.8 year block: the year, its own stat subtotal, and a
 * GitHub-style contribution grid — Sunday-to-Saturday weeks as columns, month labels above,
 * Mon/Wed/Fri row labels to the left. Fetches its own data (`useYearGraph`) rather than
 * receiving it as a prop, the same reasoning as any other self-contained panel here
 * (ActivitiesPanel, ActivityHistogram): `ActivityGraph` only needs to know which years exist,
 * not carry every year's data itself.
 */
export function YearGrid({ year, shadeBy }: YearGridProps) {
  const { graph, stats, error } = useYearGraph(year);
  const system = useUnitSystem();

  const { cells, weekCount } = useMemo(
    () => buildYearCells(year, graph?.days ?? []),
    [year, graph],
  );
  const labels = useMemo(() => monthLabels(year, cells), [year, cells]);
  const distanceBounds = useMemo(() => distanceThresholds(graph?.days ?? []), [graph]);

  return (
    <section className="year-grid" aria-label={`${year} activity grid`}>
      <div className="year-grid__header">
        <h3 className="year-grid__year">{year}</h3>
        <span className="year-grid__stats">
          {error ? error : stats ? statsLine(stats.yearStats, system) : 'Loading…'}
        </span>
      </div>

      <div className="year-grid__body">
        <div className="year-grid__row-labels">
          {[0, 1, 2, 3, 4, 5, 6].map((row) => (
            <span key={row} className="year-grid__row-label">
              {ROW_LABELS[row] ?? ''}
            </span>
          ))}
        </div>

        <div className="year-grid__chart">
          <div
            className="year-grid__months"
            style={{ gridTemplateColumns: `repeat(${weekCount}, ${CELL_SIZE_PX}px)` }}
          >
            {labels.map((tick) => (
              <span key={tick.col} className="year-grid__month" style={{ gridColumnStart: tick.col + 1 }}>
                {tick.label}
              </span>
            ))}
          </div>
          <div
            className="year-grid__cells"
            style={{ gridTemplateColumns: `repeat(${weekCount}, ${CELL_SIZE_PX}px)` }}
          >
            {cells.map((cell) => (
              <span
                key={cell.date}
                className={
                  cell.inYear
                    ? `year-grid__cell year-grid__cell--level-${shadeLevel(cell, shadeBy, distanceBounds)}`
                    : 'year-grid__cell year-grid__cell--pad'
                }
                title={cellTitle(cell, system)}
              />
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}
