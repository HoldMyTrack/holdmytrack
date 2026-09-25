package httpapi

import (
	"archive/zip"
	"net/http"
	"sort"
	"strings"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/takeout"
)

// takeoutMarkerPrefix identifies a Google Takeout / Google Health export among a zip's own
// entry names, distinguishing it from a plain multi-file `.zip` (handleZipUpload) before any
// of it is extracted. Confirmed against a real ~2000-file, 1.9 GB sample export: every entry
// sits under this prefix regardless of which specific Health/Fitbit sub-category was selected
// when the export was created.
const takeoutMarkerPrefix = "Takeout/Google Health/"

func isTakeoutArchive(zr *zip.Reader) bool {
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, takeoutMarkerPrefix) {
			return true
		}
	}
	return false
}

// handleTakeoutUpload is handleUpload's branch for a Google Takeout export — detected by
// isTakeoutArchive, distinct from a plain `.zip` (handleZipUpload) since a Takeout export has
// no standalone .gpx/.fit/.tcx entries to walk directly; its GPS and exercise data live in
// separate CSV/JSON families that internal/takeout joins.
//
// Reuses the exact same zipUploadResponse/zipEntryResult shape handleZipUpload returns — the
// frontend (UploadPanel.tsx) doesn't need to know or care which extraction strategy produced
// a batch of per-file outcomes, only that one request turned into several.
//
// Extracts one activity type at a time (takeout.Archive.Extract's doc comment says why), and
// persists each activity as the GPX internal/takeout writes, with the type threaded through
// as ingest.Job.ActivityType — a bare per-activity GPX carries no `<type>` element for the
// parser to read it back from. Activities go in file-name order, as they did when they were
// files pathify wrote to a directory.
func (s *Server) handleTakeoutUpload(w http.ResponseWriter, r *http.Request, zr *zip.Reader, filename string) {
	ctx := r.Context()
	userID := userIDFromContext(ctx)

	archive, err := takeout.Open(zr)
	if err != nil {
		s.log.Error("takeout open failed", "err", err)
		http.Error(w, "could not read this Takeout export — see server logs", http.StatusInternalServerError)
		return
	}

	results := make([]zipEntryResult, 0)
	for _, t := range archive.Types() {
		// A swim with no coordinates is not something anyone can hand over as a track. These
		// logs' own metrics (distance, calories, heart rate) are a separate, not-yet-built
		// import path — see IMPLEMENTATION.md's Takeout section.
		if t.WithGPS == 0 {
			continue
		}

		activities, err := archive.Extract(t.Name)
		if err != nil {
			s.log.Error("takeout extraction failed", "err", err, "type", t.Name)
			results = append(results, zipEntryResult{Filename: t.Name, Status: "skipped", Reason: "extraction failed"})
			continue
		}
		if len(activities) == 0 {
			// Logs that claim GPS but whose day files are missing or empty over their window —
			// the join isn't total in real exports either.
			results = append(results, zipEntryResult{Filename: t.Name, Status: "skipped", Reason: "no GPS points found"})
			continue
		}

		names := takeout.FileNames(activities)
		order := make([]int, len(activities))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(a, b int) bool { return names[order[a]] < names[order[b]] })

		for _, i := range order {
			name := names[i]
			externalID, alreadyProcessed, err := s.persistAndEnqueue(ctx, uploadFileParams{
				UserID:       userID,
				Source:       "takeout",
				Filename:     name,
				Ext:          ".gpx",
				ActivityType: t.Name,
				Data:         activities[i].GPX(),
			})
			if err != nil {
				s.log.Error("takeout entry persist/enqueue failed", "err", err, "file", name)
				results = append(results, zipEntryResult{Filename: name, Status: "skipped", Reason: "internal error"})
				continue
			}
			status := "enqueued"
			if alreadyProcessed {
				status = "already_processed"
			}
			results = append(results, zipEntryResult{Filename: name, Status: status, ExternalID: externalID})
		}
	}

	writeJSON(w, http.StatusAccepted, zipUploadResponse{Status: "zip_processed", Filename: filename, Files: results})
}
