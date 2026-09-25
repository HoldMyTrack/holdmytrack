import { useCallback, useState } from 'react';
import { uploadFile, type UploadOutcome, type ZipEntryResult } from '../api';
import { useUploadHistory, type UploadHistoryState } from './useUploadHistory';

/** §4.0.1's cap on an individually-selected (non-`.zip`) batch — `.zip` archives bypass this
 *  entirely, since they exist specifically for bulk historical imports. */
const MAX_PLAIN_FILES = 20;

export interface InFlightFile {
  id: string;
  filename: string;
  sizeBytes: number;
  status: 'queued' | 'uploading' | 'error';
  progress: number; // 0..1, meaningful only while status === 'uploading'
  error?: string;
}

export interface ImportNotice {
  id: string;
  kind: 'info' | 'error';
  message: string;
}

export interface ImportsState {
  inFlight: InFlightFile[];
  notices: ImportNotice[];
  /** Every source at once — file uploads, Takeout, and what the Android app synced. */
  history: UploadHistoryState;
  /** This session's in-flight uploads plus every job still processing for this user — the
   *  Sync tab's badge. */
  badgeCount: number;
  enqueueFiles: (files: FileList | File[]) => void;
  dismissNotice: (id: string) => void;
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
 * The upload queue and import history behind the Activities panel's Sync tab (SyncTab.tsx).
 * Lives in MapView, not in the tab: the panel only renders in Normal map mode and the tab only
 * while it's selected, but an upload in flight — and the polling that notices ingest finishing
 * and refreshes the map through `onUploaded` — has to carry on regardless of either.
 *
 * Both entry points go multi-file (drag and the picker input), up to `MAX_PLAIN_FILES`
 * individually-selected files per batch; a `.zip` bypasses that cap and is extracted
 * server-side (handleZipUpload) into one job per contained file. Never blocking: each file
 * uploads and is tracked independently, and `onUploaded` fires per file, not once at the end
 * of a batch — right after that file's own upload request resolves (too early to have a new
 * `activities` row yet — the worker hasn't parsed it — but immediate enough to show the file
 * as "Processing" without delay) *and* again on every poll tick while anything's still
 * processing, which is what actually catches ingest finishing.
 */
export function useImports(onUploaded?: () => void): ImportsState {
  const [inFlight, setInFlight] = useState<InFlightFile[]>([]);
  const [notices, setNotices] = useState<ImportNotice[]>([]);
  const history = useUploadHistory(undefined, onUploaded);
  const { refresh } = history;

  const pushNotice = useCallback((kind: ImportNotice['kind'], message: string) => {
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
        refresh();
      } catch (error) {
        const message = error instanceof Error ? error.message : 'upload failed';
        setInFlight((prev) => prev.map((f) => (f.id === id ? { ...f, status: 'error', error: message } : f)));
      }
    },
    [onUploaded, pushNotice, refresh],
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

  return { inFlight, notices, history, badgeCount, enqueueFiles, dismissNotice };
}
