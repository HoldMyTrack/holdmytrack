/**
 * The Go backend's upload endpoint (IMPLEMENTATION.md §4.0).
 * VITE_API_BASE_URL follows the same convention as the rest of the web client's env vars —
 * a committed, non-secret default here, overridable via apps/web/.env.local.
 */
export const API_BASE_URL: string =
  (import.meta.env.VITE_API_BASE_URL as string | undefined) ?? 'http://localhost:8080';

/**
 * The sole place the API's major version lives on this side — mirrors
 * services/server/internal/httpapi/server.go's own `apiPrefix`/`tilesPrefix` constants, so a
 * future `/v2` cutover is a matter of introducing a second constant here, not a
 * hunt-and-replace across every call site below.
 */
export const API_V1 = '/v1';
export const TILES_V1 = '/tiles/v1';

/**
 * An error response body is either plain text (`http.Error(w, "message", code)`, most
 * endpoints) or JSON shaped like `{"error": "machine_code", "message": "human text"}` (the
 * handful that need a distinguishable code alongside a human message for the frontend to
 * branch on — requireVerified's `email_not_verified`, requireNotDemo's `demo_read_only`).
 * Whichever it is, this pulls out the part actually worth putting in front of a person: the
 * JSON body's own `message` field when there is one, the plain text otherwise — never the raw
 * `{"error":...,"message":...}` blob itself, which is what every caller below used to throw
 * verbatim (reported live: the Settings page, when it was React, showing the whole JSON string as its
 * save error).
 */
function messageFromErrorBody(text: string, fallback: string): string {
  if (!text) return fallback;
  try {
    const parsed: unknown = JSON.parse(text);
    if (parsed && typeof parsed === 'object' && typeof (parsed as { message?: unknown }).message === 'string') {
      return (parsed as { message: string }).message;
    }
  } catch {
    // Not JSON — a plain-text http.Error body, which already is the message.
  }
  return text;
}

/** `messageFromErrorBody` for the `fetch`-based functions below, which all throw through this
 *  rather than reading `res.text()` and constructing an `Error` themselves — `uploadFile`'s
 *  XMLHttpRequest path (no `Response` object to read) calls `messageFromErrorBody` directly
 *  against `xhr.responseText` instead. */
async function errorMessageFromResponse(res: Response, fallback: string): Promise<string> {
  const text = await res.text().catch(() => '');
  return messageFromErrorBody(text, fallback);
}

/**
 * Simple email+password auth (services/server/internal/httpapi/auth.go) — every request in
 * this file sends `credentials: 'include'` now (and `uploadFile`'s XHR sets
 * `withCredentials`) so the browser attaches the session cookie these endpoints set/read.
 * Signing in, signing up, password reset and email verification are server-rendered pages
 * (ADR-0012), not calls from here, and so is signing out (the page header's form); this app
 * only reads the session (`getCurrentUser`).
 */
/** The Settings page's own fields (`/settings`) — carried by both `AuthUser` and
 *  `DemoUser` uniformly, since a demo account is a real `users` row with real column
 *  defaults, not a special case that skips having them. `displayName`/`country`/`avatarUrl`
 *  are `''` when unset, not `undefined` — every caller (units.ts) checks
 *  for an empty string, never an absent field. `country` is an ISO 3166-1 alpha-2 code or
 *  `''` (defaults the whole app to metric — see `ui/units.ts`). `avatarUrl` is already a
 *  full API path (`/v1/account/avatar?v=...`) — callers prefix `API_BASE_URL`. */
export interface UserProfile {
  displayName: string;
  country: string;
  avatarUrl: string;
  /** IANA zone name (e.g. "America/New_York"), never `''` — unlike displayName/country there
   *  is no "unset" state (services/server/migrations/0021_user_timezone.sql's column is
   *  `NOT NULL DEFAULT 'UTC'`). Drives every day-bucketing query server-side
   *  (docs/KNOWN_ISSUES.md's "UTC-day bucketing" entry) — the client never buckets by day
   *  itself, it only offers this value for editing in Settings. */
  timezone: string;
}

export interface AuthUser extends UserProfile {
  email: string;
  /** docs/SPEC.md FR-1.8 — always `true` for a
   *  `DemoUser` (the gate never applies to one, so that type doesn't carry this field at all),
   *  reflects the account's real `users.email_verified` column for a real one. App.tsx checks
   *  this to decide whether to render the map or leave for the verify-email page. */
  emailVerified: boolean;
}

