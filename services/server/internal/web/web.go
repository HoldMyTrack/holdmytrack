// Package web renders HoldMyTrack's HTML pages — ADR-0012: every page is a server-rendered
// html/template sharing one layout and one header, and React is left to the map page alone.
// The templates and the pages' own stylesheet are embedded in the binary; in dev they can be
// read from disk instead (config's WEB_DEV_DIR), re-parsed on every request, so editing a
// template needs no image rebuild.
//
// This package only renders. Which page a request gets, and who is signed in, is decided by
// httpapi (pages.go), which owns the session lookup and passes the result in as PageData.
package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
)

//go:embed templates static
var embedded embed.FS

// Embedded is the templates and static files compiled into the binary — what production
// serves. Rooted so templates/ and static/ sit at its top level, same as a WEB_DEV_DIR checkout.
func Embedded() fs.FS { return embedded }

// OpenCollectiveSlug names the collective that funds the project — a fact about the project,
// not about a deployment, so a constant rather than config (IMPLEMENTATION.md §4.16). Empty
// until the collective exists; the header's Donate then links to About's funding section.
// apps/web/src/funding.ts holds the same value for the map page's React header until that
// header is this one too (ADR-0012's phase 5); keep the two in step.
const OpenCollectiveSlug = ""

// Link is one entry of a header menu.
type Link struct {
	Href  string
	Label string
}

// InfoLinks is the header's Info menu. apps/web/src/ui/InfoMenu.tsx repeats it for the map
// page's React header until that header is this one (ADR-0012); keep the two in step.
var InfoLinks = []Link{
	{Href: "/about", Label: "About"},
	{Href: "/help", Label: "Help"},
	{Href: "/contacts", Label: "Contacts"},
}

// User is the signed-in account as the header shows it; nil when nobody is signed in.
type User struct {
	Email       string // "" for a demo session
	DisplayName string
	AvatarURL   string // "" when no avatar is set
	IsDemo      bool
}

// Label is what the account menu shows as its first line: the email for a real account, the
// display name for a demo one (its email is an internal placeholder never shown anywhere).
func (u *User) Label() string {
	if u.Email != "" {
		return u.Email
	}
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return "Demo account"
}

// PageData is everything a page template can read. Title/Description/Path/User/NoIndex come
// from the handler; the rest the Renderer fills in.
type PageData struct {
	Title       string
	Description string
	// Path is the page's own URL path — the Info menu marks the matching entry current, and
	// the canonical link is built from it.
	Path    string
	NoIndex bool
	User    *User
	// Page holds whatever a single page needs beyond the shared fields.
	Page any

	InfoLinks []Link
	DonateURL string
	Canonical string
}

// Renderer parses each page template together with the shared layout and header, once at
// startup — or on every render when reload is set (dev).
type Renderer struct {
	fsys    fs.FS
	reload  bool
	version string
	baseURL string

	mu    sync.Mutex
	pages map[string]*template.Template
}

// New builds a Renderer over fsys (Embedded(), or os.DirFS of a checkout's internal/web in dev).
// version cache-busts the stylesheet URL; baseURL (APP_BASE_URL) is what canonical links are
// built from. A template that fails to parse is a startup error here, not a 500 later.
func New(fsys fs.FS, reload bool, version, baseURL string) (*Renderer, error) {
	r := &Renderer{fsys: fsys, reload: reload, version: version, baseURL: baseURL}
	pages, err := r.parse()
	if err != nil {
		return nil, err
	}
	r.pages = pages
	return r, nil
}

func (r *Renderer) parse() (map[string]*template.Template, error) {
	files, err := fs.Glob(r.fsys, "templates/pages/*.html")
	if err != nil {
		return nil, err
	}
	funcs := template.FuncMap{
		// asset is a static file's URL with the build version as a cache-buster, so /static/
		// can be served with a long max-age and still change the moment a deploy does.
		"asset": func(name string) string {
			return "/static/" + name + "?v=" + url.QueryEscape(r.version)
		},
	}
	pages := make(map[string]*template.Template, len(files))
	for _, file := range files {
		name := path.Base(file)
		name = name[:len(name)-len(".html")]
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(r.fsys, "templates/layout.html", "templates/header.html", file)
		if err != nil {
			return nil, fmt.Errorf("web: parse %s: %w", file, err)
		}
		pages[name] = t
	}
	return pages, nil
}

// Render writes page (a templates/pages/ file name, without .html) with data. It renders into a
// buffer first, so a template error becomes a clean 500 rather than half a page.
func (r *Renderer) Render(w http.ResponseWriter, status int, page string, data PageData) {
	pages := r.pages
	if r.reload {
		fresh, err := r.parse()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		r.mu.Lock()
		r.pages = fresh
		r.mu.Unlock()
		pages = fresh
	}
	t, ok := pages[page]
	if !ok {
		http.Error(w, "unknown page "+page, http.StatusInternalServerError)
		return
	}
	data.InfoLinks = InfoLinks
	data.DonateURL = "/about#funding"
	if OpenCollectiveSlug != "" {
		data.DonateURL = "https://opencollective.com/" + url.PathEscape(OpenCollectiveSlug) + "/donate"
	}
	data.Canonical = r.baseURL + data.Path

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Pages carry the signed-in account's name and avatar in their header, so no shared cache
	// may keep one — and a browser shouldn't show a stale header after sign-out either.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// StaticHandler serves static/ (the pages' stylesheet, the logo) at /static/. Every reference
// goes through the asset func's ?v= cache-buster, so a year's max-age is safe.
func (r *Renderer) StaticHandler() http.Handler {
	sub, err := fs.Sub(r.fsys, "static")
	if err != nil {
		panic(err) // fs.Sub only fails on an invalid path literal
	}
	files := http.StripPrefix("/static/", http.FileServerFS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// No directory listings — only the files the templates actually reference.
		if strings.HasSuffix(req.URL.Path, "/") {
			http.NotFound(w, req)
			return
		}
		if !r.reload {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, req)
	})
}
