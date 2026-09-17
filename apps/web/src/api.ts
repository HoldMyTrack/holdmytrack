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
 * Simple email+password auth (services/server/internal/httpapi/auth.go) — every request in
 * this file sends `credentials: 'include'` now (and `uploadFile`'s XHR sets
 * `withCredentials`) so the browser attaches the session cookie these endpoints set/read.
 * `signup`/`login` throw on failure rather than returning a result union, matching every
 * other function here's "throw with the server's own message" convention — the caller
 * (AuthGate.tsx) catches and displays it.
 */
/** The Settings page's own fields (SettingsPage.tsx) — carried by both `AuthUser` and
 *  `DemoUser` uniformly, since a demo account is a real `users` row with real column
 *  defaults, not a special case that skips having them. `displayName`/`country`/`avatarUrl`
 *  are `''` when unset, not `undefined` — every caller (units.ts, SettingsPage.tsx) checks
 *  for an empty string, never an absent field. `country` is an ISO 3166-1 alpha-2 code or
 *  `''` (defaults the whole app to metric — see `ui/units.ts`). `avatarUrl` is already a
 *  full API path (`/v1/account/avatar?v=...`) — callers prefix `API_BASE_URL`. */
export interface UserProfile {
  displayName: string;
  country: string;
  avatarUrl: string;
  privacyTrimM: number;
}

export interface AuthUser extends UserProfile {
  email: string;
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
  display_name: string;
  country: string;
  avatar_url: string;
  privacy_trim_m: number;
}

function toProfile(body: AuthResponseBody): UserProfile {
  return { displayName: body.display_name, country: body.country, avatarUrl: body.avatar_url, privacyTrimM: body.privacy_trim_m };
}

function toSessionUser(body: AuthResponseBody): SessionUser {
  return body.isDemo ? toProfile(body) : { email: body.email, ...toProfile(body) };
}

async function postAuth(path: string, email: string, password: string): Promise<AuthUser> {
  const res = await fetch(`${API_BASE_URL}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ email, password }),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(text || `request failed (${res.status})`);
  }
  const body = (await res.json()) as AuthResponseBody;
  return { email: body.email, ...toProfile(body) };
}

export function signup(email: string, password: string): Promise<AuthUser> {
  return postAuth(`${API_V1}/auth/signup`, email, password);
}

export function login(email: string, password: string): Promise<AuthUser> {
  return postAuth(`${API_V1}/auth/login`, email, password);
}

/** VISION.md §8.2's "no-signup, drag-a-file-in, see-your-fog-map page" — a real
 *  session behind the scenes (auth.go's handleDemoStart), so nothing else in this file needs
 *  a demo-specific branch: uploads, tiles, everything just works once this resolves. */
export async function startDemo(): Promise<DemoUser> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/auth/demo`, { method: 'POST', credentials: 'include' });
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(text || `couldn't start the demo (${res.status})`);
  }
  const body = (await res.json()) as AuthResponseBody;
  return toProfile(body);
}

/** IMPLEMENTATION.md §4.11's password recovery. Always resolves — never rejects on "no such
 *  account," matching the backend's own deliberate non-leaking response — there is nothing
 *  more specific for a caller to do differently either way. */
export async function forgotPassword(email: string): Promise<void> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/auth/forgot-password`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ email }),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(text || `request failed (${res.status})`);
  }
}

/** Never a `DemoUser` — a reset always targets a real, already-claimed account (the backend
 *  only ever creates a reset token for one — see auth.go's sendPasswordReset). */
export async function resetPassword(token: string, password: string): Promise<AuthUser> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/auth/reset-password`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ token, password }),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(text || `request failed (${res.status})`);
  }
  const body = (await res.json()) as AuthResponseBody;
  return { email: body.email, ...toProfile(body) };
}

export async function logout(): Promise<void> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/auth/logout`, { method: 'POST', credentials: 'include' });
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(text || `logout failed (${res.status})`);
  }
}

/** Resolves to the signed-in user (real or demo), or `null` if there is no valid session —
 *  not an error: this is the normal "nobody's signed in yet" state on first load, checked
 *  deliberately rather than treated as a failure. */
export async function getCurrentUser(): Promise<SessionUser | null> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/auth/me`, { credentials: 'include' });
  if (res.status === 401) return null;
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(text || `session check failed (${res.status})`);
  }
  const body = (await res.json()) as AuthResponseBody;
  return toSessionUser(body);
}

