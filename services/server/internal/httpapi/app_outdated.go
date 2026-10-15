package httpapi

import (
	"net/http"
	"strconv"
	"strings"
)

// An Android build older than MIN_ANDROID_BUILD is refused on /v1/ with 426 app_outdated, which
// the app answers with its Update screen (apps/android/docs/SPEC.md FR-1.8, docs/IMPLEMENTATION.md
// §4.33).

// androidAgentPrefix starts the Android app's User-Agent, "HoldMyTrack-Android/0.13 (62da50b;
// 812)": the release number, then the commit and the versionCode in brackets.
const androidAgentPrefix = "HoldMyTrack-Android/"

// SetMinAndroidBuild sets the oldest Android versionCode /v1/ still answers; 0, the default,
// answers every build.
func (s *Server) SetMinAndroidBuild(n int) {
	s.minAndroidBuild = n
}

// androidBuild is the versionCode in an Android User-Agent, and false for any other agent. A
// build from before the agent carried one reads as 0: it's older than any minimum.
func androidBuild(userAgent string) (int, bool) {
	rest, ok := strings.CutPrefix(userAgent, androidAgentPrefix)
	if !ok {
		return 0, false
	}
	_, inBrackets, ok := strings.Cut(rest, "(")
	if !ok {
		return 0, true
	}
	inBrackets, _, _ = strings.Cut(inBrackets, ")")
	parts := strings.Split(inBrackets, ";")
	n, err := strconv.Atoi(strings.TrimSpace(parts[len(parts)-1]))
	if err != nil || n < 0 {
		return 0, true
	}
	return n, true
}

// appOutdated reports a request from an Android build below minBuild. A browser, or anything
// else that isn't the Android app, is never refused, and neither is anything when minBuild is 0.
func appOutdated(userAgent string, minBuild int) bool {
	if minBuild <= 0 {
		return false
	}
	build, ok := androidBuild(userAgent)
	return ok && build < minBuild
}

// refuseOutdatedApp answers 426 to a /v1/ request from a build below the minimum, and reports
// whether it did.
func (s *Server) refuseOutdatedApp(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, apiPrefix+"/") || !appOutdated(r.UserAgent(), s.minAndroidBuild) {
		return false
	}
	httpErrorT(w, r, http.StatusUpgradeRequired, "error.app_outdated")
	return true
}
