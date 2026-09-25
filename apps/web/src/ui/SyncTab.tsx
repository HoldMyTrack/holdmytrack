import { useRef, useState } from 'react';
import { X } from 'lucide-react';
import type { UploadHistoryRow } from '../api';
import { formatDistance, formatFileSize, formatShortDate, formatSourceLabel } from './format';
import { useUnitSystem } from './units';
import type { ImportsState } from './useImports';

const ACCEPT = '.gpx,.fit,.tcx,.zip';

/** A file row's own filename is meaningful; a synced row's `filename` is a raw external id (a
 *  Health Connect session UUID, a GPS Logger-minted UUID, `+ ".json"`) that was never meant to
 *  be read directly — `docs/IMPLEMENTATION.md` §4.0's own note on why Android's
 *  SyncStatusActivity already avoids showing it. `formatSourceLabel` is the title a synced row
 *  shows instead, regardless of status. */
function rowTitle(u: UploadHistoryRow): string {
  return u.source === 'upload' || u.source === 'takeout' ? u.filename : formatSourceLabel(u.source);
}

export interface SyncTabProps {
  /** MapView's useImports — the queue and history outlive this tab (see that hook's comment). */
  imports: ImportsState;
  /** A demo account (`docs/SPEC.md` FR-2.1–FR-2.3) — the backend already rejects a demo
   *  upload regardless (requireNotDemo), so the drop zone is replaced with a note saying why
   *  rather than offered only to fail. */
  readOnly: boolean;
  /** A finished row's "View on map" — MapView's viewActivityOnMap, via ActivitiesPanel, which
   *  also switches back to the Activities tab so the focused row is in view. */
  onViewOnMap: (activityId: string, startedAt: string) => void;
}

/**
 * The Activities panel's Sync tab (§4.0.1) — getting activities in: file upload (drag/drop or
 * the picker, `.gpx`/`.fit`/`.tcx` or a `.zip`/Google Takeout archive) on top, then one history
 * list of everything imported, whatever the source — uploaded files alongside what the Android
 * app synced (Health Connect, GPS Logger recordings). There's no "sync now" here: that sync is
 * phone-triggered and nothing on the web can request it. It used to be the header's Import
 * dropdown, with Files and Sync as tabs of its own; it moved here to leave the header to
 * navigation, and the two histories merged into the one list.
 */
