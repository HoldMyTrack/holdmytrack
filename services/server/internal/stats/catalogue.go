package stats

import "fmt"

// kind is whether a stat is read as it stands when the snapshot runs (state) or counted over
// the day itself by a timestamp column (flow); IMPLEMENTATION.md §4.32.
type kind int

const (
	state kind = iota
	flow
)

// stat is one entry of the catalogue. A stat with key returns one value; one with prefix
// returns (group, value) rows, each stored as keyFor(prefix, group), or, with fold, (group,
// value, accounts) rows folded by foldSmallGroups first. groups are written as 0 when the
// query has no row for them, so a series exists before its first non-zero day.
//
// query follows queryHead, which defines p (d0 and d1, the start of the day and of the next;
// demo, the demo account's id) and acct, the accounts every value counts: not deleted, not the
// demo. A query that needs CTEs of its own starts with ", name AS (…)". A NULL value is left
// out (a percentile over no rows).
type stat struct {
	key    string
	prefix string
	kind   kind
	fold   bool
	groups []string
	query  string
}

// queryHead is put before every catalogue query. The casts type $1–$3 even in a query that
// doesn't refer to p.
const queryHead = `WITH p AS (SELECT $1::timestamptz AS d0, $2::timestamptz AS d1, $3::uuid AS demo),
acct AS (
	SELECT u.id, u.email_verified, u.password_hash IS NOT NULL AS has_password, u.locale, u.country, u.last_seen_at
	FROM users u, p WHERE u.deleted_at IS NULL AND u.id <> p.demo
)
`

// sources are activities.source's values.
var sources = []string{"upload", "healthconnect", "healthkit", "recorded"}

// activeSince is the SQL for "last seen in the n days ending with D".
func activeSince(n int) string {
	return fmt.Sprintf(`last_seen_at >= (SELECT d0 FROM p) - make_interval(days => %d)`, n-1)
}

