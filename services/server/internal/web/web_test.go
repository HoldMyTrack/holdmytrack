package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// The React app's shell loads the built bundle by its fixed names, or Vite's dev server's
// modules — and the header either way.
func TestRenderApp(t *testing.T) {
	r, err := New(Embedded(), false, "abc123", "https://app.example")
	if err != nil {
		t.Fatal(err)
	}
	user := &User{Email: "someone@example.com"}

	rec := httptest.NewRecorder()
	r.RenderApp(rec, AppPage{PageData: PageData{Title: "Map", Path: "/", User: user}})
	body := rec.Body.String()
	for _, want := range []string{
		`<script type="module" src="/assets/app.js?v=abc123"></script>`,
		`<link rel="stylesheet" href="/assets/app.css?v=abc123" />`,
		`<link rel="stylesheet" href="/static/tokens.css?v=abc123" />`,
		`<link rel="stylesheet" href="/static/header.css?v=abc123" />`,
		`class="page-header"`,
		`<div id="root"></div>`,
		"someone@example.com",
		`href="/profile"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("built: missing %q", want)
		}
	}
	for _, not := range []string{"/@vite/client", "pages.css", "page-footer"} {
		if strings.Contains(body, not) {
			t.Errorf("built: unexpected %q", not)
		}
	}

	rec = httptest.NewRecorder()
	r.RenderApp(rec, AppPage{PageData: PageData{Title: "Map", Path: "/", User: user}, ViteDev: true})
	body = rec.Body.String()
	for _, want := range []string{`src="/@vite/client"`, `src="/src/main.tsx"`, "/@react-refresh"} {
		if !strings.Contains(body, want) {
			t.Errorf("dev: missing %q", want)
		}
	}
	if strings.Contains(body, "/assets/app.js") {
		t.Errorf("dev: still loads the built bundle")
	}
}

// A demo session's account menu offers the way to a real account.
func TestHeaderDemoMenu(t *testing.T) {
	r, err := New(Embedded(), false, "v", "https://app.example")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	r.RenderApp(rec, AppPage{PageData: PageData{Title: "Map", Path: "/", User: &User{DisplayName: "Demo User", IsDemo: true}}})
	if body := rec.Body.String(); !strings.Contains(body, "Demo User") || !strings.Contains(body, `href="/signup">Create your own account`) {
		t.Errorf("demo menu missing its name or Create your own account")
	}
}