export function SyncTab({ imports, readOnly, onViewOnMap }: SyncTabProps) {
  const { inFlight, notices, history, enqueueFiles, dismissNotice } = imports;
  const [dragOver, setDragOver] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const system = useUnitSystem();

  const page = history.page;
  const pageEnd = page ? Math.min(page.offset + page.limit, page.total) : 0;

  return (
    <div className="sync-tab" data-testid="sync-tab">
      {readOnly ? (
        <p className="sync-tab__hint">Not available for demo accounts — create an account to import your own data.</p>
      ) : (
        <>
          <div
            className={dragOver ? 'sync-tab__dropzone sync-tab__dropzone--drag' : 'sync-tab__dropzone'}
            data-testid="sync-tab-dropzone"
            onClick={() => inputRef.current?.click()}
            onDragOver={(event) => {
              event.preventDefault();
              setDragOver(true);
            }}
            onDragLeave={() => setDragOver(false)}
            onDrop={(event) => {
              event.preventDefault();
              setDragOver(false);
              enqueueFiles(event.dataTransfer.files);
            }}
          >
            Drop files here or choose from disk
            <span className="sync-tab__dropzone-hint">.gpx, .fit, .tcx, or a .zip archive containing them</span>
          </div>
          <input
            ref={inputRef}
            type="file"
            multiple
            accept={ACCEPT}
            className="sync-tab__input"
            data-testid="sync-tab-input"
            onChange={(event) => {
              if (event.target.files) enqueueFiles(event.target.files);
              event.target.value = ''; // allow re-selecting the same file(s)
            }}
          />
          <p className="sync-tab__hint">
            Activity from the HoldMyTrack Android app — Health Connect and GPS Logger recordings — lands here once you
            tap "Sync Now" on your phone.
          </p>
        </>
      )}

      {notices.length > 0 && (
        <ul className="sync-tab__notices">
          {notices.map((notice) => (
            <li
              key={notice.id}
              className={notice.kind === 'error' ? 'sync-tab__notice sync-tab__notice--error' : 'sync-tab__notice'}
              role="status"
            >
              {notice.message}
              <button
                type="button"
                className="sync-tab__notice-dismiss"
                aria-label="Dismiss"
                onClick={() => dismissNotice(notice.id)}
              >
                <X size={14} />
              </button>
            </li>
          ))}
        </ul>
      )}

      <div className="sync-tab__list-head">
        <span className="sync-tab__list-title">History</span>
        <span className="sync-tab__list-summary">
          {page
            ? `${page.total.toLocaleString()} ${page.total === 1 ? 'activity' : 'activities'}${
                page.processing > 0 ? ` · ${page.processing} in progress` : ''
              }`
            : '…'}
        </span>
      </div>

      <ul className="sync-tab__rows" data-testid="sync-tab-rows">
        {inFlight.map((f) => (
          <li key={f.id} className="sync-tab__row">
            <span className="sync-tab__row-name" title={f.filename}>
              {f.filename}
            </span>
            <span className="sync-tab__row-size">{formatFileSize(f.sizeBytes)}</span>
            <span className="sync-tab__row-detail">
              {f.status === 'error' ? (
                <span className="sync-tab__row-status sync-tab__row-status--error">{f.error}</span>
              ) : (
                <>
                  <span className="sync-tab__row-status">
                    {f.status === 'queued' ? 'Queued' : `Uploading ${Math.round(f.progress * 100)}%`}
                  </span>
                  <span className="sync-tab__progress">
                    <span
                      className="sync-tab__progress-fill"
                      style={{ width: `${f.status === 'uploading' ? f.progress * 100 : 0}%` }}
                    />
                  </span>
                </>
              )}
            </span>
          </li>
        ))}

        {history.error && <li className="sync-tab__note sync-tab__note--error">{history.error}</li>}
        {!history.error && page && page.uploads.length === 0 && inFlight.length === 0 && (
          <li className="sync-tab__note">Nothing imported yet.</li>
        )}
        {page?.uploads.map((u) => (
          <li key={u.externalId} className="sync-tab__row">
            <span className="sync-tab__row-name" title={rowTitle(u)}>
              {rowTitle(u)}
            </span>
            <span className="sync-tab__row-size" />
            <span className="sync-tab__row-detail">
              {u.status === 'processing' && <span className="sync-tab__row-status">Processing…</span>}
              {u.status === 'failed' && (
                <span className="sync-tab__row-status sync-tab__row-status--error">Failed{u.error ? `: ${u.error}` : ''}</span>
              )}
              {u.status === 'done' && (
                <>
                  <span className="sync-tab__row-status sync-tab__row-status--ready">Ready</span>
                  {u.startedAt && u.distanceMeters !== undefined && (
                    <span className="sync-tab__row-meta">
                      {formatShortDate(u.startedAt)} · {formatDistance(u.distanceMeters, system)}
                    </span>
                  )}
                  {u.activityId && u.startedAt && (
                    <button
                      type="button"
                      className="sync-tab__row-view"
                      onClick={() => onViewOnMap(u.activityId!, u.startedAt!)}
                    >
                      View on map
                    </button>
                  )}
                </>
              )}
            </span>
          </li>
        ))}
      </ul>

      {page && page.total > page.limit && (
        <div className="sync-tab__pager">
          <span>{page.total === 0 ? '0 of 0' : `${page.offset + 1}–${pageEnd} of ${page.total}`}</span>
          <div className="sync-tab__pager-buttons">
            <button
              type="button"
              disabled={page.offset === 0}
              onClick={() => history.setOffset(Math.max(0, page.offset - page.limit))}
              aria-label="Previous page"
            >
              ←
            </button>
            <button
              type="button"
              disabled={pageEnd >= page.total}
              onClick={() => history.setOffset(page.offset + page.limit)}
              aria-label="Next page"
            >
              →
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
