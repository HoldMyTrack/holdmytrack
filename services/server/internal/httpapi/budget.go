package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/HoldMyTrack/platform/services/server/internal/budget"
)

// The daily R2 budget (internal/budget, ADR-0044) as the API applies it: what adds objects is
// refused from budget.AddingPaused, what reads them for the map from budget.ReadsPaused, each
// with a 503 and a Retry-After to the day's end. Everything that only reads the database, the
// track and Spots tiles included, is untouched at every stage.

// SetBudget gives the server the daily R2 budget's Gate; without one, nothing is refused.
func (s *Server) SetBudget(g *budget.Gate) {
	s.budget = g
}

// budgeted is h, refused once the day's stage reaches at.
func (s *Server) budgeted(at budget.Stage, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.budget.Stage(r.Context()) >= at {
			writeBudgetPaused(w, r)
			return
		}
		h(w, r)
	}
}

// writeBudgetPaused is budgeted's refusal: a 503 with a Retry-After to the day's end.
func writeBudgetPaused(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Retry-After", strconv.Itoa(int(budget.UntilReset(time.Now()).Seconds())+1))
	httpErrorT(w, r, http.StatusServiceUnavailable, "error.daily_capacity")
}

// overBudget is budgeted's check for an account core shared with a page form (an avatar, a data
// download), as the error both report: nil while the day's stage is below at.
func (s *Server) overBudget(ctx context.Context, at budget.Stage) error {
	if s.budget.Stage(ctx) >= at {
		return accountFailure(http.StatusServiceUnavailable, "error.daily_capacity")
	}
	return nil
}
