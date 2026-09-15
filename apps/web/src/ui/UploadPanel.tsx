import { useCallback, useEffect, useRef, useState } from 'react';
import { uploadFile, type UploadOutcome, type ZipEntryResult } from '../api';
import { formatDistance, formatFileSize, formatShortDate } from './format';
import { useUnitSystem } from './units';
import { useUploadHistory } from './useUploadHistory';

/** §4.0.1's cap on an individually-selected (non-`.zip`) batch — `.zip` archives bypass this
 *  entirely, since they exist specifically for bulk historical imports. */
const MAX_PLAIN_FILES = 20;

const ACCEPT = '.gpx,.fit,.tcx,.zip';

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

/**
 * The header's upload control (IMPLEMENTATION.md §4.0.1) — a dropdown panel,
 * not a hover popover any more: drop zone + file picker at the top, a paginated
 * "UPLOADS" list below combining this session's in-flight files with the persistent history
 * `useUploadHistory` reads. Replaces the single-file `UploadWidget.tsx` entirely.
 *
 * Both entry points go multi-file (drag and the picker input), up to `MAX_PLAIN_FILES`
 * individually-selected files per batch; a `.zip` bypasses that cap and is extracted
 * server-side (handleZipUpload) into one job per contained file. Never blocking: each file
 * uploads and is tracked independently, and `onUploaded` fires per file, not once at the end
 * of a batch — right after that file's own upload request resolves (too early to have a new
 * `activities` row yet — the worker hasn't parsed it — but immediate enough to show the file
 * as "Processing" without delay) *and* again on every `useUploadHistory` poll tick while
 * anything's still processing, which is what actually catches ingest finishing.
 */
export interface UploadPanelProps {
  /** Called once a file's upload request is confirmed, and again on every poll tick while it
   *  (or anything else) is still processing — MapView refreshes its tracks layer, the
   *  Activities list, totals and the histogram from this. See useUploadHistory's own doc
   *  comment for why it needs both calls, not just the first one. */
  onUploaded?: () => void;
}

