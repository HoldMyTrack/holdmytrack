package mapstyle

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The style is generated from apps/web/src/map/style.ts, so these tests deliberately assert
// the contract this package is responsible for — that every flavor is embedded, parses, and
// has its origin substituted — not the style's content, which is style.ts's to define and
// `npm run verify:style` to police.

func TestFlavorsMatchWebClient(t *testing.T) {
	// Mirrors FLAVORS in apps/web/src/map/style.ts. If that list changes, the generator
	// writes a new file and this fails, which is the intended way to notice.
	want := []string{"black", "dark", "grayscale", "light", "white"}
	got := Flavors()
	if len(got) != len(want) {
		t.Fatalf("Flavors() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Flavors()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDocumentSubstitutesOrigin(t *testing.T) {
	const origin = "https://cdn.example.com"
	for _, flavor := range Flavors() {
		doc, err := Document(flavor, origin)
		if err != nil {
			t.Fatalf("Document(%q): %v", flavor, err)
		}
		if strings.Contains(string(doc), originPlaceholder) {
			t.Errorf("%s: placeholder survived substitution", flavor)
		}

		var style struct {
			Version int    `json:"version"`
			Glyphs  string `json:"glyphs"`
			Sprite  string `json:"sprite"`
			Sources map[string]struct {
				URL         string `json:"url"`
				Attribution string `json:"attribution"`
			} `json:"sources"`
			Layers []json.RawMessage `json:"layers"`
		}
		if err := json.Unmarshal(doc, &style); err != nil {
			t.Fatalf("%s: not valid JSON: %v", flavor, err)
		}
		if style.Version != 8 {
			t.Errorf("%s: version = %d, want 8", flavor, style.Version)
		}
		if len(style.Layers) == 0 {
			t.Errorf("%s: no layers", flavor)
		}
		if !strings.HasPrefix(style.Glyphs, origin) {
			t.Errorf("%s: glyphs = %q, want prefix %q", flavor, style.Glyphs, origin)
		}
		if !strings.HasSuffix(style.Sprite, "/"+flavor) {
			t.Errorf("%s: sprite = %q, want it to name its own flavor", flavor, style.Sprite)
		}
		src, ok := style.Sources["protomaps"]
		if !ok {
			t.Fatalf("%s: missing the protomaps source", flavor)
		}
		if !strings.Contains(src.URL, origin) {
			t.Errorf("%s: source url = %q, want it to carry the origin", flavor, src.URL)
		}
		// ODbL "Produced Work" credit is mandatory, not decoration (style.ts's ATTRIBUTION)
		// — a client rendering this document must be able to show OSM credit.
		if !strings.Contains(src.Attribution, "OpenStreetMap") {
			t.Errorf("%s: attribution missing OSM credit: %q", flavor, src.Attribution)
		}
	}
}

func TestDocumentTrimsTrailingSlash(t *testing.T) {
	with, err := Document("light", "https://cdn.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	without, err := Document("light", "https://cdn.example.com")
	if err != nil {
		t.Fatal(err)
	}
	// A trailing slash would otherwise produce "https://cdn.example.com//basemap/...".
	if string(with) != string(without) {
		t.Error("a trailing slash on origin changed the document")
	}
}

func TestDocumentUnknownFlavor(t *testing.T) {
	if _, err := Document("neon", "https://example.com"); !errors.Is(err, ErrUnknownFlavor) {
		t.Errorf("err = %v, want ErrUnknownFlavor so the handler can answer 404", err)
	}
}

func TestETagVariesWithOrigin(t *testing.T) {
	a, _ := Document("light", "https://a.example.com")
	b, _ := Document("light", "https://b.example.com")
	if ETag(a) == ETag(b) {
		t.Error("same ETag for different origins — a client would cache the wrong asset URLs")
	}
	if ETag(a) != ETag(a) {
		t.Error("ETag is not stable for identical input")
	}
}
