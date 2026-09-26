package ingest

import (
	"errors"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
)

// The failure codes an ingest job can end with, stored in jobs.error_code beside last_error's
// English diagnostic. The worker writes a job's failure long before anyone reads it, and not
// knowing in which language, so it stores a code; the handlers that list jobs render
// "ingest_error.<code>" from the catalog in the reader's language (IMPLEMENTATION.md §4.21).
// Every code but FailInternal is the person's file rather than a fault here.
const (
	FailUnsupportedFormat = "unsupported_format"
	FailUnreadableFile    = "unreadable_file"
	FailNoTrack           = "no_track"
	FailTooFewPoints      = "too_few_points"
	FailNoTimestamps      = "no_timestamps"
	FailInternal          = "internal"
)

// errTooFewPoints fails a track with fewer than 2 timed points: nothing to draw a line with.
var errTooFewPoints = errors.New("fewer than 2 points recorded")

// parseError marks a failure of the file's own parser, so FailureCode can tell a file it
// couldn't read from a failure further along the pipeline, which is ours.
type parseError struct{ err error }

func (e parseError) Error() string { return "ingest: parse: " + e.err.Error() }
func (e parseError) Unwrap() error { return e.err }

// FailureCode classifies a failed ingest's error as one of the Fail* codes. The specific
// causes are checked before parseError, which also wraps parse.ErrNoPoints and
// parse.ErrUnsupportedFormat.
func FailureCode(err error) string {
	var pe parseError
	switch {
	case errors.Is(err, parse.ErrUnsupportedFormat):
		return FailUnsupportedFormat
	case errors.Is(err, parse.ErrNoPoints):
		return FailNoTrack
	case errors.Is(err, errNoTimestamps):
		return FailNoTimestamps
	case errors.Is(err, errTooFewPoints):
		return FailTooFewPoints
	case errors.As(err, &pe):
		return FailUnreadableFile
	default:
		return FailInternal
	}
}
