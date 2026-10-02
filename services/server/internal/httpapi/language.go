package httpapi

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
)

// languageCookieAge is how long the header's language choice lasts on a browser — long
// enough to be a preference, not a visit.
const languageCookieAge = 365 * 24 * time.Hour

// handleLanguageForm serves `POST /language` — the header's language menu, on every page,
// signed in or not (ADR-0025), and the web's only language control. It remembers the choice
// on this browser (i18n.CookieName), so a visitor without an account can read the site in a
// language their browser doesn't ask for. A signed-in real account's Language setting is
// saved too, since that setting outranks the cookie (i18n.Resolve) and would otherwise undo
// the choice; a demo account can't save settings, so it gets the cookie alone. An empty lang
// is "Automatic": the cookie is cleared and the setting set back to NULL, so the browser's
// own language decides again. Then back to the page the menu was on.
func (s *Server) handleLanguageForm(w http.ResponseWriter, r *http.Request) {
	lang := r.PostFormValue("lang")
	if lang != "" && !i18n.IsSupported(lang) {
		http.Error(w, "unsupported language", http.StatusBadRequest)
		return
	}
	if acct := s.pageAccount(r); acct != nil && !acct.info.isDemo {
		if _, err := s.pool.Exec(r.Context(), `UPDATE users SET locale = NULLIF($2, '') WHERE id = $1`, acct.info.userID, lang); err != nil {
			s.log.Error("language save failed", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	s.setLanguageCookie(w, lang)
	http.Redirect(w, r, languageReturnPath(r), http.StatusSeeOther)
}

// setLanguageCookie remembers lang on this browser; "" forgets it. HttpOnly, since nothing on
// the client reads it: the server decides the language (ADR-0014).
func (s *Server) setLanguageCookie(w http.ResponseWriter, lang string) {
	c := &http.Cookie{
		Name:     i18n.CookieName,
		Value:    lang,
		Path:     "/",
		HttpOnly: true,
		Secure:   strings.HasPrefix(s.appBaseURL, "https://"), // same derivation as startSession
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(languageCookieAge / time.Second),
	}
	if lang == "" {
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
}

// languageReturnPath is where the language menu sends the browser back to: the page it was
// on, query included (a reset link's token, say), from the Referer — which sameOrigin has
// already checked is this app's own, when present. Only its path and query are kept, so the
// redirect can't leave the site; anything unusable goes to `/`.
func languageReturnPath(r *http.Request) string {
	ref, err := url.Parse(r.Referer())
	if err != nil || !strings.HasPrefix(ref.Path, "/") || strings.HasPrefix(ref.Path, "//") || strings.Contains(ref.Path, `\`) {
		return "/"
	}
	return (&url.URL{Path: ref.Path, RawQuery: ref.RawQuery}).String()
}
