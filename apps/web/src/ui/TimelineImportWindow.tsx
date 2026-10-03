import { useEffect, useMemo, useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import { syncActivities } from '../api';
import { t, tn } from '../i18n';
import { clearTimelinePreview, previewBounds, setTimelinePreview } from '../map/timelinePreview';
import {
  MODES_OFF_BY_DEFAULT,
  readTimeline,
  selectSegments,
  summarize,
  syncBatches,
  TimelineFormatError,
  type TimelineRead,
} from '../timeline/reader';
import { formatActivityType, formatDayLabel, formatTotalDistance } from './format';
import { useUnitSystem } from './units';

/**
 * The Google Maps Timeline import (`SPEC.md` FR-3.10, `IMPLEMENTATION.md` §4.0.5): a window over
 * the map's top-left, like the Edit window, opened by a .json chosen in the header's Upload menu
 * or dropped on the map. The file is read here, in the browser; the person narrows it to a range
 * of days and a set of modes, sees the selection drawn dashed on the map, and only that is sent,
 * a hundred activities to a request, all under one batch so the Upload menu shows the import as
 * one row.
 */
type Loaded = { file: string; read: TimelineRead; first: string; last: string };
type Sent = { done: number; total: number; enqueued: number; already: number; rejected: number };

export function TimelineImportWindow({ map, file, onClose }: { map: MapLibreMap; file: File | null; onClose: () => void }) {
  const units = useUnitSystem();
  const [reading, setReading] = useState<string | null>(null);
  const [loaded, setLoaded] = useState<Loaded | null>(null);
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const [modes, setModes] = useState<ReadonlySet<string>>(new Set());
  const [sending, setSending] = useState(false);
  const [sent, setSent] = useState<Sent | null>(null);
  const [error, setError] = useState<string | null>(null);

  const allModes = useMemo(() => (loaded ? summarize(loaded.read.segments) : []), [loaded]);
  const selected = useMemo(
    () => (loaded ? selectSegments(loaded.read.segments, { from, to, modes }) : []),
    [loaded, from, to, modes],
  );
  const selectedDistance = selected.reduce((m, s) => m + s.distanceM, 0);

  const finished = sent !== null && !sending && sent.done === sent.total;

  // The preview follows the selection, and comes back after a theme swap drops it. Once sent,
  // it goes: the real tracks take its place as they're processed.
  const preview = useMemo(() => (finished ? [] : selected), [finished, selected]);
  useEffect(() => {
    const draw = () => setTimelinePreview(map, preview);
    draw();
    map.on('styledata', draw);
    return () => {
      map.off('styledata', draw);
    };
  }, [map, preview]);
  useEffect(() => () => clearTimelinePreview(map), [map]);

  async function choose(file: File) {
    setReading(file.name);
    setError(null);
    setLoaded(null);
    setSent(null);
    try {
      let data: unknown;
      try {
        data = JSON.parse(await file.text());
      } catch {
        throw new Error(t('timeline.unreadable'));
      }
      const read = readTimeline(data);
      if (read.segments.length === 0) throw new Error(t('timeline.empty'));
      const first = read.segments[0]!.day;
      const last = read.segments.reduce((d, s) => (s.day > d ? s.day : d), first);
      setLoaded({ file: file.name, read, first, last });
      setFrom(first);
      setTo(last);
      setModes(new Set(summarize(read.segments).map((m) => m.mode).filter((m) => !MODES_OFF_BY_DEFAULT.has(m))));
      const bounds = previewBounds(read.segments);
      if (bounds) map.fitBounds(bounds, { padding: 60, duration: 600 });
    } catch (err) {
      setError(
        err instanceof TimelineFormatError
          ? t(err.format === 'ios' ? 'timeline.unsupported_ios' : err.format === 'takeout' ? 'timeline.unsupported_takeout' : 'timeline.not_timeline')
          : err instanceof Error
            ? err.message
            : String(err),
      );
    } finally {
      setReading(null);
    }
  }

  // A file from the Upload menu or a drop on the map, read as soon as it arrives; the window's own
  // button picks one too. Not while sending: a new file would replace what's being sent.
  useEffect(() => {
    if (file && !sending) void choose(file);
  }, [file]);

  function toggleMode(mode: string) {
    setModes((prev) => {
      const next = new Set(prev);
      if (next.has(mode)) next.delete(mode);
      else next.add(mode);
      return next;
    });
  }

  async function send() {
    if (!loaded || selected.length === 0 || sending) return;
    setSending(true);
    setError(null);
    const batch = crypto.randomUUID();
    const progress: Sent = { done: 0, total: selected.length, enqueued: 0, already: 0, rejected: 0 };
    setSent({ ...progress });
    try {
      for (const activities of syncBatches(selected)) {
        const results = await syncActivities({ source: 'timeline', batch, batchTitle: loaded.file, activities });
        for (const r of results) {
          if (r.status === 'enqueued') progress.enqueued += 1;
          else if (r.status === 'already_processed') progress.already += 1;
          else progress.rejected += 1;
        }
        progress.done += activities.length;
        setSent({ ...progress });
        // The Upload menu reads what's processing now rather than at its next idle tick.
        window.dispatchEvent(new CustomEvent('hmt:imports-sent'));
      }
    } catch (err) {
      // Sending again is safe: what already arrived comes back as already imported.
      setError(err instanceof TypeError ? t('common.network_error') : err instanceof Error ? err.message : String(err));
    } finally {
      setSending(false);
    }
  }

  return (
    <section className="edit-track timeline-import" aria-label={t('timeline.title')} data-testid="timeline-import">
      <header className="edit-track__head">
        <span className="edit-track__title">{t('timeline.title')}</span>
        {loaded && (
          <span className="edit-track__subtitle">
            {loaded.file} · {t('timeline.span', { from: formatDayLabel(loaded.first), to: formatDayLabel(loaded.last) })}
          </span>
        )}
      </header>

      {!loaded && (
        <>
          <p className="edit-track__note">
            {t('timeline.how')}{' '}
            <a href="/help/timeline-export" target="_blank" rel="noopener">
              {t('timeline.guide')}
            </a>
          </p>
          <p className="edit-track__note">{t('timeline.private')}</p>
        </>
      )}

      {!sending && !finished && (
        <label className="edit-track__btn timeline-import__choose">
          {reading ? t('timeline.reading', { file: reading }) : loaded ? t('timeline.choose_other') : t('timeline.choose')}
          <input
            type="file"
            accept=".json,application/json"
            hidden
            disabled={reading !== null}
            onChange={(e) => {
              const file = e.target.files?.[0];
              e.target.value = '';
              if (file) void choose(file);
            }}
          />
        </label>
      )}

      {loaded && !finished && (
        <fieldset className="timeline-import__fields" disabled={sending}>
          <div className="edit-track__row">
            <label className="timeline-import__date">
              {t('timeline.from')}
              <input
                type="date"
                className="settings-page__input"
                value={from}
                min={loaded.first}
                max={to}
                required
                onChange={(e) => e.target.value && setFrom(e.target.value)}
              />
            </label>
            <label className="timeline-import__date">
              {t('timeline.to')}
              <input
                type="date"
                className="settings-page__input"
                value={to}
                min={from}
                max={loaded.last}
                required
                onChange={(e) => e.target.value && setTo(e.target.value)}
              />
            </label>
          </div>
          <ul className="timeline-import__modes">
            {allModes.map((m) => (
              <li key={m.mode}>
                <label className="timeline-import__mode">
                  <input type="checkbox" checked={modes.has(m.mode)} onChange={() => toggleMode(m.mode)} />
                  <span className="timeline-import__mode-name">{formatActivityType(m.type)}</span>
                  <span className="timeline-import__mode-count">
                    {t('timeline.mode_row', { count: m.count, distance: formatTotalDistance(m.distanceM, units) })}
                  </span>
                </label>
              </li>
            ))}
          </ul>
          {loaded.read.skipped > 0 && <p className="edit-track__note">{tn('timeline.skipped', loaded.read.skipped)}</p>}
          <p className="edit-track__note">
            {selected.length === 0
              ? t('timeline.none_selected')
              : tn('timeline.selected', selected.length, { distance: formatTotalDistance(selectedDistance, units) })}{' '}
            {selected.length > 0 && t('timeline.preview_note')}
          </p>
        </fieldset>
      )}

      {sent && (
        <p className="edit-track__note" role="status">
          {finished
            ? t('timeline.done', { total: sent.total, enqueued: sent.enqueued, already: sent.already, rejected: sent.rejected })
            : t('timeline.sending', { done: sent.done, total: sent.total })}
        </p>
      )}
      {error && <p className="edit-track__error">{error}</p>}

      <div className="edit-track__row edit-track__row--footer">
        <span className="edit-track__spacer" aria-hidden="true" />
        <button type="button" className="edit-track__btn" disabled={sending} onClick={onClose}>
          {finished ? t('timeline.close') : t('common.cancel')}
        </button>
        {loaded && !finished && (
          <button
            type="button"
            className="edit-track__btn edit-track__btn--primary"
            disabled={sending || selected.length === 0}
            onClick={() => void send()}
            data-testid="timeline-import-send"
          >
            {sending ? t('common.working') : t('timeline.import')}
          </button>
        )}
      </div>
    </section>
  );
}