/** SettingsPage.tsx's Save button — a full replace of all three fields at once (`PATCH
 *  /v1/account/settings`), not per-field auto-save. Returns the updated profile so the
 *  caller can merge it into AuthContext directly (`useAuth().updateUser`) with no extra
 *  round trip. */
export async function updateSettings(patch: { displayName: string; country: string; privacyTrimM: number }): Promise<UserProfile> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/account/settings`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ display_name: patch.displayName, country: patch.country, privacy_trim_m: patch.privacyTrimM }),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(text || `request failed (${res.status})`);
  }
  const body = (await res.json()) as AuthResponseBody;
  return toProfile(body);
}

/** `POST /v1/account/avatar` — a small image, not large enough to need `uploadFile`'s
 *  XMLHttpRequest-for-progress treatment (5 MiB server-side cap). */
export async function uploadAvatar(file: File): Promise<UserProfile> {
  const form = new FormData();
  form.append('file', file);
  const res = await fetch(`${API_BASE_URL}${API_V1}/account/avatar`, { method: 'POST', credentials: 'include', body: form });
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(text || `upload failed (${res.status})`);
  }
  const body = (await res.json()) as AuthResponseBody;
  return toProfile(body);
}

export async function removeAvatar(): Promise<UserProfile> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/account/avatar`, { method: 'DELETE', credentials: 'include' });
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(text || `request failed (${res.status})`);
  }
  const body = (await res.json()) as AuthResponseBody;
  return toProfile(body);
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
        reject(new Error(xhr.responseText || `upload failed (${xhr.status})`));
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
  status: 'processing' | 'done' | 'failed';
  error?: string;
  submittedAt: string;
  /** Set only when status is "done" — this activity's own date and distance, so a finished
   *  row can read "9 Sep · 34.7 km" rather than just repeating its own filename. */
  startedAt?: string;
  distanceMeters?: number;
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
  status: string;
  error?: string;
  submitted_at: string;
  started_at?: string;
  distance_meters?: number;
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
}

export async function getUploadHistory(query: UploadHistoryQuery = {}, signal?: AbortSignal): Promise<UploadHistoryPage> {
  const params = new URLSearchParams();
  if (query.limit !== undefined) params.set('limit', String(query.limit));
  if (query.offset !== undefined) params.set('offset', String(query.offset));
  const qs = params.toString();
  const res = await fetch(`${API_BASE_URL}${API_V1}/uploads${qs ? `?${qs}` : ''}`, {
    credentials: 'include',
    ...(signal ? { signal } : {}),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(text || `upload history failed (${res.status})`);
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
      status: u.status as UploadHistoryRow['status'],
      submittedAt: u.submitted_at,
      ...(u.error ? { error: u.error } : {}),
      ...(u.started_at ? { startedAt: u.started_at } : {}),
      ...(u.distance_meters !== undefined ? { distanceMeters: u.distance_meters } : {}),
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
    const text = await res.text().catch(() => '');
    throw new Error(text || `activity list failed (${res.status})`);
  }
  const body = (await res.json()) as ActivitiesBody;
  return body.activities.map(toActivity);
}

/**
 * §4.7.4's `PATCH /v1/activities/{id}` — the edit-type-name-and-description dialog's Save
 * button. Full-replace-on-save like `updateSettings`, not per-field: all three fields commit
 * together.
 * Returns the updated row so the caller can `reload()` the list (EditActivityDialog.tsx does,
 * matching the "just refetch" convention an upload completion already uses) rather than
 * needing this return value directly — returned anyway for the same reason `updateSettings`
 * returns the updated profile: one fewer thing for a caller to assume about the request.
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
    const text = await res.text().catch(() => '');
    throw new Error(text || `activity update failed (${res.status})`);
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
    const text = await res.text().catch(() => '');
    throw new Error(text || `activity delete failed (${res.status})`);
  }
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
    const text = await res.text().catch(() => '');
    throw new Error(text || `activity summary failed (${res.status})`);
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
    const text = await res.text().catch(() => '');
    throw new Error(text || `activity histogram failed (${res.status})`);
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

/**
 * §4.8's activity graph cells: one calendar year of §4.7's histogram, via the same endpoint's
 * `from`/`to` calendar-window mode rather than a new one — see
 * IMPLEMENTATION.md §4.8, "reuses GET /v1/activities/histogram's day-bucket
 * shape for the grid cells." Empty days are absent, same as the day-page mode above; the
 * caller lays out the full Jan 1–Dec 31 grid itself and places these `days` into it.
 */
export interface ActivityYearGraph {
  from: string;
  to: string;
  days: HistogramBucket[];
  earliest: string | null;
}

export async function getActivityYearGraph(year: number, signal?: AbortSignal): Promise<ActivityYearGraph> {
  const params = new URLSearchParams({ from: `${year}-01-01`, to: `${year}-12-31` });
  const res = await fetch(`${API_BASE_URL}${API_V1}/activities/histogram?${params.toString()}`, {
    ...(signal ? { signal } : {}),
    credentials: 'include',
  });
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(text || `activity year graph failed (${res.status})`);
  }
  const body = (await res.json()) as HistogramBody;
  return {
    from: body.from,
    to: body.to,
    days: body.buckets.map((b) => ({ date: b.date, count: b.count, distanceMeters: b.distance_meters })),
    earliest: body.earliest ?? null,
  };
}

