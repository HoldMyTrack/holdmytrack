import { t } from './i18n';
import type { SpotCategory, SpotInArea } from './map/spots';

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
 *  rather than reading `res.text()` and constructing an `Error` themselves. */
async function errorMessageFromResponse(res: Response, fallback: string): Promise<string> {
  const text = await res.text().catch(() => '');
  return messageFromErrorBody(text, fallback);
}

/**
 * Simple email+password auth (services/server/internal/httpapi/auth.go) — every request in
 * this file sends `credentials: 'include'` so the browser attaches the session cookie these
 * endpoints set/read.
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
   *  is no "unset" state (services/server/migrations/0001_users_and_auth.sql's column is
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
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
  }
  const body = (await res.json()) as AuthResponseBody;
  return toSessionUser(body);
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
  /** The Stories it's in, newest first — the Activities panel's Story badge. */
  stories: { id: string; name: string }[];
}

/** GeoJSON bbox ordering, which is also what MapLibre's fitBounds takes as a flat array. */
export type BBox = [number, number, number, number];

/** Filters, all optional — an absent one means "no restriction", per §4.7's convention. */
export interface ActivityQuery {
  from?: string;
  to?: string;
  types?: string[];
  /** One Story's activities only (`SPEC.md` FR-14.4). */
  story?: string;
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
  stories: { id: string; name: string }[];
}

interface ActivitiesBody {
  activities: ActivityRowBody[];
}

function activityQueryString(query: ActivityQuery): string {
  const params = new URLSearchParams();
  if (query.from) params.set('from', query.from);
  if (query.to) params.set('to', query.to);
  if (query.types?.length) params.set('types', query.types.join(','));
  if (query.story) params.set('story', query.story);
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
    stories: a.stories ?? [],
  };
}

/**
 * §4.7's `GET /v1/activities`. Unpaginated: every activity matching the filter comes back in
 * one response, since the Activities panel needs the full set at once to draw the map, total
 * the stats and compute its type/distance facets.
 */
export async function listActivities(query: ActivityQuery = {}, signal?: AbortSignal): Promise<Activity[]> {
  // A conditional spread: exactOptionalPropertyTypes:true
  // rejects `{ signal: undefined }` against RequestInit's `signal?: AbortSignal | null`.
  const res = await fetch(`${API_BASE_URL}${API_V1}/activities${activityQueryString(query)}`, {
    ...(signal ? { signal } : {}),
    credentials: 'include',
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
  }
  const body = (await res.json()) as ActivitiesBody;
  return body.activities.map(toActivity);
}

/**
 * §4.7.4's `PATCH /v1/activities/{id}` — the Edit window's Save, for its Activity tab's
 * fields. Full-replace-on-save, not per-field: all three fields commit together.
 * Returns the updated row so the caller can `reload()` the list (EditActivityWindow.tsx's caller does,
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
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
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
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
  }
}

/**
 * One day of §4.7's histogram — a day that actually has activities. Days with none are
 * simply absent, at every level: the endpoint never returns them, and the date slider never
 * gives one a slot (DateRangeSlider.tsx), so a day's neighbours are the adjacent days the user
 * *recorded*, however far apart their real dates are.
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
   *  date slider can keep paging, independent of this page's own bounds. */
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
 * Never takes the type/distance filter: this is the whole history a range is picked from, not a
 * view of the current one. It pages by *days that have activity* rather than by calendar window because that
 * is what the slider's slots are — one per such day, packed — so a page is exactly `limit`
 * days however sparse the underlying history is.
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
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
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
}

export interface ActivityTrackMetrics {
  activityId: string;
  points: TrackMetricPoint[];
}

interface TrackMetricPointBody {
  lon: number;
  lat: number;
  speed_mps: number;
}

interface ActivityTrackMetricsBody {
  activity_id: string;
  points: TrackMetricPointBody[];
}

/**
 * `GET /v1/activities/track-metrics/{id}` — per-simplified-vertex speed for one activity's own
 * track, matching the exact vertex sequence its display trajectory already renders. Backs
 * MapView.tsx's pace-colored segments, shown only while exactly one activity has row-click
 * focus.
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
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
  }
  const body = (await res.json()) as ActivityTrackMetricsBody;
  return {
    activityId: body.activity_id,
    points: body.points.map((p) => ({ lon: p.lon, lat: p.lat, speedMps: p.speed_mps })),
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
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
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
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
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
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
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
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
  }
  return toPrivateLocation((await res.json()) as PrivateLocationBody);
}

export async function deletePrivateLocation(id: string): Promise<void> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/private-locations/${id}`, { method: 'DELETE', credentials: 'include' });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
  }
}

/** A Story's joint statistics, or one activity type's share of them (`SPEC.md` FR-14.1). */
export interface StoryTotals {
  count: number;
  distanceMeters: number;
  movingSeconds: number;
  elapsedSeconds: number;
}

/** A hand-picked, private set of the account's activities (`SPEC.md` FR-14). `description` is
 *  `''` when unset. */
export interface Story {
  id: string;
  name: string;
  description: string;
  /** Every member, earliest activity first. */
  activityIds: string[];
  stats: StoryTotals & { byType: (StoryTotals & { activityType: string })[] };
}

interface StoryTotalsBody {
  count: number;
  distance_meters: number;
  moving_seconds: number;
  elapsed_seconds: number;
}

