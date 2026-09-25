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
	Path string
	// CanonicalPath, when set, is the canonical link's path instead of Path — for a page that
	// is the same content as another URL (/about is the signed-out home page, `/`).
	CanonicalPath string
	NoIndex       bool
	User          *User
	// Page holds whatever a single page needs beyond the shared fields.
	Page any

	InfoLinks []Link
	DonateURL string
	Canonical string
	// SiteURL is APP_BASE_URL — for the absolute URLs link previews and structured data need.
	SiteURL string
	// ShareImage is the link-preview image's absolute URL (static/og-image.jpg).
	ShareImage string
}

// Renderer parses each page template together with the shared layout, header and partials
// (every templates/*.html), once at startup — or on every render when reload is set (dev).
type Renderer struct {
	fsys    fs.FS
	reload  bool
	version string
	baseURL string

	mu    sync.Mutex
	pages map[string]*template.Template
	app   *template.Template
	// public caches RenderPublic's output — a signed-out page's bytes, by page and path.
	public sync.Map
}

// AppPage is what the React app's shell (templates/app/app.html) reads: the shared header's
// fields, plus how to load the bundle.
type AppPage struct {
	PageData
	// ViteDev loads the app from Vite's dev server (/@vite/client, /src/main.tsx) instead of
	// the built /assets/app.js and app.css — see Dev.
	ViteDev bool
}

// New builds a Renderer over fsys (Embedded(), or os.DirFS of a checkout's internal/web in dev).
// version cache-busts the stylesheet URL; baseURL (APP_BASE_URL) is what canonical links are
// built from. A template that fails to parse is a startup error here, not a 500 later.
func New(fsys fs.FS, reload bool, version, baseURL string) (*Renderer, error) {
	r := &Renderer{fsys: fsys, reload: reload, version: version, baseURL: baseURL}
	pages, app, err := r.parse()
	if err != nil {
		return nil, err
	}
	r.pages, r.app = pages, app
	return r, nil
}

// Dev reports whether this Renderer is a dev one (WEB_DEV_DIR). Only then may a request ask
// for the app's Vite-dev-server assets (AppPage.ViteDev); in production the flag is ignored.
func (r *Renderer) Dev() bool { return r.reload }

func (r *Renderer) parse() (map[string]*template.Template, *template.Template, error) {
	files, err := fs.Glob(r.fsys, "templates/pages/*.html")
	if err != nil {
		return nil, nil, err
	}
	funcs := template.FuncMap{
		// asset is a static file's URL with the build version as a cache-buster, so /static/
		// can be served with a long max-age and still change the moment a deploy does.
		"asset": func(name string) string {
			return "/static/" + name + "?v=" + url.QueryEscape(r.version)
		},
		// appAsset is one of the React build's two stable-named entry files (apps/web's
		// vite.config.ts names them app.js and app.css), with the same cache-buster.
		"appAsset": func(name string) string {
			return "/assets/" + name + "?v=" + url.QueryEscape(r.version)
		},
	}
	pages := make(map[string]*template.Template, len(files))
	for _, file := range files {
		name := path.Base(file)
		name = name[:len(name)-len(".html")]
		// Every templates/*.html (the layout, the header, shared partials) goes with each page.
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(r.fsys, "templates/*.html", file)
		if err != nil {
			return nil, nil, fmt.Errorf("web: parse %s: %w", file, err)
		}
		pages[name] = t
	}
	// The React app's shell is its own root, not a page inside layout.html: no footer, no
	// page styles, just the shared header above the app's mount point.
	app, err := template.New("app.html").Funcs(funcs).ParseFS(r.fsys, "templates/*.html", "templates/app/app.html")
	if err != nil {
		return nil, nil, fmt.Errorf("web: parse templates/app/app.html: %w", err)
	}
	return pages, app, nil
}

// Render writes page (a templates/pages/ file name, without .html) with data. It renders into a
// buffer first, so a template error becomes a clean 500 rather than half a page.
func (r *Renderer) Render(w http.ResponseWriter, status int, page string, data PageData) {
	pages, _, ok := r.current(w)
	if !ok {
		return
	}
	t, ok := pages[page]
	if !ok {
		http.Error(w, "unknown page "+page, http.StatusInternalServerError)
		return
	}
	r.fill(&data)
	r.execute(w, status, t, data)
}

// RenderApp writes the React app's shell (the map page, and for now Profile and Settings).
func (r *Renderer) RenderApp(w http.ResponseWriter, data AppPage) {
	_, app, ok := r.current(w)
	if !ok {
		return
	}
	r.fill(&data.PageData)
	r.execute(w, http.StatusOK, app, data)
}

// current is the parsed templates — re-parsed first when reloading (dev).
func (r *Renderer) current(w http.ResponseWriter) (map[string]*template.Template, *template.Template, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reload {
		pages, app, err := r.parse()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return nil, nil, false
		}
		r.pages, r.app = pages, app
	}
	return r.pages, r.app, true
}

// fill sets the fields every page's header and head share.
func (r *Renderer) fill(data *PageData) {
	data.InfoLinks = InfoLinks
	data.DonateURL = "/about#funding"
	if OpenCollectiveSlug != "" {
		data.DonateURL = "https://opencollective.com/" + url.PathEscape(OpenCollectiveSlug) + "/donate"
	}
	canonical := data.Path
	if data.CanonicalPath != "" {
		canonical = data.CanonicalPath
	}
	data.Canonical = r.baseURL + canonical
	data.SiteURL = r.baseURL
	data.ShareImage = r.baseURL + "/static/og-image.jpg?v=" + url.QueryEscape(r.version)
}

func (r *Renderer) execute(w http.ResponseWriter, status int, t *template.Template, data any) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	// A page carries the signed-in account's name and avatar in its header, so no shared cache
	// may keep one — and a browser shouldn't show a stale header after sign-out either.
	// Signed-out pages that are the same for everyone go through RenderPublic instead.
	writeHTML(w, status, "no-store", buf.Bytes())
}

// RenderPublic is Render for a page with no session behind it and nothing request-specific
// in it (About, Help, Contacts, the signed-out home page): the same bytes for every visitor
// and every crawler. So they're rendered once and kept (per page and path; not in dev, where
// templates reload), and sent cacheable for five minutes. `Vary: Cookie` keeps a browser from
// reusing the signed-out copy once it holds a session cookie, whose pages have a different
// header.
func (r *Renderer) RenderPublic(w http.ResponseWriter, page string, data PageData) {
	key := page + "|" + data.Path
	if cached, ok := r.public.Load(key); ok && !r.reload {
		writePublic(w, cached.([]byte))
		return
	}
	pages, _, ok := r.current(w)
	if !ok {
		return
	}
	t, ok := pages[page]
	if !ok {
		http.Error(w, "unknown page "+page, http.StatusInternalServerError)
		return
	}
	r.fill(&data)
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	if !r.reload {
		r.public.Store(key, buf.Bytes())
	}
	writePublic(w, buf.Bytes())
}

func writePublic(w http.ResponseWriter, body []byte) {
	w.Header().Set("Vary", "Cookie")
	writeHTML(w, http.StatusOK, "public, max-age=300", body)
}

func writeHTML(w http.ResponseWriter, status int, cacheControl string, body []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", cacheControl)
	w.WriteHeader(status)
	_, _ = w.Write(body)
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