/** One of §4.8's two stat-card rows (a selected year, or all-time). */
export interface ActivityStatsBlock {
  count: number;
  distanceMeters: number;
  activeDays: number;
  longestStreakDays: number;
}

export interface ActivityGraphStats {
  year: number;
  /** The requested year's own numbers — the subtotal line under that year's grid. */
  yearStats: ActivityStatsBlock;
  /** Every activity ever recorded, regardless of year — the page's header cards. */
  allTime: ActivityStatsBlock;
}

interface ActivityStatsBlockBody {
  count: number;
  distance_meters: number;
  active_days: number;
  longest_streak_days: number;
}

interface ActivityGraphStatsBody {
  year: number;
  year_stats: ActivityStatsBlockBody;
  all_time: ActivityStatsBlockBody;
}

function toStatsBlock(b: ActivityStatsBlockBody): ActivityStatsBlock {
  return {
    count: b.count,
    distanceMeters: b.distance_meters,
    activeDays: b.active_days,
    longestStreakDays: b.longest_streak_days,
  };
}

/**
 * `GET /v1/activities/graph-stats?year=` — §4.8's four stat-card numbers (count, distance,
 * active days, longest streak), for one calendar year and for all time in the same response.
 * Deliberately not derived from `getActivityYearGraph`'s day buckets client-side, even though
 * three of the four technically could be: longest streak is real server-side work regardless,
 * and computing the other three two different ways (client-side per year, server-side for
 * all-time) would be more code than just reading all four from here every time.
 */
export async function getActivityGraphStats(year: number, signal?: AbortSignal): Promise<ActivityGraphStats> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/activities/graph-stats?year=${year}`, {
    ...(signal ? { signal } : {}),
    credentials: 'include',
  });
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(text || `activity graph stats failed (${res.status})`);
  }
  const body = (await res.json()) as ActivityGraphStatsBody;
  return { year: body.year, yearStats: toStatsBlock(body.year_stats), allTime: toStatsBlock(body.all_time) };
}

export type TrendBucket = 'week' | 'month';

export interface TrendPeriod {
  periodStart: string;
  count: number;
  distanceMeters: number;
  movingSeconds: number;
  elevationGainM: number;
}

export interface ActivityTrends {
  bucket: TrendBucket;
  from: string;
  to: string;
  periods: TrendPeriod[];
}

interface TrendPeriodBody {
  period_start: string;
  count: number;
  distance_meters: number;
  moving_seconds: number;
  elevation_gain_m: number;
}

interface ActivityTrendsBody {
  bucket: TrendBucket;
  from: string;
  to: string;
  periods: TrendPeriodBody[];
}

/**
 * `GET /v1/activities/trends?bucket=week|month` — VISION.md §5.3's "trends": count/
 * distance/moving-time/elevation-gain per calendar bucket, over the trailing 12 months by
 * default (same default window as the year-graph's histogram calls).
 */
export async function getActivityTrends(bucket: TrendBucket, signal?: AbortSignal): Promise<ActivityTrends> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/activities/trends?bucket=${bucket}`, {
    ...(signal ? { signal } : {}),
    credentials: 'include',
  });
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(text || `activity trends failed (${res.status})`);
  }
  const body = (await res.json()) as ActivityTrendsBody;
  return {
    bucket: body.bucket,
    from: body.from,
    to: body.to,
    periods: body.periods.map((p) => ({
      periodStart: p.period_start,
      count: p.count,
      distanceMeters: p.distance_meters,
      movingSeconds: p.moving_seconds,
      elevationGainM: p.elevation_gain_m,
    })),
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
    const text = await res.text().catch(() => '');
    throw new Error(text || `track metrics failed (${res.status})`);
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
