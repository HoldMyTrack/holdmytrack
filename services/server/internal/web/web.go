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

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
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

// Link is one entry of a header menu. LabelKey is its label's catalog key (i18n), translated
// when the page renders.
type Link struct {
	Href     string
	LabelKey string
}

// InfoLinks is the header's Info menu. apps/web/src/ui/InfoMenu.tsx repeats it for the map
// page's React header until that header is this one (ADR-0012); keep the two in step.
var InfoLinks = []Link{
	{Href: "/about", LabelKey: "nav.about"},
	{Href: "/help", LabelKey: "nav.help"},
	{Href: "/contacts", LabelKey: "nav.contacts"},
}

// User is the signed-in account as the header shows it; nil when nobody is signed in.
type User struct {
	Email       string // "" for a demo session
	DisplayName string
	AvatarURL   string // "" when no avatar is set
	IsDemo      bool
	// IsAdmin shows the account menu's Admin item (the admin panel, FR-12).
	IsAdmin bool
}

// Label is what the account menu shows as its first line: the email for a real account, the
// display name for a demo one (its email is an internal placeholder never shown anywhere).
// "" for a demo account without a name — the header shows its own "Demo account" then.
func (u *User) Label() string {
	if u.Email != "" {
		return u.Email
	}
	return u.DisplayName
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
	// Lang is the language the page renders in (i18n.Resolve) — "" is English.
	Lang string
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

	mu   sync.Mutex
	sets map[string]*templateSet
	// public caches RenderPublic's output — a signed-out page's bytes, by page, path and
	// language.
	public sync.Map
}

// templateSet is every template parsed for one language: its catalog is bound into the
// t/tn/th funcs at parse time, so a template calls {{t "key"}} wherever it is, `.` or not.
type templateSet struct {
	pages map[string]*template.Template
	app   *template.Template
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
	sets, err := r.parseAll()
	if err != nil {
		return nil, err
	}
	r.sets = sets
	return r, nil
}

// Dev reports whether this Renderer is a dev one (WEB_DEV_DIR). Only then may a request ask
// for the app's Vite-dev-server assets (AppPage.ViteDev); in production the flag is ignored.
func (r *Renderer) Dev() bool { return r.reload }

func (r *Renderer) parseAll() (map[string]*templateSet, error) {
	sets := make(map[string]*templateSet, len(i18n.Supported))
	for _, lang := range i18n.Supported {
		set, err := r.parse(i18n.Get(lang))
		if err != nil {
			return nil, err
		}
		sets[lang] = set
	}
	return sets, nil
}

// parse builds one language's templates. A page may have a whole-page translation beside it,
// pages/about.ru.html — for the long prose pages, where a key per paragraph would only make
// them harder to write — which that language uses instead of pages/about.html.
func (r *Renderer) parse(l *i18n.Localizer) (*templateSet, error) {
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
		// appAsset is one of the React build's two stable-named entry files (apps/web's
		// vite.config.ts names them app.js and app.css), with the same cache-buster.
		"appAsset": func(name string) string {
			return "/assets/" + name + "?v=" + url.QueryEscape(r.version)
		},
		// t is a catalog message, {name}s filled from name/value pairs: {{t "key" "email" .Email}}.
		"t": l.T,
		// tn is a count's message in the plural form n takes: {{tn "key" .Count}}.
		"tn": func(key string, n any, args ...any) string { return l.N(key, toInt64(n), args...) },
		// th is t for a message that carries its own markup (<strong>, <a>): the catalog is
		// trusted, the arguments are escaped.
		"th": func(key string, args ...any) template.HTML {
			escaped := make([]any, len(args))
			for i, a := range args {
				escaped[i] = a
				if i%2 == 1 {
					escaped[i] = template.HTMLEscapeString(fmt.Sprint(a))
				}
			}
			return template.HTML(l.T(key, escaped...))
		},
		"lang": l.Lang,
	}
	set := &templateSet{pages: make(map[string]*template.Template, len(files))}
	for _, file := range files {
		name := strings.TrimSuffix(path.Base(file), ".html")
		if strings.Contains(name, ".") {
			continue // a translation of another page, parsed in its place below
		}
		if translated := "templates/pages/" + name + "." + l.Lang() + ".html"; fileExists(r.fsys, translated) {
			file = translated
		}
		// Every templates/*.html (the layout, the header, shared partials) goes with each page.
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(r.fsys, "templates/*.html", file)
		if err != nil {
			return nil, fmt.Errorf("web: parse %s: %w", file, err)
		}
		set.pages[name] = t
	}
	// The React app's shell is its own root, not a page inside layout.html: no footer, no
	// page styles, just the shared header above the app's mount point.
	set.app, err = template.New("app.html").Funcs(funcs).ParseFS(r.fsys, "templates/*.html", "templates/app/app.html")
	if err != nil {
		return nil, fmt.Errorf("web: parse templates/app/app.html: %w", err)
	}
	return set, nil
}

func fileExists(fsys fs.FS, name string) bool {
	_, err := fs.Stat(fsys, name)
	return err == nil
}

func toInt64(n any) int64 {
	switch v := n.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	}
	return 0
}

// Render writes page (a templates/pages/ file name, without .html) with data. It renders into a
// buffer first, so a template error becomes a clean 500 rather than half a page.
func (r *Renderer) Render(w http.ResponseWriter, status int, page string, data PageData) {
	set, ok := r.current(w, data.Lang)
	if !ok {
		return
	}
	t, ok := set.pages[page]
	if !ok {
		http.Error(w, "unknown page "+page, http.StatusInternalServerError)
		return
	}
	r.fill(&data)
	r.execute(w, status, t, data)
}

// RenderApp writes the React app's shell (the map page, and for now Profile and Settings).
func (r *Renderer) RenderApp(w http.ResponseWriter, data AppPage) {
	set, ok := r.current(w, data.Lang)
	if !ok {
		return
	}
	r.fill(&data.PageData)
	r.execute(w, http.StatusOK, set.app, data)
}

// current is lang's parsed templates (English's for an unknown lang) — every language
// re-parsed first when reloading (dev).
func (r *Renderer) current(w http.ResponseWriter, lang string) (*templateSet, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reload {
		sets, err := r.parseAll()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return nil, false
		}
		r.sets = sets
	}
	set, ok := r.sets[lang]
	if !ok {
		set = r.sets[i18n.Default]
	}
	return set, true
}

// fill sets the fields every page's header and head share.
func (r *Renderer) fill(data *PageData) {
	if !i18n.IsSupported(data.Lang) {
		data.Lang = i18n.Default
	}
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
// and every crawler in one language. So they're rendered once and kept (per page, path and
// language; not in dev, where templates reload), and sent cacheable for five minutes.
// `Vary: Cookie` keeps a browser from reusing the signed-out copy once it holds a session
// cookie, whose pages have a different header; `Vary: Accept-Language` keeps a shared cache
// from handing one language's copy to a browser that asked for another.
func (r *Renderer) RenderPublic(w http.ResponseWriter, page string, data PageData) {
	r.fill(&data)
	key := page + "|" + data.Path + "|" + data.Lang
	if cached, ok := r.public.Load(key); ok && !r.reload {
		writePublic(w, cached.([]byte))
		return
	}
	set, ok := r.current(w, data.Lang)
	if !ok {
		return
	}
	t, ok := set.pages[page]
	if !ok {
		http.Error(w, "unknown page "+page, http.StatusInternalServerError)
		return
	}
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
	w.Header().Set("Vary", "Cookie, Accept-Language")
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