/** VISION.md §8.2's ephemeral demo account. Its real email is an internal,
 *  never-shown placeholder the backend strips (see auth.go's handleMe/handleDemoStart), and
 *  having no `email` field at all is what lets `'email' in user` narrow a SessionUser
 *  correctly with no separate discriminant tag needed — the rest of `UserProfile` is still
 *  real, though, since a demo account is a real row with real column defaults. */
export interface DemoUser extends UserProfile {}

/** What `getCurrentUser` can resolve to — the one place this file doesn't already know in
 *  advance which of the two it's got. `login`/`signup` are never a demo account (a
 *  successful one always returns a real AuthUser) and `startDemo` always is, so those three
 *  keep their own narrower return types instead of forcing every caller to re-narrow
 *  something that can't happen for them. */
export type SessionUser = AuthUser | DemoUser;

interface AuthResponseBody {
  email: string;
  isDemo: boolean;
  email_verified: boolean;
  display_name: string;
  country: string;
  avatar_url: string;
  timezone: string;
}

function toProfile(body: AuthResponseBody): UserProfile {
  return {
    displayName: body.display_name,
    country: body.country,
    avatarUrl: body.avatar_url,
    timezone: body.timezone,
  };
}

function toAuthUser(body: AuthResponseBody): AuthUser {
  return { email: body.email, emailVerified: body.email_verified, ...toProfile(body) };
}

function toSessionUser(body: AuthResponseBody): SessionUser {
  return body.isDemo ? toProfile(body) : toAuthUser(body);
}

/** Resolves to the signed-in user (real or demo), or `null` if there is no valid session —
 *  not an error: this is the normal "nobody's signed in yet" state on first load, checked
 *  deliberately rather than treated as a failure. */
