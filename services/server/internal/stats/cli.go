package stats

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5"
)

// Usage is the `holdmytrack stats` subcommand's usage line.
const Usage = "usage: holdmytrack stats [<YYYY-MM-DD> | <key or prefix> [--days <n>]]"

// defaultDays is how many days a key's series shows without --days.
const defaultDays = 30

// request is what `holdmytrack stats` was asked for: one day's values (day set, or neither
// set for the newest day), the series of key and the keys under it over days, or (take) a
// snapshot of day written now.
type request struct {
	day  *time.Time
	key  string
	days int
	take bool
}

// ErrUsage is RunCLI's error for arguments it can't read.
var ErrUsage = errors.New(Usage)

// parseArgs reads the arguments after "stats". --take [<day>] is for development
// (docs/DEVELOPMENT.md): it writes day's snapshot, yesterday when none is given, as the worker
// would.
func parseArgs(args []string, now time.Time) (request, error) {
	if len(args) > 0 && args[0] == "--take" {
		if len(args) > 2 {
			return request{}, ErrUsage
		}
		day, _ := dayDue(nil, now)
		if len(args) == 2 {
			d, err := time.Parse(time.DateOnly, args[1])
			if err != nil {
				return request{}, ErrUsage
			}
			day = d
		}
		return request{day: &day, take: true}, nil
	}
	switch len(args) {
	case 0:
		return request{}, nil
	case 1, 3:
	default:
		return request{}, ErrUsage
	}
	if d, err := time.Parse(time.DateOnly, args[0]); err == nil {
		if len(args) != 1 {
			return request{}, ErrUsage
		}
		return request{day: &d}, nil
	}
	if strings.HasPrefix(args[0], "-") {
		return request{}, ErrUsage
	}
	r := request{key: args[0], days: defaultDays}
	if len(args) == 3 {
		n, err := strconv.Atoi(args[2])
		if args[1] != "--days" || err != nil || n < 1 {
			return request{}, ErrUsage
		}
		r.days = n
	}
	return r, nil
}

// row is one daily_stats row.
type row struct {
	day   time.Time
	key   string
	value float64
}

// formatValue prints a count or byte sum as a whole number and anything else (a percentile)
// to two places.
func formatValue(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// dayTable is one day's rows as a key | value table, in key order.
func dayTable(rows []row) (header []string, cells [][]string) {
	rows = slices.Clone(rows)
	slices.SortFunc(rows, func(a, b row) int { return strings.Compare(a.key, b.key) })
	for _, r := range rows {
		cells = append(cells, []string{r.key, formatValue(r.value)})
	}
	return []string{"key", "value"}, cells
}

// seriesTable is a series as a table of days down and keys across, oldest day first. A key
// with no row on a day shows "-".
func seriesTable(rows []row) (header []string, cells [][]string) {
	var keys []string
	var days []time.Time
	values := map[time.Time]map[string]float64{}
	for _, r := range rows {
		if !slices.Contains(keys, r.key) {
			keys = append(keys, r.key)
		}
		if values[r.day] == nil {
			values[r.day] = map[string]float64{}
			days = append(days, r.day)
		}
		values[r.day][r.key] = r.value
	}
	slices.Sort(keys)
	slices.SortFunc(days, func(a, b time.Time) int { return a.Compare(b) })
	for _, d := range days {
		line := []string{d.Format(time.DateOnly)}
		for _, k := range keys {
			if v, ok := values[d][k]; ok {
				line = append(line, formatValue(v))
			} else {
				line = append(line, "-")
			}
		}
		cells = append(cells, line)
	}
	return append([]string{"day"}, keys...), cells
}

func writeTable(w io.Writer, header []string, cells [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, line := range cells {
		fmt.Fprintln(tw, strings.Join(line, "\t"))
	}
	return tw.Flush()
}

// RunCLI is `holdmytrack stats`: it prints one day's values (the newest day by default) or a
// key's daily series, and with --take writes a day's snapshot. Arguments it can't read are
// ErrUsage.
func RunCLI(ctx context.Context, db DB, args []string, w io.Writer, now time.Time) error {
	req, err := parseArgs(args, now)
	if err != nil {
		return err
	}
	if req.take {
		n, err := Take(ctx, db, *req.day)
		if err != nil {
			return err
		}
		if n == 0 {
			fmt.Fprintf(w, "%s: already taken, nothing written\n", req.day.Format(time.DateOnly))
		} else {
			fmt.Fprintf(w, "%s: %d values written\n", req.day.Format(time.DateOnly), n)
		}
		return nil
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("stats: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read only

	var rows []row
	if req.key != "" {
		rows, err = readRows(ctx, tx, `
			SELECT day::timestamp, key, value FROM daily_stats
			WHERE (key = $1 OR starts_with(key, $1 || '.'))
			  AND day > (SELECT max(day) FROM daily_stats) - $2::int`, req.key, req.days)
	} else if req.day != nil {
		rows, err = readRows(ctx, tx, `SELECT day::timestamp, key, value FROM daily_stats WHERE day = $1`, *req.day)
	} else {
		rows, err = readRows(ctx, tx, `
			SELECT day::timestamp, key, value FROM daily_stats WHERE day = (SELECT max(day) FROM daily_stats)`)
	}
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "no values")
		return nil
	}
	if req.key != "" {
		header, cells := seriesTable(rows)
		return writeTable(w, header, cells)
	}
	fmt.Fprintln(w, rows[0].day.Format(time.DateOnly))
	header, cells := dayTable(rows)
	return writeTable(w, header, cells)
}

func readRows(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]row, error) {
	rs, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("stats: read: %w", err)
	}
	out, err := pgx.CollectRows(rs, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := r.Scan(&x.day, &x.key, &x.value)
		return x, err
	})
	if err != nil {
		return nil, fmt.Errorf("stats: read: %w", err)
	}
	return out, nil
}