var catalogue = []stat{
	// Accounts and activity.
	{key: "accounts", kind: state, query: `SELECT count(*) FROM acct`},
	{key: "accounts_verified", kind: state, query: `SELECT count(*) FROM acct WHERE email_verified`},
	// Deleted accounts count until their row is purged; deletions are derived when read.
	{key: "signups", kind: flow, query: `SELECT count(*) FROM users u, p WHERE u.created_at >= p.d0 AND u.created_at < p.d1 AND u.id <> p.demo`},
	{key: "active_1d", kind: state, query: `SELECT count(*) FROM acct WHERE ` + activeSince(1)},
	{key: "active_7d", kind: state, query: `SELECT count(*) FROM acct WHERE ` + activeSince(7)},
	{key: "active_30d", kind: state, query: `SELECT count(*) FROM acct WHERE ` + activeSince(30)},
	{prefix: "signin_method", kind: state, groups: []string{"password", "google", "facebook"}, query: `
		SELECT 'password', count(*) FROM acct WHERE has_password
		UNION ALL
		SELECT i.provider, count(*) FROM user_identities i JOIN acct a ON a.id = i.user_id GROUP BY i.provider`},
	{prefix: "locale", kind: state, query: `SELECT coalesce(locale, ''), count(*) FROM acct GROUP BY 1`},

	// Countries: folded by the accounts behind each value.
	{prefix: "accounts_by_country", kind: state, fold: true, query: `
		SELECT coalesce(country, ''), count(*), count(*) FROM acct GROUP BY 1`},
	{prefix: "active_30d_by_country", kind: state, fold: true, query: `
		SELECT coalesce(country, ''), count(*), count(*) FROM acct WHERE ` + activeSince(30) + ` GROUP BY 1`},
	{prefix: "activity_accounts_by_country", kind: state, fold: true, query: `
		SELECT c.code, count(DISTINCT a.user_id), count(DISTINCT a.user_id)
		FROM activity_country ac
		JOIN activities a ON a.id = ac.activity_id
		JOIN acct u ON u.id = a.user_id
		JOIN admin_countries c ON c.id = ac.country_id
		GROUP BY c.code`},
	{prefix: "activities_by_country", kind: state, fold: true, query: `
		SELECT c.code, count(*), count(DISTINCT a.user_id)
		FROM activity_country ac
		JOIN activities a ON a.id = ac.activity_id
		JOIN acct u ON u.id = a.user_id
		JOIN admin_countries c ON c.id = ac.country_id
		GROUP BY c.code`},

	// Activities.
	{key: "activities", kind: state, query: `SELECT count(*) FROM activities a JOIN acct u ON u.id = a.user_id`},
	{key: "activities_added", kind: flow, query: `
		SELECT count(*) FROM activities a JOIN acct u ON u.id = a.user_id, p
		WHERE a.created_at >= p.d0 AND a.created_at < p.d1`},
	{prefix: "activities_by_source", kind: state, groups: sources, query: `
		SELECT a.source, count(*) FROM activities a JOIN acct u ON u.id = a.user_id GROUP BY a.source`},
	{prefix: "activities_added_by_source", kind: flow, groups: sources, query: `
		SELECT a.source, count(*) FROM activities a JOIN acct u ON u.id = a.user_id, p
		WHERE a.created_at >= p.d0 AND a.created_at < p.d1 GROUP BY a.source`},
	{prefix: "activities_per_account", kind: state, query: `
		, per AS (SELECT count(*)::float8 AS c FROM activities a JOIN acct u ON u.id = a.user_id GROUP BY a.user_id),
		v AS (
			SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY c) AS p50,
			       percentile_cont(0.9) WITHIN GROUP (ORDER BY c) AS p90,
			       max(c) AS mx
			FROM per
		)
		SELECT g, x FROM v, LATERAL (VALUES ('p50', v.p50), ('p90', v.p90), ('max', v.mx)) AS t(g, x)`},

	// Storage the database measures. A photo's files are shared by the copies of a Story it was
	// sent in (IMPLEMENTATION.md §4.23), so each image_key counts once.
	{key: "photos", kind: state, query: `SELECT count(*) FROM activity_photos ph JOIN acct u ON u.id = ph.user_id`},
	{key: "storage_bytes.photos", kind: state, query: `
		SELECT coalesce(sum(b), 0) FROM (
			SELECT DISTINCT ON (ph.image_key) ph.bytes AS b
			FROM activity_photos ph JOIN acct u ON u.id = ph.user_id
		) f`},
	{key: "storage_bytes.exports", kind: state, query: `
		SELECT coalesce(sum(s), 0)
		FROM exports e JOIN acct u ON u.id = e.user_id, unnest(e.part_sizes) s
		WHERE e.expires_at IS NULL OR e.expires_at > NOW()`},
	// Originals, by the size recorded beside each key; split pieces share one, so each key
	// counts once. Masks are in the database: pg_column_size reads each one's stored size.
	{key: "storage_bytes.originals", kind: state, query: `
		SELECT coalesce(sum(b), 0) FROM (
			SELECT max(a.raw_bytes) AS b FROM activities a JOIN acct u ON u.id = a.user_id
			WHERE a.raw_payload_key IS NOT NULL GROUP BY a.raw_payload_key
		) f`},
	{key: "storage_bytes.masks", kind: state, query: `
		SELECT coalesce(sum(pg_column_size(d.png)), 0)
		FROM activity_tile_mask_data d JOIN activities a ON a.id = d.activity_id JOIN acct u ON u.id = a.user_id`},
	{key: "storage_bytes.fog", kind: state, query: `
		SELECT coalesce(sum(f.bytes), 0) FROM fog_tiles f JOIN acct u ON u.id = f.user_id`},
	{key: "storage_bytes.heatmap", kind: state, query: `
		SELECT coalesce(sum(f.heatmap_bytes), 0) FROM fog_tiles f JOIN acct u ON u.id = f.user_id`},
	// Everything an account stores, over accounts storing anything: what a quota would bound.
	// A photo shared with a Story copy counts for each account holding it.
	{prefix: "storage_bytes_per_account", kind: state, query: `
		, per AS (
			SELECT user_id, sum(b)::float8 AS b FROM (
				SELECT a.user_id, max(a.raw_bytes) AS b FROM activities a
				WHERE a.raw_payload_key IS NOT NULL GROUP BY a.user_id, a.raw_payload_key
				UNION ALL
				SELECT a.user_id, sum(pg_column_size(d.png)) FROM activity_tile_mask_data d
				JOIN activities a ON a.id = d.activity_id GROUP BY a.user_id
				UNION ALL
				SELECT user_id, sum(coalesce(bytes, 0) + coalesce(heatmap_bytes, 0)) FROM fog_tiles GROUP BY user_id
				UNION ALL
				SELECT user_id, max(bytes) FROM activity_photos GROUP BY user_id, image_key
			) x
			WHERE user_id IN (SELECT id FROM acct)
			GROUP BY user_id HAVING sum(b) > 0
		),
		v AS (
			SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY b) AS p50,
			       percentile_cont(0.9) WITHIN GROUP (ORDER BY b) AS p90,
			       max(b) AS mx
			FROM per
		)
		SELECT g, x FROM v, LATERAL (VALUES ('p50', v.p50), ('p90', v.p90), ('max', v.mx)) AS t(g, x)`},
	// Rows naming an object whose size isn't recorded yet; 0 once backfill-object-bytes has
	// run (docs/DEPLOY.md).
	{key: "storage_bytes_unmeasured_rows", kind: state, query: `
		SELECT (SELECT count(*) FROM activities a JOIN acct u ON u.id = a.user_id
		        WHERE a.raw_payload_key IS NOT NULL AND a.raw_bytes IS NULL)
		     + (SELECT count(*) FILTER (WHERE f.object_key IS NOT NULL AND f.bytes IS NULL)
		             + count(*) FILTER (WHERE f.heatmap_object_key IS NOT NULL AND f.heatmap_bytes IS NULL)
		        FROM fog_tiles f JOIN acct u ON u.id = f.user_id)`},

	// Features.
	{key: "stories", kind: state, query: `SELECT count(*) FROM stories s JOIN acct u ON u.id = s.user_id`},
	{key: "stories_added", kind: flow, query: `
		SELECT count(*) FROM stories s JOIN acct u ON u.id = s.user_id, p
		WHERE s.created_at >= p.d0 AND s.created_at < p.d1`},
	{key: "story_sends", kind: flow, query: `
		SELECT count(*) FROM story_sends ss JOIN stories s ON s.id = ss.story_id JOIN acct u ON u.id = s.user_id, p
		WHERE ss.sent_at >= p.d0 AND ss.sent_at < p.d1`},
	{key: "spot_captures", kind: state, query: `SELECT count(*) FROM spot_captures c JOIN acct u ON u.id = c.user_id`},
	{key: "spot_captures_added", kind: flow, query: `
		SELECT count(*) FROM spot_captures c JOIN acct u ON u.id = c.user_id, p
		WHERE c.captured_at >= p.d0 AND c.captured_at < p.d1`},
	{prefix: "accounts_with", kind: state, groups: []string{"stories", "spots", "photos", "privacy_zones"}, query: `
		SELECT 'stories', count(*) FROM acct u WHERE EXISTS (SELECT 1 FROM stories x WHERE x.user_id = u.id)
		UNION ALL
		SELECT 'spots', count(*) FROM acct u WHERE EXISTS (SELECT 1 FROM spot_captures x WHERE x.user_id = u.id)
		UNION ALL
		SELECT 'photos', count(*) FROM acct u WHERE EXISTS (SELECT 1 FROM activity_photos x WHERE x.user_id = u.id)
		UNION ALL
		SELECT 'privacy_zones', count(*) FROM acct u WHERE EXISTS (SELECT 1 FROM privacy_zones x WHERE x.user_id = u.id)`},
	// An export's row stays for its 7 days, so the next day's snapshot still sees it.
	{key: "exports_requested", kind: flow, query: `
		SELECT count(*) FROM exports e JOIN acct u ON u.id = e.user_id, p
		WHERE e.requested_at >= p.d0 AND e.requested_at < p.d1`},

	// Jobs: finished ones by the day they finished, pending ones as they stand. Jobs without an
	// account (none today) count; the demo's don't.
	{prefix: "jobs", kind: flow, query: `
		SELECT j.kind || '.' || j.state, count(*) FROM jobs j, p
		WHERE j.user_id IS DISTINCT FROM p.demo
		  AND (j.state = 'pending' OR (j.finished_at >= p.d0 AND j.finished_at < p.d1))
		GROUP BY j.kind, j.state`},
	// "internal" is ours; every other code is the user's file.
	{prefix: "ingest_failed", kind: flow, query: `
		SELECT coalesce(j.error_code, 'internal'), count(*) FROM jobs j, p
		WHERE j.kind = 'ingest' AND j.state = 'failed' AND j.user_id IS DISTINCT FROM p.demo
		  AND j.finished_at >= p.d0 AND j.finished_at < p.d1
		GROUP BY 1`},
}