export async function getCurrentUser(): Promise<SessionUser | null> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/auth/me`, { credentials: 'include' });
  if (res.status === 401) return null;
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, `session check failed (${res.status})`));
  }
  const body = (await res.json()) as AuthResponseBody;
  return toSessionUser(body);
}

/** A single plain file's own upload outcome — §4.0's original one-file response shape. */
export interface SingleUploadResult {
  kind: 'single';
  status: 'enqueued' | 'already_processed';
  externalId: string;
  filename: string;
}

/** One contained file's outcome inside a `.zip` batch (§4.0.1). */
export interface ZipEntryResult {
  filename: string;
  status: 'enqueued' | 'already_processed' | 'skipped';
  externalId?: string;
  /** Only set when status is "skipped" — why this one entry didn't become a job. */
  reason?: string;
}

/** A `.zip` archive's own upload outcome — one request, many contained files, each with its
 *  own outcome (§5.1: one bad file inside a bulk import never aborts the rest). */
export interface ZipUploadResult {
  kind: 'zip';
  filename: string;
  files: ZipEntryResult[];
  /** The archive had more entries than the server processes in one request — the rest were
   *  never looked at, not merely skipped for a per-file reason. */
  truncated: boolean;
}

export type UploadOutcome = SingleUploadResult | ZipUploadResult;

interface UploadResponseBody {
  status: string;
  external_id?: string;
  filename: string;
  files?: { filename: string; status: string; external_id?: string; reason?: string }[];
  truncated?: boolean;
}

function toUploadOutcome(body: UploadResponseBody): UploadOutcome {
  if (body.status === 'zip_processed') {
    return {
      kind: 'zip',
      filename: body.filename,
      truncated: body.truncated ?? false,
      files: (body.files ?? []).map((f) => ({
        filename: f.filename,
        status: f.status as ZipEntryResult['status'],
        ...(f.external_id ? { externalId: f.external_id } : {}),
        ...(f.reason ? { reason: f.reason } : {}),
      })),
    };
  }
  return {
    kind: 'single',
    status: body.status as SingleUploadResult['status'],
    externalId: body.external_id!,
    filename: body.filename,
  };
}

/**
 * POSTs one file (a plain activity file or a `.zip` archive — the server tells them apart by
 * extension, not this function) to §4.0's upload endpoint. Plain `fetch` can't report upload
 * progress in a cross-browser way (no `ReadableStream` request body support everywhere this
 * app needs to run), so this uses `XMLHttpRequest` directly instead — the one place in this
 * codebase that does, specifically for `onProgress`, which a `.zip` archive large enough to
 * take real time uploading needs to be able to show.
 */
export function uploadFile(
  file: File,
  onProgress?: (fraction: number) => void,
  signal?: AbortSignal,
): Promise<UploadOutcome> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', `${API_BASE_URL}${API_V1}/activities/upload`);
    xhr.withCredentials = true; // send the session cookie — this endpoint requires auth now
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable && onProgress) onProgress(event.loaded / event.total);
    };
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          resolve(toUploadOutcome(JSON.parse(xhr.responseText) as UploadResponseBody));
        } catch {
          reject(new Error('upload succeeded but the response could not be read'));
        }
      } else {
        reject(new Error(messageFromErrorBody(xhr.responseText, `upload failed (${xhr.status})`)));
      }
    };
    xhr.onerror = () => reject(new Error('upload failed (network error)'));
    xhr.onabort = () => reject(new DOMException('upload aborted', 'AbortError'));
    if (signal) {
      if (signal.aborted) {
        xhr.abort();
        return;
      }
      signal.addEventListener('abort', () => xhr.abort());
    }
    const form = new FormData();
    form.append('file', file);
    xhr.send(form);
  });
}

/** One row of §4.0.1's persistent upload history — every upload ever, including ones whose
 *  browser tab is long closed, which live in-flight status alone can never answer. */
export interface UploadHistoryRow {
  filename: string;
  externalId: string;
  /** `"upload"` | `"takeout"` | `"healthconnect"` | `"healthkit"` | `"recorded"` — what
   *  `sources` filters by, and (for an uploaded file) what makes `filename` worth showing at
   *  all; a synced row's own filename is a raw external id, never meant to be read directly
   *  (`formatSourceLabel` is what SyncTab.tsx shows as a synced row's title instead). */
  source: string;
  status: 'processing' | 'done' | 'failed';
  error?: string;
  submittedAt: string;
  /** Set only when status is "done" — this activity's own date and distance, so a finished
   *  row can read "9 Sep · 34.7 km" rather than just repeating its own filename. */
  startedAt?: string;
  distanceMeters?: number;
  /** The resulting activity's own id, once one exists — ROADMAP.md's "View on map" item.
   *  Same nullability as startedAt/distanceMeters: nothing to link to before ingest finishes. */
  activityId?: string;
}

export interface UploadHistoryPage {
  total: number;
  /** Every pending ingest job for this user, regardless of which page is being viewed — the
   *  header badge's "N in progress" needs the true count, not just this page's own. */
  processing: number;
  limit: number;
  offset: number;
  uploads: UploadHistoryRow[];
}

interface UploadHistoryRowBody {
  filename: string;
  external_id: string;
  source: string;
  status: string;
  error?: string;
  submitted_at: string;
  started_at?: string;
  distance_meters?: number;
  activity_id?: string;
}

interface UploadHistoryBody {
  total: number;
  processing: number;
  limit: number;
  offset: number;
  uploads: UploadHistoryRowBody[];
}

export interface UploadHistoryQuery {
  limit?: number;
  offset?: number;
  /** Comma-joined server-side, matching §4.3's own `types` filter convention — absent means
   *  every source — what SyncTab.tsx's one combined history asks for. */
  sources?: readonly string[];
}

export async function getUploadHistory(query: UploadHistoryQuery = {}, signal?: AbortSignal): Promise<UploadHistoryPage> {
  const params = new URLSearchParams();
  if (query.limit !== undefined) params.set('limit', String(query.limit));
  if (query.offset !== undefined) params.set('offset', String(query.offset));
  if (query.sources && query.sources.length > 0) params.set('source', query.sources.join(','));
  const qs = params.toString();
  const res = await fetch(`${API_BASE_URL}${API_V1}/uploads${qs ? `?${qs}` : ''}`, {
    credentials: 'include',
    ...(signal ? { signal } : {}),
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, `upload history failed (${res.status})`));
  }
  const body = (await res.json()) as UploadHistoryBody;
  return {
    total: body.total,
    processing: body.processing,
    limit: body.limit,
    offset: body.offset,
    uploads: body.uploads.map((u) => ({
      filename: u.filename,
      externalId: u.external_id,
      source: u.source,
      status: u.status as UploadHistoryRow['status'],
      submittedAt: u.submitted_at,
      ...(u.error ? { error: u.error } : {}),
      ...(u.started_at ? { startedAt: u.started_at } : {}),
      ...(u.distance_meters !== undefined ? { distanceMeters: u.distance_meters } : {}),
      ...(u.activity_id ? { activityId: u.activity_id } : {}),
    })),
  };
}

/**
 * One row of IMPLEMENTATION.md §4.7's activity list. `distanceMeters` and `durationSeconds`
 * are nullable because the schema is — a file that carried no distance is not a
 * zero-distance activity, and the UI says "—" rather than "0.0 km".
 *
 * `name` (§4.7's revised "no name column" decision — a user-entered title only, never
 * parsed from a source file) is what a row leads with when set; `startedAt` is the fallback
 * for a row that has none. `description` (§4.7.4) stays a separate, later addition — longer
 * free text, shown only as a hover tooltip, not the row's visible title. Both are edited via
 * `updateActivity` below, and `null` means never set for either, not "set to empty."
 */
export interface Activity {
  id: string;
  startedAt: string;
  activityType: string;
  name: string | null;
  distanceMeters: number | null;
  durationSeconds: number | null;
  description: string | null;
  /** [minLon, minLat, maxLon, maxLat], or null for a row with no trajectory. */
  bbox: BBox | null;
  /** An Edit track reprocess (§4.7.7) is queued or running — the row's numbers and geometry
   *  are still the pre-edit ones until it finishes. */
  pending: boolean;
  /** The track carries a user edit, so "Reset to original track" has something to undo. */
  edited: boolean;
}

/** GeoJSON bbox ordering, which is also what MapLibre's fitBounds takes as a flat array. */
export type BBox = [number, number, number, number];

/** Filters, all optional — an absent one means "no restriction", per §4.7's convention. */
export interface ActivityQuery {
  from?: string;
  to?: string;
  types?: string[];
}

interface ActivityRowBody {
  id: string;
  started_at: string;
  activity_type: string;
  name: string | null;
  distance_meters: number | null;
  duration_seconds: number | null;
  description: string | null;
  bbox: number[] | null;
  pending: boolean;
  edited: boolean;
}

interface ActivitiesBody {
  activities: ActivityRowBody[];
}

function activityQueryString(query: ActivityQuery): string {
  const params = new URLSearchParams();
  if (query.from) params.set('from', query.from);
  if (query.to) params.set('to', query.to);
  if (query.types?.length) params.set('types', query.types.join(','));
  const qs = params.toString();
  return qs ? `?${qs}` : '';
}

/** Shared by listActivities and updateActivity below — both endpoints return the same row
 *  shape (activityByIDQuery/listActivitiesQuery share one Scan in the backend too). */
function toActivity(a: ActivityRowBody): Activity {
  return {
    id: a.id,
    startedAt: a.started_at,
    activityType: a.activity_type,
    name: a.name,
    distanceMeters: a.distance_meters,
    durationSeconds: a.duration_seconds,
    description: a.description,
    bbox: a.bbox && a.bbox.length === 4 ? ([...a.bbox] as BBox) : null,
    pending: a.pending,
    edited: a.edited,
  };
}

/**
 * §4.7's `GET /v1/activities`. Unpaginated: every activity matching the filter comes back in
 * one response, since the Activities panel needs the full set at once to draw the map, total
 * the stats and compute its type/distance facets.
 */
export async function listActivities(query: ActivityQuery = {}, signal?: AbortSignal): Promise<Activity[]> {
  // Conditional spread again (see getUploadHistory below): exactOptionalPropertyTypes:true
  // rejects `{ signal: undefined }` against RequestInit's `signal?: AbortSignal | null`.
  const res = await fetch(`${API_BASE_URL}${API_V1}/activities${activityQueryString(query)}`, {
    ...(signal ? { signal } : {}),
    credentials: 'include',
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, `activity list failed (${res.status})`));
  }
  const body = (await res.json()) as ActivitiesBody;
  return body.activities.map(toActivity);
}

/**
 * §4.7.4's `PATCH /v1/activities/{id}` — the edit-type-name-and-description dialog's Save
 * button. Full-replace-on-save, not per-field: all three fields commit together.
 * Returns the updated row so the caller can `reload()` the list (EditActivityDialog.tsx does,
 * matching the "just refetch" convention an upload completion already uses) rather than
 * needing this return value directly — returned anyway: one fewer thing for a caller to
 * assume about the request.
 */
export async function updateActivity(
  id: string,
  patch: { activityType: string; name: string; description: string },
): Promise<Activity> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/activities/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ activity_type: patch.activityType, name: patch.name, description: patch.description }),
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, `activity update failed (${res.status})`));
  }
  const body = (await res.json()) as ActivityRowBody;
  return toActivity(body);
}

/**
 * §4.7.5's `DELETE /v1/activities/{id}` — a full purge (track, fog/heatmap coverage,
 * the raw upload), not a soft delete. `204 No Content` on success, same
 * convention `logout` already uses for "succeeded, nothing to say back" — nothing to parse or
 * return here either.
 */
export async function deleteActivity(id: string): Promise<void> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/activities/${id}`, {
    method: 'DELETE',
    credentials: 'include',
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, `activity delete failed (${res.status})`));
  }
}