interface StoryBody {
  id: string;
  name: string;
  description: string | null;
  activity_ids: string[];
  stats: StoryTotalsBody & { by_type: (StoryTotalsBody & { activity_type: string })[] };
}

function toStoryTotals(body: StoryTotalsBody): StoryTotals {
  return {
    count: body.count,
    distanceMeters: body.distance_meters,
    movingSeconds: body.moving_seconds,
    elapsedSeconds: body.elapsed_seconds,
  };
}

function toStory(body: StoryBody): Story {
  return {
    id: body.id,
    name: body.name,
    description: body.description ?? '',
    activityIds: body.activity_ids,
    stats: {
      ...toStoryTotals(body.stats),
      byType: body.stats.by_type.map((t) => ({ ...toStoryTotals(t), activityType: t.activity_type })),
    },
  };
}

/** Mirrors stories.go's maxStoryNameLen/maxStoryDescriptionLen, so a limit shows before a
 *  400 does. */
export const STORY_MAX_NAME_LEN = 200;
export const STORY_MAX_DESCRIPTION_LEN = 2000;

/** Thrown by `getStory` for a Story that doesn't exist or isn't this account's — the two are
 *  one `404` on purpose (`SPEC.md` FR-14.5). */
export class StoryNotFoundError extends Error {}

/** `GET /v1/stories/{id}`. */
export async function getStory(id: string, signal?: AbortSignal): Promise<Story> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/stories/${encodeURIComponent(id)}`, {
    ...(signal ? { signal } : {}),
    credentials: 'include',
  });
  if (res.status === 404) throw new StoryNotFoundError(t('stories.not_found'));
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
  }
  return toStory((await res.json()) as StoryBody);
}

/** `PATCH /v1/stories/{id}` — name and description, both every time. */
export async function updateStory(id: string, input: { name: string; description: string }): Promise<Story> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/stories/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ name: input.name, description: input.description }),
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
  }
  return toStory((await res.json()) as StoryBody);
}

/** `GET /v1/stories` — every Story of the account, newest first. */
export async function listStories(signal?: AbortSignal): Promise<Story[]> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/stories`, {
    ...(signal ? { signal } : {}),
    credentials: 'include',
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
  }
  return ((await res.json()) as { stories: StoryBody[] }).stories.map(toStory);
}

/** `DELETE /v1/stories/{id}` — the Story only; its activities stay. */
export async function deleteStory(id: string): Promise<void> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/stories/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    credentials: 'include',
  });
  if (!res.ok && res.status !== 404) {
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
  }
}

/** `POST /v1/stories/{id}/activities` — puts them in the Story; any already there stay as they are. */
export async function addStoryActivities(id: string, activityIds: string[]): Promise<Story> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/stories/${encodeURIComponent(id)}/activities`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ activity_ids: activityIds }),
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
  }
  return toStory((await res.json()) as StoryBody);
}

/** `DELETE /v1/stories/{id}/activities` — takes them out of the Story; the activities stay. */
export async function removeStoryActivities(id: string, activityIds: string[]): Promise<Story> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/stories/${encodeURIComponent(id)}/activities`, {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ activity_ids: activityIds }),
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
  }
  return toStory((await res.json()) as StoryBody);
}

/** `POST /v1/stories` — a new Story holding `activityIds` from the start, in one request. */
export async function createStory(input: { name: string; description: string; activityIds: string[] }): Promise<Story> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/stories`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ name: input.name, description: input.description, activity_ids: input.activityIds }),
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
  }
  return toStory((await res.json()) as StoryBody);
}

/**
 * `GET /v1/coverage/status` — whether the account still has an unfinished job that changes its
 * Fog/Heatmap rasters (an upload not yet parsed, a track edit, a re-render). Polled after an
 * upload or delete by `useCoverageRefresh.ts`, which refetches both layers once this reports
 * nothing left.
 */
export interface CoverageStatus {
  rendering: boolean;
  /** The account's map version — changes whenever any of its map tiles may have, mid-job
   *  too, e.g. when a Pending activity drops out of coverage. */
  version: number;
  /** `version` as the opaque key tile URLs carry as `cv`, so the browser can keep each tile
   *  (coverageVersion.ts). */
  tile_version: string;
}

export async function getCoverageStatus(signal?: AbortSignal): Promise<CoverageStatus> {
  const res = await fetch(`${API_BASE_URL}${API_V1}/coverage/status`, {
    ...(signal ? { signal } : {}),
    credentials: 'include',
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
  }
  return (await res.json()) as CoverageStatus;
}

/** `GET /v1/spots` — "Show in this area" (FR-15.5): the places in a box, in the chosen
 *  categories. `total` is how many the box holds; more than `spots.length` when the server cut
 *  the answer at its limit. */
export interface SpotsInArea {
  spots: SpotInArea[];
  total: number;
}

export async function getSpotsInArea(
  bbox: [west: number, south: number, east: number, north: number],
  categories: readonly SpotCategory[],
  signal?: AbortSignal,
): Promise<SpotsInArea> {
  const params = new URLSearchParams({ bbox: bbox.map((v) => v.toFixed(5)).join(','), categories: categories.join(',') });
  const res = await fetch(`${API_BASE_URL}${API_V1}/spots?${params}`, {
    ...(signal ? { signal } : {}),
    credentials: 'include',
  });
  if (!res.ok) {
    throw new Error(await errorMessageFromResponse(res, t('common.request_failed', { status: res.status })));
  }
  return (await res.json()) as SpotsInArea;
}
