package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/web"
)

// The admin panel (ADR-0013, IMPLEMENTATION.md §4.20, docs/SPEC.md FR-12): every account, and
// any one account's activities with their ids — rendered on the server like /profile, read-only.
// Only an account with users.is_admin (granted by the `set-admin` CLI subcommand, nowhere on
// the web) gets in; everyone else, signed out or not, gets the ordinary 404 page, so the panel
// doesn't announce that it exists.

// adminActivitiesPerPage is how many activities one page of /admin/users/{id} lists.
const adminActivitiesPerPage = 100

// adminUsersView is what templates/pages/admin.html reads from PageData.Page.
type adminUsersView struct {
	Users []adminUserRow
}

type adminUserRow struct {
	ID          string
	Email       string
	DisplayName string
	SignedUp    string
	Country     string
	Timezone    string
	IsDemo      bool
	IsAdmin     bool
	Verified    bool
	Activities  string
	// ActivityCount is Activities as a number, for its plural form.
	ActivityCount int64
	First, Last   string // "" when the account has no activities
	Distance      string
}

// adminUserView is what templates/pages/admin-user.html reads from PageData.Page.
type adminUserView struct {
	User       adminUserRow
	Activities []adminActivityRow
	// Range is "1–100 of 250"; "" when the account has no activities.
	Range    string
	PrevHref string
	NextHref string
}

type adminActivityRow struct {
	ID           string
	Started      string
	Type         string
	Name         string
	Distance     string
	Duration     string
	Source       string
	Countries    string
	Regions      string
	SupersededBy string
	Hidden       bool
	Edited       bool
}

// adminAccount is the page's account when it may see the admin panel, or nil after answering
// the request itself: the 404 page for anyone who isn't an admin, the account's own home
// (/verify-pending, /settings) for an admin who isn't past those yet.
func (s *Server) adminAccount(w http.ResponseWriter, r *http.Request) *pageAccount {
	acct := s.pageAccount(r)
	if acct == nil || !acct.info.isAdmin || acct.info.isDemo {
		s.notFound(w, r)
		return nil
	}
	if home := acct.home(); home != "/" {
		http.Redirect(w, r, home, http.StatusSeeOther)
		return nil
	}
	return acct
}