/**
 * One row of FR-3.7's duplicate list (`GET /v1/activities/duplicates`, `IMPLEMENTATION.md`
 * §4.6) — an activity cross-source dedup took out of circulation, alongside the richer copy
 * that superseded it. Both sides carry `source`, since that's the actual answer to "why is
 * this gone": the same activity, already in from somewhere else. Mirrors the Android app's own
 * `SyncStatusActivity` duplicates section (`apps/android/docs/IMPLEMENTATION.md` §6).
 */
export interface DuplicateActivity {
  id: string;
  startedAt: string;
  activityType: string;
  distanceMeters: number | null;
  source: string;
  supersededBy: {
    id: string;
    source: string;
    startedAt: string;
  };
}

interface DuplicateActivityBody {
  id: string;
  started_at: string;
  activity_type: string;
  distance_meters: number | null;
  source: string;
  superseded_by: {
    id: string;
    source: string;
    started_at: string;
  };
}

interface DuplicatesBody {
  duplicates: DuplicateActivityBody[];
}

/** No filter, no pagination — duplicates are a small set beside the history they came from,
 *  the same reasoning `duplicatesQuery`'s own server-side comment gives. */
export async function getDuplicates(signal?: AbortSignal): Promise<DuplicateActivity[]> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/activities/duplicates`, {
    credentials: 'include',
    ...(signal ? { signal } : {}),
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, `duplicates fetch failed (${res.status})`));
  }
  const body = (await res.json()) as DuplicatesBody;
  return body.duplicates.map((d) => ({
    id: d.id,
    startedAt: d.started_at,
    activityType: d.activity_type,
    distanceMeters: d.distance_meters,
    source: d.source,
    supersededBy: {
      id: d.superseded_by.id,
      source: d.superseded_by.source,
      startedAt: d.superseded_by.started_at,
    },
  }));
}

/**
 * §4.7's range summary: the aggregate behind the header badge, the panel's subtext and the
 * histogram's stats line. Unlike the per-row metrics these are never null — a sum over zero
 * matching activities is legitimately 0, not unknown.
 */
export interface ActivityTotals {
  count: number;
  distanceMeters: number;
  durationSeconds: number;
  elevationGainM: number;
}

interface ActivityTotalsBody {
  count: number;
  distance_meters: number;
  duration_seconds: number;
  elevation_gain_m: number;
}

/** `GET /v1/activities/summary`, over the same from/to/types filter as listActivities. */
export async function getActivityTotals(query: ActivityQuery = {}, signal?: AbortSignal): Promise<ActivityTotals> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/activities/summary${activityQueryString(query)}`, {
    ...(signal ? { signal } : {}),
    credentials: 'include',
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, `activity summary failed (${res.status})`));
  }
  const body = (await res.json()) as ActivityTotalsBody;
  return {
    count: body.count,
    distanceMeters: body.distance_meters,
    durationSeconds: body.duration_seconds,
    elevationGainM: body.elevation_gain_m,
  };
}

