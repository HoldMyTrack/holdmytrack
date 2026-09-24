package httpapi

import (
	"archive/zip"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// pathifyBinary is the executable `handleTakeoutUpload` shells out to — pathify
// (github.com/np25071984/pathify), a separate MIT-licensed Rust CLI built specifically to
// read Google Health/Fitbit Takeout exports (its own `takeout` subcommand) and join what an
// export keeps apart: per-day GPS logs and the exercise records saying which stretch of a day
// was which activity. Reimplementing that join in Go — UTC-offset self-correction (the
// exercise logs' own timestamps carry no offset), multi-device dedup (a phone and a watch
// both logging the same walk), midnight day-file stitching — would duplicate real,
// already-solved work. See pathify's CHANGELOG.md 1.4.0 entry for the full account of what it
// handles, and IMPLEMENTATION.md's Takeout section for why this shells out
// rather than reimplementing.
const pathifyBinary = "pathify"

// pathifyTakeoutListing mirrors `pathify takeout --list --json`'s response shape.
type pathifyTakeoutListing struct {
	Types []struct {
		Name    string `json:"name"`
		Logs    int    `json:"logs"`
		WithGPS int    `json:"with_gps"`
	} `json:"types"`
}

// handleTakeoutUpload is handleUpload's branch for a Google Takeout export — detected by
// isTakeoutArchive, distinct from a plain `.zip` (handleZipUpload) since a Takeout export has
// no standalone .gpx/.fit/.tcx entries to walk directly; its GPS and exercise data live in
// separate CSV/JSON families `pathify takeout` already knows how to join.
//
// Reuses the exact same zipUploadResponse/zipEntryResult shape handleZipUpload returns — the
// frontend (UploadPanel.tsx) doesn't need to know or care which extraction strategy produced
// a batch of per-file outcomes, only that one request turned into several.
//
// Invokes `pathify takeout` once per activity type that carries GPS, each into its own temp
// directory, rather than one combined invocation across every type — pathify's own
// `--per-activity` mode has no way to report back which output file came from which
// requested type when several are requested at once, and a per-type directory split gives
// that for free, with no filename-parsing needed to recover it. Each extracted file's known
// type is threaded through as ingest.Job.ActivityType, an override the parser itself has no
// way to reconstruct on its own — a bare per-activity GPX carries no `<type>` element.
//
// Handles exactly one archive per request — a Takeout export split by Google into several
// parts (`…-001.zip`, `…-002.zip`, …) needs every part passed to pathify together, since the
// exercise logs and the GPS days can land in different parts and neither is useful alone.
// Multi-part stitching isn't built here; not needed for any archive seen so far, and adding
// it blind would be speculative complexity ahead of a real case that needs it.
func (s *Server) handleTakeoutUpload(w http.ResponseWriter, r *http.Request, data []byte, filename string) {
	ctx := r.Context()
	userID := userIDFromContext(ctx)

	tempDir, err := os.MkdirTemp("", "holdmytrack-takeout-*")
	if err != nil {
		s.log.Error("takeout temp dir failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tempDir)

	archivePath := filepath.Join(tempDir, "archive.zip")
	if err := os.WriteFile(archivePath, data, 0o600); err != nil {
		s.log.Error("takeout archive write failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	listing, err := pathifyListTypes(ctx, archivePath)
	if err != nil {
		s.log.Error("pathify takeout --list failed", "err", err)
		http.Error(w, "could not read this Takeout export — see server logs", http.StatusInternalServerError)
		return
	}

	results := make([]zipEntryResult, 0)
	for _, t := range listing.Types {
		// Exactly what pathify's own `takeout` (without --type) refuses to hand anyone: "a
		// swim with no coordinates is not something anyone can hand you as a track." These
		// logs' own metrics (distance, calories, heart rate) are a separate, not-yet-built
		// import path — see IMPLEMENTATION.md's Takeout section.
		if t.WithGPS == 0 {
			continue
		}

		typeDir := filepath.Join(tempDir, sanitizeDirName(t.Name))
		if err := os.Mkdir(typeDir, 0o700); err != nil {
			s.log.Error("takeout type dir failed", "err", err, "type", t.Name)
			results = append(results, zipEntryResult{Filename: t.Name, Status: "skipped", Reason: "internal error"})
			continue
		}

		cmd := exec.CommandContext(ctx, pathifyBinary, "takeout", archivePath,
			"--type", t.Name, "--per-activity", "-o", typeDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			s.log.Error("pathify takeout extraction failed", "err", err, "type", t.Name, "output", string(out))
			results = append(results, zipEntryResult{Filename: t.Name, Status: "skipped", Reason: "extraction failed"})
			continue
		}

		results = append(results, s.persistTakeoutType(ctx, userID, typeDir, t.Name)...)
	}

	writeJSON(w, http.StatusAccepted, zipUploadResponse{Status: "zip_processed", Filename: filename, Files: results})
}

// pathifyListTypes runs `pathify takeout <archive> --list --json` and parses its response.
func pathifyListTypes(ctx context.Context, archivePath string) (pathifyTakeoutListing, error) {
	out, err := exec.CommandContext(ctx, pathifyBinary, "takeout", archivePath, "--list", "--json").Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return pathifyTakeoutListing{}, &pathifyError{err: err, stderr: string(exitErr.Stderr)}
		}
		return pathifyTakeoutListing{}, err
	}
	var listing pathifyTakeoutListing
	if err := json.Unmarshal(out, &listing); err != nil {
		return pathifyTakeoutListing{}, err
	}
	return listing, nil
}

// pathifyError carries a subprocess's stderr alongside the exec error, so a log line names
// what pathify itself said (e.g. "no location data in this archive") rather than just the Go
// runtime's own generic "exit status 1".
type pathifyError struct {
	err    error
	stderr string
}

func (e *pathifyError) Error() string { return e.err.Error() + ": " + e.stderr }
func (e *pathifyError) Unwrap() error { return e.err }

// persistTakeoutType reads every file `pathify takeout --per-activity` wrote for one activity
// type and persists each exactly the way handleZipUpload persists one zip entry — same
// persistAndEnqueue, same per-file outcome shape — except `Source` is "takeout" and
// `ActivityType` is the type this whole directory was extracted for, since a bare
// per-activity GPX file carries no `<type>` element of its own to read it back from.
func (s *Server) persistTakeoutType(ctx context.Context, userID, typeDir, activityType string) []zipEntryResult {
	entries, err := os.ReadDir(typeDir)
	if err != nil {
		s.log.Error("takeout type dir read failed", "err", err, "type", activityType)
		return []zipEntryResult{{Filename: activityType, Status: "skipped", Reason: "internal error"}}
	}

	results := make([]zipEntryResult, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		fileData, err := os.ReadFile(filepath.Join(typeDir, name))
		if err != nil {
			s.log.Error("takeout extracted file read failed", "err", err, "file", name)
			results = append(results, zipEntryResult{Filename: name, Status: "skipped", Reason: "internal error"})
			continue
		}

		externalID, alreadyProcessed, err := s.persistAndEnqueue(ctx, uploadFileParams{
			UserID:       userID,
			Source:       "takeout",
			Filename:     name,
			Ext:          strings.ToLower(filepath.Ext(name)),
			ActivityType: activityType,
			Data:         fileData,
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
	return results
}

// sanitizeDirName defends the temp-directory-per-type layout against a type name containing
// a path separator — not a realistic case (these names come from pathify's own reading of a
// real export's data, not arbitrary user input), but §5.1's "assume hostile input" standard
// for anything derived from an uploaded file applies here too.
func sanitizeDirName(name string) string {
	return strings.ReplaceAll(name, "/", "_")
}
