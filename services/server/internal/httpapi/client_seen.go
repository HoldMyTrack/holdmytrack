package httpapi

import (
	"fmt"
	"strings"
	"time"
)

// The client census (IMPLEMENTATION.md §4.32): with last_seen_at, each account keeps the day it
// last used the website and the Android app, and the app's build, so the daily snapshot can count
// who uses which and which builds are still around. Folded into last_seen_at's write, so an
// account costs at most one write a day per client, plus one when the app updates.

// client is which of HoldMyTrack's clients sent a request.
type client int

const (
	clientNone    client = iota // the app's WebView: counted through the app's own requests
	clientWeb                   // a browser
	clientAndroid               // the Android app
)

// requestClient is the client behind a request with userAgent, and the Android build. inApp is
// the app's WebView (inApp), whose pages are the website's but whose use is the app's.
func requestClient(userAgent string, inApp bool) (client, int) {
	if build, ok := androidBuild(userAgent); ok {
		return clientAndroid, build
	}
	if inApp {
		return clientNone, 0
	}
	return clientWeb, 0
}

// seen is what an account has stored: nil for never.
type seen struct {
	last, web, android *time.Time
	androidBuild       *int
}

// seenWrite is what a request at now writes: last_seen_at and the client's own day once a UTC
// day each, and the Android build whenever it differs.
type seenWrite struct {
	last, web, android bool
	build              int
}

func seenDue(s seen, c client, build int, now time.Time) seenWrite {
	w := seenWrite{last: lastSeenDue(s.last, now)}
	switch c {
	case clientWeb:
		w.web = lastSeenDue(s.web, now)
	case clientAndroid:
		w.android = lastSeenDue(s.android, now) || s.androidBuild == nil || *s.androidBuild != build
		w.build = build
	}
	return w
}

// update is w as one UPDATE of account $1, or "" when there is nothing to write.
func (w seenWrite) update() (string, []any) {
	var set []string
	var args []any
	if w.last {
		set = append(set, "last_seen_at = NOW()")
	}
	if w.web {
		set = append(set, "web_seen_at = NOW()")
	}
	if w.android {
		args = append(args, w.build)
		set = append(set, "android_seen_at = NOW()", fmt.Sprintf("android_build = $%d", len(args)+1))
	}
	if len(set) == 0 {
		return "", nil
	}
	return "UPDATE users SET " + strings.Join(set, ", ") + " WHERE id = $1", args
}