/**
 * One day of §4.7's histogram — a day that actually has activities. Days with none are
 * simply absent, at every level: the endpoint never returns them, and the range picker's
 * strip never draws a slot for one (RangePicker.tsx), so a bar's neighbours are the
 * adjacent days the user *recorded*, however far apart their real dates are.
 */
export interface HistogramBucket {
  date: string;
  count: number;
  distanceMeters: number;
}

export interface ActivityDayPage {
  /** "day" — §4.7's resolved granularity. Present so a future weekly rollup is detectable. */
  bucket: string;
  /** The first and last day this page actually returned, or "" for an empty page. */
  from: string;
  to: string;
  /** Ascending by date, exactly `limit` entries unless the history ran out. */
  days: HistogramBucket[];
  /** This user's first activity's UTC day, or null if they have none — how far back the
   *  range picker's strip can keep paging, independent of this page's own bounds. */
  earliest: string | null;
}

interface HistogramBody {
  bucket: string;
  from: string;
  to: string;
  buckets: { date: string; count: number; distance_meters: number }[];
  earliest?: string;
}

export interface DayPageQuery {
  /** How many days-with-activity to return. */
  limit: number;
  /** Return the `limit` most recent days strictly before this one; omit for the newest. */
  before?: string;
}

/**
 * `GET /v1/activities/histogram?days=&before=` — §4.7's activity-day pagination mode.
 *
 * Never takes the type/distance filter: this is the whole timeline a selected sub-range is
 * highlighted against, not a view of the current one. It pages by *days that have activity*
 * rather than by calendar window because that is what the strip draws — one bar per such
 * day, packed — so a page is exactly `limit` bars however sparse the underlying history is.
 * The endpoint's other mode (`from`/`to`, a real calendar window) has no reader here; §4.8's
 * planned year grid is the thing that wants it.
 */