export function UploadPanel({ onUploaded }: UploadPanelProps) {
  const [open, setOpen] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  const [inFlight, setInFlight] = useState<InFlightFile[]>([]);
  const [notices, setNotices] = useState<Notice[]>([]);
  const system = useUnitSystem();
  const inputRef = useRef<HTMLInputElement>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  // onUploaded also runs on every poll tick while something's still processing (see
  // useUploadHistory's own doc comment) — that's what actually catches a job finishing,
  // since the call below (right after the upload request itself resolves) fires before the
  // worker has parsed anything at all.
  const history = useUploadHistory(onUploaded);

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
        history.refresh();
      } catch (error) {
        const message = error instanceof Error ? error.message : 'upload failed';
        setInFlight((prev) => prev.map((f) => (f.id === id ? { ...f, status: 'error', error: message } : f)));
      }
    },
    [onUploaded, pushNotice, history],
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
  const badgeCount = inFlightCount + (history.page?.processing ?? 0);

  const page = history.page;
  const pageEnd = page ? Math.min(page.offset + page.limit, page.total) : 0;

  return (
    <div className="upload-panel" ref={rootRef} data-testid="upload-panel">
      <button
        type="button"
        className="upload-panel__trigger"
        data-testid="upload-panel-trigger"
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen((was) => !was)}
      >
        Upload activity
        {badgeCount > 0 && <span className="upload-panel__badge">{badgeCount}</span>}
        <span className="upload-panel__caret" aria-hidden="true">
          ▾
        </span>
      </button>

      {open && (
        <div className="upload-panel__dropdown" role="dialog" aria-label="Upload activity" data-testid="upload-panel-dropdown">
          <h3 className="upload-panel__title">Upload activity</h3>
          <p className="upload-panel__hint">.gpx, .fit, .tcx, or a .zip archive containing them</p>

          <div
            className={dragOver ? 'upload-panel__dropzone upload-panel__dropzone--drag' : 'upload-panel__dropzone'}
            data-testid="upload-panel-dropzone"
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
            className="upload-panel__input"
            data-testid="upload-panel-input"
            onChange={(event) => {
              if (event.target.files) enqueueFiles(event.target.files);
              event.target.value = ''; // allow re-selecting the same file(s)
            }}
          />

          {notices.length > 0 && (
            <ul className="upload-panel__notices">
              {notices.map((notice) => (
                <li
                  key={notice.id}
                  className={
                    notice.kind === 'error' ? 'upload-panel__notice upload-panel__notice--error' : 'upload-panel__notice'
                  }
                  role="status"
                >
                  {notice.message}
                  <button
                    type="button"
                    className="upload-panel__notice-dismiss"
                    aria-label="Dismiss"
                    onClick={() => dismissNotice(notice.id)}
                  >
                    ×
                  </button>
                </li>
              ))}
            </ul>
          )}

          <div className="upload-panel__list-head">
            <span className="upload-panel__list-title">Uploads</span>
            <span className="upload-panel__list-summary">
              {page
                ? `${page.total.toLocaleString()} ${page.total === 1 ? 'file' : 'files'}${
                    page.processing > 0 ? ` · ${page.processing} in progress` : ''
                  }`
                : '…'}
            </span>
          </div>

          <ul className="upload-panel__rows" data-testid="upload-panel-rows">
            {inFlight.map((f) => (
              <li key={f.id} className="upload-panel__row">
                <span className="upload-panel__row-name" title={f.filename}>
                  {f.filename}
                </span>
                <span className="upload-panel__row-size">{formatFileSize(f.sizeBytes)}</span>
                <span className="upload-panel__row-detail">
                  {f.status === 'error' ? (
                    <span className="upload-panel__row-status upload-panel__row-status--error">{f.error}</span>
                  ) : (
                    <>
                      <span className="upload-panel__row-status">
                        {f.status === 'queued' ? 'Queued' : `Uploading ${Math.round(f.progress * 100)}%`}
                      </span>
                      <span className="upload-panel__progress">
                        <span
                          className="upload-panel__progress-fill"
                          style={{ width: `${f.status === 'uploading' ? f.progress * 100 : 0}%` }}
                        />
                      </span>
                    </>
                  )}
                </span>
              </li>
            ))}

            {history.error && <li className="upload-panel__note upload-panel__note--error">{history.error}</li>}
            {!history.error && page && page.uploads.length === 0 && inFlight.length === 0 && (
              <li className="upload-panel__note">Nothing uploaded yet.</li>
            )}
            {page?.uploads.map((u) => (
              <li key={u.externalId} className="upload-panel__row">
                <span className="upload-panel__row-name" title={u.filename}>
                  {u.filename}
                </span>
                <span className="upload-panel__row-size" />
                <span className="upload-panel__row-detail">
                  {u.status === 'processing' && <span className="upload-panel__row-status">Processing…</span>}
                  {u.status === 'failed' && (
                    <span className="upload-panel__row-status upload-panel__row-status--error">
                      Failed{u.error ? `: ${u.error}` : ''}
                    </span>
                  )}
                  {u.status === 'done' && (
                    <>
                      <span className="upload-panel__row-status upload-panel__row-status--ready">Ready</span>
                      {u.startedAt && u.distanceMeters !== undefined && (
                        <span className="upload-panel__row-meta">
                          {formatShortDate(u.startedAt)} · {formatDistance(u.distanceMeters, system)}
                        </span>
                      )}
                    </>
                  )}
                </span>
              </li>
            ))}
          </ul>

          {page && page.total > page.limit && (
            <div className="upload-panel__pager">
              <span>
                {page.total === 0 ? '0 of 0' : `${page.offset + 1}–${pageEnd} of ${page.total}`}
              </span>
              <div className="upload-panel__pager-buttons">
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
      )}
    </div>
  );
}
