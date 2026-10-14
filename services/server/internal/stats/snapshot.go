// Package stats keeps the service's daily statistics (IMPLEMENTATION.md §4.32): once per UTC
// day the worker runs the catalogue against the database and writes one daily_stats row per
// value. Every value is an aggregate across accounts; nothing here reads or writes one account.
package stats

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// demoUserID is the shared demo account (httpapi.DemoCustomerUserID, which a test holds this
// to), left out of every value.
const demoUserID = "22222222-2222-2222-2222-222222222222"

// DB is what a snapshot runs on: the pool, or in a test a transaction (whose Begin is a
// savepoint).
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// TakeDue takes the snapshot of the day that just ended at now, unless it's taken already.
// It returns the day and how many values it wrote (0 when there was nothing to do).
func TakeDue(ctx context.Context, db DB, now time.Time) (time.Time, int, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("stats: begin: %w", err)
	}
	var last *time.Time
	err = tx.QueryRow(ctx, `SELECT max(day)::timestamp FROM daily_stats`).Scan(&last)
	tx.Rollback(ctx) //nolint:errcheck // read only
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("stats: newest day: %w", err)
	}
	if last != nil {
		l := last.UTC()
		last = &l
	}
	day, due := dayDue(last, now)
	if !due {
		return day, 0, nil
	}
	n, err := Take(ctx, db, day)
	return day, n, err
}

// Take writes day's snapshot, all of it or none: one transaction, which a second worker
// process skips (an advisory lock) and in which a day already taken writes nothing. It
// returns how many values it wrote.
func Take(ctx context.Context, db DB, day time.Time) (int, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("stats: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('daily_stats'))`).Scan(&locked); err != nil {
		return 0, fmt.Errorf("stats: lock: %w", err)
	}
	if !locked {
		return 0, nil // another worker is taking it
	}
	var taken bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM daily_stats WHERE day = $1)`, day).Scan(&taken); err != nil {
		return 0, fmt.Errorf("stats: check day: %w", err)
	}
	if taken {
		return 0, nil
	}

	values, err := compute(ctx, tx, day)
	if err != nil {
		return 0, err
	}
	keys := make([]string, 0, len(values))
	vals := make([]float64, 0, len(values))
	for k, v := range values {
		keys = append(keys, k)
		vals = append(vals, v)
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO daily_stats (day, key, value)
		SELECT $1, k, v FROM unnest($2::text[], $3::float8[]) AS t(k, v)
		ON CONFLICT (day, key) DO NOTHING`, day, keys, vals)
	if err != nil {
		return 0, fmt.Errorf("stats: write: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("stats: commit: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// compute runs the catalogue for day and returns every value by key.
func compute(ctx context.Context, tx pgx.Tx, day time.Time) (map[string]float64, error) {
	d0 := day.UTC()
	d1 := d0.AddDate(0, 0, 1)
	out := map[string]float64{}
	for _, s := range catalogue {
		name := s.key
		if name == "" {
			name = s.prefix
		}
		rows, err := tx.Query(ctx, queryHead+s.query, d0, d1, demoUserID)
		if err != nil {
			return nil, fmt.Errorf("stats: %s: %w", name, err)
		}
		groups := map[string]float64{}
		accounts := map[string]float64{}
		for rows.Next() {
			vs, err := rows.Values()
			if err != nil {
				rows.Close()
				return nil, fmt.Errorf("stats: %s: %w", name, err)
			}
			if s.key != "" {
				if v, ok := toFloat(vs[0]); ok {
					out[s.key] = v
				}
				continue
			}
			group, _ := vs[0].(string)
			g := groupName(group)
			v, ok := toFloat(vs[1])
			if !ok {
				continue
			}
			groups[g] += v
			if s.fold {
				a, _ := toFloat(vs[2])
				accounts[g] += a
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("stats: %s: %w", name, err)
		}
		if s.key != "" {
			continue
		}
		for _, g := range s.groups {
			if _, ok := groups[g]; !ok {
				groups[g] = 0
			}
		}
		if s.fold {
			groups = foldSmallGroups(groups, accounts, minGroupAccounts)
		}
		for g, v := range groups {
			out[keyFor(s.prefix, g)] = v
		}
	}
	return out, nil
}

// toFloat is a value as the catalogue's queries return it (a count, a sum, a percentile), or
// false for NULL.
func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case nil:
		return 0, false
	case int64:
		return float64(x), true
	case int32:
		return float64(x), true
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case pgtype.Numeric:
		if !x.Valid {
			return 0, false
		}
		f, err := x.Float64Value()
		if err != nil || !f.Valid {
			return 0, false
		}
		return f.Float64, true
	}
	return 0, false
}