export async function getActivityDayPage(query: DayPageQuery, signal?: AbortSignal): Promise<ActivityDayPage> {
  const params = new URLSearchParams({ days: String(query.limit) });
  if (query.before) params.set('before', query.before);
  const res = await fetch(`${API_BASE_URL}${API_V1}/activities/histogram?${params.toString()}`, {
    ...(signal ? { signal } : {}),
    credentials: 'include',
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, `activity histogram failed (${res.status})`));
  }
  const body = (await res.json()) as HistogramBody;
  return {
    bucket: body.bucket,
    from: body.from,
    to: body.to,
    days: body.buckets.map((b) => ({ date: b.date, count: b.count, distanceMeters: b.distance_meters })),
    earliest: body.earliest ?? null,
  };
}

export interface TrackMetricPoint {
  lon: number;
  lat: number;
  speedMps: number;
  /** Cumulative distance from the first vertex, in meters — always present, unlike
   *  heartrate/elevationM below. */
  distanceM: number;
  /** Only present when `heartrateAvailable` is true on the parent response — see
   *  ActivityTrackMetrics' own doc comment. */
  heartrate?: number;
  /** Only present when `elevationAvailable` is true on the parent response. */
  elevationM?: number;
}

export interface ActivityTrackMetrics {
  activityId: string;
  heartrateAvailable: boolean;
  elevationAvailable: boolean;
  points: TrackMetricPoint[];
}

interface TrackMetricPointBody {
  lon: number;
  lat: number;
  speed_mps: number;
  distance_m: number;
  heartrate?: number;
  elevation_m?: number;
}

interface ActivityTrackMetricsBody {
  activity_id: string;
  heartrate_available: boolean;
  elevation_available: boolean;
  points: TrackMetricPointBody[];
}

/**
 * `GET /v1/activities/track-metrics/{id}` — per-simplified-vertex distance, speed, and (when
 * available) heart rate and elevation for one activity's own track, matching the exact vertex
 * sequence its display trajectory already renders. Backs MapView.tsx's colored zone segments
 * and TrackProfile.tsx's straight-line profile, both shown only while exactly one activity has
 * row-click focus. `heartrateAvailable`/`elevationAvailable` are false for any activity with
 * even one gap in that metric's coverage — see the backend's own all-or-nothing note.
 */
export async function getActivityTrackMetrics(
  activityId: string,
  signal?: AbortSignal,
): Promise<ActivityTrackMetrics> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/activities/track-metrics/${activityId}`, {
    ...(signal ? { signal } : {}),
    credentials: 'include',
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, `track metrics failed (${res.status})`));
  }
  const body = (await res.json()) as ActivityTrackMetricsBody;
  return {
    activityId: body.activity_id,
    heartrateAvailable: body.heartrate_available,
    elevationAvailable: body.elevation_available,
    points: body.points.map((p) => ({
      lon: p.lon,
      lat: p.lat,
      speedMps: p.speed_mps,
      distanceM: p.distance_m,
      ...(p.heartrate !== undefined ? { heartrate: p.heartrate } : {}),
      ...(p.elevation_m !== undefined ? { elevationM: p.elevation_m } : {}),
    })),
  };
}

/**
 * A track edit (§4.7.7), as the server stores it: every value is a point timestamp in unix
 * milliseconds. A point survives when it's inside `keep` (inclusive; absent keeps everything),
 * outside every `remove` range (inclusive), and not in `drop`. Mirrors `ingest.TrackEdit`.
 */
export interface TrackEdit {
  keep?: [number, number];
  remove?: [number, number][];
  drop?: number[];
}

/** One recorded point: [lon, lat, unix ms]. */
export type TrackPoint = [number, number, number];

export interface ActivityTrackPoints {
  /** Every point the activity is processed from, clipped by Private locations, before the user's edit. */
  points: TrackPoint[];
  /** The edit currently applied on top of `points`, or null for an unedited track. */
  edit: TrackEdit | null;
}

/** `GET /v1/activities/track-points/{id}` — what the track editor opens with. */
export async function getActivityTrackPoints(activityId: string, signal?: AbortSignal): Promise<ActivityTrackPoints> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/activities/track-points/${activityId}`, {
    ...(signal ? { signal } : {}),
    credentials: 'include',
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, `track points failed (${res.status})`));
  }
  return (await res.json()) as ActivityTrackPoints;
}

