package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/web"
)

// The Settings page (ADR-0012, IMPLEMENTATION.md §4.12, docs/SPEC.md FR-1.7): Avatar, Name,
// Country, Timezone as plain forms. Saving calls the same cores `PATCH /v1/account/settings`
// and `POST`/`DELETE /v1/account/avatar` do (account.go), so the two can't drift. It is also
// the first-run page: a real account that has never saved it (no Country) is sent here from
// every app page (pageAccount.home) and leaves for the map once it saves.

// settingsForm is what templates/pages/settings.html reads from PageData.Page.
type settingsForm struct {
	// Onboarding is FR-1.7's first run: a welcome title and intro, no way back to a map the
	// account hasn't unlocked yet, and "Save and continue", which goes on to the map.
	Onboarding bool
	// IsDemo shows the fields disabled with a note — the demo account is shared and read-only.
	IsDemo      bool
	AvatarURL   string
	DisplayName string
	Country     string
	Timezone    string
	Countries   []web.Country
	Timezones   []web.TimezoneGroup
	// Error belongs to the Name/Country/Timezone form, AvatarError to the avatar's;
	// Notice is a PRG confirmation (?saved, ?avatar, ?avatar-removed).
	Error       string
	AvatarError string
	Notice      string
}

// settingsAccount is the check every Settings request starts with: a live, verified (or demo)
// session, or a redirect to where the visitor belongs instead.
func (s *Server) settingsAccount(w http.ResponseWriter, r *http.Request) *pageAccount {
	acct := s.pageAccount(r)
	if acct == nil {
		http.Redirect(w, r, "/signin", http.StatusSeeOther)
		return nil
	}
	if !acct.info.isDemo && !acct.info.emailVerified {
		http.Redirect(w, r, "/verify-pending", http.StatusSeeOther)
		return nil
	}
	return acct
}

func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, status int, acct *pageAccount, form settingsForm) {
	form.Onboarding = !acct.info.isDemo && acct.profile.Country == ""
	form.IsDemo = acct.info.isDemo
	form.AvatarURL = acct.profile.AvatarURL
	form.Countries = web.Countries
	form.Timezones = web.TimezoneGroups(time.Now(), form.Timezone)
	title := "Settings — HoldMyTrack"
	if form.Onboarding {
		title = "Welcome — HoldMyTrack"
	}
	s.pages.Render(w, status, "settings", web.PageData{Title: title, Path: "/settings", NoIndex: true, User: acct.user, Page: form})
}

// formWithSaved is the form showing the account's saved values.
func formWithSaved(acct *pageAccount) settingsForm {
	return settingsForm{DisplayName: acct.profile.DisplayName, Country: acct.profile.Country, Timezone: acct.profile.Timezone}
}

// GET /settings.
func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	acct := s.settingsAccount(w, r)
	if acct == nil {
		return
	}
	form := formWithSaved(acct)
	q := r.URL.Query()
	switch {
	case q.Has("saved"):
		form.Notice = "Saved."
	case q.Has("avatar"):
		form.Notice = "Avatar updated."
	case q.Has("avatar-removed"):
		form.Notice = "Avatar removed."
	}
	s.renderSettings(w, r, http.StatusOK, acct, form)
}

// errDemoSettings is what a demo session gets from any Settings form — the page shows the
// fields disabled, so this is only reached by a hand-made request.
var errDemoSettings = accountFailure(http.StatusForbidden, "demo accounts can't change settings — create an account to save your own")

// POST /settings — Name, Country and Timezone together, as one save.
func (s *Server) handleSettingsForm(w http.ResponseWriter, r *http.Request) {
	acct := s.settingsAccount(w, r)
	if acct == nil {
		return
	}
	form := settingsForm{DisplayName: r.PostFormValue("display_name"), Country: r.PostFormValue("country"), Timezone: r.PostFormValue("timezone")}
	err := errDemoSettings
	if !acct.info.isDemo {
		err = s.saveSettings(r.Context(), acct.info.userID, form.DisplayName, form.Country, form.Timezone)
	}
	if err != nil {
		status, msg := s.settingsError("save settings", err)
		form.Error = msg
		s.renderSettings(w, r, status, acct, form)
		return
	}
	// First run ends here: on to the map, which Country and Timezone now make sense of.
	if acct.profile.Country == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?saved", http.StatusSeeOther)
}

// POST /settings/avatar — multipart, the image in field "file".
func (s *Server) handleSettingsAvatarForm(w http.ResponseWriter, r *http.Request) {
	s.avatarForm(w, r, "?avatar", func(acct *pageAccount) error { return s.setAvatar(w, r, acct.info.userID) })
}

// POST /settings/avatar/remove.
func (s *Server) handleSettingsAvatarRemoveForm(w http.ResponseWriter, r *http.Request) {
	s.avatarForm(w, r, "?avatar-removed", func(acct *pageAccount) error { return s.removeAvatar(r.Context(), acct.info.userID) })
}

func (s *Server) avatarForm(w http.ResponseWriter, r *http.Request, notice string, do func(*pageAccount) error) {
	acct := s.settingsAccount(w, r)
	if acct == nil {
		return
	}
	err := errDemoSettings
	if !acct.info.isDemo {
		err = do(acct)
	}
	if err != nil {
		status, msg := s.settingsError("avatar", err)
		form := formWithSaved(acct)
		form.AvatarError = msg
		s.renderSettings(w, r, status, acct, form)
		return
	}
	http.Redirect(w, r, "/settings"+notice, http.StatusSeeOther)
}

// settingsError is renderAuthError's counterpart for Settings: an accountError's own status
// and message as a sentence, or a logged 500 with a generic one.
func (s *Server) settingsError(op string, err error) (int, string) {
	var ae *accountError
	if errors.As(err, &ae) {
		return ae.status, sentence(ae.msg)
	}
	s.log.Error(op+" failed", "err", err)
	return http.StatusInternalServerError, "Something went wrong on our side. Please try again."
}
