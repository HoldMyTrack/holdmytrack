package httpapi

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/web"
)

// The Stories page (ADR-0012, IMPLEMENTATION.md §4.23, docs/SPEC.md FR-14.6): every Story of
// the account with its statistics and per-type breakdown, View on map, and Delete —
// server-rendered like Profile, from the same loadStories the API reads. Delete asks first in
// a <details>, like the header's menus, so it needs no script, and is a same-origin form POST
// that redirects back here (PRG) with a notice.

// storiesView is what templates/pages/stories.html reads from PageData.Page.
type storiesView struct {
	Stories []storyCard
	// IsDemo disables Delete: a demo account's Stories are the Demo Customer's (FR-2.1).
	IsDemo bool
	Notice string
	Error  string
}

type storyCard struct {
	ID          string
	Name        string
	Description string
	Stats       string
	Types       []storyTypeRow
	// MapHref is the map's Story view, `/?story=<id>`.
	MapHref string
}

type storyTypeRow struct{ Label, Stats string }

// GET /stories — `?deleted` after a Delete.
func (s *Server) handleStoriesPage(w http.ResponseWriter, r *http.Request) {
	acct := s.storiesAccount(w, r)
	if acct == nil {
		return
	}
	var view storiesView
	if r.URL.Query().Has("deleted") {
		view.Notice = i18n.Get(pageLang(acct, r)).T("stories.deleted")
	}
	s.renderStories(w, r, http.StatusOK, acct, view)
}

// POST /stories/{id}/delete.
func (s *Server) handleStoryDeleteForm(w http.ResponseWriter, r *http.Request) {
	acct := s.storiesAccount(w, r)
	if acct == nil {
		return
	}
	if acct.info.isDemo {
		s.renderStories(w, r, http.StatusForbidden, acct, storiesView{Error: i18n.Get(pageLang(acct, r)).T("error.demo_read_only")})
		return
	}
	id := r.PathValue("id")
	err := errStoryNotFound
	if uuidPattern.MatchString(id) {
		err = s.deleteStory(r.Context(), acct.info.userID, id)
	}
	switch {
	case errors.Is(err, errStoryNotFound):
		s.notFound(w, r)
	case err != nil:
		s.log.Error("story delete form failed", "err", err)
		s.renderStories(w, r, http.StatusInternalServerError, acct, storiesView{Error: i18n.Get(pageLang(acct, r)).T("error.internal")})
	default:
		http.Redirect(w, r, "/stories?deleted", http.StatusSeeOther)
	}
}

// storiesAccount is the signed-in account the page is for, or nil once it has redirected
// somewhere else: sign-in, or wherever an account that isn't done with first run belongs.
func (s *Server) storiesAccount(w http.ResponseWriter, r *http.Request) *pageAccount {
	acct := s.pageAccount(r)
	if acct == nil {
		http.Redirect(w, r, "/signin", http.StatusSeeOther)
		return nil
	}
	if home := acct.home(); home != "/" {
		http.Redirect(w, r, home, http.StatusSeeOther)
		return nil
	}
	return acct
}

func (s *Server) renderStories(w http.ResponseWriter, r *http.Request, status int, acct *pageAccount, view storiesView) {
	lang := pageLang(acct, r)
	l := i18n.Get(lang)
	stories, err := s.loadStories(r.Context(), s.pool, acct.info.userID, nil)
	if err != nil {
		s.log.Error("stories page failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	view.IsDemo = acct.info.isDemo
	view.Stories = storyCards(l, stories, web.Imperial(acct.profile.Country))
	s.pages.Render(w, status, "stories", web.PageData{Title: l.T("meta.stories_title"), Path: "/stories", NoIndex: true, User: acct.user, Page: view, Lang: lang})
}

func storyCards(l *i18n.Localizer, stories []story, imperial bool) []storyCard {
	cards := make([]storyCard, 0, len(stories))
	for _, st := range stories {
		card := storyCard{ID: st.ID, Name: st.Name, MapHref: "/?story=" + url.QueryEscape(st.ID)}
		if st.Description != nil {
			card.Description = *st.Description
		}
		if st.Stats.Count > 0 {
			card.Stats = storyStatsLine(l, st.Stats.Count, st.Stats.DistanceMeters, st.Stats.MovingSeconds, imperial)
		}
		for _, t := range st.Stats.ByType {
			card.Types = append(card.Types, storyTypeRow{
				Label: web.ActivityType(l, t.ActivityType),
				Stats: storyStatsLine(l, t.Count, t.DistanceMeters, t.MovingSeconds, imperial),
			})
		}
		cards = append(cards, card)
	}
	return cards
}

func storyStatsLine(l *i18n.Localizer, count int64, meters float64, movingSeconds int64, imperial bool) string {
	return l.T("stories.stats",
		"activities", l.N("count.activities", count),
		"distance", web.FormatTotalDistance(l, meters, imperial),
		"moving", web.FormatDuration(l, movingSeconds))
}