/**
 * `POST /v1/activities/track-edit/{id}` — the editor's Apply. Sends the complete new edit
 * (null resets to the original track); the server marks the activity pending and reprocesses
 * it in the background, so this resolves as soon as that's queued (`202`).
 */
export async function saveActivityTrackEdit(activityId: string, edit: TrackEdit | null): Promise<void> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/activities/track-edit/${activityId}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ edit }),
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, `track edit failed (${res.status})`));
  }
}

/** One of the account's Private locations (FR-8.1): a circle whose contents are clipped off
 *  the ends of every track at ingest. `name` is `''` when unset. */
export interface PrivateLocation {
  id: string;
  name: string;
  lon: number;
  lat: number;
  radiusM: number;
}

/** What POST and PATCH send — the whole circle, never a partial update. */
export type PrivateLocationInput = Omit<PrivateLocation, 'id'>;

interface PrivateLocationBody {
  id: string;
  name: string;
  lon: number;
  lat: number;
  radius_m: number;
}

function toPrivateLocation(body: PrivateLocationBody): PrivateLocation {
  return { id: body.id, name: body.name, lon: body.lon, lat: body.lat, radiusM: body.radius_m };
}

export const PRIVATE_LOCATION_MIN_RADIUS_M = 50;
export const PRIVATE_LOCATION_MAX_RADIUS_M = 2000;

export async function listPrivateLocations(signal?: AbortSignal): Promise<PrivateLocation[]> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/private-locations`, {
    ...(signal ? { signal } : {}),
    credentials: 'include',
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, `private locations failed (${res.status})`));
  }
  return ((await res.json()) as PrivateLocationBody[]).map(toPrivateLocation);
}

/**
 * Creates (no `id`) or replaces a Private location. The server marks every activity the old or
 * new circle could clip as pending and reprocesses them in the background, so this resolves as
 * soon as that's queued — the Activity List's Pending badges show the rest.
 */
export async function savePrivateLocation(input: PrivateLocationInput, id?: string): Promise<PrivateLocation> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/private-locations${id ? `/${id}` : ''}`, {
    method: id ? 'PATCH' : 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ name: input.name, lon: input.lon, lat: input.lat, radius_m: input.radiusM }),
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, `saving the private location failed (${res.status})`));
  }
  return toPrivateLocation((await res.json()) as PrivateLocationBody);
}

export async function deletePrivateLocation(id: string): Promise<void> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/private-locations/${id}`, { method: 'DELETE', credentials: 'include' });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, `deleting the private location failed (${res.status})`));
  }
}

/**
 * `GET /v1/coverage/status` — whether the account still has an unfinished job that changes its
 * Fog/Heatmap rasters (an upload not yet parsed, a track edit, a re-render). Polled after an
 * upload or delete by `useCoverageRefresh.ts`, which refetches both layers once this reports
 * nothing left.
 */
export interface CoverageStatus {
  rendering: boolean;
  /** When any of the account's Fog/Heatmap tiles was last written (Unix ms; 0 before the
   *  first) — changes mid-job too, e.g. when a Pending activity drops out of coverage. */
  version: number;
}

export async function getCoverageStatus(signal?: AbortSignal): Promise<CoverageStatus> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/coverage/status`, {
    ...(signal ? { signal } : {}),
    credentials: 'include',
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, `coverage status failed (${res.status})`));
  }
  return (await res.json()) as CoverageStatus;
}
