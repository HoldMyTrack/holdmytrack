// Package mapstyle serves the basemap style document ARCHITECTURE.md §2.1 calls for:
// "Serve the style as a document from the API rather than reimplementing it per client —
// three hand-maintained copies of a 71-layer style would diverge."
//
// The style is *not* defined here. It is defined once in apps/web/src/map/style.ts, and
// apps/web/scripts/build-style.mjs renders that function to the JSON files embedded below.
// Nothing in this package knows what a layer is, which is the point: a Go definition of the
// style would be the second hand-maintained copy §2.1 exists to prevent. Regenerate with
// `npm run build:style` in apps/web; `npm run verify:style` fails the build if the committed
// JSON has drifted from style.ts.
//
// Consumers: MapLibre Native on Android (apps/android/docs/ROADMAP.md, Phase 2) and
// eventually the headless export renderer (IMPLEMENTATION.md §5.5). The web client still
// calls buildStyle() in-process — it already has the function, and fetching a document it
// can construct locally would add a round trip to first paint for no gain.
package mapstyle

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
)

//go:embed styles/*.json
var styles embed.FS

// originPlaceholder is baked into the generated JSON in place of a real host, because one
// built artifact has to serve every deployment — dev resolves basemap assets against the
// app's own origin, a production deployment may resolve them against a CDN. Must match
// ORIGIN_PLACEHOLDER in apps/web/scripts/build-style.mjs.
const originPlaceholder = "__FITMAP_BASEMAP_ORIGIN__"

// ErrUnknownFlavor is returned for a flavor with no embedded document, so the caller can
// answer 404 rather than 500 — an unknown flavor is a bad request path, not a server fault.
var ErrUnknownFlavor = fmt.Errorf("mapstyle: unknown flavor")

var (
	once     sync.Once
	flavors  []string
	rendered map[string][]byte // placeholder still in place; origin substituted per request
)

func load() {
	once.Do(func() {
		rendered = make(map[string][]byte)
		entries, err := fs.ReadDir(styles, "styles")
		if err != nil {
			panic("mapstyle: embedded styles unreadable: " + err.Error())
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".json") {
				continue
			}
			b, err := styles.ReadFile(path.Join("styles", name))
			if err != nil {
				panic("mapstyle: " + err.Error())
			}
			flavor := strings.TrimSuffix(name, ".json")
			rendered[flavor] = b
			flavors = append(flavors, flavor)
		}
		sort.Strings(flavors)
	})
}

// Flavors lists the embedded flavors, sorted. Mirrors FLAVORS in apps/web/src/map/style.ts.
func Flavors() []string {
	load()
	out := make([]string, len(flavors))
	copy(out, flavors)
	return out
}

// Document returns the style for one flavor with basemap asset URLs resolved against origin.
// origin is an absolute origin with no trailing slash, e.g. "https://map.example.com".
func Document(flavor, origin string) ([]byte, error) {
	load()
	b, ok := rendered[flavor]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownFlavor, flavor)
	}
	return bytes.ReplaceAll(b, []byte(originPlaceholder), []byte(strings.TrimRight(origin, "/"))), nil
}

// ETag is a strong validator over the document's actual bytes. The style changes only when
// style.ts is regenerated or the configured origin changes, so a client that caches one can
// revalidate cheaply instead of refetching ~100 KB of layer definitions on every launch —
// which matters more on a phone than in the browser (apps/android/docs/ROADMAP.md, Phase 2).
func ETag(doc []byte) string {
	sum := sha256.Sum256(doc)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}
