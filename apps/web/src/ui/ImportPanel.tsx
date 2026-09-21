import { useCallback, useEffect, useRef, useState } from 'react';
import { uploadFile, type UploadHistoryRow, type UploadOutcome, type ZipEntryResult } from '../api';
import { formatDistance, formatFileSize, formatShortDate, formatSourceLabel } from './format';
import { useUnitSystem } from './units';
import { useUploadHistory } from './useUploadHistory';

/** §4.0.1's cap on an individually-selected (non-`.zip`) batch — `.zip` archives bypass this
 *  entirely, since they exist specifically for bulk historical imports. */
const MAX_PLAIN_FILES = 20;

const ACCEPT = '.gpx,.fit,.tcx,.zip';

/** Module-level, not inline literals, so their identity is stable across renders — passed
 *  straight to useUploadHistory's dependency array, which would otherwise restart both poll
 *  loops on every ImportPanel render. */
const FILE_SOURCES = ['upload', 'takeout'] as const;
const SYNC_SOURCES = ['healthconnect', 'healthkit', 'recorded'] as const;

type ImportTab = 'files' | 'sync';

interface InFlightFile {
  id: string;
  filename: string;
  sizeBytes: number;
  status: 'queued' | 'uploading' | 'error';
  progress: number; // 0..1, meaningful only while status === 'uploading'
  error?: string;
}

interface Notice {
  id: string;
  kind: 'info' | 'error';
  message: string;
}

let nextId = 0;
function makeId(): string {
  nextId += 1;
  return `u${nextId}`;
}

function summarizeZip(outcome: Extract<UploadOutcome, { kind: 'zip' }>): string {
  const counts = outcome.files.reduce(
    (acc, f: ZipEntryResult) => {
      if (f.status === 'skipped') acc.skipped += 1;
      else acc.added += 1;
      return acc;
    },
    { added: 0, skipped: 0 },
  );
  const parts = [`${counts.added} ${counts.added === 1 ? 'file' : 'files'} added`];
  if (counts.skipped > 0) parts.push(`${counts.skipped} skipped`);
  const suffix = outcome.truncated ? ' (archive had more files than could be processed in one batch)' : '';
  return `${outcome.filename}: ${parts.join(', ')}${suffix}`;
}

/** A Files-tab row's own filename is meaningful; a Sync-tab row's `filename` is a raw
 *  external id (a Health Connect session UUID, a GPS Logger-minted UUID, `+ ".json"`) that
 *  was never meant to be read directly — `docs/IMPLEMENTATION.md` §4.0's own note on why
 *  Android's SyncStatusActivity already avoids showing it. `formatSourceLabel` is the title a
 *  Sync row shows instead, regardless of status. */
function rowTitle(u: UploadHistoryRow): string {
  return u.source === 'upload' || u.source === 'takeout' ? u.filename : formatSourceLabel(u.source);
}

/**
 * The header's Import control (`docs/ROADMAP.md`'s "Rename Upload to Import" item) — a
 * dropdown panel with two tabs: **Files** (drag/drop + `.zip`/Takeout, this component's
 * original single-purpose behavior, unchanged) and **Sync** (status for Health Connect/GPS
 * Logger activity synced from the Android app — a read-only history, not a "sync now"
 * button, since that sync is phone-triggered and nothing here can request it). Both tabs
 * read the same `GET /v1/uploads` history, filtered server-side by `source` (`useUploadHistory`).
 *
 * Both entry points go multi-file (drag and the picker input), up to `MAX_PLAIN_FILES`
 * individually-selected files per batch; a `.zip` bypasses that cap and is extracted
 * server-side (handleZipUpload) into one job per contained file. Never blocking: each file
 * uploads and is tracked independently, and `onUploaded` fires per file, not once at the end
 * of a batch — right after that file's own upload request resolves (too early to have a new
 * `activities` row yet — the worker hasn't parsed it — but immediate enough to show the file
 * as "Processing" without delay) *and* again on every poll tick, from either tab's history,
 * while anything's still processing, which is what actually catches ingest finishing.
 */
