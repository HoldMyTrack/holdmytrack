package httpapi

import (
	"net/http"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/web"
)

// settingsOption is one choice in a Settings list: the value `PATCH /v1/account/settings`
// takes, and what to show for it.
type settingsOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// settingsTimezoneGroup is one region of the Timezone list: its IANA name ("Europe") and that
// name in the request's language, as the page's <optgroup> labels it.
type settingsTimezoneGroup struct {
	Region  string           `json:"region"`
	Label   string           `json:"label"`
	Options []settingsOption `json:"options"`
}

// settingsOptionsResponse is what `GET /v1/account/settings/options` answers with.
type settingsOptionsResponse struct {
	Countries []settingsOption        `json:"countries"`
	Timezones []settingsTimezoneGroup `json:"timezones"`
	Languages []settingsOption        `json:"languages"`
}

// handleSettingsOptions serves `GET /v1/account/settings/options` — the Country, Timezone and
// Language lists the Settings page renders (settings_page.go's renderSettings), for a native
// client that has no page to read them from. The same functions build both, so the app offers
// exactly what saveSettings accepts: a client-side list (Android's own ISO countries, its own
// zone ids) would drift from the server's and fail a save. Countries are named in the
// request's language, and the account's own timezone is always in the list, as on the page.
func (s *Server) handleSettingsOptions(w http.ResponseWriter, r *http.Request) {
	lang := requestLang(r)
	l := i18n.Get(lang)
	resp := settingsOptionsResponse{}
	for _, c := range web.CountriesIn(lang) {
		resp.Countries = append(resp.Countries, settingsOption{Value: c.Code, Label: c.Name})
	}
	for _, g := range web.TimezoneGroups(time.Now(), timezoneFromContext(r.Context())) {
		group := settingsTimezoneGroup{Region: g.Region, Label: l.T("tz.region." + g.Region)}
		for _, o := range g.Options {
			group.Options = append(group.Options, settingsOption{Value: o.ID, Label: o.Label})
		}
		resp.Timezones = append(resp.Timezones, group)
	}
	for _, code := range i18n.Supported {
		resp.Languages = append(resp.Languages, settingsOption{Value: code, Label: i18n.Names[code]})
	}
	writeJSON(w, http.StatusOK, resp)
}
