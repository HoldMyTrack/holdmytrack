# ADR-0012: Every page is server-rendered HTML sharing one header; React is kept for the map page

## Status

Accepted.

## Context

The web client started as one React single-page app (`ARCHITECTURE.md` §2: "no SSR framework — a single WebGL page with no SEO surface"). As it grew past the map, everything else was fitted into that one page: Profile and Settings became in-memory views switched by local state in `App.tsx`, with no URL of their own, so a refresh on Settings landed back on the map and the browser's Back button left the app instead of returning to it. The public About page, which has to be readable by a visitor or a crawler without an account or the ~1.5 MB map bundle, was split out as a static `about.html` built by Vite — and a Help page after it — each needing a copy of the header, first by hand and then via a build-time injected partial (`staticHeader.ts`). That left the header existing three times (React's `Header.tsx`, the static partial, and its stylesheet), kept in step by hand.

The pages coming next make the pattern worse, not better: Contacts, a Cloud-integrations page, and Profile and Settings themselves. Each is the same layout for every visitor with a few per-user values in it — a name, an avatar, a year's worth of activity counts. A static file can only carry values known at build time, so none of them can be a static page; within the single-page app they can only be client-rendered views that fetch their values after load.

## Decision

**HoldMyTrack is a web app made of real HTML pages.** The Go server renders every page from `html/template` templates (`services/server/internal/web`, embedded in the binary), all sharing one layout and one header template. The header's menus are `<details>` dropdowns, and its Sign out is a form POST, so a page needs no JavaScript to navigate or sign out.

**React is kept for the map page only**, where it earns its place: the map, the Activities panel, filters, the date-range picker and export are one tightly interactive WebGL surface. Import moved out of the header into the Activities panel (its Sync tab) as part of this, because the header becomes shared page chrome, not a toolbar for the map.

It lands in phases, each shippable on its own: the rendering foundation plus About, Help and Contacts first; then sign-in and the other auth screens; then the map page itself, served by Go with the shared header around the React mount point; then Settings and Profile. Until the map page moves, Caddy and Vite's dev proxy send only the listed page paths to the Go server, and the React header stays a hand-kept copy of the template's.

## Alternatives considered

- **Keep one React app and give it a client-side router.** Fixes refresh and Back for Profile and Settings, but not the rest: every page still downloads the map bundle, renders blank until its API calls land, and needs a second static copy for anything a crawler or a signed-out visitor should read. The header stays split between React and whatever the static pages use.
- **Static pages with a small script that fills in the per-user values.** Works for a signed-in avatar in the header, but every page becomes a hand-written fetch-and-patch script — a second, framework-less client to maintain beside React — and still renders without its data first.
- **A server-side-rendering JavaScript framework (Next.js, Remix, Astro).** Would render pages on the server too, but as a second server runtime (Node) beside the Go API, contradicting §1.1's one language, one deployable. The pages here are forms and lists, well within `html/template`.

## Consequences

- Every page has a URL, and refresh and Back behave like any other website. A signed-out visitor, a crawler and a signed-in user get the same page with a different header, with no bundle to download first.
- The header exists once (the template) as soon as the map page moves to it; until then, its React copy is kept in step by hand, with comments on both sides.
- Pages are rendered per request with the session looked up, so they are sent `Cache-Control: no-store`. They cost one session query each, the same as any API call.
- Plain form POSTs remove the barrier the JSON API relied on against cross-site request forgery (a cross-site form can't send a JSON body or a PATCH/DELETE). Every page form is behind an Origin/Referer check against `APP_BASE_URL`, on top of the session cookie's `SameSite=Lax`.
- The Go server now serves HTML, so page changes are server changes: templates and their stylesheet live in `services/server/internal/web`, not `apps/web`. Dev reads them from disk (`WEB_DEV_DIR`) so editing one needs no image rebuild, and Vite's dev server proxies the page paths to Go so dev keeps one origin like production.
- Interactivity on the rendered pages is deliberately plainer than React's: a native `<select>` rather than a typeahead picker, an SVG `<title>` rather than a hover tooltip. Where a page genuinely needs more, a small script (or a React island) can be added to that page alone; the default is none.