export interface ImportPanelProps {
  /** Called once a file's upload request is confirmed, and again on every poll tick (from
   *  either tab) while it — or anything else — is still processing. MapView refreshes its
   *  tracks layer, the Activities list, totals and the histogram from this. */
  onUploaded?: () => void;
  /** A demo account (`docs/SPEC.md` FR-2.1–FR-2.3) —
   *  the backend already rejects a demo upload regardless (requireNotDemo), so this only
   *  disables the trigger (with an explaining title) rather than opening a dropdown that
   *  would just fail. Deliberately not hidden: showing the control, disabled, demonstrates
   *  the feature exists rather than leaving a demo visitor to wonder — MapView.tsx's own
   *  comment on ActivitiesPanel's matching `readOnly` prop has the fuller reasoning. */
  readOnly?: boolean;
  /** `docs/ROADMAP.md`'s "View on map" item — a finished row's action. Takes the resulting
   *  activity's id and start time rather than doing the fly-to itself: only MapView holds the
   *  date-range/focus state this needs (narrowing the range first if the activity falls
   *  outside it), and this component has no reason to know about either. */
  onViewOnMap: (activityId: string, startedAt: string) => void;
}

export function ImportPanel({ onUploaded, readOnly = false, onViewOnMap }: ImportPanelProps) {
  const [open, setOpen] = useState(false);
  const [tab, setTab] = useState<ImportTab>('files');
  const [dragOver, setDragOver] = useState(false);
  const [inFlight, setInFlight] = useState<InFlightFile[]>([]);
  const [notices, setNotices] = useState<Notice[]>([]);
  const system = useUnitSystem();
  const inputRef = useRef<HTMLInputElement>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  // Both always mounted, regardless of which tab is active — see the component doc comment
  // above for why polling can't be conditional on tab visibility. `processing` (the badge's
  // own count) is the same global number either instance reports, so either can feed it.
  const filesHistory = useUploadHistory(FILE_SOURCES, onUploaded);
  const syncHistory = useUploadHistory(SYNC_SOURCES, onUploaded);
  const activeHistory = tab === 'files' ? filesHistory : syncHistory;

  // Dismiss on a click anywhere else, or on Escape — the same pattern UserMenu.tsx uses for
  // its own dropdown, not shared into a hook for two call sites.
  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open]);

  const pushNotice = useCallback((kind: Notice['kind'], message: string) => {
    const id = makeId();
    setNotices((prev) => [...prev, { id, kind, message }]);
  }, []);
  const dismissNotice = useCallback((id: string) => {
    setNotices((prev) => prev.filter((n) => n.id !== id));
  }, []);

  const uploadOne = useCallback(
    async (id: string, file: File) => {
      setInFlight((prev) => prev.map((f) => (f.id === id ? { ...f, status: 'uploading' } : f)));
      try {
        const outcome = await uploadFile(file, (fraction) => {
          setInFlight((prev) => prev.map((f) => (f.id === id ? { ...f, progress: fraction } : f)));
        });
        setInFlight((prev) => prev.filter((f) => f.id !== id));
        if (outcome.kind === 'zip') {
          pushNotice('info', summarizeZip(outcome));
        } else if (outcome.status === 'already_processed') {
          // The one outcome that otherwise leaves no trace anywhere: it doesn't enqueue a
          // job (persistAndEnqueue's dedupe check short-circuits before that), so it never
          // becomes a new row in the history list either — without this notice, dropping an
          // already-uploaded file here would look like nothing happened at all.
          pushNotice('info', `${outcome.filename} was already uploaded before.`);
        }
        onUploaded?.();
        filesHistory.refresh();
      } catch (error) {
        const message = error instanceof Error ? error.message : 'upload failed';
        setInFlight((prev) => prev.map((f) => (f.id === id ? { ...f, status: 'error', error: message } : f)));
      }
    },
    [onUploaded, pushNotice, filesHistory],
  );

  const enqueueFiles = useCallback(
    (files: FileList | File[]) => {
      const all = Array.from(files);
      const zips = all.filter((f) => f.name.toLowerCase().endsWith('.zip'));
      const plains = all.filter((f) => !f.name.toLowerCase().endsWith('.zip'));

      let accepted = plains;
      if (plains.length > MAX_PLAIN_FILES) {
        pushNotice(
          'error',
          `Too many files selected (${plains.length}) — zip them and upload the archive instead.`,
        );
        accepted = [];
      }

      const queued = [...zips, ...accepted];
      if (queued.length === 0) return;

      const entries: InFlightFile[] = queued.map((file) => ({
        id: makeId(),
        filename: file.name,
        sizeBytes: file.size,
        status: 'queued',
        progress: 0,
      }));
      setInFlight((prev) => [...prev, ...entries]);

      // Sequential, not parallel: a batch of up to 20 files plus however many zips is not
      // worth a concurrency-limited pool for — these are small requests against a personal
      // server, and sequential keeps per-file progress reporting simple (one XHR at a time,
      // one `onprogress` stream to reason about).
      void (async () => {
        for (let i = 0; i < queued.length; i += 1) {
          await uploadOne(entries[i]!.id, queued[i]!);
        }
      })();
    },
    [pushNotice, uploadOne],
  );

  const inFlightCount = inFlight.filter((f) => f.status !== 'error').length;
  const badgeCount = inFlightCount + (filesHistory.page?.processing ?? 0);

  const page = activeHistory.page;
  const pageEnd = page ? Math.min(page.offset + page.limit, page.total) : 0;
  const emptyMessage = tab === 'files' ? 'Nothing uploaded yet.' : 'Nothing synced yet.';
  const tabHasInFlight = tab === 'files' && inFlight.length > 0;

  const viewOnMap = useCallback(
    (u: UploadHistoryRow) => {
      if (!u.activityId || !u.startedAt) return;
      setOpen(false);
      onViewOnMap(u.activityId, u.startedAt);
    },
    [onViewOnMap],
  );

  return (
    <div className="import-panel" ref={rootRef} data-testid="import-panel">
      <button
        type="button"
        className="import-panel__trigger"
        data-testid="import-panel-trigger"
        aria-haspopup="dialog"
        aria-expanded={open}
        disabled={readOnly}
        title={readOnly ? 'Not available for demo accounts — create an account to import your own data' : undefined}
        onClick={() => setOpen((was) => !was)}
      >
        Import
        {badgeCount > 0 && <span className="import-panel__badge">{badgeCount}</span>}
        <span className="import-panel__caret" aria-hidden="true">
          ▾
        </span>
      </button>

      {open && !readOnly && (
        <div className="import-panel__dropdown" role="dialog" aria-label="Import" data-testid="import-panel-dropdown">
          <div className="import-panel__tabs" role="tablist" aria-label="Import source">
            <button
              type="button"
              role="tab"
              aria-selected={tab === 'files'}
              className={tab === 'files' ? 'import-panel__tab import-panel__tab--active' : 'import-panel__tab'}
              onClick={() => setTab('files')}
            >
              Files
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={tab === 'sync'}
              className={tab === 'sync' ? 'import-panel__tab import-panel__tab--active' : 'import-panel__tab'}
              onClick={() => setTab('sync')}
            >
              Sync
            </button>
          </div>

          {tab === 'files' ? (
            <>
              <p className="import-panel__hint">.gpx, .fit, .tcx, or a .zip archive containing them</p>

              <div
                className={dragOver ? 'import-panel__dropzone import-panel__dropzone--drag' : 'import-panel__dropzone'}
                data-testid="import-panel-dropzone"
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
              </div>
              <input
                ref={inputRef}
                type="file"
                multiple
                accept={ACCEPT}
                className="import-panel__input"
                data-testid="import-panel-input"
                onChange={(event) => {
                  if (event.target.files) enqueueFiles(event.target.files);
                  event.target.value = ''; // allow re-selecting the same file(s)
                }}
              />

              {notices.length > 0 && (
                <ul className="import-panel__notices">
                  {notices.map((notice) => (
                    <li
                      key={notice.id}
                      className={
                        notice.kind === 'error' ? 'import-panel__notice import-panel__notice--error' : 'import-panel__notice'
                      }
                      role="status"
                    >
                      {notice.message}
                      <button
                        type="button"
                        className="import-panel__notice-dismiss"
                        aria-label="Dismiss"
                        onClick={() => dismissNotice(notice.id)}
                      >
                        ×
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </>
          ) : (
            <p className="import-panel__hint">
              Synced from the FitMap Android app — Health Connect activity and GPS Logger recordings land here once you
              tap "Sync Now" on your phone.
            </p>
          )}

          <div className="import-panel__list-head">
            <span className="import-panel__list-title">{tab === 'files' ? 'Files' : 'Sync'}</span>
            <span className="import-panel__list-summary">
              {page
                ? `${page.total.toLocaleString()} ${page.total === 1 ? 'activity' : 'activities'}${
                    page.processing > 0 ? ` · ${page.processing} in progress` : ''
                  }`
                : '…'}
            </span>
          </div>

          <ul className="import-panel__rows" data-testid="import-panel-rows">
            {tab === 'files' &&
              inFlight.map((f) => (
                <li key={f.id} className="import-panel__row">
                  <span className="import-panel__row-name" title={f.filename}>
                    {f.filename}
                  </span>
                  <span className="import-panel__row-size">{formatFileSize(f.sizeBytes)}</span>
                  <span className="import-panel__row-detail">
                    {f.status === 'error' ? (
                      <span className="import-panel__row-status import-panel__row-status--error">{f.error}</span>
                    ) : (
                      <>
                        <span className="import-panel__row-status">
                          {f.status === 'queued' ? 'Queued' : `Uploading ${Math.round(f.progress * 100)}%`}
                        </span>
                        <span className="import-panel__progress">
                          <span
                            className="import-panel__progress-fill"
                            style={{ width: `${f.status === 'uploading' ? f.progress * 100 : 0}%` }}
                          />
                        </span>
                      </>
                    )}
                  </span>
                </li>
              ))}

            {activeHistory.error && <li className="import-panel__note import-panel__note--error">{activeHistory.error}</li>}
            {!activeHistory.error && page && page.uploads.length === 0 && !tabHasInFlight && (
              <li className="import-panel__note">{emptyMessage}</li>
            )}
            {page?.uploads.map((u) => (
              <li key={u.externalId} className="import-panel__row">
                <span className="import-panel__row-name" title={rowTitle(u)}>
                  {rowTitle(u)}
                </span>
                <span className="import-panel__row-size" />
                <span className="import-panel__row-detail">
                  {u.status === 'processing' && <span className="import-panel__row-status">Processing…</span>}
                  {u.status === 'failed' && (
                    <span className="import-panel__row-status import-panel__row-status--error">
                      Failed{u.error ? `: ${u.error}` : ''}
                    </span>
                  )}
                  {u.status === 'done' && (
                    <>
                      <span className="import-panel__row-status import-panel__row-status--ready">Ready</span>
                      {u.startedAt && u.distanceMeters !== undefined && (
                        <span className="import-panel__row-meta">
                          {formatShortDate(u.startedAt)} · {formatDistance(u.distanceMeters, system)}
                        </span>
                      )}
                      {u.activityId && u.startedAt && (
                        <button type="button" className="import-panel__row-view" onClick={() => viewOnMap(u)}>
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
            <div className="import-panel__pager">
              <span>
                {page.total === 0 ? '0 of 0' : `${page.offset + 1}–${pageEnd} of ${page.total}`}
              </span>
              <div className="import-panel__pager-buttons">
                <button
                  type="button"
                  disabled={page.offset === 0}
                  onClick={() => activeHistory.setOffset(Math.max(0, page.offset - page.limit))}
                  aria-label="Previous page"
                >
                  ←
                </button>
                <button
                  type="button"
                  disabled={pageEnd >= page.total}
                  onClick={() => activeHistory.setOffset(page.offset + page.limit)}
                  aria-label="Next page"
                >
                  →
                </button>
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