// GET /admin.
func (s *Server) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	acct := s.adminAccount(w, r)
	if acct == nil {
		return
	}
	lang := pageLang(acct, r)
	l := i18n.Get(lang)
	users, err := s.adminUsers(r.Context(), l, "", web.Imperial(acct.profile.Country))
	if err != nil {
		s.log.Error("admin page failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.pages.Render(w, http.StatusOK, "admin", web.PageData{Title: l.T("meta.admin_title"), Path: "/admin", NoIndex: true, User: acct.user, Page: adminUsersView{Users: users}, Lang: lang})
}

// GET /admin/users/{id}.
func (s *Server) handleAdminUserPage(w http.ResponseWriter, r *http.Request) {
	acct := s.adminAccount(w, r)
	if acct == nil {
		return
	}
	userID := r.PathValue("id")
	if !uuidPattern.MatchString(userID) {
		s.notFound(w, r)
		return
	}
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 1 {
		page = p
	}
	imperial := web.Imperial(acct.profile.Country)
	lang := pageLang(acct, r)
	l := i18n.Get(lang)

	users, err := s.adminUsers(r.Context(), l, userID, imperial)
	if err == nil && len(users) == 0 {
		s.notFound(w, r)
		return
	}
	var view adminUserView
	if err == nil {
		view, err = s.adminUserActivities(r.Context(), l, users[0], page, imperial)
	}
	if err != nil {
		s.log.Error("admin user page failed", "err", err, "user_id", userID)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.pages.Render(w, http.StatusOK, "admin-user", web.PageData{Title: l.T("meta.admin_user_title", "name", adminUserLabel(view.User)), Path: r.URL.Path, NoIndex: true, User: acct.user, Page: view, Lang: lang})
}

// adminUsers lists every account, newest first, or just userID's when it's set. Counts and
// distance are over live activities only — a superseded duplicate isn't one the account shows.
func (s *Server) adminUsers(ctx context.Context, l *i18n.Localizer, userID string, imperial bool) ([]adminUserRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.email, COALESCE(u.display_name, ''), u.created_at, COALESCE(u.country, ''),
		       u.timezone, u.demo_expires_at IS NOT NULL, u.is_admin, u.email_verified,
		       count(a.id), min(a.started_at), max(a.started_at),
		       COALESCE(sum(a.distance_meters), 0)::float8
		FROM users u
		LEFT JOIN activities a ON a.user_id = u.id AND a.superseded_by IS NULL
		WHERE $1 = '' OR u.id::text = $1
		GROUP BY u.id
		ORDER BY u.created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("admin: list users: %w", err)
	}
	defer rows.Close()
	var users []adminUserRow
	for rows.Next() {
		var u adminUserRow
		var created time.Time
		var first, last *time.Time
		var count int64
		var meters float64
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &created, &u.Country,
			&u.Timezone, &u.IsDemo, &u.IsAdmin, &u.Verified,
			&count, &first, &last, &meters); err != nil {
			return nil, fmt.Errorf("admin: scan user: %w", err)
		}
		loc := adminLocation(u.Timezone)
		u.SignedUp = created.In(loc).Format("2006-01-02")
		u.ActivityCount = count
		u.Activities = l.Int(count)
		u.Distance = web.FormatTotalDistance(l, meters, imperial)
		if first != nil && last != nil {
			u.First, u.Last = first.In(loc).Format("2006-01-02"), last.In(loc).Format("2006-01-02")
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// adminUserActivities is one page of u's activities, newest first — superseded and hidden ones
// included, marked as such, since the panel is for seeing what's actually stored.
func (s *Server) adminUserActivities(ctx context.Context, l *i18n.Localizer, u adminUserRow, page int, imperial bool) (adminUserView, error) {
	view := adminUserView{User: u}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM activities WHERE user_id = $1`, u.ID).Scan(&total); err != nil {
		return view, fmt.Errorf("admin: count activities: %w", err)
	}
	offset := (page - 1) * adminActivitiesPerPage
	if total == 0 {
		return view, nil
	}
	if offset >= total {
		// A page past the end (a stale link after deletions): empty, with a way back.
		view.Range = l.T("admin.range_empty")
		view.PrevHref = adminUserHref(u.ID, 1)
		return view, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.started_at, a.activity_type, COALESCE(a.name, ''),
		       COALESCE(a.distance_meters, 0)::float8, COALESCE(a.duration_seconds, 0), a.source,
		       COALESCE(a.superseded_by::text, ''), a.trajectory IS NULL, a.track_edit IS NOT NULL,
		       COALESCE((SELECT string_agg(c.name, ', ' ORDER BY c.name)
		                 FROM activity_country ac JOIN admin_countries c ON c.id = ac.country_id
		                 WHERE ac.activity_id = a.id), ''),
		       COALESCE((SELECT string_agg(rg.name, ', ' ORDER BY rg.name)
		                 FROM activity_region ar JOIN admin_regions rg ON rg.id = ar.region_id
		                 WHERE ar.activity_id = a.id), '')
		FROM activities a
		WHERE a.user_id = $1
		ORDER BY a.started_at DESC, a.id
		LIMIT $2 OFFSET $3
	`, u.ID, adminActivitiesPerPage, offset)
	if err != nil {
		return view, fmt.Errorf("admin: list activities: %w", err)
	}
	defer rows.Close()
	loc := adminLocation(u.Timezone)
	for rows.Next() {
		var a adminActivityRow
		var started time.Time
		var meters float64
		var seconds int64
		if err := rows.Scan(&a.ID, &started, &a.Type, &a.Name, &meters, &seconds, &a.Source,
			&a.SupersededBy, &a.Hidden, &a.Edited, &a.Countries, &a.Regions); err != nil {
			return view, fmt.Errorf("admin: scan activity: %w", err)
		}
		a.Started = started.In(loc).Format("2006-01-02 15:04")
		a.Distance = web.FormatDistance(l, meters, imperial)
		a.Duration = adminDuration(seconds)
		view.Activities = append(view.Activities, a)
	}
	if err := rows.Err(); err != nil {
		return view, fmt.Errorf("admin: list activities: %w", err)
	}

	view.Range = adminRange(l, offset, len(view.Activities), total)
	if page > 1 {
		view.PrevHref = adminUserHref(u.ID, page-1)
	}
	if offset+len(view.Activities) < total {
		view.NextHref = adminUserHref(u.ID, page+1)
	}
	return view, nil
}

// adminRange is "1–100 of 250".
func adminRange(l *i18n.Localizer, offset, n, total int) string {
	return l.T("admin.range", "from", offset+1, "to", offset+n, "total", total)
}

func adminUserHref(userID string, page int) string {
	if page <= 1 {
		return "/admin/users/" + userID
	}
	return fmt.Sprintf("/admin/users/%s?page=%d", userID, page)
}

// adminUserLabel names an account in a title: its display name, else its email.
func adminUserLabel(u adminUserRow) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Email
}

// adminDuration is "0:42" or "12:05" — hours and minutes, which is all a list needs.
func adminDuration(seconds int64) string {
	m := (seconds + 30) / 60
	return fmt.Sprintf("%d:%02d", m/60, m%60)
}

// adminLocation is the account's own timezone, so dates read as that account saw them.
func adminLocation(tz string) *time.Location {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

// SetAdmin grants or revokes the admin panel for the account with this email — the
// `set-admin` CLI subcommand, the only thing that writes users.is_admin. A demo account can't
// be made one: a demo session never reaches the panel anyway (adminAccount).
func SetAdmin(ctx context.Context, pool *pgxpool.Pool, rawEmail string, admin bool) error {
	email, err := normalizeEmail(rawEmail)
	if err != nil {
		return err
	}
	tag, err := pool.Exec(ctx,
		`UPDATE users SET is_admin = $2 WHERE email = $1 AND demo_expires_at IS NULL`, email, admin)
	if err != nil {
		return fmt.Errorf("set admin: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("set admin: no account with email %q", email)
	}
	return nil
}
